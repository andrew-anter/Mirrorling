package main

import (
	"bytes"
	"testing"

	"github.com/godbus/dbus/v5"
)

// TestARGBByteOrder pins the wire format: SNI ARGB32 packs 0xAARRGGBB as
// bytes A,R,G,B. Output pixel (12,21) at size 24 lands fully inside the stand
// base (x [0.30,0.70], y [0.85,0.93]), so it must be opaque grey.
func TestARGBByteOrder(t *testing.T) {
	icon := drawIcon(24, colorGrey)
	i := (21*24 + 12) * 4
	got := icon.Data[i : i+4]
	want := []byte{0xff, colorGrey.r, colorGrey.g, colorGrey.b}
	if !bytes.Equal(got, want) {
		t.Fatalf("pixel (12,21) packed as % x, want % x (A,R,G,B order)", got, want)
	}
}

// TestDownsampleProducesInk checks the glyph actually rendered: every size
// must have real ink (non-empty alpha) without covering the whole canvas.
func TestDownsampleProducesInk(t *testing.T) {
	for _, size := range iconSizes {
		for _, c := range []rgb{colorGreen, colorAmber, colorGrey, colorDark} {
			icon := drawIcon(size, c)
			opaque := 0
			for i := 0; i < len(icon.Data); i += 4 {
				if icon.Data[i] == 0xff {
					opaque++
				}
			}
			if opaque == 0 || opaque == size*size {
				t.Fatalf("size %d color %v: %d/%d opaque pixels — glyph not drawn correctly", size, c, opaque, size*size)
			}
		}
	}
}

// TestWireSignatures guards the struct field order that becomes the D-Bus
// signatures SNI and dbusmenu clients parse strictly.
func TestWireSignatures(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{
		{pixmap{}, "(iiay)"},
		{toolTip{}, "(sa(iiay)ss)"},
		{propPair{}, "(ia{sv})"},
		{removedProps{}, "(ias)"},
		{layoutNode{}, "(ia{sv}av)"},
	} {
		if got := dbus.SignatureOf(tc.v).String(); got != tc.want {
			t.Fatalf("signature of %T = %s, want %s", tc.v, got, tc.want)
		}
	}
}
