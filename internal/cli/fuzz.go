package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/sim"
)

// FuzzOptions configures fuzz (spec §10).
type FuzzOptions struct {
	Scene      string
	World      string    // world instead of a scene
	At         *[2]int32 // the world's start cell
	Games      int
	Ticks      int
	Seed       uint64
	Keys       []string // keys the random player uses (default: DefaultFuzzKeys)
	Invariants []string // default: the project's
	Parallel   int      // concurrent games (default: GOMAXPROCS)
}

// DefaultFuzzKeys are the keys random players press.
var DefaultFuzzKeys = []string{"KeyW", "KeyA", "KeyS", "KeyD", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Space", "Enter", "ShiftLeft", "KeyE", "KeyQ", "KeyR"}

// FuzzViolation is the first invariant violation of one random game.
type FuzzViolation struct {
	Game      int    `json:"game"`
	Seed      uint64 `json:"seed"`
	Tick      int    `json:"tick"`
	Invariant string `json:"invariant"`
	Detail    string `json:"detail"`
}

// FuzzReport is the result of fuzz.
type FuzzReport struct {
	OK          bool            `json:"ok"`
	Scene       string          `json:"scene,omitempty"`
	World       string          `json:"world,omitempty"`
	At          *[2]int32       `json:"at,omitempty"`
	Games       int             `json:"games"`
	Ticks       int             `json:"ticks"`
	Seed        uint64          `json:"seed"`
	Invariants  []string        `json:"invariants"`
	Violations  []FuzzViolation `json:"violations"`
	Repro       string          `json:"repro,omitempty"`        // minimized scenario written
	ReproTicks  int             `json:"repro_ticks,omitempty"`  // its length
	ReproInputs int             `json:"repro_inputs,omitempty"` // its input events
	MinimizeRun int             `json:"minimize_runs,omitempty"`
	Millis      int64           `json:"ms"`
}

// ExitCode is 1 when a violation was found.
func (r *FuzzReport) ExitCode() int {
	if r.OK {
		return 0
	}
	return 1
}

// Human summarizes the run.
func (r *FuzzReport) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "fuzz %s: %d games × %d ticks, seed %d, %d violating (%d ms)\n", r.Scene+r.World, r.Games, r.Ticks, r.Seed, len(r.Violations), r.Millis)
	for i, v := range r.Violations {
		if i == 10 {
			fmt.Fprintf(&b, "  … %d more\n", len(r.Violations)-10)
			break
		}
		fmt.Fprintf(&b, "  game %d (seed %d): %s at tick %d — %s\n", v.Game, v.Seed, v.Invariant, v.Tick, v.Detail)
	}
	if r.Repro != "" {
		fmt.Fprintf(&b, "minimized repro: %s (%d ticks, %d input events, %d runs)\n", r.Repro, r.ReproTicks, r.ReproInputs, r.MinimizeRun)
	}
	return b.String()
}

// hold is a key held from Start to End (End 0: never released) — the unit minimization
// removes, so press and release always stay paired.
type hold struct {
	Key        string
	Start, End int
}

// fuzzGame is the random input of one game.
type fuzzGame struct {
	holds  []hold
	mouse  []mouseEv
	sticks []stickEv
}

type stickEv struct {
	Tick int
	X, Y float32
}

type mouseEv struct {
	Tick    int
	X, Y    float32
	Buttons []string // nil: unchanged
}

