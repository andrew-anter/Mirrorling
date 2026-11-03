// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Mirrorling contributors.

package main

// The freedesktop StatusNotifierItem surface: exported properties and
// methods, watcher registration with retry, and the state-change signals.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	sniIface      = "org.kde.StatusNotifierItem"
	sniBusPrefix  = "org.kde.StatusNotifierItem"
	watcherBus    = "org.kde.StatusNotifierWatcher"
	propsIface    = "org.freedesktop.DBus.Properties"
	itemPath      = dbus.ObjectPath("/StatusNotifierItem")
	menuPath      = dbus.ObjectPath("/MenuBar")
	registerRetry = 3 * time.Second
	statusTimeout = 5 * time.Second // same bound as ui/'s ctl status
)

// statusReply is the part of `doubletake-ctl status` the tray renders.
// StatePINRequired covers both an on-screen PIN and a configured receiver
// password; CredentialKind/NeedsCredential tell which prompt is actually
// pending (older daemons send neither, so PIN remains the default).
type statusReply struct {
	State           string `json:"state"`
	Device          string `json:"device"`
	NeedsCredential bool   `json:"needs_credential"`
	CredentialKind  string `json:"credential_kind"`
}

// credentialPassword is the CredentialKind a receiver that requires a
// configured password reports; every other kind (pin, empty, unknown) keeps
// the PIN wording.
const credentialPassword = "password"

// passwordPending reports whether the waiting credential is a configured
// password rather than the on-screen PIN. NeedsCredential gates the kind so a
// status reply with no credential metadata keeps the legacy PIN prompt.
func (st statusReply) passwordPending() bool {
	return st.NeedsCredential && st.CredentialKind == credentialPassword
}

// trayState is the rendered mirroring state; equality (==) drives the
// change detection that gates signal emission.
type trayState struct {
	desc    string // tooltip subtitle
	color   rgb
	startOn bool // menu item 3 enabled
	stopOn  bool // menu item 4 enabled
}

// stateUnreachable renders a dead/undecipherable daemon. Start stays enabled:
// `mirrorling on` is the cold-start command (it boots the daemon itself), so
// this is exactly the state where Start must work.
func stateUnreachable() trayState {
	return trayState{desc: "Daemon not running", color: colorDark, startOn: true}
}

// renderState maps a status reply to icon + tooltip + menu enablement. An
// unknown state string is treated as idle-ish: newer daemons may grow states
// the tray doesn't know and idle controls are the safest default.
func renderState(st statusReply) trayState {
	device := st.Device
	if device == "" {
		device = "TV"
	}
	switch st.State {
	case "discovering":
		return trayState{desc: "Discovering TVs…", color: colorAmber}
	case "connecting":
		return trayState{desc: "Connecting to " + device + "…", color: colorAmber, stopOn: true}
	case "pin_required":
		if st.passwordPending() {
			return trayState{desc: "Waiting for password on " + device, color: colorAmber, stopOn: true}
		}
		return trayState{desc: "Waiting for PIN on " + device, color: colorAmber, stopOn: true}
	case "streaming":
		return trayState{desc: "Casting to " + device, color: colorGreen, stopOn: true}
	default: // idle and unknown
		return trayState{desc: "Idle", color: colorGrey, startOn: true}
	}
}

// stateStore holds the current rendered state, shared by the poll loop
// (writer) and the property providers (readers).
type stateStore struct {
	mu sync.Mutex
	st trayState
}

func (s *stateStore) get() trayState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// set stores st and returns the previous one for change detection.
func (s *stateStore) set(st trayState) trayState {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.st
	s.st = st
	return old
}

// sniItem implements org.kde.StatusNotifierItem. Hosts call Activate on left
// click and ContextMenu/SecondaryActivate on right click; all route to the
// same "open control panel" action as menu item 1. Scroll is ignored.
type sniItem struct {
	open func()
}

func (it *sniItem) Activate(x, y int32) *dbus.Error          { it.open(); return nil }
func (it *sniItem) SecondaryActivate(x, y int32) *dbus.Error { it.open(); return nil }
func (it *sniItem) ContextMenu(x, y int32) *dbus.Error       { it.open(); return nil }
func (it *sniItem) Scroll(delta int32, orientation string) *dbus.Error {
	return nil
}

