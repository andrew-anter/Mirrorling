package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const headerTimeout = 5 * time.Second

// newCommand builds a command whose whole process group dies on timeout:
// mirrorling is bash and spawns children of its own (doubletake-ctl, systemctl,
// avahi-browse), so the CommandContext default of killing only the direct
// child would orphan those grandchildren — a timed-out "mirrorling on" would
// leave a live doubletake-ctl connect behind while the caller already got
// {ok:false, output:"timed out"}. WaitDelay bounds the wait when a killed
// child's surviving grandchildren still hold the output pipes.
func newCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// exec runs name with args under a per-call timeout derived from parent ctx.
//
// Stdin is left nil on purpose: os/exec then attaches the null device, so the
// child never sees a TTY and mirrorling's interactive prompts (which are
// TTY-guarded) can never fire. Every call is logged to stderr with its
// duration for debugging.
func exec_(parent context.Context, timeout time.Duration, name string, args ...string) (exit int, stdout, stderr string, timedOut bool) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := newCommand(ctx, name, args...)
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

	cmd := newCommand(ctx, name, args...)
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
// maxLen bytes, no control characters and no shell substitution
// metacharacters ($, `) — mirrorling's config is sourced shell, and the UI is
// the network-facing conduit: it must not pass through to mirrorling anything
// that becomes executable if a downstream denylist ever misses. The remaining
// character policy is mirrorling's own to enforce, and its die message is
// surfaced to the user.
func sanitize(s string, maxLen int) (string, bool) {
	if s == "" || len(s) > maxLen {
		return "", false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '$' || r == '`' {
			return "", false
		}
	}
	return s, true
}
