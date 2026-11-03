// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Mirrorling contributors.

// Command mirrorling-ui serves the mirrorling local web control panel.
//
// By default it runs as a plain headless HTTP server. With --window it opens
// the same panel inside a native WebKitGTK desktop window (via Wails v2):
// if nothing is listening on --addr yet it owns the port and serves it on TCP
// as well, otherwise it attaches to the already-running instance. The TCP
// listener lives in the owner's window process — closing that window shuts
// the server down (in-flight action commands are killed first). An attached
// window does not own the port: closing it leaves the owner running, while
// closing the owner first leaves the attached window reporting that the
// server is gone.
//
// On Linux the window backend is webkit2gtk-4.1; build it with
// `go build -tags production,webkit2_41`. production enables Wails'
// production frontend (without it — and without the dev tag — wails.Run
// returns a build-tags error at startup); webkit2_41 selects the
// libsoup3/webkit2gtk-4.1 API (without it the legacy 4.0 API is linked).
// `desktop` is not a Go build tag in Wails v2.17 — it is a wails CLI output
// type, so passing it is a harmless no-op.
//
// SECURITY MODEL: this API carries no authentication — it can start and stop
// mirroring, rewrite the persisted mirrorling config and read daemon logs. It is
// therefore bound to loopback addresses only and relies on same-user trust:
// anything that can reach 127.0.0.1 on this machine already runs as this
// user. --allow-nonlocal deliberately disables that model; do not use it on a
// multi-user host or an untrusted network.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	assetserver "github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed index.html
var indexFS embed.FS

var indexBytes []byte

type server struct {
	mirrorling string // path to the mirrorling CLI
	ctl        string // path to doubletake-ctl

	// baseCtx, when non-nil, is the lifetime of the process that owns the
	// panel (the window in --window owner mode). Every request context is
	// derived from it, so canceling it kills the child process groups of
	// in-flight actions before the HTTP server is shut down.
	baseCtx context.Context
}

func main() {
	log.SetFlags(log.Ltime)

	mirrorlingFlag := flag.String("mirrorling", "", "path to the mirrorling CLI (default: <dir of executable>/../mirrorling)")
	ctlFlag := flag.String("ctl", "", "path to doubletake-ctl (default: <dir of executable>/../bin/doubletake-ctl)")
	addrFlag := flag.String("addr", "127.0.0.1:8791", "listen address; loopback only unless --allow-nonlocal")
	allowNonLocal := flag.Bool("allow-nonlocal", false, "allow binding a non-loopback address (disables the localhost-only security model)")
	windowFlag := flag.Bool("window", false, "open the panel in a native desktop window; attach to an instance already running on --addr if there is one")
	flag.Parse()

	index, err := indexFS.ReadFile("index.html")
	if err != nil {
		log.Fatalf("embedded index.html missing: %v", err)
	}
	indexBytes = index

	s := &server{
		mirrorling: resolvePath(*mirrorlingFlag, "mirrorling"),
		ctl:        resolvePath(*ctlFlag, filepath.Join("bin", "doubletake-ctl")),
	}
	for _, pc := range []struct{ label, flag, path string }{
		{"mirrorling", "mirrorling", s.mirrorling},
		{"doubletake-ctl", "ctl", s.ctl},
	} {
		if pc.path == "" {
			log.Printf("warning: %s path could not be resolved (pass --%s)", pc.label, pc.flag)
			continue
		}
		if fi, err := os.Stat(pc.path); err != nil || fi.IsDir() {
			log.Printf("warning: %s not found at %s — related actions will fail until the path is fixed", pc.label, pc.path)
		}
	}

	host, _, err := validateAddr(*addrFlag)
	if err != nil {
		log.Fatalf("%v", err)
	}
	loopback := isLoopbackHost(host)
	if !loopback {
		if !*allowNonLocal {
			log.Fatalf("refusing to bind %q: the API is unauthenticated and localhost-only; pass --allow-nonlocal to override", *addrFlag)
		}
		log.Printf("WARNING: --allow-nonlocal set: the unauthenticated API is reachable from other machines via %q", *addrFlag)
	}

	if *windowFlag {
		runWindowMode(s, *addrFlag, loopback)
		return
	}

	mux := s.newMux()

	log.Printf("mirrorling ui listening on http://%s", *addrFlag)
	var handler http.Handler = mux
	if loopback {
		handler = hostGuard(mux)
	}
	srv := &http.Server{
		Addr:              *addrFlag,
		Handler:           handler,
		ReadHeaderTimeout: headerTimeout,
	}
	log.Fatal(srv.ListenAndServe())
}

