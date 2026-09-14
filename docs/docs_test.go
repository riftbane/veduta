package docs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
)

// TestDefaultsQuoted checks the manifest defaults the references quote against
// asset.DefaultProject, so a changed default cannot leave an agent reasoning about the
// old image size or tick rate.
func TestDefaultsQuoted(t *testing.T) {
	d := asset.DefaultProject
	for _, c := range []struct{ topic, text string }{
		{"project", fmt.Sprintf("| `resolution` | `[width, height]` | `[%d, %d]` |", d.Resolution[0], d.Resolution[1])},
		{"project", fmt.Sprintf("| `inspect_resolution` | `[width, height]` | `[%d, %d]` |", d.InspectResolution[0], d.InspectResolution[1])},
		{"project", fmt.Sprintf("| `tick_rate` | integer | `%d` |", d.TickRate)},
		{"inspect", fmt.Sprintf("(`inspect_resolution`, %d×%d by default)", d.InspectResolution[0], d.InspectResolution[1])},
	} {
		text, err := Get(c.topic)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, c.text) {
			t.Errorf("%s.md does not quote the default as %q", c.topic, c.text)
		}
	}
}

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
