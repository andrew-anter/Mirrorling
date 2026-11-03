// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Mirrorling contributors.

package main

import (
	"testing"
	"time"
)

// The menu enablement matrix is the tray's one piece of stateful logic and
// the easiest thing to regress silently (an icon still draws when it is
// wrong) — pin it here.

func TestRenderStateEnablement(t *testing.T) {
	tests := []struct {
		name            string
		st              statusReply
		desc            string
		color           rgb
		startOn, stopOn bool
	}{
		{"idle", statusReply{State: "idle"}, "Idle", colorGrey, true, false},
		{"discovering", statusReply{State: "discovering"}, "Discovering TVs…", colorAmber, false, false},
		{"connecting", statusReply{State: "connecting", Device: "Sam58"}, "Connecting to Sam58…", colorAmber, false, true},
		{"pin_required", statusReply{State: "pin_required"}, "Waiting for PIN on TV", colorAmber, false, true},
		{"streaming", statusReply{State: "streaming", Device: "Sam58"}, "Casting to Sam58", colorGreen, false, true},
		// unknown states are idle-ish and must not strand the user without Start
		{"unknown", statusReply{State: "hibernating"}, "Idle", colorGrey, true, false},
		// daemon down is exactly the cold-start case: `mirrorling on` boots it
		{"unreachable", statusReply{}, "Daemon not running", colorDark, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got trayState
			if tt.name == "unreachable" {
				got = stateUnreachable()
			} else {
				got = renderState(tt.st)
			}
			if got.desc != tt.desc || got.color != tt.color || got.startOn != tt.startOn || got.stopOn != tt.stopOn {
				t.Fatalf("renderState(%+v) = %+v, want desc=%q startOn=%v stopOn=%v",
					tt.st, got, tt.desc, tt.startOn, tt.stopOn)
			}
		})
	}
}

// enabledOf gates the mirrorling actions on the binary existing: a missing
// mirrorling must disable every mirrorling-spawning item in every state, Quit aside.
func TestEnabledOfMirrorlingMissing(t *testing.T) {
	m := &dbusMenu{acts: &actions{mirrorlingOK: false}}
	for _, st := range []trayState{
		stateUnreachable(),
		renderState(statusReply{State: "idle"}),
		renderState(statusReply{State: "streaming"}),
	} {
		if m.enabledOf(st, itemStart) || m.enabledOf(st, itemStop) || m.enabledOf(st, itemOpen) {
			t.Fatalf("enabledOf with mirrorling missing: start/stop/open must be disabled in %+v", st)
		}
		if !m.enabledOf(st, itemQuit) {
			t.Fatalf("enabledOf with mirrorling missing: quit must stay enabled in %+v", st)
		}
	}
}

// Two different unknown daemon states render identically; the == change
// detection must treat them as equal so the tray does not re-emit signals
// for zero visible change.
func TestRenderStateUnknownStatesEqual(t *testing.T) {
	a := renderState(statusReply{State: "weird1"})
	b := renderState(statusReply{State: "weird2"})
	if a != b {
		t.Fatalf("distinct unknown states render unequal: %+v vs %+v", a, b)
	}
}

// A receiver waiting for a configured password must not be described as a PIN
// prompt; the older pin_required state and replies without credential
// metadata keep the PIN wording.
func TestRenderStateCredentialWording(t *testing.T) {
	tests := []struct {
		name string
		st   statusReply
		desc string
	}{
		{"pin", statusReply{State: "pin_required", Device: "Sam58", NeedsCredential: true, CredentialKind: "pin"}, "Waiting for PIN on Sam58"},
		{"password", statusReply{State: "pin_required", Device: "Sam58", NeedsCredential: true, CredentialKind: "password"}, "Waiting for password on Sam58"},
		{"legacy daemon without metadata", statusReply{State: "pin_required", Device: "Sam58"}, "Waiting for PIN on Sam58"},
		// A kind without NeedsCredential is not a complete reply; do not invent
		// a password prompt from half the contract.
		{"kind without needs_credential", statusReply{State: "pin_required", CredentialKind: "password"}, "Waiting for PIN on TV"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderState(tt.st)
			if got.desc != tt.desc {
				t.Fatalf("renderState(%+v).desc = %q, want %q", tt.st, got.desc, tt.desc)
			}
			if got.color != colorAmber || got.startOn || !got.stopOn {
				t.Fatalf("renderState(%+v) = %+v, want amber waiting state with only Stop enabled", tt.st, got)
			}
		})
	}
}

// A non-positive --interval panics time.NewTicker; the tray must reject it at
// startup instead of entering a crash loop.
func TestCheckInterval(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second, -time.Millisecond} {
		if err := checkInterval(d); err == nil {
			t.Fatalf("checkInterval(%v) = nil, want error", d)
		}
	}
	for _, d := range []time.Duration{time.Nanosecond, time.Second, time.Hour} {
		if err := checkInterval(d); err != nil {
			t.Fatalf("checkInterval(%v) = %v, want nil", d, err)
		}
	}
}
