//go:build windows

package platform

// extendedKey marks, in a scanCodes index, a key that sends the 0xE0 prefix (the
// extended-key flag, bit 24 of a key message's lParam).
const extendedKey = 0x100

// scanCodes maps a keyboard scan code to a W3C KeyboardEvent.code for a US layout. The
// index is the set-1 make code (bits 16–23 of a WM_KEYDOWN lParam) plus extendedKey
// when the extended flag is set. Scan codes identify physical keys, so the table does not
// depend on the active keyboard layout or on NumLock: the key right of "0" on the numpad
// is Numpad0 whatever it types.
//
// Keys outside asset.KeyCodes map to "": NumLock (0x45 extended; Windows reports it with
// the flag and Pause without), Pause, ScrollLock (0x46), PrintScreen (0x37 extended),
// ContextMenu (0x5D extended), IntlBackslash (0x56) and media/browser keys.
var scanCodes = [2 * extendedKey]string{
	0x01: "Escape",
	0x02: "Digit1", 0x03: "Digit2", 0x04: "Digit3", 0x05: "Digit4", 0x06: "Digit5",
	0x07: "Digit6", 0x08: "Digit7", 0x09: "Digit8", 0x0A: "Digit9", 0x0B: "Digit0",
	0x0C: "Minus", 0x0D: "Equal", 0x0E: "Backspace", 0x0F: "Tab",
	0x10: "KeyQ", 0x11: "KeyW", 0x12: "KeyE", 0x13: "KeyR", 0x14: "KeyT",
	0x15: "KeyY", 0x16: "KeyU", 0x17: "KeyI", 0x18: "KeyO", 0x19: "KeyP",
	0x1A: "BracketLeft", 0x1B: "BracketRight", 0x1C: "Enter", 0x1D: "ControlLeft",
	0x1E: "KeyA", 0x1F: "KeyS", 0x20: "KeyD", 0x21: "KeyF", 0x22: "KeyG",
	0x23: "KeyH", 0x24: "KeyJ", 0x25: "KeyK", 0x26: "KeyL",
	0x27: "Semicolon", 0x28: "Quote", 0x29: "Backquote", 0x2A: "ShiftLeft", 0x2B: "Backslash",
	0x2C: "KeyZ", 0x2D: "KeyX", 0x2E: "KeyC", 0x2F: "KeyV", 0x30: "KeyB",
	0x31: "KeyN", 0x32: "KeyM", 0x33: "Comma", 0x34: "Period", 0x35: "Slash",
	0x36: "ShiftRight", 0x37: "NumpadMultiply", 0x38: "AltLeft", 0x39: "Space",
	0x3A: "CapsLock",
	0x3B: "F1", 0x3C: "F2", 0x3D: "F3", 0x3E: "F4", 0x3F: "F5",
	0x40: "F6", 0x41: "F7", 0x42: "F8", 0x43: "F9", 0x44: "F10",
	0x47: "Numpad7", 0x48: "Numpad8", 0x49: "Numpad9", 0x4A: "NumpadSubtract",
	0x4B: "Numpad4", 0x4C: "Numpad5", 0x4D: "Numpad6", 0x4E: "NumpadAdd",
	0x4F: "Numpad1", 0x50: "Numpad2", 0x51: "Numpad3", 0x52: "Numpad0",
	0x53: "NumpadDecimal",
	0x57: "F11", 0x58: "F12",

	extendedKey | 0x1C: "NumpadEnter",
	extendedKey | 0x1D: "ControlRight",
	extendedKey | 0x35: "NumpadDivide",
	extendedKey | 0x38: "AltRight",
	extendedKey | 0x47: "Home",
	extendedKey | 0x48: "ArrowUp",
	extendedKey | 0x49: "PageUp",
	extendedKey | 0x4B: "ArrowLeft",
	extendedKey | 0x4D: "ArrowRight",
	extendedKey | 0x4F: "End",
	extendedKey | 0x50: "ArrowDown",
	extendedKey | 0x51: "PageDown",
	extendedKey | 0x52: "Insert",
	extendedKey | 0x53: "Delete",
	extendedKey | 0x5B: "MetaLeft",
	extendedKey | 0x5C: "MetaRight",
}

// keyLParam is the decoded lParam of WM_KEYDOWN, WM_KEYUP, WM_SYSKEYDOWN and WM_SYSKEYUP.
type keyLParam struct {
	scan     uint32 // set-1 make code, bits 16–23
	extended bool   // bit 24: the key sends the 0xE0 prefix
	repeat   bool   // bit 30: the key was already down (auto-repeat; always set on key up)
	release  bool   // bit 31: the key is being released
}

// decodeKeyLParam splits a key message's lParam into its fields.
func decodeKeyLParam(lp uintptr) keyLParam {
	return keyLParam{
		scan:     uint32(lp>>16) & 0xFF,
		extended: lp>>24&1 != 0,
		repeat:   lp>>30&1 != 0,
		release:  lp>>31&1 != 0,
	}
}

// keyIndex returns the scanCodes index of a key, or ok false when the key message is
// synthetic and must be dropped.
func keyIndex(scan uint32, extended bool) (i int, ok bool) {
	i = int(scan & 0xFF)
	if extended {
		i |= extendedKey
	}
	switch i {
	case extendedKey | 0x2A:
		// E0 2A is the "fake Shift" keyboards send around the gray navigation keys; it
		// is not a key press.
		return 0, false
	case extendedKey | 0x36:
		// CJK IMEs set the extended flag on Right Shift.
		i = 0x36
	}
	return i, true
}
