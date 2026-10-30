// Command samcast-ui serves the samsung-cast local web control panel.
//
// SECURITY MODEL: this API carries no authentication — it can start and stop
// mirroring, rewrite the persisted samcast config and read daemon logs. It is
// therefore bound to loopback addresses only and relies on same-user trust:
// anything that can reach 127.0.0.1 on this machine already runs as this
// user. --allow-nonlocal deliberately disables that model; do not use it on a
// multi-user host or an untrusted network.
package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed index.html
var indexFS embed.FS

var indexBytes []byte

type server struct {
	samcast string // path to the samcast CLI
	ctl     string // path to doubletake-ctl
}

func main() {
	log.SetFlags(log.Ltime)

	samcastFlag := flag.String("samcast", "", "path to the samcast CLI (default: <dir of executable>/../samcast)")
	ctlFlag := flag.String("ctl", "", "path to doubletake-ctl (default: <dir of executable>/../bin/doubletake-ctl)")
	addrFlag := flag.String("addr", "127.0.0.1:8791", "listen address; loopback only unless --allow-nonlocal")
	allowNonLocal := flag.Bool("allow-nonlocal", false, "allow binding a non-loopback address (disables the localhost-only security model)")
	flag.Parse()

	index, err := indexFS.ReadFile("index.html")
	if err != nil {
		log.Fatalf("embedded index.html missing: %v", err)
	}
	indexBytes = index

	s := &server{
		samcast: resolvePath(*samcastFlag, "samcast"),
		ctl:     resolvePath(*ctlFlag, filepath.Join("bin", "doubletake-ctl")),
	}
	for label, p := range map[string]string{"samcast": s.samcast, "doubletake-ctl": s.ctl} {
		if p == "" {
			log.Printf("warning: %s path could not be resolved (pass --%s)", label, label)
			continue
		}
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			log.Printf("warning: %s not found at %s — related actions will fail until the path is fixed", label, p)
		}
	}

	host, _, err := net.SplitHostPort(*addrFlag)
	if err != nil {
		log.Fatalf("invalid --addr %q: %v", *addrFlag, err)
	}
	if !isLoopbackHost(host) {
		if !*allowNonLocal {
			log.Fatalf("refusing to bind %q: the API is unauthenticated and localhost-only; pass --allow-nonlocal to override", *addrFlag)
		}
		log.Printf("WARNING: --allow-nonlocal set: the unauthenticated API is reachable from other machines via %q", *addrFlag)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.safe(s.handleIndex))
	mux.HandleFunc("/api/health", s.safe(s.handleHealth))
	mux.HandleFunc("/api/state", s.safe(s.handleState))
	mux.HandleFunc("/api/tvs", s.safe(s.handleTVs))
	mux.HandleFunc("/api/monitors", s.safe(s.handleMonitors))
	mux.HandleFunc("/api/tv", s.safe(s.handleSetTV))
	mux.HandleFunc("/api/tv/forget", s.safe(s.handleForgetTV))
	mux.HandleFunc("/api/monitor", s.safe(s.handleSetMonitor))
	mux.HandleFunc("/api/on", s.safe(s.handleOn))
	mux.HandleFunc("/api/off", s.safe(s.handleOff))
	mux.HandleFunc("/api/start", s.safe(s.handleStart))
	mux.HandleFunc("/api/stop", s.safe(s.handleStop))
	mux.HandleFunc("/api/pin", s.safe(s.handlePin))
	mux.HandleFunc("/api/mute", s.safe(s.handleMute))
	mux.HandleFunc("/api/unmute", s.safe(s.handleUnmute))
	mux.HandleFunc("/api/logs", s.safe(s.handleLogs))

	log.Printf("samsung-cast ui listening on http://%s", *addrFlag)
	srv := &http.Server{
		Addr:              *addrFlag,
		Handler:           mux,
		ReadHeaderTimeout: headerTimeout,
	}
	log.Fatal(srv.ListenAndServe())
}

// resolvePath returns flagPath when set, otherwise flagPath resolved relative
// to the executable: <exe dir>/../<rel>. Both defaults land on repo layout
// whether the binary sits in ui/ or bin/.
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

// isLoopbackHost reports whether the listen host is loopback. An empty host
// (":port") binds every interface and is treated as non-loopback;
// "localhost" is accepted and resolved by the OS.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// safe wraps a handler so a panic becomes a JSON 500 instead of a dropped
// connection; handlers must never take the page down.
func (s *server) safe(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic in %s %s: %v", r.Method, r.URL.Path, v)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "output": fmt.Sprintf("internal error: %v", v)})
			}
		}()
		h(w, r)
	}
}
