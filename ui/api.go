// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Mirrorling contributors.

package main

import (
	"encoding/json"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	timeoutCtlStatus = 5 * time.Second
	timeoutRaw       = 5 * time.Second
	timeoutList      = 20 * time.Second
	timeoutSetter    = 10 * time.Second
	timeoutOn        = 90 * time.Second
	timeoutOff       = 10 * time.Second
	timeoutStartStop = 30 * time.Second
	timeoutPin       = 15 * time.Second
	timeoutMute      = 10 * time.Second
	timeoutLogs      = 10 * time.Second
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func methodOnly(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "output": "method not allowed"})
		return false
	}
	return true
}

// isJSONContentType reports whether r declares a JSON body. It is the single
// policy used by both the central POST gate in safe (body-less actions) and
// decodeBody (endpoints that parse one).
func isJSONContentType(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// decodeBody reads a small JSON object into dst; a bad body is a 400.
//
// The Content-Type must be application/json: the API is unauthenticated, and
// a cross-site page can POST text/plain bodies (a no-CORS-preflight form or
// fetch) — accepting any type would hand every endpoint to any webpage the
// user visits. The panel itself always sends application/json. safe enforces
// the same rule for every POST; this repeats it for the handlers that decode
// a body so they stay correct if they are ever mounted elsewhere.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !isJSONContentType(r) {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"ok": false, "output": "Content-Type must be application/json"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "output": "invalid request body: " + err.Error()})
		return false
	}
	return true
}

// handleIndex serves the embedded page; the panel is a live appliance, so it
// must never be cached.
func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "output": "not found"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(indexBytes)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleState is the cheap poll target. It runs four short calls in sequence,
// each with its own 5s timeout, and degrades gracefully: a dead daemon makes
// ctl null, failed --raw reads make the config fields empty.
func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	ctx := r.Context()
	resp := struct {
		DaemonRunning     bool            `json:"daemon_running"`
		ConfiguredTV      string          `json:"configured_tv"`
		ConfiguredMonitor string          `json:"configured_monitor"`
		Ctl               json.RawMessage `json:"ctl"`
	}{Ctl: json.RawMessage("null")}

	if exit, stdout, _, timedOut := exec_(ctx, timeoutCtlStatus, s.ctl, "status"); exit == 0 && !timedOut && json.Valid([]byte(strings.TrimSpace(stdout))) {
		resp.Ctl = json.RawMessage(strings.TrimSpace(stdout))
	}

	if exit, _, _, _ := exec_(ctx, timeoutRaw, "systemctl", "--user", "is-active", "mirrorling"); exit == 0 {
		resp.DaemonRunning = true
	}

	if exit, stdout, _, _ := exec_(ctx, timeoutRaw, s.mirrorling, "tv", "--raw"); exit == 0 {
		resp.ConfiguredTV = strings.TrimSpace(stdout)
	}
	if exit, stdout, _, _ := exec_(ctx, timeoutRaw, s.mirrorling, "monitor", "--raw"); exit == 0 {
		resp.ConfiguredMonitor = strings.TrimSpace(stdout)
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleTVs lists Samsung AirPlay receivers — live ones first, then saved
// (remembered but currently undiscoverable) ones. `mirrorling tvs --raw` exits
// non-zero with a stderr message only when NOTHING at all was found — that is
// the empty state, not a failure.
func (s *server) handleTVs(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	exit, stdout, stderr, timedOut := exec_(r.Context(), timeoutList, s.mirrorling, "tvs", "--raw")
	if timedOut {
		writeJSON(w, http.StatusOK, map[string]any{"tvs": []tv{}, "error": "timed out"})
		return
	}
	if exit != 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"tvs":   []tv{},
			"error": firstNonEmpty(strings.TrimSpace(stderr), strings.TrimSpace(stdout)),
		})
		return
	}
	tvs := []tv{}
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		// state rides in the 4th column; an older mirrorling prints only three
		// (it had no saved TVs), so an absent state can only mean "live".
		state := strings.TrimSpace(col(parts, 3))
		if state == "" {
			state = "live"
		}
		tvs = append(tvs, tv{
			Name:  strings.TrimSpace(col(parts, 0)),
			IP:    strings.TrimSpace(col(parts, 1)),
			Model: strings.TrimSpace(col(parts, 2)),
			State: state,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tvs": tvs})
}

type tv struct {
	Name  string `json:"name"`
	IP    string `json:"ip"`
	Model string `json:"model"`
	State string `json:"state"`
}

func (s *server) handleMonitors(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	exit, stdout, stderr, timedOut := exec_(r.Context(), timeoutList, s.mirrorling, "monitors", "--raw")
	if timedOut {
		writeJSON(w, http.StatusOK, map[string]any{"monitors": []string{}, "error": "timed out"})
		return
	}
	if exit != 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"monitors": []string{},
			"error":    firstNonEmpty(strings.TrimSpace(stderr), strings.TrimSpace(stdout)),
		})
		return
	}
	mons := []string{}
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			mons = append(mons, line)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"monitors": mons})
}

