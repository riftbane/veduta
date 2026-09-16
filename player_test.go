package veduta

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/platform"
	"github.com/riftbane/veduta/v2/sim"
)

// fakeWindow hands the player one batch of events per poll and keeps the frames it gets.
type fakeWindow struct {
	polls   [][]platform.Event
	frames  int
	lastImg *gfx.Image
}

func (w *fakeWindow) Poll() ([]platform.Event, error) {
	if len(w.polls) == 0 {
		return []platform.Event{{Kind: platform.Close}}, nil
	}
	evs := w.polls[0]
	w.polls = w.polls[1:]
	return evs, nil
}

func (w *fakeWindow) Present(img *gfx.Image) error { w.frames++; w.lastImg = img; return nil }
func (w *fakeWindow) Size() (int, int)             { return 64, 48 }
func (w *fakeWindow) Close() error                 { return nil }

// TestPlayerRecordsAScenario: F5 restarts the game and records the buttons, F5 again saves
// them as a scenario that parses, with every button at the tick it reached the game.
func TestPlayerRecordsAScenario(t *testing.T) {
	p, a := testAssets()
	p.TickRate = 1000 // a millisecond per tick: the test does not wait
	dir := t.TempDir()
	press := func(b sim.Button) platform.Event { return platform.Event{Kind: platform.Press, Button: b} }
	release := func(b sim.Button) platform.Event { return platform.Event{Kind: platform.Release, Button: b} }
	win := &fakeWindow{polls: [][]platform.Event{
		{press(sim.ButtonDown)}, // before the recording: forgotten by the restart
		{{Kind: platform.Tool, Tool: platform.ToolRecord}},
		nil,
		{press(sim.ButtonRight)},
		nil, nil,
		{press(sim.ButtonUp), release(sim.ButtonRight)},
		{press(sim.ButtonA), release(sim.ButtonA)}, // a tap within one tick
		nil, nil,
		{release(sim.ButtonUp)},
		nil,
		{{Kind: platform.Tool, Tool: platform.ToolOverlay}},
		{{Kind: platform.Tool, Tool: platform.ToolRecord}},
	}}
	old := openWindow
	openWindow = func(platform.Options) (platform.Window, error) { return win, nil }
	t.Cleanup(func() { openWindow = old })

	var log strings.Builder
	if err := runPlayer(&testGame{}, p, a, dir, &log); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "tests", "scenarios", "recorded-*.scenario.json"))
	if len(files) != 1 {
		t.Fatalf("recordings %v; log:\n%s", files, log.String())
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	sc, err := asset.ParseScenario(files[0], data)
	if err != nil {
		t.Fatalf("the recording does not parse: %v\n%s", err, data)
	}
	// The restart is tick 0; its poll's update is tick 1, so the press two polls later
	// reaches the game at tick 3.
	want := `"inputs": [
    { "tick": 3, "press": ["right"] },
    { "tick": 6, "press": ["up"], "release": ["right"] },
    { "tick": 7, "press": ["a"] },
    { "tick": 8, "release": ["a"] },
    { "tick": 10, "release": ["up"] }
  ]`
	if !strings.Contains(string(data), want) || sc.Ticks != 12 || sc.Scene != "main" {
		t.Fatalf("recording:\n%s\nwant inputs\n%s", data, want)
	}
	if !strings.Contains(log.String(), "saved tests/scenarios/recorded-") {
		t.Errorf("log: %s", log.String())
	}
	if win.frames == 0 {
		t.Error("no frame presented")
	}
}

// TestPlayerReload: F9 reloads a Reloader game and restarts it; a reload that fails keeps
// the game running and says why.
func TestPlayerReload(t *testing.T) {
	p, a := testAssets()
	p.TickRate = 1000
	old, oldLoad := openWindow, loadProjectFunc
	t.Cleanup(func() { openWindow, loadProjectFunc = old, oldLoad })
	win := &fakeWindow{polls: [][]platform.Event{nil, {{Kind: platform.Tool, Tool: platform.ToolReload}}, nil,
		{{Kind: platform.Tool, Tool: platform.ToolReload}}, nil}}
	openWindow = func(platform.Options) (platform.Window, error) { return win, nil }
	loads := 0
	loadProjectFunc = func(string) (*asset.Project, *Assets, error) { loads++; return p, a, nil }
	g := &reloadingGame{fail: []error{nil, errors.New("main.lua:3: broken")}}
	var log strings.Builder
	if err := runPlayer(g, p, a, t.TempDir(), &log); err != nil {
		t.Fatal(err)
	}
	if g.reloads != 2 || g.inits != 2 || loads != 1 {
		t.Fatalf("reloads %d, inits %d, loads %d; log:\n%s", g.reloads, g.inits, loads, log.String())
	}
	if !strings.Contains(log.String(), "veduta: reloaded") || !strings.Contains(log.String(), "reload: main.lua:3: broken") {
		t.Fatalf("log:\n%s", log.String())
	}
}

type reloadingGame struct {
	testGame
	fail    []error
	reloads int
	inits   int
}

func (g *reloadingGame) Init(ctx *Context) error { g.inits++; return g.testGame.Init(ctx) }

func (g *reloadingGame) Reload() error {
	err := g.fail[g.reloads]
	g.reloads++
	return err
}
