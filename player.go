package veduta

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/internal/sheet"
	"github.com/riftbane/veduta/v2/platform"
	"github.com/riftbane/veduta/v2/sim"
	"github.com/riftbane/veduta/v2/sprite"
	"github.com/riftbane/veduta/v2/world"
)

// Reloader is implemented by a game that can read its code again while the player runs (a
// script game): the player calls Reload before restarting, on F9.
type Reloader interface {
	Reload() error
}

// player runs a game on the screen: one tick per 1/tick_rate seconds, one rendered frame
// per tick (no interpolation), input from the pad and the keyboard. A keyboard's tool keys
// work the player itself: F1 shows timings and triangles against the console's budget, F5
// restarts and records the buttons into a scenario (F5 again saves it), F9 reads scripts
// and assets again and restarts from the start. The simulator also watches the scripts and
// the asset sources: when one changes it reads everything again and restarts in place, in
// the scene (or the world cell) the game was in, so a level or a hud is edited while it
// shows. A script game's error does not close the window: the frame stays with the error
// over it until the next reload.
type player struct {
	game   Game
	proj   *asset.Project
	assets *Assets
	dir    string
	stderr io.Writer
	start  runOptions // where a run starts: the flags, or the project's defaults

	e     *engine
	input sim.InputState

	failed error      // the error the run stopped on, shown until a reload succeeds
	last   *gfx.Image // the last frame presented, shown while failed

	overlay     bool
	updateMs    float64
	renderMs    float64
	triangles   int
	notice      string
	noticeUntil time.Time

	rec   *recording
	stamp string // the scripts and assets as last seen, to reload when they change
}

// watchEnv makes the player watch its files on a machine that is not the simulator.
const watchEnv = "VEDUTA_WATCH"

// sourceStamp summarizes the files a reload reads again: every script and every asset
// source, by path, size and modification time.
func sourceStamp(dir string, p *asset.Project) (string, error) {
	var b strings.Builder
	cooked := filepath.Clean(filepath.Join(dir, p.Cooked))
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != dir && (strings.HasPrefix(name, ".") || name == "out" || name == "bin" || filepath.Clean(path) == cooked) {
				return filepath.SkipDir
			}
			return nil
		}
		// Scripts anywhere, asset sources and the manifest; not the scenarios, which a
		// recording writes.
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(path, ".lua") || rel == asset.ProjectFile ||
			strings.HasSuffix(path, ".json") && strings.HasPrefix(rel, p.Assets+"/") {
			info, err := d.Info()
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "%s %d %d\n", path, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return b.String(), err
}

// openWindow opens the player's screen; tests replace it.
var openWindow = platform.Open

// watchPeriod is how often the simulator looks for changed scripts and assets.
var watchPeriod = time.Second

// playOptions say where the player starts: a scene, or a world and its start cell, and
// the seed; empty means the project's defaults.
type playOptions struct {
	Scene string
	World string
	At    string // "x,z"
	Seed  uint64 // 0: the project's
}

// resolve turns the options into the run's start.
func (o playOptions) resolve(p *asset.Project) (runOptions, error) {
	opt, err := resolveTarget(p, o.Scene, o.World, o.At)
	if err != nil {
		return opt, err
	}
	opt.Seed = o.Seed
	if opt.Seed == 0 {
		opt.Seed = p.DefaultSeed
	}
	return opt, nil
}