// propsStore implements org.freedesktop.DBus.Properties (godbus does not
// synthesize it) via per-interface providers. Values are recomputed on every
// read so GetAll always serves the live icon/menu state. Set is accepted and
// ignored — nothing in the SNI contract is host-settable.
type propsStore struct {
	providers map[string]func() map[string]dbus.Variant
}

func (p *propsStore) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	props, err := p.getAll(iface)
	if err != nil {
		return dbus.Variant{}, err
	}
	if v, ok := props[name]; ok {
		return v, nil
	}
	return dbus.Variant{}, noSuchProperty(iface, name)
}

func (p *propsStore) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return p.getAll(iface)
}

func (p *propsStore) Set(iface, name string, val dbus.Variant) *dbus.Error {
	return nil // ignored
}

func (p *propsStore) getAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	prov, ok := p.providers[iface]
	if !ok {
		return nil, noSuchProperty(iface, "")
	}
	return prov(), nil
}

func noSuchProperty(iface, name string) *dbus.Error {
	return &dbus.Error{
		Name: "org.freedesktop.DBus.Error.UnknownProperty",
		Body: []any{iface + ": no such property " + name},
	}
}

// trayServer owns the bus connection and drives the SNI lifecycle: exports,
// watcher registration with retry, and state-change signals.
type trayServer struct {
	conn    *dbus.Conn
	busName string
	id      string

	state *stateStore
	icons *iconCache
	menu  *dbusMenu
	acts  *actions

	regMu      sync.Mutex
	registered bool
	loggedOK   bool // first successful registration logged once
	loggedFail bool // first failure of a failure streak logged once
}

// export registers the item, menu and properties interfaces. libdbusmenu and
// SNI hosts address the fixed interfaces directly and never introspect, which
// matters because godbus does not merge its Introspectable stub onto exported
// paths (Introspect there answers UnknownInterface).
func (t *trayServer) export() error {
	props := &propsStore{providers: map[string]func() map[string]dbus.Variant{
		sniIface:  t.sniProps,
		menuIface: t.menu.props,
	}}
	if err := t.conn.Export(&sniItem{open: t.acts.open}, itemPath, sniIface); err != nil {
		return err
	}
	if err := t.conn.Export(t.menu, menuPath, menuIface); err != nil {
		return err
	}
	// One props store serves both paths; the interfaces differ per path, which
	// is what providers are keyed on.
	if err := t.conn.Export(props, itemPath, propsIface); err != nil {
		return err
	}
	return t.conn.Export(props, menuPath, propsIface)
}

// sniProps builds the current org.kde.StatusNotifierItem property set. Status
// is pinned to "Active" in every state: hosts may hide Passive (and
// NeedsAttention) items, and the tray must stay visible — the pixmaps and
// tooltip carry the state instead. IconName stays empty so the icon is our
// pixmap, never a theme icon.
func (t *trayServer) sniProps() map[string]dbus.Variant {
	st := t.state.get()
	pix := t.icons.forColor(st.color)
	return map[string]dbus.Variant{
		"Category":            dbus.MakeVariant("ApplicationStatus"),
		"Id":                  dbus.MakeVariant(t.id),
		"Title":               dbus.MakeVariant("Mirrorling"),
		"Status":              dbus.MakeVariant("Active"),
		"WindowId":            dbus.MakeVariant(int32(0)),
		"IconName":            dbus.MakeVariant(""),
		"OverlayIconName":     dbus.MakeVariant(""),
		"AttentionIconName":   dbus.MakeVariant(""),
		"AttentionMovieName":  dbus.MakeVariant(""),
		"IconPixmap":          dbus.MakeVariant(pix),
		"OverlayIconPixmap":   dbus.MakeVariant([]pixmap{}),
		"AttentionIconPixmap": dbus.MakeVariant([]pixmap{}),
		"ToolTip":             dbus.MakeVariant(toolTip{Title: "Mirrorling", Desc: st.desc, Pixmaps: pix}),
		"Menu":                dbus.MakeVariant(menuPath),
		"ItemIsMenu":          dbus.MakeVariant(false),
	}
}

