package main

// Tray icons are drawn in code — no image assets — so the helper stays one
// static binary and the icon color always matches the live daemon state.

import (
	"math"
	"sync"
)

// pixmap is one SNI icon entry. Data is packed ARGB32: 4 bytes per pixel in
// A,R,G,B order (a network-order uint32 0xAARRGGBB) — the byte order waybar
// and every StatusNotifier host reads via a(iiay).
type pixmap struct {
	W, H int32
	Data []byte
}

// toolTip mirrors the SNI ToolTip tuple (sa(iiay)ss).
type toolTip struct {
	IconName string
	Pixmaps  []pixmap
	Title    string
	Desc     string
}

// rgb is a 24-bit icon color. SNI pixmaps use straight (non-premultiplied)
// alpha, so the color bytes never depend on coverage.
type rgb struct{ r, g, b uint8 }

var (
	colorGreen = rgb{0x46, 0xc4, 0x6e} // streaming
	colorAmber = rgb{0xe0, 0xa6, 0x3d} // connecting / discovering / PIN
	colorGrey  = rgb{0x8a, 0x94, 0xa0} // idle
	colorDark  = rgb{0x4a, 0x54, 0x5e} // daemon not running (faded)
)

// Sizes advertised in IconPixmap; the host picks one for its tray height.
var iconSizes = []int{24, 32, 48, 64}

// Draw at 4x and box-average down: cheap antialiasing with no image library.
const supersample = 4

// iconCache holds one immutable pixmap set per state color so every GetAll
// can serve the same slices. The mutex is load-bearing: godbus dispatches
// each incoming method call on its own goroutine, and waybar's reaction to a
// NewIcon/NewToolTip pair can race two property reads against each other.
type iconCache struct {
	mu   sync.Mutex
	sets map[rgb][]pixmap
}

func (ic *iconCache) forColor(c rgb) []pixmap {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	if set, ok := ic.sets[c]; ok {
		return set
	}
	if ic.sets == nil {
		ic.sets = make(map[rgb][]pixmap, 4)
	}
	set := make([]pixmap, 0, len(iconSizes))
	for _, size := range iconSizes {
		set = append(set, drawIcon(size, c))
	}
	ic.sets[c] = set
	return set
}

// drawIcon renders the monitor glyph (rounded-rect screen outline plus stem
// and base stand) stroked in c at the given pixel size.
func drawIcon(size int, c rgb) pixmap {
	hi := size * supersample
	cover := make([]int, hi*hi)
	for y := 0; y < hi; y++ {
		for x := 0; x < hi; x++ {
			px := (float64(x) + 0.5) / float64(hi)
			py := (float64(y) + 0.5) / float64(hi)
			if glyphCover(px, py) {
				cover[y*hi+x] = 1
			}
		}
	}
	data := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sum := 0
			for dy := 0; dy < supersample; dy++ {
				row := (y*supersample + dy) * hi
				for dx := 0; dx < supersample; dx++ {
					sum += cover[row+x*supersample+dx]
				}
			}
			i := (y*size + x) * 4
			data[i] = byte(sum * 255 / (supersample * supersample)) // alpha = coverage
			data[i+1] = c.r
			data[i+2] = c.g
			data[i+3] = c.b
		}
	}
	return pixmap{W: int32(size), H: int32(size), Data: data}
}

// glyphCover reports whether a unit-square point is inside the glyph: a
// stroked rounded-rect screen over a stem and base stand, coordinates chosen
// to leave even margins at 24px.
func glyphCover(x, y float64) bool {
	if inStroke(x, y, 0.08, 0.10, 0.92, 0.70, 0.10, 0.085) {
		return true
	}
	if inRect(x, y, 0.455, 0.70, 0.545, 0.85) { // stand stem
		return true
	}
	return inRect(x, y, 0.30, 0.85, 0.70, 0.93) // stand base
}

func inRect(x, y, x0, y0, x1, y1 float64) bool {
	return x >= x0 && x < x1 && y >= y0 && y < y1
}

// inStroke reports whether the point sits within w of the rounded-rect
// boundary (a signed-distance test).
func inStroke(x, y, x0, y0, x1, y1, r, w float64) bool {
	return math.Abs(roundedRectDist(x, y, x0, y0, x1, y1, r)) <= w/2
}

// roundedRectDist is the signed distance to a rounded rect (negative inside).
func roundedRectDist(x, y, x0, y0, x1, y1, r float64) float64 {
	cx, cy := (x0+x1)/2, (y0+y1)/2
	hx, hy := (x1-x0)/2-r, (y1-y0)/2-r
	dx, dy := math.Abs(x-cx)-hx, math.Abs(y-cy)-hy
	ox, oy := math.Max(dx, 0), math.Max(dy, 0)
	return math.Hypot(ox, oy) + math.Min(math.Max(dx, dy), 0) - r
}