// runPlayer puts the game on the screen and runs it until the player leaves (Home,
// Select+Start, Ctrl+Q, or closing the simulator window).
func runPlayer(g Game, p *asset.Project, a *Assets, dir string, stderr io.Writer, o playOptions) error {
	start, err := o.resolve(p)
	if err != nil {
		return err
	}
	win, err := openWindow(platform.Options{Title: p.Title, Width: p.Resolution[0], Height: p.Resolution[1]})
	if err != nil {
		return fmt.Errorf("%w (the player draws on a framebuffer, or on Windows in the simulator: it runs on a console, at a Linux text console or on Windows; use -headless to render and simulate)", err)
	}
	defer win.Close()
	pl := &player{game: g, proj: p, assets: a, dir: dir, stderr: stderr, start: start}
	if err := pl.restart(); err != nil {
		return err
	}
	// The simulator reloads by itself when a script or an asset changes; a console never
	// looks at its files again.
	watch := runtime.GOOS == "windows" || os.Getenv(watchEnv) == "1"
	if watch {
		pl.stamp, _ = sourceStamp(dir, p)
	}
	nextWatch := time.Now().Add(watchPeriod)
	defer func() { pl.e.close() }()
	period := time.Second / time.Duration(p.TickRate)
	next := time.Now()
	for {
		events, err := win.Poll()
		if err != nil {
			return err
		}
		for _, ev := range events {
			switch ev.Kind {
			case platform.Press:
				pl.input.Press(ev.Button)
				pl.record(ev)
			case platform.Release:
				pl.input.Release(ev.Button)
				pl.record(ev)
			case platform.FocusLost:
				pl.input.ReleaseAll()
			case platform.Close:
				return pl.stopRecording()
			case platform.Tool:
				if err := pl.tool(ev.Tool); err != nil {
					return err
				}
			}
		}
		if watch && time.Now().After(nextWatch) {
			nextWatch = time.Now().Add(watchPeriod)
			if stamp, err := sourceStamp(pl.dir, pl.proj); err == nil && stamp != pl.stamp {
				pl.stamp = stamp
				if err := pl.reload(true); err != nil {
					return err
				}
			}
		}
		if pl.failed != nil {
			// The run stopped: hold the last frame with the error over it, and keep
			// watching for the reload that fixes it.
			if w, h := win.Size(); w > 0 && h > 0 {
				img := pl.lastFrame(w, h)
				pl.drawOverlay(img)
				if err := win.Present(img); err != nil {
					return err
				}
			}
			next = pl.wait(next, period)
			continue
		}
		start := time.Now()
		if err := pl.e.step(pl.input.Next()); err != nil {
			if _, ok := pl.game.(Reloader); !ok {
				return err
			}
			pl.fail(err)
			continue
		}
		updated := time.Now()
		if w, h := win.Size(); w > 0 && h > 0 {
			cam, err := pl.e.ctx.Scene.CameraPreset("scene", float32(w)/float32(h))
			if err != nil {
				return err
			}
			f, err := pl.e.render(cam, w, h, gfx.ModeColor, false)
			if err != nil {
				return err
			}
			pl.updateMs = ms(updated.Sub(start))
			pl.renderMs = ms(time.Since(updated))
			pl.triangles = f.Stats.Triangles
			img := f.FB.Image()
			pl.last = &gfx.Image{W: img.W, H: img.H, Pix: slices.Clone(img.Pix)}
			pl.drawOverlay(img)
			if err := win.Present(img); err != nil {
				return err
			}
		}
		next = pl.wait(next, period)
	}
}

// wait holds the tick rate: it sleeps until next and returns the tick after it; when far
// behind (a stall), it skips ahead instead of racing.
func (pl *player) wait(next time.Time, period time.Duration) time.Time {
	next = next.Add(period)
	now := time.Now()
	if d := next.Sub(now); d > 0 {
		time.Sleep(d)
	} else if -d > 5*period {
		next = now
	}
	return next
}

// lastFrame is the frame to show while the run is stopped: the last one presented, or a
// dark one when none was.
func (pl *player) lastFrame(w, h int) *gfx.Image {
	if pl.last != nil {
		return &gfx.Image{W: pl.last.W, H: pl.last.H, Pix: slices.Clone(pl.last.Pix)}
	}
	img := gfx.NewImage(w, h)
	for i := range img.Pix {
		img.Pix[i] = sheet.Background
	}
	return img
}

