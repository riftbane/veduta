package cli

import (
	"encoding/json"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/sim"
)

// TestFuzzSticks: random players move the stick too, from a stream of their own, so the keys
// and the mouse of a seed are the games an older tool played; and a game's stick moves
// become scenario events a repro file can hold.
func TestFuzzSticks(t *testing.T) {
	const ticks = 400
	keys := genGame(sim.NewRNG(gameSeed(7, 0)), ticks, DefaultFuzzKeys, 320, 240)
	sticks := genSticks(sim.NewRNG(gameSeed(7, 0)^stickSalt), ticks)
	if len(sticks) == 0 {
		t.Fatal("no stick moves in 400 ticks")
	}
	if again := genSticks(sim.NewRNG(gameSeed(7, 0)^stickSalt), ticks); len(again) != len(sticks) || again[0] != sticks[0] {
		t.Fatal("the same seed drew different stick moves")
	}
	g := keys
	g.sticks = sticks
	limit := sticks[len(sticks)/2].Tick
	events := trimGame(g, limit).events(limit)
	data, err := json.Marshal(asset.ScenarioSource{Veduta: asset.TypeScenario, Scene: "main", Ticks: limit, Inputs: events})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := asset.ParseScenario("fuzz.scenario.json", data)
	if err != nil {
		t.Fatalf("the repro does not parse: %v\n%s", err, data)
	}
	n := 0
	for _, in := range sc.Inputs {
		if in.Stick != nil {
			n++
		}
	}
	if want := len(sticks)/2 + 1; n != want {
		t.Fatalf("%d stick events up to tick %d, want %d", n, limit, want)
	}
}
