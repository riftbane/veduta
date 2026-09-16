package platform

import "github.com/riftbane/veduta/sim"

// Environment variables of the player.
const (
	renderScaleEnv = "VEDUTA_SCALE" // console: divides the panel; simulator: the window's scale
	panelEnv       = "VEDUTA_PANEL" // simulator: 0 shows the frame's own colors instead of the panel's
)

// What the simulator window shows and how it reads the keyboard, kept out of the Windows
// files so it is tested on every machine.

// fitScale returns the largest whole scale at which a fw×fh frame fits a cw×ch client area
// (at least 1), and the offset that centers the scaled frame.
func fitScale(cw, ch, fw, fh int) (scale, x, y int) {
	scale = 1
	if fw > 0 && fh > 0 {
		scale = max(min(cw/fw, ch/fh), 1)
	}
	return scale, (cw - fw*scale) / 2, (ch - fh*scale) / 2
}

// panelColors writes src as the console's 16-bit panel shows it: each 0xAARRGGBB pixel
// cut to RGB565 and widened back, opaque.
func panelColors(dst, src []uint32) {
	for i, c := range src {
		r5, g6, b5 := c>>19&0x1f, c>>10&0x3f, c>>3&0x1f
		dst[i] = 0xff000000 | (r5<<3|r5>>2)<<16 | (g6<<2|g6>>4)<<8 | (b5<<3 | b5>>2)
	}
}

// Keyboard scan codes (set 1, as Windows reports them in a key message) the simulator reads,
// with extendedScan set for the keys that send the 0xE0 prefix.
const (
	extendedScan = 0x100

	scanEsc       = 0x01
	scanBackspace = 0x0e
	scanTab       = 0x0f
	scanQ         = 0x10
	scanW         = 0x11
	scanEnter     = 0x1c
	scanLCtrl     = 0x1d
	scanA         = 0x1e
	scanS         = 0x1f
	scanD         = 0x20
	scanLShift    = 0x2a
	scanZ         = 0x2c
	scanX         = 0x2d
	scanRShift    = 0x36
	scanSpace     = 0x39
	scanRCtrl     = extendedScan | 0x1d
	scanKPEnter   = extendedScan | 0x1c
	scanUp        = extendedScan | 0x48
	scanLeft      = extendedScan | 0x4b
	scanRight     = extendedScan | 0x4d
	scanDown      = extendedScan | 0x50
)

// scanButtons maps a keyboard scan code to the button it presses, the same keys the console
// reads from a keyboard: the arrows or W, A, S, D for the D-pad, Space or Z for A, X or
// Shift for B, Enter or Tab for Select, Escape or Backspace for Cancel.
var scanButtons = map[int]sim.Button{
	scanUp: sim.ButtonUp, scanDown: sim.ButtonDown, scanLeft: sim.ButtonLeft, scanRight: sim.ButtonRight,
	scanW: sim.ButtonUp, scanS: sim.ButtonDown, scanA: sim.ButtonLeft, scanD: sim.ButtonRight,
	scanSpace: sim.ButtonA, scanZ: sim.ButtonA,
	scanX: sim.ButtonB, scanLShift: sim.ButtonB, scanRShift: sim.ButtonB,
	scanEnter: sim.ButtonSelect, scanKPEnter: sim.ButtonSelect, scanTab: sim.ButtonSelect,
	scanEsc: sim.ButtonCancel, scanBackspace: sim.ButtonCancel,
}

// keyboard turns key presses and releases into button events: repeats are dropped, a button
// held by two keys goes up with the last, and Ctrl+Q (either Ctrl) closes.
type keyboard struct {
	down  [2 * extendedScan]bool // keys down, by scan code
	holds [sim.NumButtons]int    // keys holding each button
}

// key reports a key going down or up and appends the events it causes.
func (k *keyboard) key(out []Event, scan int, down bool) []Event {
	if scan < 0 || scan >= len(k.down) {
		return out
	}
	if k.down[scan] == down {
		return out // auto-repeat, or a release of a key never seen down
	}
	k.down[scan] = down
	if down && scan == scanQ && (k.down[scanLCtrl] || k.down[scanRCtrl]) {
		return append(k.releaseAll(out), Event{Kind: Close})
	}
	b, ok := scanButtons[scan]
	if !ok {
		return out
	}
	if down {
		if k.holds[b]++; k.holds[b] == 1 {
			out = append(out, Event{Kind: Press, Button: b})
		}
		return out
	}
	if k.holds[b]--; k.holds[b] == 0 {
		out = append(out, Event{Kind: Release, Button: b})
	}
	return out
}

// releaseAll forgets every key and releases every held button, in button order.
func (k *keyboard) releaseAll(out []Event) []Event {
	k.down = [2 * extendedScan]bool{}
	for b := range k.holds {
		if k.holds[b] > 0 {
			k.holds[b] = 0
			out = append(out, Event{Kind: Release, Button: sim.Button(b)})
		}
	}
	return out
}
