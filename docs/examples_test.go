package docs_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/docs"
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
		asset.TypeScene:    "main.vscene",
		asset.TypeMaterial: "coin.vmat",
		asset.TypeScenario: "coin.vscenario",
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

// Every JSON example of the world and model topics compiles, so the formats an agent
// copies are valid.
func TestWorldAndModelExamplesCompile(t *testing.T) {
	for _, topic := range []string{"world", "model", "prefab"} {
		text, err := docs.Get(topic)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rest := text; ; {
			i := strings.Index(rest, "```json\n")
			if i < 0 {
				break
			}
			rest = rest[i+len("```json\n"):]
			j := strings.Index(rest, "\n```")
			block := rest[:j]
			rest = rest[j:]
			var h struct{ Veduta string }
			if json.Unmarshal([]byte(block), &h) != nil {
				continue // fragments and error examples
			}
			switch h.Veduta {
			case asset.TypeWorld:
				_, err = asset.ParseWorld("overworld.vworld", []byte(block), nil)
			case asset.TypePrefab:
				_, err = asset.ParsePrefab("house.vprefab", []byte(block))
			case asset.TypeScenario:
				_, err = asset.ParseScenario("walk.vscenario", []byte(block))
			default:
				continue
			}
			if err != nil {
				t.Errorf("%s example %d (%s): %v", topic, n, h.Veduta, err)
			}
			n++
		}
		if topic != "model" && n == 0 {
			t.Errorf("%s: no example compiled", topic)
		}
	}
}
