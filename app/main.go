// samcast single-file launcher: one static binary carrying the whole
// samsung-cast appliance (samcast CLI, samcast-picker, bin/doubletake,
// bin/doubletake-ctl, bin/samcast-ui) as an embedded archive, staged by
// `make app`. On first run it unpacks itself into the user's cache and execs
// the real CLI from there; later runs reuse that tree, keyed by a content
// hash so a changed image always extracts fresh.
//
// Trust model: this file is code+data for one user, unpacked into that user's
// own cache and executed as that user — treat the binary exactly like the
// scripts it replaces; it confers no privilege the user did not already have.
package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

//go:embed all:assets
var assets embed.FS

const (
	brand = "samsung-cast"
	// app/assets/.gitignore is only the embed anchor for a not-yet-staged
	// tree — not part of the appliance, so it is neither hashed nor extracted.
	anchor = ".gitignore"
)

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "samcast: "+f+"\n", a...)
	os.Exit(1)
}

// assetList returns the embedded asset paths (assets/-relative), sorted.
func assetList() []string {
	var names []string
	err := fs.WalkDir(assets, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() != anchor {
			names = append(names, p)
		}
		return nil
	})
	if err != nil {
		die("reading embedded assets: %v", err)
	}
	sort.Strings(names)
	return names
}

// imageHash fingerprints every asset's path + bytes; its first 12 hex chars
// name the extraction directory, so any changed byte picks a fresh one.
func imageHash() string {
	h := sha256.New()
	for _, name := range assetList() {
		data, err := assets.ReadFile(name)
		if err != nil {
			die("reading embedded asset %s: %v", name, err)
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)[:6])
}

func cacheDir() string {
	if c := os.Getenv("XDG_CACHE_HOME"); c != "" {
		return c
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".cache")
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return filepath.Join(u.HomeDir, ".cache")
	}
	die("no home directory ($HOME unset, no passwd entry) — cannot find the cache")
	return ""
}

// extract unpacks the appliance into a sibling temp dir and renames it into
// place. The rename onto an existing non-empty dir is the concurrent-first-run
// guard: a rival extraction of the same image wins and ours is discarded.
// Returns an error instead of dying so the deferred tmp cleanup fires on
// every failure path — os.Exit would skip it and leak the full payload.
func extract(dir, hash string) error {
	tmp := filepath.Join(filepath.Dir(dir), fmt.Sprintf("app-%s.tmp-%d", hash, os.Getpid()))
	defer os.RemoveAll(tmp) // no-op once tmp has been renamed away
	// A leftover tmp from a killed extractor with this pid must not wedge us.
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("clearing stale temp dir %s: %v", tmp, err)
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("creating %s: %v", tmp, err)
	}
	for _, name := range assetList() {
		data, err := assets.ReadFile(name)
		if err != nil {
			return fmt.Errorf("reading embedded asset %s: %v", name, err)
		}
		rel, err := filepath.Rel("assets", name)
		if err != nil {
			return fmt.Errorf("bad asset path %s: %v", name, err)
		}
		dst := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("creating %s: %v", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			return fmt.Errorf("writing %s: %v", dst, err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, ".stamp"), []byte(hash), 0o644); err != nil {
		return fmt.Errorf("writing stamp: %v", err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		if stamp, rerr := os.ReadFile(filepath.Join(dir, ".stamp")); rerr == nil && string(stamp) == hash {
			return nil // concurrent first run of the same image got there first
		}
		// Nothing below replaces an existing dir, so a stale or damaged one
		// (wrong stamp, half-wiped cache) would fail every later run with a
		// bare rename error. It is our own hash-keyed cache entry — drop it
		// and retry once.
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return fmt.Errorf("extracting to %s: %v (stale dir could not be cleared: %v)", dir, err, rmErr)
		}
		if err := os.Rename(tmp, dir); err != nil {
			return fmt.Errorf("extracting to %s: %v", dir, err)
		}
	}
	return nil
}

// sweepGC prunes abandoned temp dirs from killed extractions and extraction
// dirs of older images (every rebuilt image gets a fresh hash-keyed copy).
// Old image dirs go only after a day: a concurrently running older image
// (a second samcast on PATH, the tray unit) execs from its own dir and must
// not have it yanked mid-run — and things persisting paths into an extraction
// dir (daemon unit, tray unit, desktop entry) re-resolve on their next run.
// Best effort: cache housekeeping is never worth failing the launch over.
func sweepGC(dir string) {
	parent := filepath.Dir(dir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "app-") || name == filepath.Base(dir) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(parent, name))
	}
}

// sniff warns (never blocks) about the engine's exec'd runtime deps, one
// stderr line each, with the install hint for the running distro.
func sniff() {
	for _, dep := range []struct{ bin, deb, other string }{
		{"gst-launch-1.0",
			"apt install gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad",
			"pacman -S gstreamer"},
		{"avahi-browse",
			"apt install avahi-utils (and enable avahi-daemon)",
			"pacman -S avahi (and enable avahi-daemon)"},
	} {
		if _, err := exec.LookPath(dep.bin); err != nil {
			if debianish() {
				dep.other = dep.deb
			}
			fmt.Fprintf(os.Stderr, "samcast: %s not found on PATH — casting needs it: %s\n", dep.bin, dep.other)
		}
	}
}

// debianish reports a Debian-family distro (apt hints) vs everything else
// (pacman), via ID/ID_LIKE in /etc/os-release.
func debianish() bool {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && (key == "ID" || key == "ID_LIKE") && strings.Contains(strings.ToLower(value), "debian") {
			return true
		}
	}
	return false
}

func main() {
	// `go build` inside app/ without `make app`'s staging embeds only the
	// anchor — fail with the fix instead of a cryptic exec error later.
	if len(assetList()) == 0 {
		die("no embedded assets staged — build the image with: make app")
	}
	hash := imageHash()
	dir := filepath.Join(cacheDir(), brand, "app-"+hash)

	// A valid .stamp short-circuits the extraction on every later run.
	if stamp, err := os.ReadFile(filepath.Join(dir, ".stamp")); err != nil || string(stamp) != hash {
		if err := extract(dir, hash); err != nil {
			die("%v", err)
		}
	}

	// A valid .stamp still does not prove the tree intact (a partial cache
	// wipe or disk damage leaves a correct stamp and no binary — exec would
	// then die with a bare "no such file" on every later run). Our own keyed
	// cache entry: drop it and extract fresh.
	cli := filepath.Join(dir, "samcast")
	if _, err := os.Stat(cli); err != nil {
		if err := os.RemoveAll(dir); err != nil {
			die("clearing damaged extraction dir %s: %v", dir, err)
		}
		if err := extract(dir, hash); err != nil {
			die("%v", err)
		}
	}
	sweepGC(dir)

	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"ui"} // this file IS the app: no arguments opens the control panel
	}
	if args[0] == "version" { // intercepted: identify the image, never reaches the CLI
		fmt.Printf("samcast image %s\n%s\n", hash, dir)
		return
	}

	sniff()

	// Exec replaces this process: signals/Ctrl-C behave exactly as under the
	// plain bash CLI.
	if err := syscall.Exec(cli, append([]string{cli}, args...), os.Environ()); err != nil {
		die("exec %s: %v", cli, err)
	}
}
