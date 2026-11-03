// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Mirrorling contributors.

// Command mirrorling-tray is the mirrorling tray helper: one StatusNotifierItem
// icon in the waybar/freedesktop tray whose pixmap and tooltip mirror the
// mirroring state, with a menu to open the control panel and start/stop
// mirroring while the panel window is closed.
//
// Started by `mirrorling tray` (mirrorling-tray.service) and polls
// doubletake-ctl every --interval to keep icon and menu current.
//
// SECURITY MODEL: the item exposes only start/stop mirroring plus an
// open-panel action, and only on the local session bus — the same
// loopback-style trust as mirrorling-ui: anything that can reach us on the
// session bus already runs as this user, which is exactly what the panel and
// the mirrorling CLI would allow anyway. No config rewrite or log access is
// exposed through the tray.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

func main() {
	log.SetFlags(log.Ltime)

	ctlFlag := flag.String("ctl", "", "path to doubletake-ctl (default: <dir of executable>/../bin/doubletake-ctl)")
	mirrorlingFlag := flag.String("mirrorling", "", "path to the mirrorling CLI (default: <dir of executable>/../mirrorling)")
	interval := flag.Duration("interval", 2*time.Second, "state poll interval")
	id := flag.String("id", "mirrorling", "tray item id")
	flag.Parse()

	if err := checkInterval(*interval); err != nil {
		log.Fatalf("%v", err)
	}

	conn, err := dbus.SessionBus()
	if err != nil {
		log.Fatalf("cannot connect to the session bus: %v", err)
	}
	// A fixed bus name is the single-instance lock: two helpers (manual run,
	// restart race) must never draw two icons. DoNotQueue makes the loser fail
	// immediately instead of queueing for a name it can never own, and a loser
	// is not an error worth a restart, so it exits 0.
	busName := sniBusPrefix + "-mirrorling"
	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		log.Fatalf("cannot request %s: %v", busName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		log.Printf("%s already owned — another tray is running, exiting", busName)
		return
	}

	ctl := resolvePath(*ctlFlag, filepath.Join("bin", "doubletake-ctl"))
	mirrorlingPath := resolvePath(*mirrorlingFlag, "mirrorling")
	for _, pc := range []struct{ label, flag, path string }{
		{"mirrorling", "mirrorling", mirrorlingPath},
		{"doubletake-ctl", "ctl", ctl},
	} {
		if pc.path == "" {
			log.Printf("warning: %s path could not be resolved (pass --%s)", pc.label, pc.flag)
			continue
		}
		if fi, err := os.Stat(pc.path); err != nil || fi.IsDir() {
			log.Printf("warning: %s not found at %s — related actions will fail until the path is fixed", pc.label, pc.path)
		}
	}
	acts := &actions{mirrorling: mirrorlingPath, mirrorlingOK: mirrorlingPath != "" && isFile(mirrorlingPath)}
	if !acts.mirrorlingOK {
		log.Printf("mirrorling missing — tray actions disabled (Quit still works)")
	}

	state := &stateStore{}
	menu := &dbusMenu{state: state, acts: acts}
	srv := &trayServer{
		conn:    conn,
		busName: busName,
		id:      *id,
		state:   state,
		icons:   &iconCache{},
		menu:    menu,
		acts:    acts,
	}
	if err := srv.export(); err != nil {
		log.Fatalf("export: %v", err)
	}

	sigCh := make(chan *dbus.Signal, 8)
	conn.Signal(sigCh)
	// Watch the watcher: a waybar restart drops our registration and the new
	// owner must see a fresh RegisterStatusNotifierItem call.
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, watcherBus),
	); err != nil {
		log.Fatalf("cannot watch %s: %v", watcherBus, err)
	}

	// One poll before registering so the first GetAll already carries real
	// state; then the loops take over.
	srv.applyState(pollState(ctl))
	go srv.runRegistration(sigCh)
	go pollLoop(ctl, *interval, srv)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		// SIGTERM (systemd stop) exits 0 so Restart=on-failure stays quiet.
		log.Printf("terminated, exiting")
		os.Exit(0)
	}()

	select {} // the goroutines own everything from here
}

// pollLoop refreshes the rendered state every interval. A panic in one
// iteration is logged and swallowed — the tray outlives everything.
func pollLoop(ctl string, interval time.Duration, srv *trayServer) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for range tick.C {
		func() {
			defer func() {
				if v := recover(); v != nil {
					log.Printf("panic in poll loop: %v", v)
				}
			}()
			srv.applyState(pollState(ctl))
		}()
	}
}

// checkInterval rejects non-positive poll intervals before any ticker exists:
// time.NewTicker panics on those, and a crash loop under systemd is worse than
// one clear startup error.
func checkInterval(d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("--interval must be greater than zero, got %s", d)
	}
	return nil
}

// resolvePath returns flagPath when set, otherwise flagPath resolved relative
// to the executable: <exe dir>/../<rel>. Same layout rule as mirrorling-ui.
func resolvePath(flagPath, rel string) string {
	if flagPath != "" {
		return flagPath
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "..", rel)
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// actions runs mirrorling subcommands detached from the tray.
type actions struct {
	mirrorling   string
	mirrorlingOK bool
}

func (a *actions) run(args ...string) {
	if !a.mirrorlingOK {
		log.Printf("cannot run mirrorling %s: binary missing", strings.Join(args, " "))
		return
	}
	// Detached: stdin is the null device so mirrorling's TTY-guarded prompts can
	// never fire. stdout/stderr are wired to the helper's stderr (the journal),
	// so a failing action — a first-run `mirrorling on` above all — is visible
	// instead of vanishing with the inherited nil stdio.
	cmd := exec.Command(a.mirrorling, args...)
	cmd.Stdin = nil
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("mirrorling %s: %v", strings.Join(args, " "), err)
		return
	}
	// Wait off-thread so exited children don't linger as zombies, and log any
	// non-zero exit against the action that asked for it.
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("mirrorling %s failed: %v", strings.Join(args, " "), err)
		}
	}()
}

func (a *actions) open()  { a.run("ui") }
func (a *actions) start() { a.run("on") }
func (a *actions) stop()  { a.run("off") }
