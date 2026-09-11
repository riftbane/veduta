//go:build linux

package platform

// Keysym → W3C KeyboardEvent.code for a US layout, and keysym → typed text.
//
// A key's code comes from the first keysym of its keycode (group 1, unshifted), which
// on a US layout names the physical key: "a" is KeyA, "KP_Home" (the unshifted keysym of
// keypad 7) is Numpad7. Text comes from the keysym selected by the Shift, Lock and
// NumLock modifiers, following the core protocol's interpretation rules.

// Modifier bits of the core protocol's key/button state.
const (
	maskShift   = 0x01
	maskLock    = 0x02
	maskControl = 0x04
	maskMod1    = 0x08 // Alt on common keymaps
	maskMod2    = 0x10 // NumLock on common keymaps
	maskMod4    = 0x40 // Super on common keymaps
)

const keysymNumLock = 0xff7f

// keysymCode returns the W3C code of the physical key whose unshifted keysym (US layout)
// is ks, or "" when the key is not in asset.KeyCodes.
func keysymCode(ks uint32) string {
	switch {
	case ks >= 'a' && ks <= 'z':
		return letterCodes[ks-'a']
	case ks >= 'A' && ks <= 'Z':
		return letterCodes[ks-'A']
	case ks >= '0' && ks <= '9':
		return digitCodes[ks-'0']
	case ks >= 0xffbe && ks <= 0xffc9: // F1 … F12
		return fnCodes[ks-0xffbe]
	case ks >= 0xffb0 && ks <= 0xffb9: // KP_0 … KP_9
		return numpadCodes[ks-0xffb0]
	}
	switch ks {
	case ' ':
		return "Space"
	case '-':
		return "Minus"
	case '=':
		return "Equal"
	case '[':
		return "BracketLeft"
	case ']':
		return "BracketRight"
	case '\\':
		return "Backslash"
	case ';':
		return "Semicolon"
	case '\'':
		return "Quote"
	case '`':
		return "Backquote"
	case ',':
		return "Comma"
	case '.':
		return "Period"
	case '/':
		return "Slash"
	case 0xff0d: // Return
		return "Enter"
	case 0xff1b: // Escape
		return "Escape"
	case 0xff09: // Tab
		return "Tab"
	case 0xff08: // BackSpace
		return "Backspace"
	case 0xffe1: // Shift_L
		return "ShiftLeft"
	case 0xffe2: // Shift_R
		return "ShiftRight"
	case 0xffe3: // Control_L
		return "ControlLeft"
	case 0xffe4: // Control_R
		return "ControlRight"
	case 0xffe9: // Alt_L
		return "AltLeft"
	case 0xffea, 0xfe03, 0xff7e: // Alt_R, ISO_Level3_Shift (AltGr), Mode_switch
		return "AltRight"
	case 0xffeb, 0xffe7: // Super_L, Meta_L
		return "MetaLeft"
	case 0xffec, 0xffe8: // Super_R, Meta_R
		return "MetaRight"
	case 0xffe5: // Caps_Lock
		return "CapsLock"
	case 0xff52: // Up
		return "ArrowUp"
	case 0xff54: // Down
		return "ArrowDown"
	case 0xff51: // Left
		return "ArrowLeft"
	case 0xff53: // Right
		return "ArrowRight"
	case 0xff63: // Insert
		return "Insert"
	case 0xffff: // Delete
		return "Delete"
	case 0xff50: // Home
		return "Home"
	case 0xff57: // End
		return "End"
	case 0xff55: // Prior
		return "PageUp"
	case 0xff56: // Next
		return "PageDown"
	// Keypad: the unshifted keysyms of the keypad digits are the navigation keysyms.
	case 0xff9e: // KP_Insert
		return "Numpad0"
	case 0xff9c: // KP_End
		return "Numpad1"
	case 0xff99: // KP_Down
		return "Numpad2"
	case 0xff9b: // KP_Next
		return "Numpad3"
	case 0xff96: // KP_Left
		return "Numpad4"
	case 0xff9d: // KP_Begin
		return "Numpad5"
	case 0xff98: // KP_Right
		return "Numpad6"
	case 0xff95: // KP_Home
		return "Numpad7"
	case 0xff97: // KP_Up
		return "Numpad8"
	case 0xff9a: // KP_Prior
		return "Numpad9"
	case 0xff9f, 0xffae: // KP_Delete, KP_Decimal
		return "NumpadDecimal"
	case 0xffab: // KP_Add
		return "NumpadAdd"
	case 0xffad: // KP_Subtract
		return "NumpadSubtract"
	case 0xffaa: // KP_Multiply
		return "NumpadMultiply"
	case 0xffaf: // KP_Divide
		return "NumpadDivide"
	case 0xff8d: // KP_Enter
		return "NumpadEnter"
	}
	return ""
}

