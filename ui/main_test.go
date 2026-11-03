// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Mirrorling contributors.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	b, err := indexFS.ReadFile("index.html")
	if err != nil {
		fmt.Fprintln(os.Stderr, "embedded index.html missing:", err)
		os.Exit(1)
	}
	indexBytes = b
	os.Exit(m.Run())
}

// serve runs one request through handler and returns the recorded response.
func serve(h http.Handler, method, target, contentType, body string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHostGuard(t *testing.T) {
	allow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guarded := hostGuard(allow)

	cases := []struct {
		name string
		host string
		want int
	}{
		{"ipv4 loopback", "127.0.0.1:8791", http.StatusOK},
		{"localhost", "localhost:8791", http.StatusOK},
		{"ipv6 loopback", "[::1]:8791", http.StatusOK},
		{"foreign hostname", "evil.example:8791", http.StatusForbidden},
		{"foreign address", "192.168.1.20:8791", http.StatusForbidden},
		{"empty host", "", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
			req.Host = tc.host
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("Host %q: status = %d, want %d", tc.host, rec.Code, tc.want)
			}
		})
	}
}

// writeStub writes an executable shell script that appends its arguments to
// path, so a test can prove whether the CLI was invoked at all.
func writeStub(t *testing.T, dir, name, script string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	return p
}

