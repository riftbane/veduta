package docs

import "testing"

// TestTopicsResolve checks that every listed topic has a file behind it and that All
// reports it, so a topic can never be offered without a reference to serve.
func TestTopicsResolve(t *testing.T) {
	listed := map[string]bool{}
	for _, name := range All() {
		listed[name] = true
	}
	for _, topic := range append(append([]string{}, Topics...), Extra...) {
		text, err := Get(topic)
		if err != nil || len(text) == 0 {
			t.Errorf("Get(%q) = %d bytes, %v", topic, len(text), err)
		}
		if !listed[topic] {
			t.Errorf("All() does not list %q", topic)
		}
	}
	if _, err := Get("nightly"); err == nil {
		t.Error("an unknown topic was accepted")
	}
}
