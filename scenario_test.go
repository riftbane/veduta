package veduta

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/sim"
)

func TestLoadScenarioFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "move.scenario.json")
	src := `{
  "veduta": "scenario/1", "scene": "main", "seed": 42, "ticks": 300,
  "inputs": [ {"tick": 10, "press": ["up"]}, {"tick": 70, "release": ["up"]} ],
  "expect": [
    {"tick": 120, "entity": "player", "path": "position.z", "op": "<", "value": -1},
    {"tick": 300, "trace": "gem_collected", "count_min": 1}
  ],
  "invariants": ["finite_positions", "entity_count_max:500"],
  "screenshots": [0, 60, 120, 300]
}`
	os.WriteFile(path, []byte(src), 0o644)
	spec, err := loadScenario(path)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "move" || spec.Ticks != 300 || len(spec.Inputs) != 2 || spec.Inputs[1].Tick != 70 || len(spec.Expect) != 2 || spec.Expect[0].Value != -1.0 {
		t.Fatalf("spec %+v", spec)
	}
	events, err := loadInputFile(path)
	if err != nil || len(events) != 2 {
		t.Fatalf("scenario as input file: %v %v", events, err)
	}
}

func TestLoadInputArray(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "in.json")
	os.WriteFile(good, []byte(`[{"tick": 1, "press": ["a", "left"]}, {"tick": 5, "release": ["a"]}]`), 0o644)
	events, err := loadInputFile(good)
	if err != nil || len(events) != 2 || events[0].Press != sim.Of(sim.ButtonA, sim.ButtonLeft) || events[1].Release != sim.Of(sim.ButtonA) {
		t.Fatalf("events %+v err %v", events, err)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`[{"tick": 1, "press": ["KeyW"]}]`), 0o644)
	_, err = loadInputFile(bad)
	var list asset.Errors
	if !errors.As(err, &list) || !strings.Contains(err.Error(), `did you mean "up"`) {
		t.Fatalf("bad button error: %v", err)
	}
}
