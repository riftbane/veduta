package asset

import (
	"os"
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

func TestButtonNames(t *testing.T) {
	if len(ButtonNames) != 8 {
		t.Fatalf("%d buttons, want 8", len(ButtonNames))
	}
	for i, b := range ButtonNames {
		if ButtonIndex(b) != i || !IsButton(b) {
			t.Errorf("ButtonIndex(%q) = %d, want %d", b, ButtonIndex(b), i)
		}
	}
	for _, s := range []string{"", "A", "Up", "home", "start", "ArrowUp", "KeyW"} {
		if IsButton(s) {
			t.Errorf("IsButton(%q) = true", s)
		}
	}
}

func TestSuggestButton(t *testing.T) {
	cases := map[string]string{
		"A": "a", "Up": "up", "ArrowUp": "up", "KeyW": "up", "ArrowLeft": "left", "Space": "a",
		"ShiftLeft": "b", "Tab": "select", "Enter": "select", "Escape": "cancel", "home": "", "jump": "",
	}
	for in, want := range cases {
		if got := suggestButton(in); got != want {
			t.Errorf("suggestButton(%q) = %q, want %q", in, got, want)
		}
	}
}

// docs/scenario.md names every button.
func TestButtonsDocumented(t *testing.T) {
	doc := string(mustRead(t, "../docs/scenario.md"))
	for _, b := range ButtonNames {
		if !strings.Contains(doc, "`"+b+"`") {
			t.Errorf("docs/scenario.md does not name button %s", b)
		}
	}
}
