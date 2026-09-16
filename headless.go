package veduta

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/internal/sheet"
	"github.com/riftbane/veduta/scene"
	"github.com/riftbane/veduta/sim"
)

// Exit codes of a game binary.
const (
	exitOK      = 0
	exitError   = 1 // the command failed; stdout holds {"ok":false,"error":...}
	exitUsage   = 2 // bad command line
	exitVerdict = 3 // simulate ran but the verdict is "fail"
)

// Run is the entry point of every game: it parses the command line, loads veduta.json
// and the project's assets, and runs the player on the framebuffer, or with -headless
// one of the subcommands the veduta tool delegates to:
//
//	game -headless render   --scene S | --world W --at x,z  --tick T --seed N --camera P --mode M --out F [--bundle]
//	game -headless simulate --scenario F | --scene S | --world W --at x,z  --ticks N --seed N --input F --out DIR
//	game -headless query    --frame F --at x,y | --coverage
//	game -headless snapshot --scene S | --world W --at x,z  --tick T --out F   (and --restore F --ticks N)
//	game -headless describe
//	game -headless bench    --scenario F | --scene S | --world W --at x,z  --ticks N --seed N --input F --width W --height H --cpus N
//
// Headless subcommands print one JSON report on stdout. Run does not return.
func Run(g Game) {
	os.Exit(runMain(g, os.Args[1:], os.Stdout, os.Stderr))
}

// RunArgs is Run with explicit arguments and outputs; it returns the exit code instead of
// exiting. Tests and tools use it to drive a game in-process.
func RunArgs(g Game, args []string, stdout, stderr io.Writer) int {
	return runMain(g, args, stdout, stderr)
}

// Hooks replaced by tests.
var (
	loadProjectFunc  = loadProject
	loadScenarioFunc = loadScenario
	loadInputFunc    = loadInputFile
)

func runMain(g Game, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("game", flag.ContinueOnError)
	fs.SetOutput(stderr)
	headlessMode := fs.Bool("headless", false, "run a headless subcommand: render, simulate, query, snapshot, describe or bench")
	dir := fs.String("project", ".", "project directory containing veduta.json")
	showVersion := fs.Bool("version", false, "print the engine version and exit")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *showVersion {
		fmt.Fprintln(stdout, Version)
		return exitOK
	}
	if *headlessMode && fs.NArg() == 0 {
		fmt.Fprintln(stderr, "usage: game -headless render|simulate|query|snapshot|describe|bench [flags]")
		return exitUsage
	}
	p, a, err := loadProjectFunc(*dir)
	if err != nil {
		return fail(stdout, stderr, err)
	}
	if !*headlessMode {
		if err := runPlayer(g, p, a, projectDir(*dir), stderr); err != nil {
			fmt.Fprintln(stderr, err)
			return exitError
		}
		return exitOK
	}
	h := &headless{game: g, project: p, assets: a, stdout: stdout, stderr: stderr}
	sub, rest := fs.Arg(0), fs.Args()[1:]
	var code int
	switch sub {
	case "render":
		err = h.render(rest)
	case "simulate":
		code, err = h.simulate(rest)
	case "query":
		err = h.query(rest)
	case "snapshot":
		err = h.snapshot(rest)
	case "describe":
		err = h.describe(rest)
	case "bench":
		err = h.bench(rest)
	default:
		fmt.Fprintf(stderr, "unknown headless subcommand %q\n", sub)
		return exitUsage
	}
	var usage usageError
	switch {
	case errors.As(err, &usage):
		fmt.Fprintln(stderr, err)
		return exitUsage
	case err != nil:
		return fail(stdout, stderr, err)
	}
	return code
}

type usageError struct{ error }

// fail prints the structured error report and returns exitError. Source errors keep
// their {file,line,col,msg} form.
func fail(stdout, stderr io.Writer, err error) int {
	rep := map[string]any{"ok": false, "error": err.Error()}
	var list asset.Errors
	var one *asset.SourceError
	switch {
	case errors.As(err, &list):
		rep["errors"] = list
	case errors.As(err, &one):
		rep["errors"] = []*asset.SourceError{one}
	}
	writeJSON(stdout, rep)
	fmt.Fprintln(stderr, err)
	return exitError
}

// writeJSON writes v as one line of JSON without HTML escaping (so "<" stays "<").
func writeJSON(w io.Writer, v any) {
	w.Write(marshalJSON(v, ""))
}