// runWindowMode implements --window. A graphical session is required before
// anything is bound (Wails panics inside wails.Run when GTK cannot init, and a
// panic after binding would leave a live listener behind), then --addr is
// probed: if nothing answers, this process owns the port and serves the same
// mux on TCP (hostGuard included, exactly like headless mode). If a healthy
// mirrorling ui answers instead, the window attaches to it through a reverse
// proxy without binding.
func runWindowMode(s *server, addr string, loopback bool) {
	_, port, err := validateAddr(addr)
	if err != nil {
		log.Fatalf("%v", err)
	}
	// Checked before the probe and before net.Listen: no graphical session
	// means no window and no reason to claim the port.
	if !hasGraphicalSession() {
		failNoWindow("")
	}

	probe, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		// Nothing answered: the port is ours to claim.
		listenWindowMode(s, addr, port, loopback)
		return
	}
	probe.Close()

	// Something answered: attach only if it is a healthy mirrorling ui —
	// silently pointing the window at a foreign service would be worse than
	// refusing to start.
	if !healthyInstance(addr) {
		portInUse(port)
	}
	if err := runWailsWindow(attachProxy(addr)); err != nil {
		failNoWindow(err.Error())
	}
}

// listenWindowMode binds addr synchronously, then serves the mux on that
// listener and opens the owning window.
//
// There is deliberately no "probe, then ListenAndServe in a goroutine" race:
// net.Listen happens once, here, and a lost race is the ordinary port-in-use
// failure instead of a server that died under a window that already opened.
// The listener is tied to this process, so the window closing tears it down.
func listenWindowMode(s *server, addr, port string, loopback bool) {
	// Bind before deriving the base context: a failed listen leaves nothing
	// behind, and exiting from bindError must not skip any cleanup.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		bindError(addr, port, err)
	}

	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	mux := s.newMux()
	var handler http.Handler = mux
	if loopback {
		handler = hostGuard(mux)
	}
	// Also reachable by the webview, which bypasses http.Server entirely:
	// safe derives each request context from this lifetime.
	s.baseCtx = baseCtx

	srv := newPanelHTTPServer(handler, baseCtx)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Printf("mirrorling ui listening on http://%s", addr)

	runErr := runWailsWindow(mux)

	// Cancel handler contexts BEFORE Shutdown: Shutdown waits for in-flight
	// handlers, and an action such as `mirrorling on` can run for 90 seconds.
	// Canceling first kills its process group instead of orphaning it when
	// the window that started it closes.
	cancelBase()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	_ = srv.Shutdown(shutdownCtx)
	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %v", err)
		}
	default:
	}

	if runErr != nil {
		failNoWindow(runErr.Error())
	}
}

// newPanelHTTPServer builds the TCP server used in owner mode. baseCtx is the
// owner's lifetime: http.Server derives every request context from it, and
// canceling it before Shutdown is what lets Shutdown finish (in-flight actions
// do not wait out their full timeouts) without orphaning their child process
// groups.
func newPanelHTTPServer(handler http.Handler, baseCtx context.Context) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: headerTimeout,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}
}

// portInUse reports a foreign service on the requested port and exits. It is
// the single wording for "somebody else has it" when a healthy instance was
// found by the attach probe.
func portInUse(port string) {
	fmt.Fprintf(os.Stderr, "mirrorling: port %s is in use by something else (try: mirrorling ui --port N)\n", port)
	os.Exit(1)
}

// bindErrorMessage renders a net.Listen failure. Only EADDRINUSE means the
// port is taken by another process; a permission, DNS or address failure is
// reported with the listener and the underlying error instead of blaming a
// foreign service.
func bindErrorMessage(addr, port string, err error) string {
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Sprintf("mirrorling: port %s is in use by something else (try: mirrorling ui --port N)", port)
	}
	return fmt.Sprintf("mirrorling: cannot listen on %s: %v", addr, err)
}

// bindError prints bindErrorMessage to stderr and exits 1. A failed net.Listen
// binds nothing, so there is no listener (or context) to release first.
func bindError(addr, port string, err error) {
	fmt.Fprintln(os.Stderr, bindErrorMessage(addr, port, err))
	os.Exit(1)
}

// hasGraphicalSession reports whether a window can even be attempted. Wails'
// Linux backend needs GTK, which needs an X11 or Wayland display; without one
// wails.Run panics ("failed to init GTK") instead of returning an error.
func hasGraphicalSession() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// failNoWindow prints the one-line actionable failure (plus an optional
// detail line for logs) and exits 1 so a launcher can fall back to the
// headless command the hint names. It never returns.
func failNoWindow(detail string) {
	if detail != "" {
		log.Printf("window unavailable: %s", detail)
	}
	fmt.Fprintln(os.Stderr, "mirrorling: no graphical session — use: mirrorling ui --no-open")
	os.Exit(1)
}

// healthyInstance reports whether addr serves this panel: the /api/health
// response must be a 200 whose JSON body says {"ok":true}. A 200 from some
// other service (or a non-JSON body) MUST NOT be adopted as the owner — the
// caller reports port-in-use instead.
func healthyInstance(addr string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return false
	}
	var probe struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.OK
}