// fail stops the run on err: the window stays, showing the error, until a reload succeeds.
func (pl *player) fail(err error) {
	pl.failed = err
	pl.rec = nil
	fmt.Fprintln(pl.stderr, "veduta: error:", err)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// restart starts the game from its first tick in a fresh engine, where the run started.
func (pl *player) restart() error { return pl.restartAt(pl.start) }

// restartAt starts the game from its first tick in a fresh engine, in opt's scene or
// world.
func (pl *player) restartAt(opt runOptions) error {
	if pl.e != nil {
		pl.e.close()
	}
	pl.e = newEngine(pl.game, pl.proj, pl.assets)
	pl.input = sim.InputState{}
	// The player has no trace reader: it prepares without recording, so no tick spends
	// time summarizing every entity (a streamed world has hundreds).
	opt.SaveDir = pl.saveDir()
	if err := pl.e.prepare(opt); err != nil {
		return err
	}
	return pl.e.endTick()
}

// here is where the game is now, for a reload in place: the scene it is in, or its world
// around the cell the focus is at (so an edit shows where the player stands), with the
// run's seed. Before a run got anywhere, it is the start.
func (pl *player) here() runOptions {
	opt := pl.start
	if pl.e == nil || pl.e.ctx.Scene == nil {
		return opt
	}
	opt.Scene, opt.World, opt.At = "", "", [2]int32{}
	if w := pl.e.world; w != nil {
		opt.World, opt.At = w.Name, w.FocusedCell()
	} else {
		opt.Scene = pl.e.ctx.Scene.Name
	}
	return opt
}

// saveDir is where the player keeps the game's saves: VEDUTA_SAVE_DIR, else out/saves in
// the project.
func (pl *player) saveDir() string {
	if d := os.Getenv(SaveDirEnv); d != "" {
		return d
	}
	return filepath.Join(pl.dir, "out", "saves")
}

// say shows a message over the frame for a few seconds and writes it to stderr.
func (pl *player) say(format string, args ...any) {
	pl.notice = fmt.Sprintf(format, args...)
	pl.noticeUntil = time.Now().Add(4 * time.Second)
	fmt.Fprintln(pl.stderr, "veduta:", pl.notice)
}

func (pl *player) tool(t platform.ToolKey) error {
	switch t {
	case platform.ToolOverlay:
		pl.overlay = !pl.overlay
	case platform.ToolRecord:
		if pl.rec != nil {
			return pl.stopRecording()
		}
		if err := pl.restart(); err != nil {
			if _, ok := pl.game.(Reloader); !ok {
				return err
			}
			pl.fail(fmt.Errorf("restart: %w", err))
			return nil
		}
		pl.failed = nil
		pl.rec = &recording{}
		pl.say("recording from tick 0: F5 saves the scenario")
	case platform.ToolReload:
		return pl.reload(false)
	}
	return nil
}

// reload reads the scripts and the assets again (stale sources are compiled in memory)
// and restarts: from the start, or in place, where the game is now. What fails to load
// or to start is shown over the frame, and the run stays stopped until a reload succeeds;
// the scripts and assets that loaded last keep being the ones that run.
func (pl *player) reload(inPlace bool) error {
	pl.rec = nil
	at := pl.start
	if inPlace {
		at = pl.here()
	}
	if r, ok := pl.game.(Reloader); ok {
		if err := r.Reload(); err != nil {
			pl.fail(fmt.Errorf("reload: %w", err))
			return nil
		}
	}
	p, a, err := loadProjectFunc(pl.dir)
	if err != nil {
		pl.fail(fmt.Errorf("reload: %w", err))
		return nil
	}
	pl.proj, pl.assets = p, a
	if err := pl.restartAt(at); err != nil {
		if _, ok := pl.game.(Reloader); !ok {
			return err
		}
		pl.fail(fmt.Errorf("restart: %w", err))
		return nil
	}
	pl.failed = nil
	if inPlace {
		pl.say("reloaded in %s", at.name())
	} else {
		pl.say("reloaded")
	}
	return nil
}

// recording is the buttons pressed and released since F5, by the tick they reach the game.
type recording struct {
	inputs []asset.InputSource
}

// record notes a button event for the tick the game is about to run.
func (pl *player) record(ev platform.Event) {
	if pl.rec == nil {
		return
	}
	tick := int(pl.e.tick) + 1
	in := &pl.rec.inputs
	name := ev.Button.String()
	if n := len(*in); n == 0 || (*in)[n-1].Tick != tick {
		*in = append(*in, asset.InputSource{Tick: tick})
	}
	last := &(*in)[len(*in)-1]
	if ev.Kind == platform.Press {
		if contains(last.Release, name) { // released and pressed again within one tick
			*in = append(*in, asset.InputSource{Tick: tick + 1})
			last = &(*in)[len(*in)-1]
		}
		last.Press = append(last.Press, name)
		return
	}
	if contains(last.Press, name) { // a tap within one tick: a scenario releases it a tick later
		*in = append(*in, asset.InputSource{Tick: tick + 1})
		last = &(*in)[len(*in)-1]
	}
	last.Release = append(last.Release, name)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// stopRecording saves the recording, if any, as tests/scenarios/recorded-<time>.scenario.json.
func (pl *player) stopRecording() error {
	rec := pl.rec
	if rec == nil {
		return nil
	}
	pl.rec = nil
	ticks := max(int(pl.e.tick), 1)
	name := "recorded-" + time.Now().Format("20060102-150405")
	path := filepath.Join(pl.dir, "tests", "scenarios", name+".scenario.json")
	var b strings.Builder
	fmt.Fprintf(&b, "{\n  \"veduta\": %q,\n", asset.TypeScenario)
	if pl.start.World != "" {
		fmt.Fprintf(&b, "  \"world\": %q,\n  \"at\": [%d, %d],\n", pl.start.World, pl.start.At[0], pl.start.At[1])
	} else {
		fmt.Fprintf(&b, "  \"scene\": %q,\n", pl.start.Scene)
	}
	fmt.Fprintf(&b, "  \"seed\": %d,\n  \"ticks\": %d,\n  \"inputs\": [", pl.start.Seed, ticks)
	n := 0
	for _, in := range rec.inputs {
		if in.Tick > ticks {
			break // after the last tick run
		}
		if n > 0 {
			b.WriteString(",")
		}
		n++
		fmt.Fprintf(&b, "\n    { \"tick\": %d", in.Tick)
		for _, part := range []struct {
			key   string
			names []string
		}{{"press", in.Press}, {"release", in.Release}} {
			if len(part.names) > 0 {
				fmt.Fprintf(&b, ", %q: [%s]", part.key, quoteList(part.names))
			}
		}
		b.WriteString(" }")
	}
	if n > 0 {
		b.WriteString("\n  ")
	}
	fmt.Fprintf(&b, "],\n  \"screenshots\": [%d]\n}\n", ticks)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		pl.say("saving the recording: %v", err)
		return nil
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		pl.say("saving the recording: %v", err)
		return nil
	}
	pl.say("saved tests/scenarios/%s.scenario.json (%d ticks)", name, ticks)
	return nil
}

func quoteList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(q, ", ")
}

