package veduta

import (
	"fmt"
	"runtime"
	"sort"
	"time"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/sim"
)

// BenchStats summarizes per-tick durations in milliseconds.
type BenchStats struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	Max  float64 `json:"max"`
}

// benchCount summarizes a per-frame count.
type benchCount struct {
	Mean float64 `json:"mean"`
	Max  int     `json:"max"`
}

// benchResult is the report of bench.
type benchResult struct {
	OK       bool      `json:"ok"`
	Scenario string    `json:"scenario,omitempty"`
	Scene    string    `json:"scene,omitempty"`
	World    string    `json:"world,omitempty"`
	At       *[2]int32 `json:"at,omitempty"`
	Seed     uint64    `json:"seed"`
	Ticks    int       `json:"ticks"`
	Width    int       `json:"width"`
	Height   int       `json:"height"`
	CPUs     int       `json:"cpus"`
	GOARCH   string    `json:"goarch"`
	// BudgetMS is one tick at the project's tick rate: the player updates and renders once
	// per tick, so a frame slower than this drops the rate.
	BudgetMS   float64    `json:"budget_ms"`
	UpdateMS   BenchStats `json:"update_ms"` // Game.Update, behaviours, physics, invariants
	RenderMS   BenchStats `json:"render_ms"` // scene and HUD draw lists, rasterization
	FrameMS    BenchStats `json:"frame_ms"`  // both
	OverBudget int        `json:"over_budget"`
	Slowest    int        `json:"slowest_tick"`
	Triangles  benchCount `json:"triangles"` // submitted per frame
	Drawn      benchCount `json:"drawn"`     // rasterized per frame
	Culled     benchCount `json:"culled"`    // entities outside the view per frame
	Reduced    benchCount `json:"reduced"`   // entities drawn at a lower level of detail per frame
}

func stats(ms []float64) BenchStats {
	if len(ms) == 0 {
		return BenchStats{}
	}
	s := append([]float64(nil), ms...)
	sort.Float64s(s)
	sum := 0.0
	for _, v := range s {
		sum += v
	}
	at := func(q float64) float64 { return round3(s[min(len(s)-1, int(q*float64(len(s))))]) }
	return BenchStats{Mean: round3(sum / float64(len(s))), P50: at(0.5), P95: at(0.95), Max: round3(s[len(s)-1])}
}

func round3(v float64) float64 { return float64(int64(v*1000+0.5)) / 1000 }

func count(n []int) benchCount {
	c := benchCount{}
	for _, v := range n {
		c.Mean += float64(v)
		c.Max = max(c.Max, v)
	}
	if len(n) > 0 {
		c.Mean = round3(c.Mean / float64(len(n)))
	}
	return c
}

// bench runs a scenario (or a scene with an input script) the way the player does,
// without recording a trace, and times every tick's update and its frame rendered with the
// scene camera at the project's resolution. Timings measure this machine, not the console.
func (h *headless) bench(args []string) error {
	fs := h.flags("bench")
	scenario := fs.String("scenario", "", "scenario file (its inputs and ticks)")
	sceneName := fs.String("scene", "", "scene (without --scenario; default: the project's default world or scene)")
	worldName := fs.String("world", "", "world instead of a scene (without --scenario)")
	at := fs.String("at", "", "with --world: start cell x,z (default 0,0)")
	ticks := fs.Int("ticks", 200, "ticks to run (without --scenario)")
	seed := fs.Uint64("seed", h.project.DefaultSeed, "RNG seed (without --scenario)")
	input := fs.String("input", "", "input script file (without --scenario)")
	width := fs.Int("width", h.project.Resolution[0], "frame width")
	height := fs.Int("height", h.project.Resolution[1], "frame height")
	cpus := fs.Int("cpus", runtime.GOMAXPROCS(0), "processors the renderer may use (the console has 4)")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *cpus < 1 {
		return usageError{fmt.Errorf("bench: --cpus must be at least 1")}
	}
	var spec *scenarioSpec
	if *scenario != "" {
		var err error
		if spec, err = loadScenarioFunc(*scenario); err != nil {
			return err
		}
	} else {
		if *ticks < 1 {
			return usageError{fmt.Errorf("bench: --ticks must be at least 1")}
		}
		target, err := h.target(*sceneName, *worldName, *at)
		if err != nil {
			return err
		}
		spec = &scenarioSpec{Scene: target.Scene, World: target.World, At: target.At, Seed: *seed, Ticks: *ticks}
		if *input != "" {
			events, err := loadInputFunc(*input)
			if err != nil {
				return err
			}
			spec.Inputs = events
		}
	}
	script, err := sim.NewScript(spec.Inputs)
	if err != nil {
		return err
	}
	prev := runtime.GOMAXPROCS(*cpus)
	defer runtime.GOMAXPROCS(prev)
	e := newEngine(h.game, h.project, h.assets)
	opt := spec.target()
	opt.Seed, opt.Headless = spec.Seed, true
	if err := e.prepare(opt); err != nil {
		return err
	}
	defer e.close()
	w, hgt := *width, *height
	if _, err := e.render(e.ctx.Scene.Camera, w, hgt, gfx.ModeColor, false); err != nil { // uploads, not timed
		return err
	}
	res := &benchResult{OK: true, Scenario: spec.Name, Scene: spec.Scene, World: spec.World, Seed: spec.Seed, Ticks: spec.Ticks,
		Width: w, Height: hgt, CPUs: *cpus, GOARCH: runtime.GOARCH, BudgetMS: round3(1000 / float64(h.project.TickRate))}
	if spec.World != "" {
		a := spec.At
		res.At = &a
	}
	var update, render, frame []float64
	var tris, drawn, culled, reduced []int
	slowest := 0.0
	for t := 1; t <= spec.Ticks; t++ {
		t0 := time.Now()
		if err := e.step(script.Input(uint64(t))); err != nil {
			return err
		}
		t1 := time.Now()
		f, err := e.render(e.ctx.Scene.Camera, w, hgt, gfx.ModeColor, false)
		if err != nil {
			return err
		}
		t2 := time.Now()
		u, r := float64(t1.Sub(t0).Microseconds())/1000, float64(t2.Sub(t1).Microseconds())/1000
		update, render, frame = append(update, u), append(render, r), append(frame, u+r)
		if u+r > res.BudgetMS {
			res.OverBudget++
		}
		if u+r > slowest {
			slowest, res.Slowest = u+r, t
		}
		tris, drawn = append(tris, f.Stats.Triangles), append(drawn, f.Stats.Drawn)
		culled, reduced = append(culled, f.Draw.Culled), append(reduced, f.Draw.Reduced)
	}
	res.UpdateMS, res.RenderMS, res.FrameMS = stats(update), stats(render), stats(frame)
	res.Triangles, res.Drawn, res.Culled, res.Reduced = count(tris), count(drawn), count(culled), count(reduced)
	writeJSON(h.stdout, res)
	return nil
}