// gameSeed derives the seed of game i from the run seed (splitmix-style mixing).
func gameSeed(seed uint64, i int) uint64 {
	z := seed + uint64(i+1)*0x9e3779b97f4a7c15
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

// genGame draws a random player: keys held for random durations, mouse moves and clicks
// (the stick is drawn by genSticks, from a stream of its own).
func genGame(rng *sim.RNG, ticks int, keys []string, w, h int) fuzzGame {
	var g fuzzGame
	held := map[string]int{} // key → index into g.holds (lookup only)
	for t := 1; t <= ticks; t++ {
		if rng.Chance(0.06) {
			k := keys[rng.Intn(len(keys))]
			if i, ok := held[k]; ok {
				g.holds[i].End = t
				delete(held, k)
			} else {
				held[k] = len(g.holds)
				g.holds = append(g.holds, hold{Key: k, Start: t})
			}
		}
		if rng.Chance(0.02) {
			ev := mouseEv{Tick: t, X: float32(rng.Intn(w)), Y: float32(rng.Intn(h))}
			if rng.Chance(0.3) {
				ev.Buttons = []string{}
				if rng.Bool() {
					ev.Buttons = []string{"left"}
				}
			}
			g.mouse = append(g.mouse, ev)
		}
	}
	return g
}

// stickSalt separates the stick's random stream from the keys' and the mouse's, so adding
// the stick did not change the games an older tool played for the same seed.
const stickSalt = 0x57_1c_4a_5e_57_1c_4a_5e

// stickPositions are where a random player puts the stick: rest, halfway and the ends,
// numbers a repro scenario shows as written.
var stickPositions = []float32{-1, -0.5, 0, 0.5, 1}

// genSticks draws a random player's stick moves.
func genSticks(rng *sim.RNG, ticks int) []stickEv {
	var out []stickEv
	for t := 1; t <= ticks; t++ {
		if rng.Chance(0.03) {
			out = append(out, stickEv{Tick: t, X: stickPositions[rng.Intn(len(stickPositions))], Y: stickPositions[rng.Intn(len(stickPositions))]})
		}
	}
	return out
}

// events turns holds, mouse and stick events (up to tick limit) into scenario input events.
func (g fuzzGame) events(limit int) []asset.InputSource {
	byTick := map[int]*asset.InputSource{}
	get := func(t int) *asset.InputSource {
		if e := byTick[t]; e != nil {
			return e
		}
		e := &asset.InputSource{Tick: t}
		byTick[t] = e
		return e
	}
	for _, h := range g.holds {
		if h.Start > limit {
			continue
		}
		get(h.Start).Press = append(get(h.Start).Press, h.Key)
		if h.End > 0 && h.End <= limit {
			get(h.End).Release = append(get(h.End).Release, h.Key)
		}
	}
	for _, m := range g.mouse {
		if m.Tick > limit {
			continue
		}
		e := get(m.Tick)
		e.Mouse = &asset.MouseSource{X: m.X, Y: m.Y}
		if m.Buttons != nil {
			e.Buttons = m.Buttons
		}
	}
	for _, st := range g.sticks {
		if st.Tick <= limit {
			get(st.Tick).Stick = &asset.StickSource{X: st.X, Y: st.Y}
		}
	}
	ticks := make([]int, 0, len(byTick))
	for t := range byTick {
		ticks = append(ticks, t)
	}
	sort.Ints(ticks)
	out := make([]asset.InputSource, 0, len(ticks))
	for _, t := range ticks {
		e := byTick[t]
		sort.Strings(e.Press)
		sort.Strings(e.Release)
		out = append(out, *e)
	}
	return out
}

// Fuzz plays random games looking for invariant violations and writes a minimized repro
// of the first one to tests/scenarios/fuzz_<hash>.scenario.json.
func (s *Session) Fuzz(o FuzzOptions) (*FuzzReport, error) {
	start := time.Now()
	if o.Scene == "" && o.World == "" {
		if s.Project.DefaultWorld != "" {
			o.World = s.Project.DefaultWorld
		} else {
			o.Scene = s.Project.DefaultScene
		}
	}
	if o.Scene != "" && o.World != "" {
		return nil, usagef("fuzz: --scene and --world are exclusive")
	}
	if o.Games <= 0 || o.Ticks <= 0 {
		return nil, usagef("fuzz: --games and --ticks must be positive")
	}
	if len(o.Keys) == 0 {
		o.Keys = DefaultFuzzKeys
	}
	for _, k := range o.Keys {
		if !asset.IsKeyCode(k) {
			return nil, usagef("fuzz: unknown key %q", k)
		}
	}
	if len(o.Invariants) == 0 {
		o.Invariants = s.Project.Invariants
	}
	if o.Parallel <= 0 {
		o.Parallel = runtime.GOMAXPROCS(0)
	}
	bin, err := s.ensureGame()
	if err != nil {
		return nil, err
	}
	dir := s.Out("fuzz", strconv.FormatUint(o.Seed, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &FuzzReport{Scene: o.Scene, World: o.World, At: o.At, Games: o.Games, Ticks: o.Ticks, Seed: o.Seed, Invariants: o.Invariants, Violations: []FuzzViolation{}}
	games := make([]fuzzGame, o.Games)
	found := make([]*FuzzViolation, o.Games)
	errs := make([]error, o.Games)
	w, h := s.Project.Resolution[0], s.Project.Resolution[1]
	for i := range games {
		games[i] = genGame(sim.NewRNG(gameSeed(o.Seed, i)), o.Ticks, o.Keys, w, h)
		games[i].sticks = genSticks(sim.NewRNG(gameSeed(o.Seed, i)^stickSalt), o.Ticks)
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for p := 0; p < o.Parallel; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				v, err := s.playGame(bin, dir, fmt.Sprintf("game-%d", i), o, gameSeed(o.Seed, i), games[i], o.Ticks)
				if v != nil {
					v.Game = i
				}
				found[i], errs[i] = v, err
			}
		}()
	}
	for i := range games {
		next <- i
	}
	close(next)
	wg.Wait()
	for i := range games {
		if errs[i] != nil {
			return nil, fmt.Errorf("fuzz game %d: %w", i, errs[i])
		}
		if found[i] != nil {
			r.Violations = append(r.Violations, *found[i])
		}
	}
	r.OK = len(r.Violations) == 0
	if !r.OK {
		if err := s.minimize(bin, dir, o, games, r); err != nil {
			return nil, err
		}
	}
	r.Millis = time.Since(start).Milliseconds()
	return r, nil
}

// playGame runs one game and returns its first violation (nil when clean).
func (s *Session) playGame(bin, dir, name string, o FuzzOptions, seed uint64, g fuzzGame, ticks int) (*FuzzViolation, error) {
	gdir := filepath.Join(dir, name)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		return nil, err
	}
	input := filepath.Join(gdir, "input.json")
	data, err := json.Marshal(g.events(ticks))
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(input, data, 0o644); err != nil {
		return nil, err
	}
	args := append([]string{"simulate"}, targetArgs(o.Scene, o.World, o.At)...)
	args = append(args, "--ticks", strconv.Itoa(ticks), "--seed", strconv.FormatUint(seed, 10),
		"--input", input, "--out", gdir, "--sheet=false")
	if len(o.Invariants) > 0 {
		args = append(args, "--invariants", strings.Join(o.Invariants, ","))
	}
	rep, _, err := s.runGameBin(bin, args...)
	if err != nil {
		return nil, err
	}
	vs, _ := rep["violations"].([]any)
	if len(vs) == 0 {
		return nil, nil
	}
	// The earliest violation of the game.
	var best map[string]any
	for _, x := range vs {
		m, _ := x.(map[string]any)
		if best == nil || asFloat(m["tick"]) < asFloat(best["tick"]) {
			best = m
		}
	}
	return &FuzzViolation{Seed: seed, Tick: int(asFloat(best["tick"])), Invariant: fmt.Sprint(best["name"]), Detail: fmt.Sprint(best["detail"])}, nil
}