// drawOverlay writes the overlay and the notice into the frame.
func (pl *player) drawOverlay(img *gfx.Image) {
	var lines []string
	if pl.overlay {
		budget := 1000 / float64(pl.proj.TickRate)
		lines = append(lines,
			fmt.Sprintf("UPDATE %5.2f ms  RENDER %5.2f ms", pl.updateMs, pl.renderMs),
			fmt.Sprintf("FRAME  %5.2f / %.0f ms", pl.updateMs+pl.renderMs, budget),
			fmt.Sprintf("TRIANGLES %d / %d", pl.triangles, world.Budget),
			fmt.Sprintf("TICK %d", pl.e.tick))
	}
	if pl.rec != nil {
		lines = append(lines, fmt.Sprintf("REC %d  (F5 saves)", pl.e.tick))
	}
	if pl.notice != "" && time.Now().Before(pl.noticeUntil) {
		lines = append(lines, strings.ToUpper(pl.notice))
	}
	for i, l := range lines {
		sheet.Label(img, 2, 2+i*10, 1, l)
	}
	if pl.failed != nil {
		drawError(img, pl.failed.Error())
	}
}

// Colors of the error panel.
const (
	errorBG = 0xd0501010
	errorFG = 0xfffff0e0
)

// drawError writes the error over the bottom of the frame: its lines wrapped to the
// width, as many as fit under a heading that says how to go on.
func drawError(img *gfx.Image, msg string) {
	f := sprite.DefaultFont()
	text := "ERROR (SAVE A FILE OR F9 RELOADS, HOME LEAVES)\n" + sprite.Wrap(f, strings.ReplaceAll(msg, "\t", "  "), img.W-8, 1)
	lines := strings.Split(text, "\n")
	if maxLines := max((img.H/2-8)/f.CellH, 2); len(lines) > maxLines {
		lines = append(lines[:maxLines-1], "...")
	}
	h := len(lines)*f.CellH + 8
	sheet.FillRect(img, 0, img.H-h, img.W, h, errorBG)
	sheet.Text(img, 4, img.H-h+4, 1, strings.Join(lines, "\n"), errorFG)
}