func markerContents(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read marker: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// TestPostRequiresJSONContentType is the CSRF matrix: every mutating route
// rejects a text/plain body (what a cross-site form or no-CORS fetch can send
// without a preflight) and must not run the CLI, while the panel's JSON POST
// succeeds and does run it.
func TestPostRequiresJSONContentType(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> '%s'\n", marker)
	mirrorling := writeStub(t, dir, "mirrorling", script)
	ctl := writeStub(t, dir, "ctl", script)
	mux := (&server{mirrorling: mirrorling, ctl: ctl}).newMux()

	routes := []struct {
		path string
		cli  string
		arg  string
	}{
		{"/api/on", "mirrorling", "on"},
		{"/api/off", "mirrorling", "off"},
		{"/api/start", "mirrorling", "start"},
		{"/api/stop", "mirrorling", "stop"},
		{"/api/mute", "ctl", "mute"},
		{"/api/unmute", "ctl", "unmute"},
	}
	for _, rt := range routes {
		t.Run(rt.path, func(t *testing.T) {
			if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}

			rec := serve(mux, http.MethodPost, rt.path, "text/plain", "{}")
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("text/plain POST status = %d, want %d (body %s)", rec.Code, http.StatusUnsupportedMediaType, rec.Body)
			}
			var rejected map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &rejected); err != nil {
				t.Fatalf("415 body is not JSON: %v (%q)", err, rec.Body.String())
			}
			if rejected["ok"] != false {
				t.Fatalf("415 body ok = %v, want false", rejected["ok"])
			}
			if out := markerContents(t, marker); out != "" {
				t.Fatalf("text/plain POST ran %s: %q", rt.cli, out)
			}

			// No Content-Type and a form type are equally cross-site friendly.
			for _, ct := range []string{"", "application/x-www-form-urlencoded"} {
				if rec := serve(mux, http.MethodPost, rt.path, ct, "{}"); rec.Code != http.StatusUnsupportedMediaType {
					t.Fatalf("content-type %q status = %d, want 415", ct, rec.Code)
				}
				if out := markerContents(t, marker); out != "" {
					t.Fatalf("content-type %q ran %s: %q", ct, rt.cli, out)
				}
			}

			rec = serve(mux, http.MethodPost, rt.path, "application/json", "{}")
			if rec.Code != http.StatusOK {
				t.Fatalf("JSON POST status = %d, want 200 (body %s)", rec.Code, rec.Body)
			}
			var body struct {
				OK     bool   `json:"ok"`
				Output string `json:"output"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("JSON POST body: %v (%q)", err, rec.Body.String())
			}
			if !body.OK {
				t.Fatalf("JSON POST not ok: %+v", body)
			}
			if out := markerContents(t, marker); !strings.Contains(out, rt.arg) {
				t.Fatalf("%s stub ran with %q, want %q", rt.cli, out, rt.arg)
			}
		})
	}
}

func TestGetRoutesDoNotRequireJSONContentType(t *testing.T) {
	s := &server{}
	mux := s.newMux()
	for _, path := range []string{"/api/health", "/"} {
		rec := serve(mux, http.MethodGet, path, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestIndexServesPanelWithOfflineBanner(t *testing.T) {
	rec := serve((&server{}).newMux(), http.MethodGet, "/", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET / Content-Type = %q", ct)
	}
	page := rec.Body.String()
	for _, want := range []string{
		`id="offlineBanner"`,
		`id="offlineBanner" role="alert" hidden`,
		"Control panel server closed",
		"setOffline(true)",
		"getJSON('/api/state')",
		"'Content-Type': 'application/json'",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
}

func TestHealthEndpointBody(t *testing.T) {
	rec := serve((&server{}).newMux(), http.MethodGet, "/api/health", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.OK {
		t.Fatalf("health body = %q (err %v), want {\"ok\":true}", rec.Body.String(), err)
	}
}

func TestValidateAddr(t *testing.T) {
	valid := []struct {
		addr string
		host string
		port string
	}{
		{"127.0.0.1:8791", "127.0.0.1", "8791"},
		{"localhost:1", "localhost", "1"},
		{":65535", "", "65535"},
	}
	for _, tc := range valid {
		host, port, err := validateAddr(tc.addr)
		if err != nil {
			t.Fatalf("validateAddr(%q) = %v, want success", tc.addr, err)
		}
		if host != tc.host || port != tc.port {
			t.Fatalf("validateAddr(%q) = (%q,%q), want (%q,%q)", tc.addr, host, port, tc.host, tc.port)
		}
	}

	invalid := []string{
		"127.0.0.1:0",
		":0",
		"127.0.0.1:65536",
		"127.0.0.1:-1",
		"127.0.0.1:http",
		"127.0.0.1",
		"",
	}
	for _, addr := range invalid {
		_, _, err := validateAddr(addr)
		if err == nil {
			t.Errorf("validateAddr(%q) succeeded, want error", addr)
			continue
		}
		if !strings.Contains(err.Error(), "invalid --addr") {
			t.Errorf("validateAddr(%q) error = %q, want an --addr message", addr, err)
		}
	}

	// Port 0 and out-of-range ports get the same actionable hint.
	for _, addr := range []string{"127.0.0.1:0", "127.0.0.1:65536"} {
		if _, _, err := validateAddr(addr); err == nil || !strings.Contains(err.Error(), "port must be 1-65535") {
			t.Errorf("validateAddr(%q) error = %v, want port range message", addr, err)
		}
	}
}

func TestHealthyInstanceRequiresOKJSON(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"healthy", http.StatusOK, `{"ok":true}`, true},
		{"healthy extra field", http.StatusOK, `{"ok":true,"extra":1}`, true},
		{"reports false", http.StatusOK, `{"ok":false}`, false},
		{"not json", http.StatusOK, `OK`, false},
		{"empty body", http.StatusOK, ``, false},
		{"wrong json shape", http.StatusOK, `[]`, false},
		{"error status", http.StatusServiceUnavailable, `{"ok":true}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			if got := healthyInstance(upstream.Listener.Addr().String()); got != tc.want {
				t.Fatalf("healthyInstance() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAttachProxyReportsClosedServer(t *testing.T) {
	// Bind and release a port so nothing answers there, like an owner window
	// that has exited.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	front := httptest.NewServer(attachProxy(addr))
	defer front.Close()

	resp, err := http.Get(front.URL + "/api/state")
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	var body struct {
		OK     bool   `json:"ok"`
		Output string `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode 502 body: %v", err)
	}
	const want = "control panel server closed — run: mirrorling ui"
	if body.OK || body.Output != want {
		t.Fatalf("502 body = %+v, want ok=false output=%q", body, want)
	}
}

// TestAttachProxyLeavesOwnerRunning covers the attach contract from the
// proxy's side: it forwards to the owner and owns nothing, so tearing the
// attached window down cannot stop the owner.
func TestAttachProxyLeavesOwnerRunning(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer upstream.Close()

	attached := httptest.NewServer(attachProxy(upstream.Listener.Addr().String()))

	resp, err := http.Get(attached.URL + "/api/health")
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxied health status = %d, want 200", resp.StatusCode)
	}
	attached.Close()

	resp, err = http.Get(upstream.URL + "/api/health")
	if err != nil {
		t.Fatalf("owner request after attached window closed: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner status after attach closed = %d, want 200", resp.StatusCode)
	}
}

func waitForPid(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stub pid file %s was never written", path)
	return 0
}

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d (or its process group) is still running after cancel", pid)
}

// TestCancelKillsInFlightAction reproduces the orphaned-action bug: the owner
// window closes while `mirrorling on` is running. Canceling the panel lifetime
// (http.Server.BaseContext and the mux's request contexts) must kill the
// child's process group, and the request must return instead of holding
// Shutdown for the full 90s action timeout.
func TestCancelKillsInFlightAction(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	childFile := filepath.Join(dir, "childpid")
	// The action is a shell with a background child, like mirrorling spawning
	// doubletake-ctl: canceling must kill the whole process group, not just
	// the direct child, or the grandchild is orphaned.
	stub := writeStub(t, dir, "mirrorling", fmt.Sprintf(
		"#!/bin/sh\nsleep 60 &\necho $! > '%s'\necho $$ > '%s'\nwait\n", childFile, pidFile))

	baseCtx, cancel := context.WithCancel(context.Background())
	s := &server{mirrorling: stub, ctl: stub, baseCtx: baseCtx}
	mux := s.newMux()

	srv := httptest.NewUnstartedServer(mux)
	srv.Config = newPanelHTTPServer(mux, baseCtx)
	srv.Start()
	defer func() {
		cancel()
		srv.Close()
	}()

	type result struct {
		status int
		body   []byte
		err    error
	}
	done := make(chan result, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/on", strings.NewReader("{}"))
		if err != nil {
			done <- result{err: err}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		done <- result{status: resp.StatusCode, body: b, err: err}
	}()

	pid := waitForPid(t, pidFile, 5*time.Second)
	childPid := waitForPid(t, childFile, 5*time.Second)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("stub %d is not running before cancel: %v", pid, err)
	}
	if err := syscall.Kill(childPid, 0); err != nil {
		t.Fatalf("stub grandchild %d is not running before cancel: %v", childPid, err)
	}

	cancel() // what the owner window does before Shutdown

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("canceled request: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("canceled request status = %d, want 200 (body %s)", res.status, res.body)
		}
		var body struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(res.body, &body); err != nil {
			t.Fatalf("decode canceled response: %v (%q)", err, res.body)
		}
		if body.OK {
			t.Fatalf("canceled action reported ok=true: %s", res.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("request did not return after canceling the panel lifetime")
	}

	waitForProcessExit(t, pid, 5*time.Second)
	waitForProcessExit(t, childPid, 5*time.Second)
}

// TestBindErrorMessage covers the bind-failure classification: a port taken by
// another process is the only case that may say "in use by something else";
// permission, DNS and address failures must report the listener and cause.
func TestBindErrorMessage(t *testing.T) {
	const addr, port = "127.0.0.1:7331", "7331"

	cases := []struct {
		name string
		err  error
	}{
		{"address in use", &net.OpError{Op: "listen", Net: "tcp", Err: os.NewSyscallError("bind", syscall.EADDRINUSE)}},
		{"wrapped address in use", fmt.Errorf("listen tcp: %w", syscall.EADDRINUSE)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const want = "mirrorling: port 7331 is in use by something else (try: mirrorling ui --port N)"
			if got := bindErrorMessage(addr, port, tc.err); got != want {
				t.Fatalf("bindErrorMessage() = %q, want %q", got, want)
			}
		})
	}

	other := []struct {
		name string
		err  error
	}{
		{"permission denied", &net.OpError{Op: "listen", Net: "tcp", Err: os.NewSyscallError("bind", syscall.EACCES)}},
		{"invalid address", &net.AddrError{Err: "invalid port", Addr: "127.0.0.1:notaport"}},
		{"dns failure", &net.DNSError{Err: "no such host", Name: "no-such-host.invalid", IsNotFound: true}},
		{"generic", fmt.Errorf("socket: %w", syscall.ENOBUFS)},
	}
	for _, tc := range other {
		t.Run(tc.name, func(t *testing.T) {
			got := bindErrorMessage(addr, port, tc.err)
			wantPrefix := "mirrorling: cannot listen on " + addr + ": "
			if !strings.HasPrefix(got, wantPrefix) {
				t.Fatalf("bindErrorMessage() = %q, want prefix %q", got, wantPrefix)
			}
			if !strings.Contains(got, tc.err.Error()) {
				t.Fatalf("bindErrorMessage() = %q, want it to include %q", got, tc.err.Error())
			}
			if strings.Contains(got, "is in use by something else") {
				t.Fatalf("bindErrorMessage() = %q blames a foreign service for %v", got, tc.err)
			}
		})
	}
}

// TestBindErrorMessageRealAddressInUse checks the classifier against the real
// error net.Listen returns for a port that is actually taken.
func TestBindErrorMessageRealAddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	if _, err := net.Listen("tcp", addr); err == nil {
		t.Fatal("second listen on the same address succeeded")
	} else if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("second listen error = %v, want EADDRINUSE", err)
	}

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	want := "mirrorling: port " + port + " is in use by something else (try: mirrorling ui --port N)"
	if got := bindErrorMessage(addr, port, syscall.EADDRINUSE); got != want {
		t.Fatalf("bindErrorMessage() = %q, want %q", got, want)
	}
}

// TestBindErrorMessagePermissionDenied exercises a real permission-denied bind
// when the test process can produce one (non-root, Linux).
func TestBindErrorMessagePermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: privileged ports are not refused")
	}
	const addr = "127.0.0.1:1"
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		ln.Close()
		t.Skip("binding a privileged port succeeded in this environment")
	}
	if !errors.Is(err, syscall.EACCES) {
		t.Skipf("listen error = %v, want EACCES", err)
	}
	got := bindErrorMessage(addr, "1", err)
	wantPrefix := "mirrorling: cannot listen on " + addr + ": "
	if !strings.HasPrefix(got, wantPrefix) || !strings.Contains(got, err.Error()) {
		t.Fatalf("bindErrorMessage() = %q, want %q + %q", got, wantPrefix, err.Error())
	}
	if strings.Contains(got, "is in use by something else") {
		t.Fatalf("bindErrorMessage() = %q misreports a permission failure", got)
	}
}