// applyState swaps in the freshly rendered state and emits only what changed:
// NewIcon/NewToolTip for the icon, ItemsPropertiesUpdated for menu
// enablement. Nothing is emitted when the state repeats.
func (t *trayServer) applyState(st trayState) {
	old := t.state.set(st)
	if old == st {
		return
	}
	if old.desc != st.desc {
		log.Printf("state: %s", st.desc)
	}
	t.emit(itemPath, sniIface+".NewIcon")
	t.emit(itemPath, sniIface+".NewToolTip")

	var changed []propPair
	for _, id := range []int32{itemStart, itemStop} {
		if t.menu.enabledOf(old, id) != t.menu.enabledOf(st, id) {
			changed = append(changed, propPair{ID: id, Props: map[string]dbus.Variant{
				"enabled": dbus.MakeVariant(t.menu.enabledOf(st, id)),
			}})
		}
	}
	if len(changed) > 0 {
		t.menu.propChange()
		t.emit(menuPath, menuIface+".ItemsPropertiesUpdated", changed, []removedProps{})
	}
}

// emit sends one signal. A failure flips back to unregistered so the retry
// loop re-registers with the watcher; a closed session bus means logout, and
// exit 0 keeps Restart=on-failure from fighting the session teardown.
func (t *trayServer) emit(path dbus.ObjectPath, sig string, args ...any) bool {
	err := t.conn.Emit(path, sig, args...)
	switch {
	case err == nil:
		return true
	case errors.Is(err, dbus.ErrClosed):
		log.Printf("session bus closed, exiting")
		os.Exit(0)
	}
	log.Printf("emit %s failed: %v — re-registering", sig, err)
	t.markUnregistered()
	return false
}

// runRegistration registers with the watcher and keeps it that way: it
// retries every 3s while unregistered and re-registers whenever the watcher's
// owner changes (e.g. waybar restart). The RegisterStatusNotifierItem
// argument is the OBJECT PATH, not our bus name: modern hosts take the bus
// name from the message sender and only need the path — that is the
// interop shape waybar works with.
func (t *trayServer) runRegistration(ch <-chan *dbus.Signal) {
	t.tryRegister()
	tick := time.NewTicker(registerRetry)
	defer tick.Stop()
	for {
		select {
		case sig, ok := <-ch:
			if !ok {
				// godbus closes registered signal channels when the connection
				// dies — treat as logout and exit cleanly.
				log.Printf("session bus closed, exiting")
				os.Exit(0)
			}
			if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
				t.markUnregistered() // watcher gone or replaced: re-register
			}
		case <-tick.C:
			if !t.isRegistered() {
				t.tryRegister()
			}
		}
	}
}

func (t *trayServer) tryRegister() {
	err := t.conn.Object(watcherBus, dbus.ObjectPath("/StatusNotifierWatcher")).
		Call(watcherBus+".RegisterStatusNotifierItem", 0, string(itemPath)).Err
	t.regMu.Lock()
	defer t.regMu.Unlock()
	if err == nil {
		t.registered = true
		t.loggedFail = false
		if !t.loggedOK {
			t.loggedOK = true
			log.Printf("registered with %s as %s", watcherBus, t.busName)
		}
		return
	}
	if errors.Is(err, dbus.ErrClosed) {
		log.Printf("session bus closed, exiting")
		os.Exit(0)
	}
	t.registered = false
	if !t.loggedFail {
		t.loggedFail = true
		log.Printf("not registered with %s yet (%v) — retrying every %s", watcherBus, err, registerRetry)
	}
}

func (t *trayServer) markUnregistered() {
	t.regMu.Lock()
	t.registered = false
	t.regMu.Unlock()
}

func (t *trayServer) isRegistered() bool {
	t.regMu.Lock()
	defer t.regMu.Unlock()
	return t.registered
}

// pollState runs one `doubletake-ctl status` and renders it; any failure
// (missing ctl, timeout, non-JSON) renders as "daemon not running".
func pollState(ctl string) trayState {
	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ctl, "status")
	cmd.Stdin = nil // null device — never a TTY
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("poll: %s status timed out", ctl)
		}
		return stateUnreachable()
	}
	var st statusReply
	if err := json.Unmarshal(out, &st); err != nil {
		return stateUnreachable()
	}
	return renderState(st)
}
