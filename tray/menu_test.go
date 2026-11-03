package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

// clickEvent builds the a(isvu) entry libdbusmenu sends for a menu action.
func clickEvent(id int32, eventID string) menuEvent {
	return menuEvent{ID: id, EventID: eventID, Data: dbus.MakeVariant(""), Timestamp: 0}
}

// stubActions makes actions.run exec /bin/true, so EventGroup dispatch is
// exercised for real without spawning mirrorling or touching the user's setup.
func stubActions(t *testing.T) *actions {
	t.Helper()
	if _, err := os.Stat("/bin/true"); err != nil {
		t.Skipf("/bin/true unavailable: %v", err)
	}
	return &actions{mirrorling: "/bin/true", mirrorlingOK: true}
}

// AboutToShowGroup must keep the libdbusmenu (aiai) wire signature: two int32
// arrays plus the DBus error. The old single-array return made libdbusmenu
// clients warn and fail to open the menu.
func TestAboutToShowGroupSignature(t *testing.T) {
	m, ok := reflect.TypeOf(&dbusMenu{}).MethodByName("AboutToShowGroup")
	if !ok {
		t.Fatal("AboutToShowGroup is not exported")
	}
	ft := m.Type
	if got, want := ft.NumIn(), 2; got != want { // receiver + ids
		t.Fatalf("AboutToShowGroup has %d inputs, want %d", got, want)
	}
	if got, want := ft.NumOut(), 3; got != want {
		t.Fatalf("AboutToShowGroup has %d outputs, want %d (aiai + error)", got, want)
	}
	ai := reflect.TypeOf([]int32{})
	if ft.Out(0) != ai || ft.Out(1) != ai {
		t.Fatalf("AboutToShowGroup outputs = (%v, %v, %v), want ([]int32, []int32, *dbus.Error)",
			ft.Out(0), ft.Out(1), ft.Out(2))
	}
	if ft.Out(2) != reflect.TypeOf((*dbus.Error)(nil)) {
		t.Fatalf("AboutToShowGroup last output = %v, want *dbus.Error", ft.Out(2))
	}
}

// AboutToShowGroup reports dirty requested ids in the first array and ids
// that are not ours in the second; a clean menu returns two empty arrays.
func TestAboutToShowGroupDirtyUnknownSplit(t *testing.T) {
	m := &dbusMenu{acts: &actions{}}
	for range 2 { // a clean menu stays clean across calls
		updates, idErrors, derr := m.AboutToShowGroup([]int32{itemOpen, 99})
		if derr != nil {
			t.Fatalf("AboutToShowGroup: %v", derr)
		}
		if len(updates) != 0 || len(idErrors) != 0 {
			t.Fatalf("clean reply = (%v, %v), want two empty arrays", updates, idErrors)
		}
	}

	m.propChange()
	updates, idErrors, derr := m.AboutToShowGroup([]int32{itemOpen, 99, itemStop, 0})
	if derr != nil {
		t.Fatalf("AboutToShowGroup: %v", derr)
	}
	if want := []int32{itemOpen, itemStop, 0}; !reflect.DeepEqual(updates, want) {
		t.Fatalf("updates = %v, want %v (dirty requested ids)", updates, want)
	}
	if want := []int32{99}; !reflect.DeepEqual(idErrors, want) {
		t.Fatalf("idErrors = %v, want %v (unknown requested ids)", idErrors, want)
	}

	// The dirty flag was consumed: the next open must not re-fetch again.
	updates, idErrors, _ = m.AboutToShowGroup([]int32{itemOpen, 99})
	if len(updates) != 0 || len(idErrors) != 0 {
		t.Fatalf("after fetch reply = (%v, %v), want two empty arrays", updates, idErrors)
	}
}

// EventGroup dispatches clicks on known ids and reports unknown clicked ids;
// events it ignores by design (hover, closed) are never failures.
func TestEventGroupDispatch(t *testing.T) {
	m := &dbusMenu{state: &stateStore{}, acts: stubActions(t)}
	failed, derr := m.EventGroup([]menuEvent{
		clickEvent(itemOpen, "clicked"),
		clickEvent(itemStart, "clicked"),
		clickEvent(itemStop, "clicked"),
		clickEvent(99, "clicked"),
		clickEvent(98, "hovered"),
	})
	if derr != nil {
		t.Fatalf("EventGroup: %v", derr)
	}
	if want := []int32{99}; !reflect.DeepEqual(failed, want) {
		t.Fatalf("EventGroup failed ids = %v, want %v", failed, want)
	}

	// No failures at all for an all-known batch.
	failed, _ = m.EventGroup([]menuEvent{clickEvent(itemOpen, "clicked")})
	if len(failed) != 0 {
		t.Fatalf("EventGroup known batch failed ids = %v, want none", failed)
	}
}

// GetLayout must serve the (ia{sv}av) tree libdbusmenu renders: root with a
// submenu marker and one child per fixed id, leaves without children, and the
// property filter applied.
func TestGetLayoutShape(t *testing.T) {
	m := &dbusMenu{state: &stateStore{}, acts: &actions{mirrorlingOK: true}}
	rev, root, derr := m.GetLayout(0, -1, nil)
	if derr != nil {
		t.Fatalf("GetLayout: %v", derr)
	}
	if rev != 0 {
		t.Fatalf("initial revision = %d, want 0", rev)
	}
	if root.ID != 0 {
		t.Fatalf("root id = %d, want 0", root.ID)
	}
	if got := root.Props["children-display"]; got.Value() != "submenu" {
		t.Fatalf("root children-display = %v, want submenu", got.Value())
	}
	if len(root.Children) != len(menuIds) {
		t.Fatalf("root has %d children, want %d", len(root.Children), len(menuIds))
	}
	for i, v := range root.Children {
		node, ok := v.Value().(layoutNode)
		if !ok {
			t.Fatalf("child %d is %T, want layoutNode", i, v.Value())
		}
		if node.ID != menuIds[i] {
			t.Fatalf("child %d id = %d, want %d", i, node.ID, menuIds[i])
		}
		if len(node.Children) != 0 {
			t.Fatalf("child %d (%d) has %d children, want leaf", i, node.ID, len(node.Children))
		}
	}

	// A non-root parent is a leaf carrying only its own properties.
	_, leaf, _ := m.GetLayout(itemStart, 0, []string{"label"})
	if leaf.ID != itemStart || len(leaf.Children) != 0 {
		t.Fatalf("leaf = %+v, want item %d with no children", leaf, itemStart)
	}
	if len(leaf.Props) != 1 || leaf.Props["label"].Value() != "Start mirroring" {
		t.Fatalf("filtered leaf props = %v, want only the start label", leaf.Props)
	}
}
