package cli

import (
	"encoding/json"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/sim"
)

// TestFuzzRepro: a random player's holds, cut at any tick, become scenario events a repro
// file holds, with every press before its release; and the same seed draws the same player.
func TestFuzzRepro(t *testing.T) {
	const ticks = 400
	g := genGame(sim.NewRNG(gameSeed(7, 0)), ticks, asset.ButtonNames)
	if len(g.holds) < 5 {
		t.Fatalf("%d holds in %d ticks", len(g.holds), ticks)
	}
	if again := genGame(sim.NewRNG(gameSeed(7, 0)), ticks, asset.ButtonNames); len(again.holds) != len(g.holds) || again.holds[0] != g.holds[0] {
		t.Fatal("the same seed drew a different player")
	}
	limit := g.holds[len(g.holds)/2].Start
	events := trimGame(g, limit).events(limit)
	data, err := json.Marshal(asset.ScenarioSource{Veduta: asset.TypeScenario, Scene: "main", Ticks: limit, Inputs: events})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := asset.ParseScenario("fuzz.scenario.json", data)
	if err != nil {
		t.Fatalf("the repro does not parse: %v\n%s", err, data)
	}
	presses := 0
	for _, in := range sc.Inputs {
		presses += len(in.Press)
	}
	if want := len(g.holds)/2 + 1; presses != want {
		t.Fatalf("%d presses up to tick %d, want %d", presses, limit, want)
	}
}
