package platform

import (
	"testing"

	"github.com/riftbane/veduta/asset"
)

// keyMax is KEY_MAX from linux/input-event-codes.h: no key code is larger.
const keyMax = 0x2ff

// TestEvdevKeymapCoversKeyCodes: a keyboard is the only way to type on a console without
// its pad, so every key a game or a scenario can name must arrive from one, and nothing
// the kernel sends may turn into a name the rest of the engine would refuse.
func TestEvdevKeymapCoversKeyCodes(t *testing.T) {
	produced := map[string]uint16{}
	for code := uint16(0); code <= keyMax; code++ {
		name, ok := evdevKeys[code]
		if !ok {
			continue
		}
		if !asset.IsKeyCode(name) {
			t.Errorf("kernel key %d → %q, which is not in asset.KeyCodes", code, name)
		}
		if other, dup := produced[name]; dup {
			t.Errorf("kernel keys %d and %d both report %s", other, code, name)
		}
		produced[name] = code
	}
	for code := range evdevKeys {
		if code > keyMax {
			t.Errorf("kernel key %d is past KEY_MAX", code)
		}
	}
	for _, name := range asset.KeyCodes {
		if _, ok := produced[name]; !ok {
			t.Errorf("no kernel key reports %s", name)
		}
	}
}

// TestEvdevKeymapNumbers pins entries against linux/input-event-codes.h, so a table that
// is complete but shifted by one is still caught.
func TestEvdevKeymapNumbers(t *testing.T) {
	for code, want := range map[uint16]string{
		1:   "Escape",         // KEY_ESC
		15:  "Tab",            // KEY_TAB
		28:  "Enter",          // KEY_ENTER
		55:  "NumpadMultiply", // KEY_KPASTERISK
		57:  "Space",          // KEY_SPACE
		68:  "F10",            // KEY_F10
		69:  "",               // KEY_NUMLOCK: not in asset.KeyCodes
		70:  "",               // KEY_SCROLLLOCK
		71:  "Numpad7",        // KEY_KP7
		74:  "NumpadSubtract", // KEY_KPMINUS
		76:  "Numpad5",        // KEY_KP5
		78:  "NumpadAdd",      // KEY_KPPLUS
		79:  "Numpad1",        // KEY_KP1
		82:  "Numpad0",        // KEY_KP0
		83:  "NumpadDecimal",  // KEY_KPDOT
		86:  "",               // KEY_102ND (IntlBackslash)
		87:  "F11",            // KEY_F11
		88:  "F12",            // KEY_F12
		96:  "NumpadEnter",    // KEY_KPENTER
		97:  "ControlRight",   // KEY_RIGHTCTRL
		98:  "NumpadDivide",   // KEY_KPSLASH
		99:  "",               // KEY_SYSRQ (PrintScreen)
		100: "AltRight",       // KEY_RIGHTALT
		103: "ArrowUp",        // KEY_UP
		111: "Delete",         // KEY_DELETE
		117: "",               // KEY_KPEQUAL
		119: "",               // KEY_PAUSE
		121: "",               // KEY_KPCOMMA
		125: "MetaLeft",       // KEY_LEFTMETA
		127: "",               // KEY_COMPOSE (ContextMenu)
	} {
		if got := evdevKeys[code]; got != want {
			t.Errorf("kernel key %d → %q, want %q", code, got, want)
		}
	}
}