// attachProxy returns a reverse proxy to an already-running mirrorling ui
// on addr. The Director rewrites the request target AND req.Host to the
// target: the attached instance's hostGuard inspects req.Host to reject
// DNS-rebinding requests, so the proxied requests must present themselves as
// loopback or the window would see 403s across the board.
//
// The ErrorHandler matters when the owner window exits: without it the proxy
// would log and drop the connection, leaving the attached page silently
// showing stale data. It answers a JSON 502 instead, which the page renders as
// a visible banner.
func attachProxy(addr string) http.Handler {
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = addr
			req.Host = addr
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("attach proxy %s %s: %v", r.Method, r.URL.Path, err)
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"ok":     false,
				"output": "control panel server closed — run: mirrorling ui",
			})
		},
	}
}

// runWailsWindow opens the native window. Every request the webview makes is
// routed to handler — the local mux in owner mode, a reverse proxy in attach
// mode. No Go<->JS bindings are installed: the page is the very same
// vanilla-JS panel speaking plain HTTP to /api/*.
//
// It returns nil when the window closes normally and an error otherwise. A
// GTK/WebKit panic is recovered here: wails.Run does not return it, and
// letting it escape would print a Go stack trace from a desktop launcher with
// no visible terminal.
func runWailsWindow(handler http.Handler) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("GTK/WebKit panic: %v", v)
		}
	}()
	return wails.Run(&options.App{
		Title:     "Mirrorling",
		Width:     460,
		Height:    780,
		MinWidth:  380,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			// Assets must be present (wails validates that index.html
			// exists), but it must not answer GETs itself — every request
			// has to reach handler so "/" and /api/* behave exactly as they
			// do over TCP. Middleware replaces the default asset chain
			// wholesale, making handler the sole responder; the Handler
			// field would only ever see requests the embedded FS missed.
			Assets:     indexFS,
			Middleware: func(http.Handler) http.Handler { return handler },
		},
		Bind:  []interface{}{},
		Linux: &linux.Options{ProgramName: "mirrorling"},
	})
}

// newMux builds the routing table shared by headless mode and window mode.
func (s *server) newMux() *http.ServeMux {
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
	return mux
}

// hostGuard rejects requests whose Host header is not this loopback server:
// a page can re-resolve its own hostname to 127.0.0.1 (DNS rebinding) and
// become "same-origin" with the unauthenticated API — the foreign Host header
// is the tell, and the browser cannot forge it. Only enforced on the default
// loopback binding; --allow-nonlocal has already widened the model on purpose.
//
// It guards the TCP listener only. The --window webview fetches through the
// in-process asset server with a "wails" Host header, which is not network
// reachable and must not be guarded.
func hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Host
		if hp, _, err := net.SplitHostPort(h); err == nil {
			h = hp
		}
		if h == "localhost" || (net.ParseIP(h) != nil && net.ParseIP(h).IsLoopback()) {
			next.ServeHTTP(w, r)
			return
		}
		log.Printf("rejected request with foreign Host %q from %s", r.Host, r.RemoteAddr)
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "output": "foreign host"})
	})
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

// validateAddr parses --addr and rejects ports outside 1-65535 (including 0,
// which asks the kernel for an ephemeral port — useless for a panel whose
// whole attach flow depends on a predictable URL). It runs before any probe
// or bind so a bad flag fails with one clear message instead of a confusing
// listen error deep inside window mode.
func validateAddr(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("invalid --addr %q: %v", addr, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("invalid --addr %q: port must be 1-65535, got %q", addr, port)
	}
	return host, port, nil
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
//
// It is also the single gate for state-changing requests: every POST must
// declare Content-Type: application/json. The API is unauthenticated, and a
// cross-site page can submit a no-CORS form or text/plain fetch (both are
// "simple" requests, so no preflight is sent); requiring JSON forces a
// preflight that this server never grants, and it covers body-less actions
// like /api/on and /api/off that never call decodeBody.
//
// Finally, every handler runs under a context derived from s.baseCtx, the
// owner's lifetime. Cancel is what kills an in-flight action's process group
// when the window that started it closes.
func (s *server) safe(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic in %s %s: %v", r.Method, r.URL.Path, v)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "output": fmt.Sprintf("internal error: %v", v)})
			}
		}()
		if r.Method == http.MethodPost && !isJSONContentType(r) {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"ok": false, "output": "Content-Type must be application/json"})
			return
		}
		if s.baseCtx != nil {
			ctx, cancel := context.WithCancel(r.Context())
			stop := context.AfterFunc(s.baseCtx, cancel)
			defer func() {
				stop()
				cancel()
			}()
			r = r.WithContext(ctx)
		}
		h(w, r)
	}
}
