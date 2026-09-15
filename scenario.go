package veduta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/sim"
)

// loadScenario reads and validates a scenario file.
func loadScenario(path string) (*scenarioSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc, err := asset.ParseScenario(path, data)
	if err != nil {
		return nil, err
	}
	return specFromScenario(sc), nil
}

// specFromScenario converts a compiled scenario into a runnable spec.
func specFromScenario(sc *asset.Scenario) *scenarioSpec {
	spec := &scenarioSpec{
		Name: sc.Name, Scene: sc.Scene, World: sc.World, At: sc.At, Seed: sc.Seed, Ticks: sc.Ticks,
		Inputs:      inputEvents(sc.Inputs),
		Invariants:  sc.Invariants,
		Screenshots: sc.Screenshots,
	}
	for _, x := range sc.Expect {
		spec.Expect = append(spec.Expect, sim.Expectation{
			Tick: uint64(x.Tick), Entity: x.Entity, Path: x.Path, Op: x.Op, Value: x.Value,
			Trace: x.Trace, CountMin: x.CountMin, CountMax: x.CountMax,
		})
	}
	return spec
}

func inputEvents(in []asset.Input) []sim.InputEvent {
	out := make([]sim.InputEvent, len(in))
	for i, e := range in {
		out[i] = sim.InputEvent{Tick: uint64(e.Tick), Press: e.Press, Release: e.Release, Mouse: e.Mouse, Buttons: e.Buttons, Text: e.Text, Stick: e.Stick}
	}
	return out
}

// loadInputFile reads an input script: a JSON array of input events (the "inputs" format
// of scenarios), or a scenario file whose inputs are used.
func loadInputFile(path string) ([]sim.InputEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		sc, err := asset.ParseScenario(path, data)
		if err != nil {
			return nil, err
		}
		return inputEvents(sc.Inputs), nil
	}
	return parseInputArray(path, data)
}

// parseInputArray validates a bare input array by wrapping it in a scenario, so the
// rules (and the located error messages) are exactly those of scenario inputs.
func parseInputArray(path string, data []byte) ([]sim.InputEvent, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &asset.SourceError{File: path, Msg: "input script must be a JSON array of input events or a scenario: " + err.Error()}
	}
	wrapped := []byte(fmt.Sprintf(`{"veduta":%q,"scene":"input","ticks":%d,"inputs":%s}`, asset.TypeScenario, asset.MaxTicks, data))
	sc, err := asset.ParseScenario("input.scenario.json", wrapped)
	if err != nil {
		return nil, fmt.Errorf("input script %s: %w", path, err)
	}
	return inputEvents(sc.Inputs), nil
}