// minimize shrinks the first violating game (earliest violation) with delta debugging
// over key holds, mouse and stick events, keeping the same invariant violated, then writes the
// repro scenario.
func (s *Session) minimize(bin, dir string, o FuzzOptions, games []fuzzGame, r *FuzzReport) error {
	first := r.Violations[0]
	for _, v := range r.Violations[1:] {
		if v.Tick < first.Tick {
			first = v
		}
	}
	g := games[first.Game]
	ticks := first.Tick
	fo := o
	fo.Invariants = []string{first.Invariant}
	runs := 0
	fails := func(c fuzzGame) bool {
		runs++
		v, err := s.playGame(bin, dir, "min", fo, first.Seed, c, ticks)
		return err == nil && v != nil
	}
	// Only inputs before the violation matter.
	g = trimGame(g, ticks)
	// ddmin on the combined list of holds, mouse and stick events.
	type item struct {
		h  *hold
		m  *mouseEv
		st *stickEv
	}
	items := func(c fuzzGame) []item {
		var out []item
		for i := range c.holds {
			out = append(out, item{h: &c.holds[i]})
		}
		for i := range c.mouse {
			out = append(out, item{m: &c.mouse[i]})
		}
		for i := range c.sticks {
			out = append(out, item{st: &c.sticks[i]})
		}
		return out
	}
	build := func(its []item) fuzzGame {
		var c fuzzGame
		for _, it := range its {
			switch {
			case it.h != nil:
				c.holds = append(c.holds, *it.h)
			case it.m != nil:
				c.mouse = append(c.mouse, *it.m)
			default:
				c.sticks = append(c.sticks, *it.st)
			}
		}
		return c
	}
	cur := items(g)
	n := 2
	for len(cur) >= 2 && runs < 400 {
		chunk := (len(cur) + n - 1) / n
		reduced := false
		for i := 0; i < len(cur); i += chunk {
			rest := append(append([]item(nil), cur[:i]...), cur[min(i+chunk, len(cur)):]...)
			if len(rest) > 0 && fails(build(rest)) || len(rest) == 0 && fails(fuzzGame{}) {
				cur, n, reduced = rest, max(n-1, 2), true
				break
			}
		}
		if !reduced {
			if n >= len(cur) {
				break
			}
			n = min(n*2, len(cur))
		}
	}
	if len(cur) == 1 && fails(fuzzGame{}) {
		cur = nil
	}
	best := build(cur)
	// The violation tick may move earlier once inputs are gone: find it again.
	if v, err := s.playGame(bin, dir, "min", fo, first.Seed, best, ticks); err == nil && v != nil {
		ticks = v.Tick
	}
	events := best.events(ticks)
	sc := asset.ScenarioSource{
		Veduta:      asset.TypeScenario,
		Scene:       o.Scene,
		World:       o.World,
		Seed:        first.Seed,
		Ticks:       max(ticks, 1),
		Inputs:      events,
		Invariants:  []string{first.Invariant},
		Screenshots: []int{0, max(ticks, 1)},
	}
	if o.At != nil {
		sc.At = []int{int(o.At[0]), int(o.At[1])}
	}
	data := marshal(sc, true)
	sum := sha256.Sum256(data)
	name := "fuzz_" + hex.EncodeToString(sum[:4])
	path := filepath.Join(s.Root, "tests", "scenarios", name+".scenario.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	r.Repro, r.ReproTicks, r.ReproInputs, r.MinimizeRun = s.Rel(path), ticks, len(events), runs
	return nil
}

// trimGame drops input after tick limit and open-ended holds' releases beyond it.
func trimGame(g fuzzGame, limit int) fuzzGame {
	var out fuzzGame
	for _, h := range g.holds {
		if h.Start > limit {
			continue
		}
		if h.End > limit {
			h.End = 0
		}
		out.holds = append(out.holds, h)
	}
	for _, m := range g.mouse {
		if m.Tick <= limit {
			out.mouse = append(out.mouse, m)
		}
	}
	for _, st := range g.sticks {
		if st.Tick <= limit {
			out.sticks = append(out.sticks, st)
		}
	}
	return out
}

func init() {
	register(command{
		name:    "fuzz",
		usage:   "fuzz --scene S | --world W --at x,z  --games N --ticks T --seed N [--keys KeyW,Space] [--invariants a,b]",
		summary: "play random-input games, report invariant violations, write a minimized repro scenario",
		project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("fuzz", env.Stderr)
			var o FuzzOptions
			var keys, invs, at string
			fs.StringVar(&o.Scene, "scene", "", "scene (default: the project's default world or scene)")
			fs.StringVar(&o.World, "world", "", "world instead of a scene")
			fs.StringVar(&at, "at", "", "with --world: start cell x,z")
			fs.IntVar(&o.Games, "games", 200, "number of random games")
			fs.IntVar(&o.Ticks, "ticks", 200, "ticks per game")
			fs.Uint64Var(&o.Seed, "seed", 1, "seed of the random players")
			fs.StringVar(&keys, "keys", "", "comma-separated keys the players use")
			fs.StringVar(&invs, "invariants", "", "comma-separated invariants (default: the project's)")
			fs.IntVar(&o.Parallel, "parallel", 0, "concurrent games (default: CPUs)")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			var err error
			if o.At, err = parseCellFlag(at); err != nil {
				return nil, err
			}
			if keys != "" {
				o.Keys = strings.Split(keys, ",")
			}
			if invs != "" {
				o.Invariants = strings.Split(invs, ",")
			}
			return s.Fuzz(o)
		},
	})
}
