package veduta

import (
	"errors"
	"io"
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
// before, when set, runs before each poll with its number, from 0: a test edits a file or
// looks at the last frame there.
type fakeWindow struct {
	polls   [][]platform.Event
	frames  int
	lastImg *gfx.Image
	before  func(poll int)
	polled  int
}

func (w *fakeWindow) Poll() ([]platform.Event, error) {
	if w.before != nil {
		w.before(w.polled)
	}
	w.polled++
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
	if err := runPlayer(&testGame{}, p, a, dir, &log, playOptions{}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "tests", "scenarios", "recorded-*.vscenario"))
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
	if err := runPlayer(g, p, a, t.TempDir(), &log, playOptions{}); err != nil {
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

// twoScenes adds a second scene, "other", to the test assets.
func twoScenes() (*asset.Project, *Assets) {
	p, a := testAssets()
	p.TickRate = 1000
	other := *a.Scenes["main"]
	other.Name = "other"
	a.Scenes["other"] = &other
	return p, a
}

// TestPlayerStartsWhereTold: -scene (or -world and -at) and -seed say where the run
// starts, and a recording names that place.
func TestPlayerStartsWhereTold(t *testing.T) {
	old := openWindow
	t.Cleanup(func() { openWindow = old })
	record := func() *fakeWindow {
		return &fakeWindow{polls: [][]platform.Event{{{Kind: platform.Tool, Tool: platform.ToolRecord}}, nil, {{Kind: platform.Tool, Tool: platform.ToolRecord}}}}
	}
	for _, tc := range []struct {
		opt  playOptions
		want string
	}{
		{playOptions{Scene: "other", Seed: 7}, "\"scene\": \"other\",\n  \"seed\": 7,"},
		{playOptions{World: "land", At: "1,-2"}, "\"world\": \"land\",\n  \"at\": [1, -2],\n  \"seed\": 1,"},
	} {
		p, a := twoScenes()
		dir := t.TempDir()
		win := record()
		openWindow = func(platform.Options) (platform.Window, error) { return win, nil }
		g := &sceneGame{}
		var log strings.Builder
		if err := runPlayer(g, p, a, dir, &log, tc.opt); err != nil {
			t.Fatal(err)
		}
		files, _ := filepath.Glob(filepath.Join(dir, "tests", "scenarios", "recorded-*.vscenario"))
		if len(files) != 1 {
			t.Fatalf("%+v: recordings %v; log:\n%s", tc.opt, files, log.String())
		}
		data, _ := os.ReadFile(files[0])
		if !strings.Contains(string(data), tc.want) {
			t.Fatalf("%+v: recording lacks %q:\n%s", tc.opt, tc.want, data)
		}
	}
	p, a := twoScenes()
	if err := runPlayer(&sceneGame{}, p, a, t.TempDir(), io.Discard, playOptions{Scene: "nope"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown scene: %v", err)
	}
}

// sceneGame loads the scene "other" at tick 2 and notes the scene each run starts in.
type sceneGame struct {
	testGame
	starts  []string
	reloads int
	err     error
}

func (g *sceneGame) Init(ctx *Context) error {
	g.starts = append(g.starts, ctx.Scene.Name)
	return g.testGame.Init(ctx)
}

func (g *sceneGame) Update(ctx *Context, in Input) {
	if ctx.Tick == 2 && ctx.Scene.Name == "main" {
		if err := ctx.LoadScene("other"); err != nil {
			panic(err)
		}
	}
	g.testGame.Update(ctx, in)
}

func (g *sceneGame) Reload() error { g.reloads++; return nil }
func (g *sceneGame) Err() error    { return g.err }

// TestPlayerReloadsInPlace: a saved file reloads the game in the scene it is in; F9
// restarts it from the start.
func TestPlayerReloadsInPlace(t *testing.T) {
	p, a := twoScenes()
	dir := t.TempDir()
	t.Setenv(watchEnv, "1")
	old, oldLoad, oldPeriod := openWindow, loadProjectFunc, watchPeriod
	t.Cleanup(func() { openWindow, loadProjectFunc, watchPeriod = old, oldLoad, oldPeriod })
	watchPeriod = 0
	loadProjectFunc = func(string) (*asset.Project, *Assets, error) { return p, a, nil }
	win := &fakeWindow{polls: [][]platform.Event{nil, nil, nil, nil, nil, nil, {{Kind: platform.Tool, Tool: platform.ToolReload}}, nil}}
	win.before = func(poll int) {
		if poll == 4 { // the game is in "other" since tick 2: a script is saved
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte("-- edited"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	openWindow = func(platform.Options) (platform.Window, error) { return win, nil }
	g := &sceneGame{}
	var log strings.Builder
	if err := runPlayer(g, p, a, dir, &log, playOptions{}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"main", "other", "main"}; strings.Join(g.starts, " ") != strings.Join(want, " ") || g.reloads != 2 {
		t.Fatalf("starts %v, reloads %d; log:\n%s", g.starts, g.reloads, log.String())
	}
	if !strings.Contains(log.String(), "veduta: reloaded in other") {
		t.Fatalf("log:\n%s", log.String())
	}
}

// TestPlayerHoldsOnAnError: a script game's error keeps the window open with the error
// over the last frame, and the next reload that works starts the game again.
func TestPlayerHoldsOnAnError(t *testing.T) {
	p, a := twoScenes()
	old, oldLoad := openWindow, loadProjectFunc
	t.Cleanup(func() { openWindow, loadProjectFunc = old, oldLoad })
	loadProjectFunc = func(string) (*asset.Project, *Assets, error) { return p, a, nil }
	g := &sceneGame{}
	win := &fakeWindow{polls: [][]platform.Event{nil, nil, nil, nil, nil, {{Kind: platform.Tool, Tool: platform.ToolReload}}, nil, nil}}
	reddish := func(img *gfx.Image) bool {
		c := img.Pix[(img.H-3)*img.W+3]
		return (c>>16)&0xff > (c>>8)&0xff+0x20
	}
	var held, shown int
	win.before = func(poll int) {
		switch {
		case poll == 1:
			g.err = errors.New("main.lua:3: boom")
		case poll >= 3 && poll <= 5: // stopped: frames keep coming, with the error panel
			held++
			if win.lastImg != nil && reddish(win.lastImg) {
				shown++
			}
		case poll == 6: // F9 reloaded: the script is fixed by then
			if win.lastImg != nil && reddish(win.lastImg) {
				t.Errorf("the error is still shown after the reload")
			}
		}
		if poll == 5 {
			g.err = nil
		}
	}
	openWindow = func(platform.Options) (platform.Window, error) { return win, nil }
	var log strings.Builder
	if err := runPlayer(g, p, a, t.TempDir(), &log, playOptions{}); err != nil {
		t.Fatal(err)
	}
	if held != 3 || shown != 3 || g.reloads != 1 || len(g.starts) != 2 {
		t.Fatalf("held %d, shown %d, reloads %d, starts %v; log:\n%s", held, shown, g.reloads, g.starts, log.String())
	}
	if !strings.Contains(log.String(), "veduta: error: main.lua:3: boom") || !strings.Contains(log.String(), "veduta: reloaded") {
		t.Fatalf("log:\n%s", log.String())
	}
	// A Go game cannot be reloaded: its error still ends the run.
	plain := &failingGame{}
	win = &fakeWindow{polls: [][]platform.Event{nil, nil, nil, nil}}
	openWindow = func(platform.Options) (platform.Window, error) { return win, nil }
	if err := runPlayer(plain, p, a, t.TempDir(), io.Discard, playOptions{}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("a Go game's error: %v", err)
	}
}

type failingGame struct{ testGame }

func (g *failingGame) Err() error { return errors.New("boom") }