func marshalJSON(v any, indent string) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(v); err != nil {
		buf.Reset()
		enc.Encode(map[string]any{"ok": false, "error": err.Error()})
	}
	return buf.Bytes()
}

type headless struct {
	game    Game
	project *asset.Project
	assets  *Assets
	stdout  io.Writer
	stderr  io.Writer
}

func (h *headless) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(h.stderr)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return usageError{err}
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("%s: unexpected argument %q", fs.Name(), fs.Arg(0))}
	}
	return nil
}

// cameraReport describes the camera of a rendered frame.
type cameraReport struct {
	Name     string     `json:"name"`
	Ortho    bool       `json:"ortho"`
	FovDeg   float32    `json:"fov_deg,omitempty"`
	Size     float32    `json:"size,omitempty"`
	Near     float32    `json:"near"`
	Far      float32    `json:"far"`
	Position gmath.Vec3 `json:"position"`
	Target   gmath.Vec3 `json:"target"`
}

func camReport(name string, c scene.Camera) cameraReport {
	return cameraReport{Name: name, Ortho: c.Ortho, FovDeg: c.FovDeg, Size: c.Size, Near: c.Near, Far: c.Far, Position: c.Position, Target: c.Target}
}

type entityPixels struct {
	ID     uint32 `json:"id"`
	Name   string `json:"name"`
	Pixels int    `json:"pixels"`
}

// visibleEntities counts ID-buffer pixels per entity, in id order.
func visibleEntities(s *scene.Scene, fb *gfx.Framebuffer) []entityPixels {
	counts := map[uint32]int{}
	for _, id := range fb.ID {
		if id != 0 {
			counts[id]++
		}
	}
	var out []entityPixels
	for _, e := range s.Entities() {
		if n := counts[e.ID]; n > 0 && e.Alive() {
			out = append(out, entityPixels{ID: e.ID, Name: e.Name, Pixels: n})
		}
	}
	return out
}

