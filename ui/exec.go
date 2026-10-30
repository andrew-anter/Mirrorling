package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os/exec"
	"strings"
	"time"
)

const headerTimeout = 5 * time.Second

// exec runs name with args under a per-call timeout derived from parent ctx.
//
// Stdin is left nil on purpose: os/exec then attaches the null device, so the
// child never sees a TTY and samcast's interactive prompts (which are
// TTY-guarded) can never fire. Every call is logged to stderr with its
// duration for debugging.
func exec_(parent context.Context, timeout time.Duration, name string, args ...string) (exit int, stdout, stderr string, timedOut bool) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil // null device — never a TTY
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start)
	joined := strings.Join(args, " ")
	if joined != "" {
		joined = " " + joined
	}
	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("exec: %s%s: timed out after %s", name, joined, timeout)
		return -1, "", "", true
	}
	exit = 0
	switch {
	case runErr == nil:
	case errors.As(runErr, new(*exec.ExitError)):
		exit = runErr.(*exec.ExitError).ExitCode()
	default:
		log.Printf("exec: %s%s: %v (dur=%s)", name, joined, runErr, dur)
		return -1, out.String(), errb.String(), false
	}
	log.Printf("exec: %s%s -> exit=%d dur=%s", name, joined, exit, dur)
	return exit, out.String(), errb.String(), false
}

// runCombined runs a command and returns ok (exit 0) plus the trimmed merged
// stdout+stderr — the shape every action endpoint returns as {ok, output}.
// A timeout collapses to {ok:false, output:"timed out"}.
func runCombined(parent context.Context, timeout time.Duration, name string, args ...string) (bool, string) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil // null device — never a TTY
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start)
	joined := strings.Join(args, " ")
	if joined != "" {
		joined = " " + joined
	}
	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("exec: %s%s: timed out after %s", name, joined, timeout)
		return false, "timed out"
	}
	ok := runErr == nil
	code := 0
	if runErr != nil {
		code = -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			code = ee.ExitCode()
		}
	}
	log.Printf("exec: %s%s -> exit=%d dur=%s", name, joined, code, dur)
	return ok, strings.TrimSpace(buf.String())
}

// sanitize validates a client-supplied string parameter: non-empty, at most
// maxLen bytes, no control characters (which covers newlines and would-be
// config injectors; the rest of the character policy is samcast's own to
// enforce, and its die message is surfaced to the user).
func sanitize(s string, maxLen int) (string, bool) {
	if s == "" || len(s) > maxLen {
		return "", false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return s, true
}