// handleSetTV persists the chosen TV via `mirrorling tv <name>`.
func (s *server) handleSetTV(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	name, ok := sanitize(body.Name, 128)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "output": "invalid tv name"})
		return
	}
	ok, out := runCombined(r.Context(), timeoutSetter, s.mirrorling, "tv", name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

// handleForgetTV drops a saved TV from the remembered list via
// `mirrorling tv --forget <name>`; the casting config itself stays untouched.
func (s *server) handleForgetTV(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	name, ok := sanitize(body.Name, 128)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "output": "invalid tv name"})
		return
	}
	ok, out := runCombined(r.Context(), timeoutSetter, s.mirrorling, "tv", "--forget", name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

// handleSetMonitor persists the capture monitor via `mirrorling monitor <name|auto>`.
func (s *server) handleSetMonitor(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	name, ok := sanitize(body.Name, 128)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "output": "invalid monitor name"})
		return
	}
	ok, out := runCombined(r.Context(), timeoutSetter, s.mirrorling, "monitor", name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

func (s *server) handleOn(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	ok, out := runCombined(r.Context(), timeoutOn, s.mirrorling, "on")
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

func (s *server) handleOff(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	ok, out := runCombined(r.Context(), timeoutOff, s.mirrorling, "off")
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

func (s *server) handleStart(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	ok, out := runCombined(r.Context(), timeoutStartStop, s.mirrorling, "start")
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

func (s *server) handleStop(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	ok, out := runCombined(r.Context(), timeoutStartStop, s.mirrorling, "stop")
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

// handlePin submits pairing credentials to a waiting stream.
func (s *server) handlePin(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Pin string `json:"pin"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	pin, ok := sanitize(body.Pin, 16)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "output": "invalid PIN"})
		return
	}
	ok, out := runCombined(r.Context(), timeoutPin, s.ctl, "pin", pin)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

func (s *server) handleMute(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	ok, out := runCombined(r.Context(), timeoutMute, s.ctl, "mute")
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

func (s *server) handleUnmute(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodPost) {
		return
	}
	ok, out := runCombined(r.Context(), timeoutMute, s.ctl, "unmute")
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "output": out})
}

// handleLogs tails the daemon unit's journal in flat "cat" format.
func (s *server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	lines := 200
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			lines = n
		}
	}
	if lines < 10 {
		lines = 10
	}
	if lines > 1000 {
		lines = 1000
	}
	ok, out := runCombined(r.Context(), timeoutLogs,
		"journalctl", "--user", "-u", "mirrorling", "-n", strconv.Itoa(lines), "--no-pager", "-o", "cat")
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []string{}, "error": firstNonEmpty(out, "journalctl failed")})
		return
	}
	logLines := []string{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		logLines = append(logLines, line)
	}
	// TrimRight above can leave one empty trailing element when the journal is
	// empty; an empty tail is harmless in a <pre>, but drop a lone empty line.
	if len(logLines) == 1 && logLines[0] == "" {
		logLines = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": logLines})
}

func col(fields []string, i int) string {
	if i < len(fields) {
		return fields[i]
	}
	return ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