var (
	letterCodes = func() (c [26]string) {
		for i := range c {
			c[i] = "Key" + string(rune('A'+i))
		}
		return c
	}()
	digitCodes  = [10]string{"Digit0", "Digit1", "Digit2", "Digit3", "Digit4", "Digit5", "Digit6", "Digit7", "Digit8", "Digit9"}
	numpadCodes = [10]string{"Numpad0", "Numpad1", "Numpad2", "Numpad3", "Numpad4", "Numpad5", "Numpad6", "Numpad7", "Numpad8", "Numpad9"}
	fnCodes     = [12]string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"}
)

// latin1Text holds the one-character strings of the printable Latin-1 code points, so
// that typing does not allocate.
var latin1Text = func() (t [256]string) {
	for r := 0x20; r < 0x100; r++ {
		if r < 0x7f || r >= 0xa0 {
			t[r] = string(rune(r))
		}
	}
	return t
}()

// keysymRune returns the character typed by keysym ks, or -1 when ks is not a printable
// character (function keys, modifiers, Return, Tab, …).
func keysymRune(ks uint32) rune {
	switch {
	case ks >= 0x20 && ks <= 0x7e, ks >= 0xa0 && ks <= 0xff: // Latin-1 keysyms are code points
		return rune(ks)
	case ks >= 0x01000100 && ks <= 0x0110ffff: // Unicode keysyms
		r := rune(ks - 0x01000000)
		if r >= 0xd800 && r <= 0xdfff {
			return -1
		}
		return r
	case ks >= 0xffb0 && ks <= 0xffb9: // KP_0 … KP_9
		return rune('0' + ks - 0xffb0)
	case ks >= 0xffaa && ks <= 0xffaf: // KP_Multiply, KP_Add, KP_Separator, KP_Subtract, KP_Decimal, KP_Divide
		return rune("*+,-./"[ks-0xffaa])
	case ks == 0xff80: // KP_Space
		return ' '
	case ks == 0xffbd: // KP_Equal
		return '='
	}
	return -1
}

// runeText returns r as a string, without allocating for Latin-1.
func runeText(r rune) string {
	if r >= 0 && r < 0x100 {
		return latin1Text[r]
	}
	return string(r)
}

// isKeypad reports whether ks is a keypad keysym (KP_Space … KP_Equal).
func isKeypad(ks uint32) bool { return ks >= 0xff80 && ks <= 0xffbd }

// latinCase returns the lower- and uppercase keysyms of a Latin-1 letter keysym, and
// false for every other keysym.
func latinCase(ks uint32) (lower, upper uint32, ok bool) {
	switch {
	case ks >= 'a' && ks <= 'z':
		return ks, ks - 0x20, true
	case ks >= 'A' && ks <= 'Z':
		return ks + 0x20, ks, true
	case ks >= 0xe0 && ks <= 0xfe && ks != 0xf7:
		return ks, ks - 0x20, true
	case ks >= 0xc0 && ks <= 0xde && ks != 0xd7:
		return ks + 0x20, ks, true
	}
	return 0, 0, false
}

// keyText returns the text typed by a key whose group-1 keysyms are k0 (unshifted) and
// k1 (shifted, 0 = NoSymbol) with modifier state, or "" when the key types nothing.
// Control, Alt (Mod1) and Super (Mod4) chords type nothing; letters are uppercase when
// exactly one of Shift and CapsLock is on; with NumLock on, keypad keys type their
// shifted keysym (the digit) unless Shift is held.
func keyText(k0, k1 uint32, state, numLockMask uint16) string {
	if state&((maskControl|maskMod1|maskMod4)&^numLockMask) != 0 {
		return ""
	}
	shift := state&maskShift != 0
	var ks uint32
	if lo, up, ok := latinCase(k0); ok && (k1 == 0 || k1 == up || k1 == lo) {
		ks = lo
		if shift != (state&maskLock != 0) {
			ks = up
		}
	} else {
		if k1 == 0 {
			k1 = k0
		}
		ks = k0
		if state&numLockMask != 0 && isKeypad(k1) {
			if !shift {
				ks = k1
			}
		} else if shift {
			ks = k1
		}
	}
	r := keysymRune(ks)
	if r < 0 {
		return ""
	}
	return runeText(r)
}
