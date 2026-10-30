package main

import "testing"

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
		// daemon down is exactly the cold-start case: `samcast on` boots it
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

// enabledOf gates the samcast actions on the binary existing: a missing
// samcast must disable every samcast-spawning item in every state, Quit aside.
func TestEnabledOfSamcastMissing(t *testing.T) {
	m := &dbusMenu{acts: &actions{samcastOK: false}}
	for _, st := range []trayState{
		stateUnreachable(),
		renderState(statusReply{State: "idle"}),
		renderState(statusReply{State: "streaming"}),
	} {
		if m.enabledOf(st, itemStart) || m.enabledOf(st, itemStop) || m.enabledOf(st, itemOpen) {
			t.Fatalf("enabledOf with samcast missing: start/stop/open must be disabled in %+v", st)
		}
		if !m.enabledOf(st, itemQuit) {
			t.Fatalf("enabledOf with samcast missing: quit must stay enabled in %+v", st)
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
