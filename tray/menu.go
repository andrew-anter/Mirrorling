package main

// The com.canonical.dbusmenu surface waybar's libdbusmenu client talks to:
// a fixed item tree plus enablement updates, so the menu tracks daemon state.

import (
	"log"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
)

const menuIface = "com.canonical.dbusmenu"

// Menu item ids, fixed so ItemsPropertiesUpdated can address them.
const (
	itemOpen  int32 = 1
	itemStart int32 = 3
	itemStop  int32 = 4
	itemQuit  int32 = 6
)

// layoutNode mirrors the GetLayout tuple (ia{sv}av); children wrap recursive
// layoutNodes in variants.
type layoutNode struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

// propPair / removedProps mirror the (ia{sv}) / (ias) tuples carried by
// ItemsPropertiesUpdated.
type propPair struct {
	ID    int32
	Props map[string]dbus.Variant
}

type removedProps struct {
	ID    int32
	Names []string
}

// dbusMenu implements com.canonical.dbusmenu over a static tree:
// 1 open panel, 2 separator, 3 start, 4 stop, 5 separator, 6 quit.
// Only enablement ever changes; ids and labels do not.
type dbusMenu struct {
	state *stateStore
	acts  *actions

	mu    sync.Mutex
	rev   uint32
	dirty bool
}

// menuIds in display order.
var menuIds = []int32{itemOpen, 2, itemStart, itemStop, 5, itemQuit}

// GetLayout serves (revision, layout). Depth is ignored: the tree is flat, so
// serving the whole subtree satisfies any recursion request. An empty
// property-name filter means "all".
func (m *dbusMenu) GetLayout(parent int32, depth int32, props []string) (uint32, layoutNode, *dbus.Error) {
	m.mu.Lock()
	m.dirty = false // the client is looking at the current state now
	rev := m.rev
	m.mu.Unlock()
	return rev, m.layout(parent, props), nil
}

func (m *dbusMenu) layout(parent int32, filter []string) layoutNode {
	node := layoutNode{ID: parent, Props: filterProps(m.propsFor(parent), filter)}
	if parent != 0 {
		return node // leaves have no children
	}
	for _, id := range menuIds {
		node.Children = append(node.Children, dbus.MakeVariant(m.layout(id, filter)))
	}
	return node
}

// GetGroupProperties returns (id, props) for the requested ids, or all ids
// when the list is empty.
func (m *dbusMenu) GetGroupProperties(ids []int32, props []string) ([]propPair, *dbus.Error) {
	if len(ids) == 0 {
		ids = menuIds
	}
	out := make([]propPair, 0, len(ids))
	for _, id := range ids {
		out = append(out, propPair{ID: id, Props: filterProps(m.propsFor(id), props)})
	}
	return out, nil
}

// GetItemProperties is the single-item variant of GetGroupProperties.
func (m *dbusMenu) GetItemProperties(id int32, props []string) (map[string]dbus.Variant, *dbus.Error) {
	return filterProps(m.propsFor(id), props), nil
}

// AboutToShow reports whether anything changed since the client last fetched;
// true makes libdbusmenu re-fetch the layout on menu open.
func (m *dbusMenu) AboutToShow(id int32) (bool, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dirty := m.dirty
	m.dirty = false
	return dirty, nil
}

// Event handles "clicked" on the action items; open/close and other events
// are ignored. The data variant is protocol noise — its contents don't matter.
func (m *dbusMenu) Event(id int32, eventId string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if eventId != "clicked" {
		return nil
	}
	switch id {
	case itemOpen:
		m.acts.open()
	case itemStart:
		m.acts.start()
	case itemStop:
		m.acts.stop()
	case itemQuit:
		log.Printf("quit requested from tray menu")
		os.Exit(0)
	}
	return nil
}

// props serves the dbusmenu object's own properties (via Properties.Get);
// Version 3 is the protocol level libdbusmenu negotiates.
func (m *dbusMenu) props() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"Version": dbus.MakeVariant(uint32(3)),
		"Status":  dbus.MakeVariant("normal"),
	}
}

// propsFor returns the full property set of one item.
func (m *dbusMenu) propsFor(id int32) map[string]dbus.Variant {
	if id == 0 {
		// Root: children-display=submenu tells libdbusmenu the tree has
		// children to fetch.
		return map[string]dbus.Variant{
			"children-display": dbus.MakeVariant("submenu"),
		}
	}
	if id == 2 || id == 5 {
		return map[string]dbus.Variant{
			"type":    dbus.MakeVariant("separator"),
			"visible": dbus.MakeVariant(true),
		}
	}
	st := m.state.get()
	p := map[string]dbus.Variant{
		"type":    dbus.MakeVariant("standard"),
		"visible": dbus.MakeVariant(true),
		"enabled": dbus.MakeVariant(m.enabledOf(st, id)),
	}
	switch id {
	case itemOpen:
		p["label"] = dbus.MakeVariant("Open control panel")
	case itemStart:
		p["label"] = dbus.MakeVariant("Start mirroring")
	case itemStop:
		p["label"] = dbus.MakeVariant("Stop mirroring")
	case itemQuit:
		p["label"] = dbus.MakeVariant("Quit")
	}
	return p
}

// enabledOf resolves item enablement. A missing samcast binary disables every
// samcast-spawning item (Quit must always work); the rest is daemon state.
func (m *dbusMenu) enabledOf(st trayState, id int32) bool {
	switch id {
	case itemOpen, itemStart, itemStop:
		if !m.acts.samcastOK {
			return false
		}
	}
	switch id {
	case itemStart:
		return st.startOn
	case itemStop:
		return st.stopOn
	}
	return true
}

// propChange records a property-only update: bumps the revision and arms the
// AboutToShow dirty flag so open menus re-fetch.
func (m *dbusMenu) propChange() {
	m.mu.Lock()
	m.rev++
	m.dirty = true
	m.mu.Unlock()
}

// filterProps applies a property-name filter; an empty filter means all.
func filterProps(all map[string]dbus.Variant, filter []string) map[string]dbus.Variant {
	if len(filter) == 0 {
		return all
	}
	out := make(map[string]dbus.Variant, len(filter))
	for _, k := range filter {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	return out
}
