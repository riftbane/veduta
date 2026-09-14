package docs_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/docs"
)

// Every JSON example of the 2d topic compiles with the parser its "veduta" header names,
// so the recipe an agent copies is valid.
func TestTwoDExamplesCompile(t *testing.T) {
	text, err := docs.Get("2d")
	if err != nil {
		t.Fatal(err)
	}
	parsers := map[string]func(file string, data []byte) error{
		asset.TypeScene:    func(f string, d []byte) error { _, err := asset.ParseScene(f, d); return err },
		asset.TypeMaterial: func(f string, d []byte) error { _, err := asset.ParseMaterial(f, d); return err },
		asset.TypeScenario: func(f string, d []byte) error { _, err := asset.ParseScenario(f, d); return err },
	}
	files := map[string]string{
		asset.TypeScene:    "main.scene.json",
		asset.TypeMaterial: "coin.mat.json",
		asset.TypeScenario: "coin.scenario.json",
	}
	n := 0
	for rest := text; ; {
		i := strings.Index(rest, "```json\n")
		if i < 0 {
			break
		}
		rest = rest[i+len("```json\n"):]
		j := strings.Index(rest, "\n```")
		if j < 0 {
			t.Fatal("unterminated json block")
		}
		block := rest[:j]
		rest = rest[j:]
		var h struct{ Veduta string }
		if err := json.Unmarshal([]byte(block), &h); err != nil {
			t.Errorf("example %d is not JSON: %v\n%s", n, err, block)
			continue
		}
		parse, ok := parsers[h.Veduta]
		if !ok {
			t.Errorf("example %d has no parser for header %q", n, h.Veduta)
			continue
		}
		if err := parse(files[h.Veduta], []byte(block)); err != nil {
			t.Errorf("example %d (%s): %v", n, h.Veduta, err)
		}
		n++
	}
	if n < 4 {
		t.Fatalf("found %d JSON examples, want at least 4", n)
	}
}
