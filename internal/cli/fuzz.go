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

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/sim"
)

// FuzzOptions configures fuzz (spec §10).
type FuzzOptions struct {
	Scene      string
	World      string    // world instead of a scene
	At         *[2]int32 // the world's start cell
	Games      int
	Ticks      int
	Seed       uint64
	Buttons    []string // buttons the random player uses (default: all of them)
	Invariants []string // default: the project's
	Parallel   int      // concurrent games (default: GOMAXPROCS)
}

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

// hold is a button held from Start to End (End 0: never released) — the unit minimization
// removes, so press and release always stay paired.
type hold struct {
	Button     string
	Start, End int
}

// fuzzGame is the random input of one game.
type fuzzGame struct {
	holds []hold
}

// gameSeed derives the seed of game i from the run seed (splitmix-style mixing).
func gameSeed(seed uint64, i int) uint64 {
	z := seed + uint64(i+1)*0x9e3779b97f4a7c15
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

// genGame draws a random player: buttons held for random durations.
func genGame(rng *sim.RNG, ticks int, buttons []string) fuzzGame {
	var g fuzzGame
	held := map[string]int{} // button → index into g.holds (lookup only)
	for t := 1; t <= ticks; t++ {
		if rng.Chance(0.06) {
			b := buttons[rng.Intn(len(buttons))]
			if i, ok := held[b]; ok {
				g.holds[i].End = t
				delete(held, b)
			} else {
				held[b] = len(g.holds)
				g.holds = append(g.holds, hold{Button: b, Start: t})
			}
		}
	}
	return g
}

// events turns holds (up to tick limit) into scenario input events.
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
		get(h.Start).Press = append(get(h.Start).Press, h.Button)
		if h.End > 0 && h.End <= limit {
			get(h.End).Release = append(get(h.End).Release, h.Button)
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
// of the first one to tests/scenarios/fuzz_<hash>.vscenario.
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
	if len(o.Buttons) == 0 {
		o.Buttons = asset.ButtonNames
	}
	for _, b := range o.Buttons {
		if !asset.IsButton(b) {
			return nil, usagef("fuzz: unknown button %q (the buttons are %s)", b, strings.Join(asset.ButtonNames, ", "))
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
	for i := range games {
		games[i] = genGame(sim.NewRNG(gameSeed(o.Seed, i)), o.Ticks, o.Buttons)
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
// over button holds, keeping the same invariant violated, then writes the repro scenario.
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
	// ddmin on the list of holds.
	type item = hold
	build := func(its []item) fuzzGame { return fuzzGame{holds: append([]hold(nil), its...)} }
	cur := append([]item(nil), g.holds...)
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
	path := filepath.Join(s.Root, "tests", "scenarios", name+".vscenario")
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
	return out
}

func init() {
	register(command{
		name:    "fuzz",
		usage:   "fuzz --scene S | --world W --at x,z  --games N --ticks T --seed N [--buttons up,a] [--invariants a,b]",
		summary: "play random-input games, report invariant violations, write a minimized repro scenario",
		project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("fuzz", env.Stderr)
			var o FuzzOptions
			var buttons, invs, at string
			fs.StringVar(&o.Scene, "scene", "", "scene (default: the project's default world or scene)")
			fs.StringVar(&o.World, "world", "", "world instead of a scene")
			fs.StringVar(&at, "at", "", "with --world: start cell x,z")
			fs.IntVar(&o.Games, "games", 200, "number of random games")
			fs.IntVar(&o.Ticks, "ticks", 200, "ticks per game")
			fs.Uint64Var(&o.Seed, "seed", 1, "seed of the random players")
			fs.StringVar(&buttons, "buttons", "", "comma-separated buttons the players use (default: all)")
			fs.StringVar(&invs, "invariants", "", "comma-separated invariants (default: the project's)")
			fs.IntVar(&o.Parallel, "parallel", 0, "concurrent games (default: CPUs)")
			if err := parseFlags(fs, args); err != nil {
				return nil, err
			}
			var err error
			if o.At, err = parseCellFlag(at); err != nil {
				return nil, err
			}
			if buttons != "" {
				o.Buttons = strings.Split(buttons, ",")
			}
			if invs != "" {
				o.Invariants = strings.Split(invs, ",")
			}
			return s.Fuzz(o)
		},
	})
}
