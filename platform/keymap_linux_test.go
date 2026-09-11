//go:build linux

package platform

import (
	"testing"

	"github.com/riftbane/veduta/asset"
)

// usKeymap is the group-1 part of the core keymap of an X.Org server with the evdev
// rules and the "us" layout (`xmodmap -pke`): keycode → {unshifted, shifted} keysym.
var usKeymap = func() map[byte][2]uint32 {
	m := map[byte][2]uint32{
		9:  {0xff1b, 0}, // Escape
		10: {'1', '!'}, 11: {'2', '@'}, 12: {'3', '#'}, 13: {'4', '$'}, 14: {'5', '%'},
		15: {'6', '^'}, 16: {'7', '&'}, 17: {'8', '*'}, 18: {'9', '('}, 19: {'0', ')'},
		20: {'-', '_'}, 21: {'=', '+'}, 22: {0xff08, 0xff08}, 23: {0xff09, 0xfe20},
		34: {'[', '{'}, 35: {']', '}'}, 36: {0xff0d, 0}, 37: {0xffe3, 0},
		47: {';', ':'}, 48: {'\'', '"'}, 49: {'`', '~'}, 50: {0xffe1, 0}, 51: {'\\', '|'},
		59: {',', '<'}, 60: {'.', '>'}, 61: {'/', '?'}, 62: {0xffe2, 0}, 63: {0xffaa, 0xffaa},
		64: {0xffe9, 0xffe7}, 65: {' ', 0}, 66: {0xffe5, 0},
		77: {0xff7f, 0}, 78: {0xff14, 0},
		79: {0xff95, 0xffb7}, 80: {0xff97, 0xffb8}, 81: {0xff9a, 0xffb9}, 82: {0xffad, 0xffad},
		83: {0xff96, 0xffb4}, 84: {0xff9d, 0xffb5}, 85: {0xff98, 0xffb6}, 86: {0xffab, 0xffab},
		87: {0xff9c, 0xffb1}, 88: {0xff99, 0xffb2}, 89: {0xff9b, 0xffb3}, 90: {0xff9e, 0xffb0},
		91: {0xff9f, 0xffae}, 92: {0xfe03, 0}, 94: {'<', '>'}, 95: {0xffc8, 0xffc8}, 96: {0xffc9, 0xffc9},
		104: {0xff8d, 0xff8d}, 105: {0xffe4, 0}, 106: {0xffaf, 0xffaf}, 107: {0xff61, 0xff15},
		108: {0xffea, 0xffe8}, 110: {0xff50, 0}, 111: {0xff52, 0}, 112: {0xff55, 0},
		113: {0xff51, 0}, 114: {0xff53, 0}, 115: {0xff57, 0}, 116: {0xff54, 0},
		117: {0xff56, 0}, 118: {0xff63, 0}, 119: {0xffff, 0}, 127: {0xff13, 0xff6b},
		133: {0xffeb, 0}, 134: {0xffec, 0}, 135: {0xff67, 0},
		203: {0xff7e, 0}, 204: {0, 0xffe9}, 205: {0, 0xffe7}, 206: {0, 0xffeb}, 207: {0, 0xffed},
	}
	for i, c := range "qwertyuiop" {
		m[byte(24+i)] = [2]uint32{uint32(c), uint32(c - 32)}
	}
	for i, c := range "asdfghjkl" {
		m[byte(38+i)] = [2]uint32{uint32(c), uint32(c - 32)}
	}
	for i, c := range "zxcvbnm" {
		m[byte(52+i)] = [2]uint32{uint32(c), uint32(c - 32)}
	}
	for i := 0; i < 10; i++ { // F1 … F10
		m[byte(67+i)] = [2]uint32{0xffbe + uint32(i), 0xffbe + uint32(i)}
	}
	return m
}()

// usModmap is the modifier map of the same server: Shift, Lock, Control, Mod1…Mod5.
var usModmap = [8][]byte{{50, 62}, {66}, {37, 105}, {64, 204, 205}, {77}, {}, {133, 134, 206, 207}, {92, 203}}

func TestKeysymTableCoversKeyCodes(t *testing.T) {
	produced := map[string]bool{}
	for ks := uint32(0); ks < 0x10000; ks++ {
		if c := keysymCode(ks); c != "" {
			if !asset.IsKeyCode(c) {
				t.Errorf("keysym 0x%x → %q, not in asset.KeyCodes", ks, c)
			}
			produced[c] = true
		}
	}
	for _, c := range asset.KeyCodes {
		if !produced[c] {
			t.Errorf("no keysym produces %s", c)
		}
	}
}

