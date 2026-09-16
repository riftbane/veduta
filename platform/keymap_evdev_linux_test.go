package platform

import (
	"testing"

	"github.com/riftbane/veduta/v2/sim"
)

// keyMax is KEY_MAX from linux/input-event-codes.h: no key code is larger.
const keyMax = 0x2ff

// TestEvdevKeymapPressesEveryButton: a keyboard is the only way to play a console without
// its pad, so every game button must be reachable from one.
func TestEvdevKeymapPressesEveryButton(t *testing.T) {
	var reached [sim.NumButtons]bool
	for code, b := range evdevKeys {
		if code > keyMax {
			t.Errorf("kernel key %d is past KEY_MAX", code)
		}
		if b >= sim.NumButtons {
			t.Errorf("kernel key %d → %v, not a button", code, b)
			continue
		}
		reached[b] = true
	}
	for b, ok := range reached {
		if !ok {
			t.Errorf("no kernel key presses %v", sim.Button(b))
		}
	}
}

// TestEvdevKeymapNumbers pins entries against linux/input-event-codes.h, so a table that
// is complete but shifted by one is still caught.
func TestEvdevKeymapNumbers(t *testing.T) {
	const none = sim.Button(sim.NumButtons)
	for code, want := range map[uint16]sim.Button{
		1:   sim.ButtonCancel, // KEY_ESC
		14:  sim.ButtonCancel, // KEY_BACKSPACE
		15:  sim.ButtonSelect, // KEY_TAB
		16:  none,             // KEY_Q: half of Ctrl+Q, nothing on its own
		17:  sim.ButtonUp,     // KEY_W
		28:  sim.ButtonSelect, // KEY_ENTER
		29:  none,             // KEY_LEFTCTRL
		30:  sim.ButtonLeft,   // KEY_A
		31:  sim.ButtonDown,   // KEY_S
		32:  sim.ButtonRight,  // KEY_D
		42:  sim.ButtonB,      // KEY_LEFTSHIFT
		44:  sim.ButtonA,      // KEY_Z
		45:  sim.ButtonB,      // KEY_X
		54:  sim.ButtonB,      // KEY_RIGHTSHIFT
		57:  sim.ButtonA,      // KEY_SPACE
		103: sim.ButtonUp,     // KEY_UP
		105: sim.ButtonLeft,   // KEY_LEFT
		106: sim.ButtonRight,  // KEY_RIGHT
		108: sim.ButtonDown,   // KEY_DOWN
	} {
		got, ok := evdevKeys[code]
		if !ok {
			got = none
		}
		if got != want {
			t.Errorf("kernel key %d → %v, want %v", code, got, want)
		}
	}
}
