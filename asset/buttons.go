package asset

import "strings"

// ButtonNames lists the console's game buttons, the names scenarios and input scripts use,
// in the engine's order: the D-pad, A, B, Select (the game's menu) and Cancel (back). Home
// is not among them: it returns to the console's home, and no game ever sees it.
var ButtonNames = []string{"up", "down", "left", "right", "a", "b", "select", "cancel"}

// IsButton reports whether s is one of ButtonNames (case-sensitive).
func IsButton(s string) bool { return ButtonIndex(s) >= 0 }

// ButtonIndex returns the position of s in ButtonNames, or -1.
func ButtonIndex(s string) int {
	for i, b := range ButtonNames {
		if s == b {
			return i
		}
	}
	return -1
}

// suggestButton returns the button an invalid name s most likely meant, or "": the same
// name in another case, or the button a keyboard key of that name presses on the console
// (the W3C key codes of engine v1, "ArrowUp", "KeyW", "Space", …).
func suggestButton(s string) string {
	for _, b := range ButtonNames {
		if strings.EqualFold(s, b) {
			return b
		}
	}
	aliases := map[string]string{
		"arrowup": "up", "keyw": "up", "w": "up",
		"arrowdown": "down", "keys": "down", "s": "down",
		"arrowleft": "left", "keya": "left",
		"arrowright": "right", "keyd": "right", "d": "right",
		"space": "a", "keyz": "a", "z": "a", "confirm": "a",
		"keyx": "b", "x": "b", "shiftleft": "b", "shiftright": "b",
		"enter": "select", "tab": "select", "menu": "select", "start": "select",
		"escape": "cancel", "esc": "cancel", "backspace": "cancel", "back": "cancel",
	}
	return aliases[strings.ToLower(s)]
}