func writePNG(path string, img *gfx.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := img.EncodePNG(&buf); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// target resolves the --scene, --world and --at flags of a command: a world when one is
// named (or the project's default world when neither is), else a scene.
func (h *headless) target(sceneName, worldName, at string) (runOptions, error) {
	var opt runOptions
	switch {
	case sceneName != "" && worldName != "":
		return opt, usageError{errors.New("--scene and --world are exclusive")}
	case worldName != "":
		opt.World = worldName
	case sceneName != "":
		opt.Scene = sceneName
	case h.project.DefaultWorld != "":
		opt.World = h.project.DefaultWorld
	default:
		opt.Scene = h.project.DefaultScene
	}
	if at != "" {
		if opt.World == "" {
			return opt, usageError{errors.New("--at needs --world (the start cell)")}
		}
		cell, err := parseCell(at)
		if err != nil {
			return opt, usageError{fmt.Errorf("--at: %w", err)}
		}
		opt.At = cell
	}
	return opt, nil
}

// parseCell reads "x,z" cell coordinates.
func parseCell(s string) ([2]int32, error) {
	a, b, ok := strings.Cut(s, ",")
	if !ok {
		return [2]int32{}, fmt.Errorf("want x,z (cells), got %q", s)
	}
	x, err1 := strconv.ParseInt(strings.TrimSpace(a), 10, 32)
	z, err2 := strconv.ParseInt(strings.TrimSpace(b), 10, 32)
	if err1 != nil || err2 != nil {
		return [2]int32{}, fmt.Errorf("want x,z (whole cells), got %q", s)
	}
	return [2]int32{int32(x), int32(z)}, nil
}

// name returns what a run is called: its scene or its world.
func (o runOptions) name() string {
	if o.World != "" {
		return o.World
	}
	return o.Scene
}

// startRun creates an engine, starts the scene or world and replays input up to tick.
func (h *headless) startRun(opt runOptions, seed uint64, invariants []string, trace io.Writer, script *sim.Script, tick int) (*engine, error) {
	e := newEngine(h.game, h.project, h.assets)
	opt.Seed, opt.Invariants, opt.Trace, opt.Headless = seed, invariants, trace, true
	if err := e.start(opt); err != nil {
		e.close()
		return nil, err
	}
	for t := 1; t <= tick; t++ {
		var in Input
		if script != nil {
			in = script.Input(uint64(t))
		}
		if err := e.step(in); err != nil {
			e.close()
			return nil, err
		}
	}
	return e, nil
}

func (h *headless) script(path string) (*sim.Script, error) {
	if path == "" {
		return sim.NewScript(nil)
	}
	events, err := loadInputFunc(path)
	if err != nil {
		return nil, err
	}
	return sim.NewScript(events)
}

func (h *headless) render(args []string) error {
	fs := h.flags("render")
	sceneName := fs.String("scene", "", "scene to render (default: the project's default world or scene)")
	worldName := fs.String("world", "", "world to render instead of a scene")
	at := fs.String("at", "", "with --world: start cell x,z (default 0,0)")
	tick := fs.Int("tick", 0, "simulate this many ticks before rendering")
	seed := fs.Uint64("seed", h.project.DefaultSeed, "RNG seed")
	camera := fs.String("camera", "scene", "camera preset: scene, top, front, back, left, right, iso, orbit:<deg>, or a camera entity")
	mode := fs.String("mode", "color", "render mode: "+strings.Join(modeNames(), ", "))
	width := fs.Int("width", h.project.InspectResolution[0], "image width")
	height := fs.Int("height", h.project.InspectResolution[1], "image height")
	out := fs.String("out", "", "output PNG (default out/render_<scene>_t<tick>_<mode>.png)")
	bundle := fs.Bool("bundle", false, "also write a frame bundle (.vframe) next to the PNG")
	input := fs.String("input", "", "input script applied during the ticks before the frame")
	if err := parse(fs, args); err != nil {
		return err
	}
	m, err := gfx.ParseRenderMode(*mode)
	if err != nil {
		return err
	}
	if *tick < 0 {
		return fmt.Errorf("render: negative tick %d", *tick)
	}
	sc, err := h.script(*input)
	if err != nil {
		return err
	}
	target, err := h.target(*sceneName, *worldName, *at)
	if err != nil {
		return err
	}
	e, err := h.startRun(target, *seed, nil, nil, sc, *tick)
	if err != nil {
		return err
	}
	defer e.close()
	cam, err := e.ctx.Scene.CameraPreset(*camera, float32(*width)/float32(*height))
	if err != nil {
		return err
	}
	f, err := e.render(cam, *width, *height, m, *bundle)
	if err != nil {
		return err
	}
	path := *out
	if path == "" {
		path = filepath.Join("out", fmt.Sprintf("render_%s_t%d_%s.png", target.name(), *tick, m))
	}
	if err := writePNG(path, f.FB.Image()); err != nil {
		return err
	}
	rep := map[string]any{
		"ok": true, "out": path, "scene": target.Scene, "tick": *tick, "seed": *seed,
		"mode": m.String(), "width": *width, "height": *height, "camera": camReport(*camera, cam),
		"stats": f.Stats, "draw": f.Draw, "entities": visibleEntities(e.ctx.Scene, f.FB), "trace_hash": e.rec.Hash(),
	}
	if target.World != "" {
		rep["world"], rep["at"] = target.World, target.At
		delete(rep, "scene")
	}
	if *bundle {
		bpath := strings.TrimSuffix(path, filepath.Ext(path)) + ".vframe"
		if err := writeBundle(bpath, e, f, *camera); err != nil {
			return err
		}
		rep["bundle"] = bpath
	}
	writeJSON(h.stdout, rep)
	return nil
}

func modeNames() []string {
	var out []string
	for _, m := range gfx.RenderModes {
		out = append(out, m.String())
	}
	return out
}

// scenarioSpec is a scenario ready to run (from a file or from flags).
type scenarioSpec struct {
	Name        string
	Scene       string
	World       string   // instead of Scene
	At          [2]int32 // the world's start cell
	Seed        uint64
	Ticks       int
	Inputs      []sim.InputEvent
	Expect      []sim.Expectation
	Invariants  []string
	Screenshots []int
}

// simResult is the report of simulate.
type simResult struct {
	OK           bool               `json:"ok"`
	Scenario     string             `json:"scenario,omitempty"`
	Scene        string             `json:"scene,omitempty"`
	World        string             `json:"world,omitempty"`
	At           *[2]int32          `json:"at,omitempty"`
	Seed         uint64             `json:"seed"`
	Ticks        int                `json:"ticks"`
	Verdict      string             `json:"verdict"`
	FirstFailure string             `json:"first_failure,omitempty"`
	Expectations []sim.ExpectResult `json:"expectations"`
	Violations   []sim.Violation    `json:"violations"`
	Invariants   []string           `json:"invariants"`
	Events       map[string]int     `json:"events"`
	TraceHash    string             `json:"trace_hash"`
	Out          string             `json:"out"`
	Trace        string             `json:"trace"`
	Sheet        string             `json:"sheet"`
	Screenshots  []int              `json:"screenshots"`
	Entities     int                `json:"entities"`
}

func (h *headless) simulate(args []string) (int, error) {
	fs := h.flags("simulate")
	scenario := fs.String("scenario", "", "scenario file (tests/scenarios/<name>.scenario.json)")
	sceneName := fs.String("scene", "", "scene (without --scenario; default: the project's default world or scene)")
	worldName := fs.String("world", "", "world instead of a scene (without --scenario)")
	at := fs.String("at", "", "with --world: start cell x,z (default 0,0)")
	ticks := fs.Int("ticks", 200, "ticks to simulate (without --scenario)")
	seed := fs.Uint64("seed", h.project.DefaultSeed, "RNG seed (without --scenario)")
	input := fs.String("input", "", "input script file (without --scenario)")
	shots := fs.String("screenshots", "", "comma-separated ticks to capture (default: 0 and the last tick)")
	invs := fs.String("invariants", "", "comma-separated invariants (default: scenario's, else the project's)")
	out := fs.String("out", "", "output directory (default out/runs/<scenario or scene>)")
	width := fs.Int("width", 0, "screenshot tile width (default fits a 640-pixel sheet)")
	withSheet := fs.Bool("sheet", true, "render screenshots and the contact sheet (false: trace and verdict only)")
	if err := parse(fs, args); err != nil {
		return 0, err
	}
	var spec *scenarioSpec
	if *scenario != "" {
		var err error
		if spec, err = loadScenarioFunc(*scenario); err != nil {
			return 0, err
		}
	} else {
		if *ticks < 1 {
			return 0, fmt.Errorf("simulate: --ticks must be at least 1")
		}
		target, err := h.target(*sceneName, *worldName, *at)
		if err != nil {
			return 0, err
		}
		spec = &scenarioSpec{Scene: target.Scene, World: target.World, At: target.At, Seed: *seed, Ticks: *ticks}
		if *input != "" {
			events, err := loadInputFunc(*input)
			if err != nil {
				return 0, err
			}
			spec.Inputs = events
		}
	}
	if *shots != "" {
		list, err := parseInts(*shots)
		if err != nil {
			return 0, fmt.Errorf("simulate: --screenshots: %w", err)
		}
		spec.Screenshots = list
	}
	if len(spec.Screenshots) == 0 {
		spec.Screenshots = []int{0, spec.Ticks}
	}
	if *invs != "" {
		spec.Invariants = strings.Split(*invs, ",")
	}
	dir := *out
	if dir == "" {
		name := spec.Name
		if name == "" {
			name = spec.target().name()
		}
		dir = filepath.Join("out", "runs", name)
	}
	res, err := h.runScenario(spec, dir, *width, *withSheet)
	if err != nil {
		return 0, err
	}
	writeJSON(h.stdout, res)
	if res.Verdict != "pass" {
		return exitVerdict, nil
	}
	return exitOK, nil
}

func parseInts(s string) ([]int, error) {
	var out []int
	for _, f := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// runScenario runs spec, writing trace.jsonl, result.json and sheet.png into dir.
func (h *headless) runScenario(spec *scenarioSpec, dir string, tileW int, withSheet bool) (*simResult, error) {
	for _, t := range spec.Screenshots {
		if t < 0 || t > spec.Ticks {
			return nil, fmt.Errorf("screenshot tick %d outside [0, %d]", t, spec.Ticks)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tracePath := filepath.Join(dir, "trace.jsonl")
	tf, err := os.Create(tracePath)
	if err != nil {
		return nil, err
	}
	defer tf.Close()
	script, err := sim.NewScript(spec.Inputs)
	if err != nil {
		return nil, err
	}
	inv := spec.Invariants
	if len(inv) == 0 {
		inv = nil // project defaults
	}
	e, err := h.startRun(spec.target(), spec.Seed, inv, tf, nil, 0)
	if err != nil {
		return nil, err
	}
	defer e.close()

	shots := map[int]bool{}
	for _, t := range spec.Screenshots {
		shots[t] = withSheet
	}
	cols, tw, th := sheetLayout(len(spec.Screenshots)+1, tileW, h.project)
	var tiles []*gfx.Image
	var labels []string
	capture := func(t int) error {
		cam, _ := e.ctx.Scene.CameraPreset("scene", float32(tw)/float32(th))
		f, err := e.render(cam, tw, th, gfx.ModeColor, false)
		if err != nil {
			return err
		}
		tiles = append(tiles, f.FB.Image().Clone())
		labels = append(labels, fmt.Sprintf("tick %d", t))
		return nil
	}
	trail := newTrails()
	trail.record(e.ctx.Scene)
	if shots[0] {
		if err := capture(0); err != nil {
			return nil, err
		}
	}
	results := make([]sim.ExpectResult, len(spec.Expect))
	evaluated := make([]bool, len(spec.Expect))
	evalAt := func(t int) {
		for i, x := range spec.Expect {
			if int(x.Tick) == t {
				results[i] = sim.Evaluate(x, e.ctx.Scene, e.rec)
				evaluated[i] = true
			}
		}
	}
	evalAt(0)
	for t := 1; t <= spec.Ticks; t++ {
		if err := e.step(script.Input(uint64(t))); err != nil {
			return nil, err
		}
		trail.record(e.ctx.Scene)
		evalAt(t)
		if shots[t] {
			if err := capture(t); err != nil {
				return nil, err
			}
		}
	}
	for i := range results {
		if !evaluated[i] {
			results[i] = sim.ExpectResult{Tick: spec.Expect[i].Tick, Error: "tick never reached"}
		}
	}
	sheetPath := ""
	if withSheet {
		traj, label, err := trail.render(e, tw, th)
		if err != nil {
			return nil, err
		}
		tiles = append(tiles, traj)
		labels = append(labels, label)
		sheetPath = filepath.Join(dir, "sheet.png")
		if err := writePNG(sheetPath, sheet.Grid(tiles, labels, cols, 4)); err != nil {
			return nil, err
		}
	}

	res := &simResult{
		OK: true, Scenario: spec.Name, Scene: spec.Scene, World: spec.World, Seed: spec.Seed, Ticks: spec.Ticks,
		Expectations: results, Violations: e.monitor.First(), Invariants: e.monitor.Names(),
		Events: e.rec.Counts(), TraceHash: e.rec.Hash(), Out: dir, Trace: tracePath, Sheet: sheetPath,
		Screenshots: spec.Screenshots, Entities: e.ctx.Scene.Len(),
	}
	if spec.World != "" {
		at := spec.At
		res.At = &at
	}
	if res.Expectations == nil {
		res.Expectations = []sim.ExpectResult{}
	}
	if res.Violations == nil {
		res.Violations = []sim.Violation{}
	}
	res.Verdict = "pass"
	for _, r := range results {
		if !r.Pass {
			res.Verdict = "fail"
			res.FirstFailure = describeFailure(r)
			break
		}
	}
	if res.Verdict == "pass" && len(res.Violations) > 0 {
		v := res.Violations[0]
		res.Verdict = "fail"
		res.FirstFailure = fmt.Sprintf("invariant %s violated at tick %d: %s", v.Name, v.Tick, v.Detail)
	}
	if err := os.WriteFile(filepath.Join(dir, "result.json"), marshalJSON(res, "  "), 0o644); err != nil {
		return nil, err
	}
	return res, nil
}

// target returns the scene or world the scenario starts in.
func (spec *scenarioSpec) target() runOptions {
	return runOptions{Scene: spec.Scene, World: spec.World, At: spec.At}
}

func describeFailure(r sim.ExpectResult) string {
	if r.Error != "" {
		return fmt.Sprintf("tick %d: %s", r.Tick, r.Error)
	}
	if r.Trace != "" {
		return fmt.Sprintf("tick %d: trace %q count %v outside [%s, %s]", r.Tick, r.Trace, r.Actual, intOr(r.CountMin, "-"), intOr(r.CountMax, "-"))
	}
	return fmt.Sprintf("tick %d: %s.%s = %v, expected %s %v", r.Tick, r.Entity, r.Path, r.Actual, r.Op, r.Value)
}

func intOr(p *int, def string) string {
	if p == nil {
		return def
	}
	return strconv.Itoa(*p)
}

// sheetLayout picks the contact sheet grid so the sheet is at most 640 pixels wide.
func sheetLayout(n, tileW int, p *asset.Project) (cols, w, h int) {
	switch {
	case n <= 4:
		cols = 2
	case n <= 9:
		cols = 3
	default:
		cols = 4
	}
	w = tileW
	if w <= 0 {
		w = (640 - (cols+1)*4) / cols
	}
	aspect := float32(p.Resolution[1]) / float32(p.Resolution[0])
	h = max(int(min(float32(w)*aspect, asset.MaxResolution+1)), 1)
	return cols, w, h
}

// trails records the path of every non-static entity for the trajectory tile.
type trails struct {
	paths map[uint32][]gmath.Vec3
	names map[uint32]string
}

func newTrails() *trails {
	return &trails{paths: map[uint32][]gmath.Vec3{}, names: map[uint32]string{}}
}

func (t *trails) record(s *scene.Scene) {
	for _, e := range s.Entities() {
		if !e.Alive() || e.Kind == scene.KindStatic || e.Kind == scene.KindCamera || e.Kind == scene.KindLight {
			continue
		}
		p := e.WorldPosition()
		ps := t.paths[e.ID]
		if n := len(ps); n == 0 || ps[n-1] != p {
			t.paths[e.ID] = append(ps, p)
			t.names[e.ID] = e.Name
		}
	}
}

// render draws the final state with every recorded path as a polyline in the entity's id
// color, and returns the tile with its label. A 3D game is seen from the top, its paths
// flattened onto XZ. A 2D game, whose scene camera is orthographic and looks along -Z
// (scene.Camera2D), moves in XY, which the top view would collapse onto a line: it is seen
// from the front instead, its paths flattened onto XY.
func (t *trails) render(e *engine, w, h int) (*gfx.Image, string, error) {
	b := e.ctx.Scene.Bounds()
	for _, ps := range t.paths {
		for _, p := range ps {
			b = b.Extend(p)
		}
	}
	if b.IsEmpty() {
		b = gmath.AABB{Min: gmath.V3(-1, -1, -1), Max: gmath.V3(1, 1, 1)}
	}
	flat, dir, label := 1, gmath.V3(0, -1, 0), "trajectories (top)"
	if looksDownZ(e.ctx.Scene.Camera) {
		flat, dir, label = 2, gmath.V3(0, 0, -1), "trajectories (xy)"
	}
	cam := scene.FrameOrtho(b, dir, float32(w)/float32(h))
	ids := make([]uint32, 0, len(t.paths))
	for id := range t.paths {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	f, err := e.renderWith(cam, w, h, gfx.ModeColor, false, func(dl *gfx.DrawList, view int) {
		near := b.Max.Get(flat) + 0.01 // in front of everything, seen along -dir
		for _, id := range ids {
			ps := t.paths[id]
			for i := 1; i < len(ps); i++ {
				a, c := ps[i-1].With(flat, near), ps[i].With(flat, near)
				dl.AddLine(gfx.DebugLine{A: a, B: c, Color: gfx.IDColor(id), View: view})
			}
		}
	})
	if err != nil {
		return nil, "", err
	}
	return f.FB.Image().Clone(), label, nil
}

// looksDownZ reports whether c is the camera of a 2D game: orthographic and looking along
// -Z to within about a degree, as scene.Camera2D does.
func looksDownZ(c scene.Camera) bool {
	return c.Ortho && c.Target.Sub(c.Position).Normalize().Z < -0.9998
}

func (h *headless) snapshot(args []string) error {
	fs := h.flags("snapshot")
	sceneName := fs.String("scene", "", "scene (default: the project's default world or scene)")
	worldName := fs.String("world", "", "world instead of a scene")
	at := fs.String("at", "", "with --world: start cell x,z (default 0,0)")
	tick := fs.Int("tick", 0, "ticks to simulate before the snapshot")
	seed := fs.Uint64("seed", h.project.DefaultSeed, "RNG seed")
	input := fs.String("input", "", "input script")
	out := fs.String("out", "", "snapshot file to write (or, with --restore, a trace file to write)")
	restore := fs.String("restore", "", "snapshot file to restore")
	ticks := fs.Int("ticks", 0, "with --restore: ticks to simulate after restoring")
	if err := parse(fs, args); err != nil {
		return err
	}
	sc, err := h.script(*input)
	if err != nil {
		return err
	}
	target, err := h.target(*sceneName, *worldName, *at)
	if err != nil {
		return err
	}
	if *restore == "" {
		if *out == "" {
			return usageError{errors.New("snapshot: --out is required")}
		}
		e, err := h.startRun(target, *seed, nil, nil, sc, *tick)
		if err != nil {
			return err
		}
		defer e.close()
		data, err := e.snapshot()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			return err
		}
		writeJSON(h.stdout, map[string]any{"ok": true, "out": *out, "scene": e.ctx.Scene.Name, "tick": *tick, "trace_hash": e.rec.Hash(), "bytes": len(data)})
		return nil
	}
	data, err := os.ReadFile(*restore)
	if err != nil {
		return err
	}
	e := newEngine(h.game, h.project, h.assets)
	defer e.close()
	target.Seed, target.Headless = *seed, true
	if err := e.prepare(target); err != nil {
		return err
	}
	var trace io.Writer
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		trace = f
	}
	if err := e.restore(data, trace); err != nil {
		return err
	}
	from := e.tick
	for t := uint64(1); t <= from; t++ { // bring the input script to the snapshot tick
		sc.Input(t)
	}
	for i := 0; i < *ticks; i++ {
		if err := e.step(sc.Input(e.tick + 1)); err != nil {
			return err
		}
	}
	writeJSON(h.stdout, map[string]any{"ok": true, "restored": *restore, "from_tick": from, "to_tick": e.tick, "trace_hash": e.rec.Hash(), "events": e.rec.Counts()})
	return nil
}

// kindReport describes a registered kind and its state fields.
type kindReport struct {
	Name  string            `json:"name"`
	State map[string]string `json:"state,omitempty"`
}

func (h *headless) describe(args []string) error {
	fs := h.flags("describe")
	if err := parse(fs, args); err != nil {
		return err
	}
	var kinds []kindReport
	for _, k := range Kinds() {
		probe := &scene.Entity{Name: "probe", Kind: k, Transform: scene.Identity(), Visible: true}
		lookupKind(k)(probe)
		kinds = append(kinds, kindReport{Name: k, State: stateSchema(probe.State)})
	}
	e := newEngine(h.game, h.project, h.assets)
	defer e.close()
	var gameInv []string
	codec := false
	if target, err := h.target("", "", ""); err == nil && target.name() != "" {
		target.Seed, target.Headless = h.project.DefaultSeed, true
		if err := e.prepare(target); err != nil {
			return err
		}
		gameInv = sortedNames(e.custom)
		codec = e.codec != nil
	}
	writeJSON(h.stdout, map[string]any{
		"ok": true, "engine": Version, "project": h.project.Name,
		"kinds":         kinds,
		"builtin_kinds": []string{scene.KindCamera, scene.KindLight, scene.KindStatic},
		"invariants": map[string]any{
			"builtin":  []string{sim.InvFinitePositions, sim.InvWithinBounds, sim.InvEntityCountMax + ":N", sim.InvNoOverlap + ":tagA,tagB"},
			"game":     gameInv,
			"defaults": h.project.Invariants,
		},
		"state_codec":    codec,
		"scenes":         sortedNames(h.assets.Scenes),
		"worlds":         sortedNames(h.assets.Worlds),
		"prefabs":        sortedNames(h.assets.Prefabs),
		"default_world":  h.project.DefaultWorld,
		"render_modes":   modeNames(),
		"camera_presets": append(append([]string{}, scene.Presets...), "orbit:<deg>"),
		"tick_rate":      h.project.TickRate,
	})
	return nil
}

// stateSchema lists the JSON fields of a state value and their Go types.
func stateSchema(v any) map[string]string {
	if v == nil {
		return nil
	}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return map[string]string{"": t.String()}
	}
	out := map[string]string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag, ok := f.Tag.Lookup("json"); ok {
			n, _, _ := strings.Cut(tag, ",")
			if n == "-" {
				continue
			}
			if n != "" {
				name = n
			}
		}
		out[name] = f.Type.String()
	}
	return out
}