func TestUSKeymapProducesEveryKeyCode(t *testing.T) {
	produced := map[string]byte{}
	for kc := 8; kc < 256; kc++ {
		if c := keysymCode(usKeymap[byte(kc)][0]); c != "" {
			if _, dup := produced[c]; !dup {
				produced[c] = byte(kc)
			}
		}
	}
	for _, c := range asset.KeyCodes {
		if _, ok := produced[c]; !ok {
			t.Errorf("no key of the US keymap produces %s", c)
		}
	}
	for kc, want := range map[byte]string{
		9: "Escape", 10: "Digit1", 19: "Digit0", 20: "Minus", 23: "Tab", 24: "KeyQ", 38: "KeyA", 49: "Backquote",
		64: "AltLeft", 65: "Space", 79: "Numpad7", 84: "Numpad5", 90: "Numpad0", 91: "NumpadDecimal",
		104: "NumpadEnter", 105: "ControlRight", 108: "AltRight", 111: "ArrowUp", 119: "Delete",
		133: "MetaLeft", 134: "MetaRight", 96: "F12", 94: "", 77: "", 204: "",
	} {
		if got := keysymCode(usKeymap[kc][0]); got != want {
			t.Errorf("keycode %d → %q, want %q", kc, got, want)
		}
	}
}

func TestKeyText(t *testing.T) {
	const numLock = maskMod2
	cases := []struct {
		k0, k1 uint32
		state  uint16
		want   string
	}{
		{'a', 'A', 0, "a"},
		{'a', 'A', maskShift, "A"},
		{'a', 'A', maskLock, "A"},
		{'a', 'A', maskShift | maskLock, "a"},
		{'a', 0, maskShift, "A"},
		{'a', 'A', maskControl, ""},
		{'a', 'A', maskMod1, ""},
		{'a', 'A', maskMod4, ""},
		{'a', 'A', numLock, "a"},
		{'1', '!', maskShift, "!"},
		{'1', '!', maskLock, "1"},
		{' ', 0, 0, " "},
		{' ', 0, maskShift, " "},
		{0xff0d, 0, 0, ""},              // Return
		{0xff09, 0xfe20, maskShift, ""}, // Tab
		{0xff95, 0xffb7, numLock, "7"},  // KP_Home / KP_7
		{0xff95, 0xffb7, 0, ""},         //
		{0xff95, 0xffb7, numLock | maskShift, ""},
		{0xff9f, 0xffae, numLock, "."}, // KP_Delete / KP_Decimal
		{0xffab, 0xffab, 0, "+"},       // KP_Add
		{0xffaf, 0xffaf, 0, "/"},       // KP_Divide
		{0xe9, 0xc9, maskShift, "É"},
		{0xe9, 0, maskLock, "É"},
		{0x10020ac, 0, 0, "€"},  // Unicode keysym
		{0xffbe, 0xffbe, 0, ""}, // F1
	}
	for _, c := range cases {
		if got := keyText(c.k0, c.k1, c.state, numLock); got != c.want {
			t.Errorf("keyText(0x%x, 0x%x, 0x%x) = %q, want %q", c.k0, c.k1, c.state, got, c.want)
		}
	}
	// Without a NumLock modifier, Mod2 is an ordinary modifier and keypad keys type
	// their unshifted keysym.
	if got := keyText(0xff95, 0xffb7, maskMod2, 0); got != "" {
		t.Errorf("keypad without a NumLock modifier = %q", got)
	}
	if n := testing.AllocsPerRun(100, func() { keyText('a', 'A', maskShift, numLock) }); n != 0 {
		t.Errorf("keyText allocates %.1f times", n)
	}
}

func TestNumLockMask(t *testing.T) {
	var syms [256][2]uint32
	for kc, k := range usKeymap {
		syms[kc] = k
	}
	mm := make([]byte, 32+8*4)
	mm[1] = 4
	for mod, keys := range usModmap {
		copy(mm[32+mod*4:], keys)
	}
	if got := numLockMask(mm, &syms); got != maskMod2 {
		t.Errorf("NumLock mask 0x%x, want Mod2", got)
	}
	// NumLock moved to Mod3.
	copy(mm[32+4*4:], []byte{0, 0, 0, 0})
	copy(mm[32+5*4:], []byte{77})
	if got := numLockMask(mm, &syms); got != 0x20 {
		t.Errorf("NumLock on Mod3: mask 0x%x", got)
	}
	if got := numLockMask(mm[:40], &syms); got != 0 {
		t.Errorf("truncated reply: mask 0x%x", got)
	}
}
