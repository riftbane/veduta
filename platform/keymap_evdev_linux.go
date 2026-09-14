package platform

// Keyboards, read the same way as the pad. These are the kernel's own KEY_* codes from
// linux/input-event-codes.h, the numbers that arrive on /dev/input/eventN, and the only
// keyboard codes the player ever sees.
//
// It matters beyond tidiness. Without it a console with no gamepad — an emulated machine,
// a PC at a text console, a board whose pad is not plugged in yet — draws its dashboard
// and then answers nothing at all, with no message to say why.
//
// Codes are by physical position, as the W3C names are: a French keyboard's A sits where
// KEY_Q is, and reports KeyQ. The keypad reports its own codes whatever NumLock says,
// because the kernel reports the key and leaves its meaning to the console's keymap.

const (
	keyEsc       = 1
	keyQ         = 16
	keyLeftCtrl  = 29
	keyRightCtrl = 97
)

// evdevKeys maps a kernel key code to a W3C code. It covers every name in asset.KeyCodes.
// Keys whose W3C name is not in that list — NumLock (69), ScrollLock (70), IntlBackslash
// (KEY_102ND, 86), PrintScreen (KEY_SYSRQ, 99), NumpadEqual (117), Pause (119),
// NumpadComma (121), ContextMenu (KEY_COMPOSE, 127), media keys — are ignored rather than
// guessed: a game reads the codes it knows, and a scenario could not name them.
var evdevKeys = map[uint16]string{
	keyEsc: "Escape",
	2:      "Digit1", 3: "Digit2", 4: "Digit3", 5: "Digit4", 6: "Digit5",
	7: "Digit6", 8: "Digit7", 9: "Digit8", 10: "Digit9", 11: "Digit0",
	12: "Minus", 13: "Equal", 14: "Backspace", 15: "Tab",
	keyQ: "KeyQ", 17: "KeyW", 18: "KeyE", 19: "KeyR", 20: "KeyT", 21: "KeyY",
	22: "KeyU", 23: "KeyI", 24: "KeyO", 25: "KeyP",
	26: "BracketLeft", 27: "BracketRight", 28: "Enter",
	keyLeftCtrl: "ControlLeft",
	30:          "KeyA", 31: "KeyS", 32: "KeyD", 33: "KeyF", 34: "KeyG",
	35: "KeyH", 36: "KeyJ", 37: "KeyK", 38: "KeyL",
	39: "Semicolon", 40: "Quote", 41: "Backquote", 42: "ShiftLeft", 43: "Backslash",
	44: "KeyZ", 45: "KeyX", 46: "KeyC", 47: "KeyV", 48: "KeyB", 49: "KeyN", 50: "KeyM",
	51: "Comma", 52: "Period", 53: "Slash", 54: "ShiftRight",
	55: "NumpadMultiply", 56: "AltLeft", 57: "Space", 58: "CapsLock",
	59: "F1", 60: "F2", 61: "F3", 62: "F4", 63: "F5", 64: "F6",
	65: "F7", 66: "F8", 67: "F9", 68: "F10",
	71: "Numpad7", 72: "Numpad8", 73: "Numpad9", 74: "NumpadSubtract",
	75: "Numpad4", 76: "Numpad5", 77: "Numpad6", 78: "NumpadAdd",
	79: "Numpad1", 80: "Numpad2", 81: "Numpad3", 82: "Numpad0", 83: "NumpadDecimal",
	87: "F11", 88: "F12",
	96:           "NumpadEnter",
	keyRightCtrl: "ControlRight",
	98:           "NumpadDivide",
	100:          "AltRight",
	102:          "Home", 103: "ArrowUp", 104: "PageUp", 105: "ArrowLeft",
	106: "ArrowRight", 107: "End", 108: "ArrowDown", 109: "PageDown",
	110: "Insert", 111: "Delete",
	125: "MetaLeft", 126: "MetaRight",
}

// keyboardExit closes the player from a keyboard, as Select and Start do from a pad: Ctrl+Q,
// with either Ctrl. It is not Escape: the dashboard already gives Escape its own meaning,
// and a game may too.
var keyboardExit = [2][2]uint16{{keyLeftCtrl, keyQ}, {keyRightCtrl, keyQ}}
