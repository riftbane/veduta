package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/asset"
)

// GameError is a failed headless command of the game binary.
type GameError struct {
	Msg    string
	Errors []*asset.SourceError
}

func (e *GameError) Error() string { return e.Msg }

// Unwrap exposes located errors to sourceErrors.
func (e *GameError) Unwrap() error {
	if len(e.Errors) == 0 {
		return nil
	}
	return asset.Errors(e.Errors)
}

// runGame builds the game and runs `game -project ROOT -headless args…`, returning the
// JSON report it prints and its exit code (0 ok, 3 simulate verdict fail).
func (s *Session) runGame(args ...string) (map[string]any, int, error) {
	bin, err := s.ensureGame()
	if err != nil {
		return nil, 0, err
	}
	return s.runGameBin(bin, args...)
}

// runGameBin runs an already built game binary (see runGame).
func (s *Session) runGameBin(bin string, args ...string) (map[string]any, int, error) {
	cmd := exec.Command(bin, append([]string{"-project", s.Root, "-headless"}, args...)...)
	cmd.Dir = s.Root
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		var ee *exec.ExitError
		if !errors.As(runErr, &ee) {
			return nil, 0, fmt.Errorf("run game: %w", runErr)
		}
		code = ee.ExitCode()
	}
	rep, perr := lastJSON(out.Bytes())
	if perr != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		return nil, code, fmt.Errorf("game -headless %s: exit %d: %s", args[0], code, tail(msg, 2000))
	}
	if ok, _ := rep["ok"].(bool); !ok {
		ge := &GameError{Msg: fmt.Sprint(rep["error"])}
		if raw, err := json.Marshal(rep["errors"]); err == nil {
			var list []*asset.SourceError
			if json.Unmarshal(raw, &list) == nil {
				ge.Errors = list
			}
		}
		return nil, code, ge
	}
	return rep, code, nil
}

