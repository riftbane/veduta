package asset

import (
	"os"
	"sort"
	"strings"
	"testing"
)

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestKeyCodes(t *testing.T) {
	if !sort.StringsAreSorted(KeyCodes) {
		t.Fatal("KeyCodes is not sorted")
	}
	for i := 1; i < len(KeyCodes); i++ {
		if KeyCodes[i] == KeyCodes[i-1] {
			t.Fatalf("duplicate key %s", KeyCodes[i])
		}
	}
	// 26 letters, 10 digits, 10 numpad digits, 12 function keys, 41 others.
	if len(KeyCodes) != 99 {
		t.Fatalf("len(KeyCodes) = %d, want 99", len(KeyCodes))
	}
	for _, k := range []string{"KeyA", "KeyZ", "Digit0", "Digit9", "F1", "F12", "Space", "ArrowLeft", "ShiftRight",
		"Backquote", "Numpad0", "NumpadEnter", "PageDown", "CapsLock", "MetaLeft"} {
		if !IsKeyCode(k) {
			t.Errorf("IsKeyCode(%q) = false", k)
		}
	}
	for _, k := range []string{"", "keyA", "KEYA", "W", "F13", "F0", "Shift", "Numpad10", "space", "KeyAA"} {
		if IsKeyCode(k) {
			t.Errorf("IsKeyCode(%q) = true", k)
		}
	}
}

func TestSuggestKey(t *testing.T) {
	cases := map[string]string{
		"w": "KeyW", "W": "KeyW", "7": "Digit7", " ": "Space", "space": "Space", "ESC": "Escape",
		"up": "ArrowUp", "arrowleft": "ArrowLeft", "shift": "ShiftLeft", "f5": "F5", "jump": "",
	}
	for in, want := range cases {
		if got := suggestKey(in); got != want {
			t.Errorf("suggestKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// docs/scenario.md lists every key, either literally or as a documented range.
func TestKeyCodesDocumented(t *testing.T) {
	doc := string(mustRead(t, "../docs/scenario.md"))
	for _, r := range []string{"`KeyA` … `KeyZ`", "`Digit0` … `Digit9`", "`F1` … `F12`", "`Numpad0` … `Numpad9`"} {
		if !strings.Contains(doc, r) {
			t.Errorf("docs/scenario.md lacks range %s", r)
		}
	}
	for _, k := range KeyCodes {
		ranged := len(k) == 4 && strings.HasPrefix(k, "Key") ||
			len(k) == 6 && strings.HasPrefix(k, "Digit") ||
			len(k) == 7 && strings.HasPrefix(k, "Numpad") && k[6] >= '0' && k[6] <= '9' ||
			k[0] == 'F' && len(k) <= 3 && k[1] >= '0' && k[1] <= '9'
		if !ranged && !strings.Contains(doc, "`"+k+"`") {
			t.Errorf("docs/scenario.md does not list key %s", k)
		}
	}
}
