package asset

import (
	"sort"
	"strings"
)

// KeyCodes lists every keyboard key name accepted in scenario and input scripts, sorted
// in byte order. Names are W3C KeyboardEvent.code values for a US layout (KeyW, Space,
// ArrowLeft, …): they identify physical keys, not the characters they type. Typed
// characters travel separately in Input.Text.
var KeyCodes = buildKeyCodes()

// MouseButtons lists the mouse button names accepted in scenario and input scripts,
// sorted.
var MouseButtons = []string{"left", "middle", "right"}

func buildKeyCodes() []string {
	var k []string
	for c := 'A'; c <= 'Z'; c++ {
		k = append(k, "Key"+string(c))
	}
	for d := '0'; d <= '9'; d++ {
		k = append(k, "Digit"+string(d), "Numpad"+string(d))
	}
	for i := 1; i <= 12; i++ {
		k = append(k, "F"+itoa(i))
	}
	k = append(k,
		"Space", "Enter", "Escape", "Tab", "Backspace",
		"ShiftLeft", "ShiftRight", "ControlLeft", "ControlRight",
		"AltLeft", "AltRight", "MetaLeft", "MetaRight",
		"ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
		"Minus", "Equal", "BracketLeft", "BracketRight", "Backslash",
		"Semicolon", "Quote", "Backquote", "Comma", "Period", "Slash",
		"CapsLock", "Insert", "Delete", "Home", "End", "PageUp", "PageDown",
		"NumpadAdd", "NumpadSubtract", "NumpadMultiply", "NumpadDivide",
		"NumpadDecimal", "NumpadEnter",
	)
	sort.Strings(k)
	return k
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// IsKeyCode reports whether s is one of KeyCodes (case-sensitive).
func IsKeyCode(s string) bool {
	i := sort.SearchStrings(KeyCodes, s)
	return i < len(KeyCodes) && KeyCodes[i] == s
}

// isMouseButton reports whether s is one of MouseButtons.
func isMouseButton(s string) bool {
	for _, b := range MouseButtons {
		if s == b {
			return true
		}
	}
	return false
}

// suggestKey returns the key code an invalid key name s most likely meant, or "".
// It maps single letters and digits ("w" → "KeyW", "1" → "Digit1"), a few common
// aliases, and case mistakes ("space" → "Space").
func suggestKey(s string) string {
	if len(s) == 1 {
		c := s[0]
		switch {
		case c >= 'a' && c <= 'z':
			return "Key" + string(c-'a'+'A')
		case c >= 'A' && c <= 'Z':
			return "Key" + s
		case c >= '0' && c <= '9':
			return "Digit" + s
		case c == ' ':
			return "Space"
		}
	}
	aliases := map[string]string{
		"up": "ArrowUp", "down": "ArrowDown", "left": "ArrowLeft", "right": "ArrowRight",
		"esc": "Escape", "return": "Enter", "shift": "ShiftLeft", "ctrl": "ControlLeft",
		"control": "ControlLeft", "alt": "AltLeft", "meta": "MetaLeft", "del": "Delete",
		"spacebar": "Space",
	}
	if a, ok := aliases[strings.ToLower(s)]; ok {
		return a
	}
	for _, k := range KeyCodes {
		if strings.EqualFold(k, s) {
			return k
		}
	}
	return ""
}