// lastJSON decodes the last non-empty line of out as a JSON object.
func lastJSON(out []byte) (map[string]any, error) {
	var last []byte
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 64<<20)
	for sc.Scan() {
		if l := bytes.TrimSpace(sc.Bytes()); len(l) > 0 {
			last = append(last[:0], l...)
		}
	}
	if last == nil {
		return nil, errors.New("no output")
	}
	var m map[string]any
	if err := json.Unmarshal(last, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// RenderOptions are the flags of render (spec §10).
type RenderOptions struct {
	Scene  string
	World  string    // world instead of a scene
	At     *[2]int32 // the world's start cell
	Tick   int
	Seed   uint64 // 0: project default
	Camera string
	Mode   string
	Width  int
	Height int
	Out    string // PNG path; "" = out/render_<scene>_t<tick>_<mode>.png
	Bundle bool
	Input  string // input script file
}

// Render renders one frame headless through the game binary.
func (s *Session) Render(o RenderOptions) (map[string]any, error) {
	args := []string{"render"}
	args = append(args, targetArgs(o.Scene, o.World, o.At)...)
	args = append(args, "--tick", strconv.Itoa(o.Tick))
	if o.Seed != 0 {
		args = append(args, "--seed", strconv.FormatUint(o.Seed, 10))
	}
	if o.Camera != "" {
		args = append(args, "--camera", o.Camera)
	}
	if o.Mode != "" {
		args = append(args, "--mode", o.Mode)
	}
	if o.Width > 0 {
		args = append(args, "--width", strconv.Itoa(o.Width))
	}
	if o.Height > 0 {
		args = append(args, "--height", strconv.Itoa(o.Height))
	}
	if o.Out != "" {
		args = append(args, "--out", absFrom(o.Out))
	}
	if o.Bundle {
		args = append(args, "--bundle")
	}
	if o.Input != "" {
		args = append(args, "--input", absFrom(o.Input))
	}
	rep, _, err := s.runGame(args...)
	if err != nil {
		return nil, err
	}
	s.relPaths(rep, "out", "bundle")
	return rep, nil
}

// targetArgs are the --scene, --world and --at flags of a game command.
func targetArgs(scene, world string, at *[2]int32) []string {
	var args []string
	if scene != "" {
		args = append(args, "--scene", scene)
	}
	if world != "" {
		args = append(args, "--world", world)
	}
	if at != nil {
		args = append(args, "--at", strconv.Itoa(int(at[0]))+","+strconv.Itoa(int(at[1])))
	}
	return args
}

// relPaths rewrites absolute paths under the project root as project-relative paths.
func (s *Session) relPaths(rep map[string]any, keys ...string) {
	for _, k := range keys {
		if p, ok := rep[k].(string); ok && p != "" {
			if !filepath.IsAbs(p) {
				p = filepath.Join(s.Root, p)
			}
			rep[k] = s.Rel(p)
		}
	}
}

// SimulateOptions are the flags of simulate (spec §10).
type SimulateOptions struct {
	Scenario    string // scenario file; or:
	Scene       string
	World       string    // world instead of a scene
	At          *[2]int32 // the world's start cell
	Ticks       int
	Seed        uint64
	Input       string          // input script file
	Inputs      json.RawMessage // inline input events (MCP); written to the run directory
	Screenshots []int
	Invariants  []string
	Width       int
}

// SimResult is the report of simulate: the game's report plus the run id.
type SimResult map[string]any

// ExitCode is 3 when the verdict is fail.
func (r SimResult) ExitCode() int {
	if r["verdict"] == "pass" {
		return 0
	}
	return 3
}

// Human summarizes the verdict.
func (r SimResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v: %v ticks, trace %v\n", r["verdict"], r["ticks"], r["trace_hash"])
	if f, ok := r["first_failure"].(string); ok && f != "" {
		fmt.Fprintf(&b, "first failure: %s\n", f)
	}
	if ex, ok := r["expectations"].([]any); ok {
		for _, e := range ex {
			m, _ := e.(map[string]any)
			mark := "ok  "
			if m["pass"] != true {
				mark = "FAIL"
			}
			if m["trace"] != nil {
				fmt.Fprintf(&b, "  %s tick %v: trace %v count %v\n", mark, m["tick"], m["trace"], m["actual"])
			} else {
				fmt.Fprintf(&b, "  %s tick %v: %v.%v %v %v (actual %v)\n", mark, m["tick"], m["entity"], m["path"], m["op"], m["value"], m["actual"])
			}
		}
	}
	if v, ok := r["violations"].([]any); ok {
		for _, x := range v {
			m, _ := x.(map[string]any)
			fmt.Fprintf(&b, "  violation tick %v: %v — %v\n", m["tick"], m["name"], m["detail"])
		}
	}
	fmt.Fprintf(&b, "run %v: %v\n", r["run_id"], r["sheet"])
	return b.String()
}

// scenarioPath accepts a scenario name ("collect") as well as a file path.
func (s *Session) scenarioPath(p string) string {
	if strings.ContainsAny(p, `/\`) || strings.HasSuffix(p, ".json") {
		return p
	}
	return filepath.Join(s.Root, "tests", "scenarios", p+".scenario.json")
}

// Simulate runs ticks through the game binary; results are kept under out/runs/<id>/.
func (s *Session) Simulate(o SimulateOptions) (SimResult, error) {
	name := o.Scene
	if o.Scenario != "" {
		o.Scenario = s.scenarioPath(o.Scenario)
		name = strings.TrimSuffix(filepath.Base(o.Scenario), ".scenario.json")
	}
	if name == "" {
		name = o.World
	}
	if name == "" {
		name = s.Project.DefaultWorld
	}
	if name == "" {
		name = s.Project.DefaultScene
	}
	id, dir, err := s.newRun(name)
	if err != nil {
		return nil, err
	}
	args := []string{"simulate", "--out", dir}
	if o.Scenario != "" {
		args = append(args, "--scenario", absFrom(o.Scenario))
	} else {
		args = append(args, targetArgs(o.Scene, o.World, o.At)...)
		if o.Ticks > 0 {
			args = append(args, "--ticks", strconv.Itoa(o.Ticks))
		}
		if o.Seed != 0 {
			args = append(args, "--seed", strconv.FormatUint(o.Seed, 10))
		}
		input := absFrom(o.Input)
		if len(o.Inputs) > 0 {
			input = filepath.Join(dir, "input.json")
			if err := os.WriteFile(input, o.Inputs, 0o644); err != nil {
				return nil, err
			}
		}
		if input != "" {
			args = append(args, "--input", input)
		}
	}
	if len(o.Screenshots) > 0 {
		var ts []string
		for _, t := range o.Screenshots {
			ts = append(ts, strconv.Itoa(t))
		}
		args = append(args, "--screenshots", strings.Join(ts, ","))
	}
	if len(o.Invariants) > 0 {
		args = append(args, "--invariants", strings.Join(o.Invariants, ","))
	}
	if o.Width > 0 {
		args = append(args, "--width", strconv.Itoa(o.Width))
	}
	rep, _, err := s.runGame(args...)
	if err != nil {
		return nil, err
	}
	rep["run_id"] = id
	s.relPaths(rep, "out", "trace", "sheet")
	return SimResult(rep), nil
}

// BenchOptions are the flags of bench.
type BenchOptions struct {
	Scenario string
	Scene    string
	World    string
	At       *[2]int32
	Ticks    int
	Seed     uint64
	Input    string
	Width    int
	Height   int
	CPUs     int
}

// BenchResult is the report of bench.
type BenchResult map[string]any

// Human summarizes the timings.
func (r BenchResult) Human() string {
	var b strings.Builder
	line := func(name string) {
		m, _ := r[name].(map[string]any)
		fmt.Fprintf(&b, "  %-10s mean %7.2f  p50 %7.2f  p95 %7.2f  max %7.2f ms\n", name, m["mean"], m["p50"], m["p95"], m["max"])
	}
	fmt.Fprintf(&b, "%v ticks at %vx%v on %v cpus (%v), budget %v ms per tick\n", r["ticks"], r["width"], r["height"], r["cpus"], r["goarch"], r["budget_ms"])
	line("update_ms")
	line("render_ms")
	line("frame_ms")
	tri, _ := r["triangles"].(map[string]any)
	drawn, _ := r["drawn"].(map[string]any)
	fmt.Fprintf(&b, "  over budget %v ticks, slowest tick %v; triangles mean %v max %v, drawn mean %v max %v\n",
		r["over_budget"], r["slowest_tick"], tri["mean"], tri["max"], drawn["mean"], drawn["max"])
	return b.String()
}

// Bench runs a scenario (or a scene) through the game the way the player does and times
// each tick's update and frame.
func (s *Session) Bench(o BenchOptions) (BenchResult, error) {
	args := []string{"bench"}
	if o.Scenario != "" {
		args = append(args, "--scenario", absFrom(s.scenarioPath(o.Scenario)))
	} else {
		args = append(args, targetArgs(o.Scene, o.World, o.At)...)
		if o.Ticks > 0 {
			args = append(args, "--ticks", strconv.Itoa(o.Ticks))
		}
		if o.Seed != 0 {
			args = append(args, "--seed", strconv.FormatUint(o.Seed, 10))
		}
		if o.Input != "" {
			args = append(args, "--input", absFrom(o.Input))
		}
	}
	for _, f := range []struct {
		name string
		v    int
	}{{"--width", o.Width}, {"--height", o.Height}, {"--cpus", o.CPUs}} {
		if f.v > 0 {
			args = append(args, f.name, strconv.Itoa(f.v))
		}
	}
	rep, _, err := s.runGame(args...)
	if err != nil {
		return nil, err
	}
	return BenchResult(rep), nil
}

// newRun allocates out/runs/<name>-<n>.
func (s *Session) newRun(name string) (id, dir string, err error) {
	runs := s.Out("runs")
	if err := os.MkdirAll(runs, 0o755); err != nil {
		return "", "", err
	}
	entries, _ := os.ReadDir(runs)
	n := 0
	for _, e := range entries {
		if rest, ok := strings.CutPrefix(e.Name(), name+"-"); ok {
			if k, err := strconv.Atoi(rest); err == nil && k > n {
				n = k
			}
		}
	}
	id = fmt.Sprintf("%s-%d", name, n+1)
	dir = filepath.Join(runs, id)
	return id, dir, os.MkdirAll(dir, 0o755)
}

// TraceSlice is a page of a previous run's trace.
type TraceSlice struct {
	RunID    string           `json:"run_id"`
	FromTick int              `json:"from_tick"`
	ToTick   int              `json:"to_tick"`
	LastTick int              `json:"last_tick"`
	Ticks    []map[string]any `json:"ticks"`
	More     bool             `json:"more"`
}

// MaxTraceTicks bounds one trace page (spec §11).
const MaxTraceTicks = 200

// Trace returns ticks [from, to] of run id (at most MaxTraceTicks), optionally keeping
// only the named events (entities are dropped then, to keep pages small).
func (s *Session) Trace(runID string, from, to int, events []string) (*TraceSlice, error) {
	if strings.ContainsAny(runID, `/\`) || runID == "" || strings.Contains(runID, "..") {
		return nil, fmt.Errorf("invalid run id %q", runID)
	}
	f, err := os.Open(s.Out("runs", runID, "trace.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("run %s: %w (run simulate first)", runID, err)
	}
	defer f.Close()
	if to < from {
		to = from
	}
	if to-from+1 > MaxTraceTicks {
		to = from + MaxTraceTicks - 1
	}
	keep := map[string]bool{}
	for _, e := range events {
		keep[e] = true
	}
	out := &TraceSlice{RunID: runID, FromTick: from, ToTick: to, Ticks: []map[string]any{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 256<<20)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, err
		}
		t := int(asFloat(rec["tick"]))
		out.LastTick = t
		if t < from || t > to {
			continue
		}
		if len(keep) > 0 {
			var evs []any
			for _, e := range asSlice(rec["events"]) {
				if m, ok := e.(map[string]any); ok && keep[fmt.Sprint(m["event"])] {
					evs = append(evs, m)
				}
			}
			if len(evs) == 0 {
				continue
			}
			rec = map[string]any{"tick": rec["tick"], "events": evs}
		}
		out.Ticks = append(out.Ticks, rec)
	}
	out.More = out.LastTick > to
	return out, sc.Err()
}

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func init() {
	register(command{
		name:    "render",
		usage:   "render --scene S [--tick T] [--seed N] [--camera preset] [--mode M] [--width W --height H] [--out f.png] [--bundle] [--input script.json]",
		summary: "render one frame headless through the game (PNG, optional .vframe bundle)",
		project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("render", env.Stderr)
			var o RenderOptions
			var at string
			fs.StringVar(&o.Scene, "scene", "", "scene (default: the project's default world or scene)")
			fs.StringVar(&o.World, "world", "", "world instead of a scene")
			fs.StringVar(&at, "at", "", "with --world: start cell x,z")
			fs.IntVar(&o.Tick, "tick", 0, "ticks to simulate first")
			fs.Uint64Var(&o.Seed, "seed", 0, "RNG seed (default: the project's)")
			fs.StringVar(&o.Camera, "camera", "scene", "camera preset")
			fs.StringVar(&o.Mode, "mode", "color", "render mode")
			fs.IntVar(&o.Width, "width", 0, "width (default: inspect_resolution)")
			fs.IntVar(&o.Height, "height", 0, "height")
			fs.StringVar(&o.Out, "out", "", "output PNG")
			fs.BoolVar(&o.Bundle, "bundle", false, "also write a .vframe bundle")
			fs.StringVar(&o.Input, "input", "", "input script applied before the frame")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			var err error
			if o.At, err = parseCellFlag(at); err != nil {
				return nil, err
			}
			return s.Render(o)
		},
	})
	register(command{
		name:    "bench",
		usage:   "bench --scenario F | --scene S | --world W --at x,z  --ticks N --seed N [--input script.json] [--width W --height H] [--cpus N]",
		summary: "time every tick's update and frame the way the player runs them: mean, p50, p95 and max in ms against the tick budget, triangles per frame",
		project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("bench", env.Stderr)
			var o BenchOptions
			var at string
			fs.StringVar(&o.Scenario, "scenario", "", "scenario name (tests/scenarios/<name>.scenario.json) or file")
			fs.StringVar(&o.Scene, "scene", "", "scene")
			fs.StringVar(&o.World, "world", "", "world instead of a scene")
			fs.StringVar(&at, "at", "", "with --world: start cell x,z")
			fs.IntVar(&o.Ticks, "ticks", 200, "ticks")
			fs.Uint64Var(&o.Seed, "seed", 0, "seed")
			fs.StringVar(&o.Input, "input", "", "input script file")
			fs.IntVar(&o.Width, "width", 0, "frame width (default: the project's resolution)")
			fs.IntVar(&o.Height, "height", 0, "frame height")
			fs.IntVar(&o.CPUs, "cpus", 0, "processors the renderer may use (the console has 4)")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			var err error
			if o.At, err = parseCellFlag(at); err != nil {
				return nil, err
			}
			return s.Bench(o)
		},
	})
	register(command{
		name:    "simulate",
		usage:   "simulate --scenario F | --scene S | --world W --at x,z  --ticks N --seed N [--input script.json] [--screenshots 0,60] [--invariants a,b]",
		summary: "run ticks through the game: trace, verdict, expectations, invariant violations, one contact sheet",
		project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("simulate", env.Stderr)
			var o SimulateOptions
			var shots, invs, at string
			fs.StringVar(&o.Scenario, "scenario", "", "scenario name (tests/scenarios/<name>.scenario.json) or file")
			fs.StringVar(&o.Scene, "scene", "", "scene")
			fs.StringVar(&o.World, "world", "", "world instead of a scene")
			fs.StringVar(&at, "at", "", "with --world: start cell x,z")
			fs.IntVar(&o.Ticks, "ticks", 200, "ticks")
			fs.Uint64Var(&o.Seed, "seed", 0, "seed")
			fs.StringVar(&o.Input, "input", "", "input script file")
			fs.StringVar(&shots, "screenshots", "", "ticks to capture, comma-separated")
			fs.StringVar(&invs, "invariants", "", "invariants, comma-separated")
			fs.IntVar(&o.Width, "width", 0, "screenshot tile width")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			var err error
			if o.At, err = parseCellFlag(at); err != nil {
				return nil, err
			}
			if shots != "" {
				for _, f := range strings.Split(shots, ",") {
					n, err := strconv.Atoi(strings.TrimSpace(f))
					if err != nil {
						return nil, usagef("simulate: --screenshots: %v", err)
					}
					o.Screenshots = append(o.Screenshots, n)
				}
			}
			if invs != "" {
				o.Invariants = strings.Split(invs, ",")
			}
			return s.Simulate(o)
		},
	})
}

// sortedKeys returns the keys of a map in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
