// Package script runs games written in Lua: a project whose veduta.json names a script.
//
// A script game is the engine's Game with the kinds its scripts define. Each run starts a
// fresh interpreter, runs the main script (which fills the game and kinds tables), and
// calls game.init, game.update and game.draw, and for every entity of a kind defined in
// Lua, kinds.<kind>.init and kinds.<kind>.update. The tool runs script games itself: no Go
// code, no build. The API is described in docs/lua.md.
package script

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riftbane/veduta/v2"
	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/lua"
	"github.com/riftbane/veduta/v2/scene"
	"github.com/riftbane/veduta/v2/sprite"
)

// APILevel is the level of the Lua API this runtime provides. It grows by one whenever a
// release adds to the API, so a game can say what it needs (veduta.json "api") and a console
// with an older runtime can refuse it up front.
const APILevel = 1

// Budget is how many steps (loop iterations and calls) one callback may run before it is
// stopped as an endless loop.
const Budget = 20_000_000

// skipDirs are directories never searched for scripts.
var skipDirs = map[string]bool{".git": true, "out": true, "bin": true, "node_modules": true}

// Game is a game written in Lua.
type Game struct {
	root    string            // the project directory
	main    string            // slash path of the main script
	sources map[string]string // slash path → source, every .lua file of the project
	stderr  io.Writer         // where print writes

	vm         *lua.VM
	ctx        *veduta.Context
	in         veduta.Input
	err        error
	entities   map[*scene.Entity]*lua.Userdata
	entityMeta *lua.Table
	modules    map[string]lua.Value
	game       *lua.Table
	kinds      *lua.Table
	engine     *lua.Table
	hud        *sprite.Batch
}

// Load reads every Lua file of the project at root, whose manifest is p. print writes to
// stderr.
func Load(root string, p *asset.Project, stderr io.Writer) (*Game, error) {
	if p.Script == "" {
		return nil, fmt.Errorf("script: %s names no script", asset.ProjectFile)
	}
	if p.API > APILevel {
		return nil, fmt.Errorf("script: the game needs Lua API level %d, and this runtime (engine %s) has level %d: update the console", p.API, veduta.Version, APILevel)
	}
	g := &Game{root: root, main: p.Script, sources: map[string]string{}, stderr: stderr}
	cooked := filepath.Clean(filepath.Join(root, p.Cooked))
	err := filepath.WalkDir(root, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if file != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") || filepath.Clean(file) == cooked) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(file, ".lua") {
			return nil
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		g.sources[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("script: %w", err)
	}
	if _, ok := g.sources[g.main]; !ok {
		return nil, fmt.Errorf("script: %s names the script %q, which does not exist", asset.ProjectFile, g.main)
	}
	return g, nil
}

// Reload reads the scripts again, for the next run: the player calls it on F9 and when the
// simulator sees a file change. On an error the scripts stay as they were.
func (g *Game) Reload() error {
	p, err := cook.ReadProject(g.root)
	if err != nil {
		return err
	}
	fresh, err := Load(g.root, p, g.stderr)
	if err != nil {
		return err
	}
	if errs := fresh.SyntaxErrors(); len(errs) > 0 {
		return errs[0]
	}
	g.main, g.sources = fresh.main, fresh.sources
	return nil
}

// SyntaxErrors compiles every script and returns the errors, in file order.
func (g *Game) SyntaxErrors() []*lua.SyntaxError {
	var out []*lua.SyntaxError
	vm := lua.New(lua.Options{})
	for _, name := range sortedKeys(g.sources) {
		if _, err := vm.Load(name, g.sources[name]); err != nil {
			var se *lua.SyntaxError
			if errors.As(err, &se) {
				out = append(out, se)
			}
		}
	}
	return out
}

// Run is the entry point of a script game, as veduta.RunArgs is of a Go game: it reads the
// project named by -project and runs the player, or with -headless a subcommand.
func Run(args []string, stdout, stderr io.Writer) int {
	root := "."
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case (a == "-project" || a == "--project") && i+1 < len(args):
			root = args[i+1]
		case strings.HasPrefix(a, "-project=") || strings.HasPrefix(a, "--project="):
			root = a[strings.IndexByte(a, '=')+1:]
		}
	}
	p, err := cook.ReadProject(root)
	if err == nil && p.Script == "" {
		err = fmt.Errorf("%s names no script: this is a Go game, run its binary", asset.ProjectFile)
	}
	var g *Game
	if err == nil {
		g, err = Load(root, p, stderr)
	}
	if err != nil {
		fmt.Fprintf(stdout, "{\"ok\":false,\"error\":%q}\n", err.Error())
		fmt.Fprintln(stderr, err)
		return 1
	}
	return veduta.RunArgs(g, args, stdout, stderr)
}

// Start begins a run: a fresh interpreter, the API, and the main script.
func (g *Game) Start(ctx *veduta.Context) error {
	g.ctx, g.err, g.in = ctx, nil, veduta.Input{}
	g.entities = map[*scene.Entity]*lua.Userdata{}
	g.modules = map[string]lua.Value{}
	g.vm = lua.New(lua.Options{Stdout: g.stderr})
	g.vm.SetBudget(Budget)
	g.vm.SetRandom(ctx.RNG)
	g.game, g.kinds, g.engine = lua.NewTable(0, 4), lua.NewTable(0, 8), lua.NewTable(0, 8)
	g.vm.SetGlobal("game", lua.TableValue(g.game))
	g.vm.SetGlobal("kinds", lua.TableValue(g.kinds))
	g.vm.SetGlobal("engine", lua.TableValue(g.engine))
	g.install()
	g.engine.SetString("api", lua.Int(APILevel))
	g.engine.SetString("name", lua.String(ctx.Project.Name))
	g.engine.SetString("title", lua.String(ctx.Project.Title))
	g.syncEngine()
	if _, err := g.require(g.main); err != nil {
		return err
	}
	return nil
}

// require runs a module once and returns what it returned.
func (g *Game) require(file string) (lua.Value, error) {
	if v, ok := g.modules[file]; ok {
		return v, nil
	}
	src, ok := g.sources[file]
	if !ok {
		return lua.Nil, fmt.Errorf("module %q not found (looked for %s)", strings.TrimSuffix(file, ".lua"), file)
	}
	f, err := g.vm.Load(file, src)
	if err != nil {
		return lua.Nil, err
	}
	g.modules[file] = lua.True // a module that requires itself gets true, as in Lua
	res, err := g.vm.Call(lua.FunctionValue(f))
	if err != nil {
		delete(g.modules, file)
		return lua.Nil, err
	}
	v := lua.True
	if len(res) > 0 && !res[0].IsNil() {
		v = res[0]
	}
	g.modules[file] = v
	return v, nil
}

// syncEngine updates the engine table the scripts read.
func (g *Game) syncEngine() {
	c := g.ctx
	g.engine.SetString("tick", lua.Int(int64(c.Tick)))
	g.engine.SetString("dt", lua.Float(float64(c.DT)))
	g.engine.SetString("width", lua.Int(int64(c.Width)))
	g.engine.SetString("height", lua.Int(int64(c.Height)))
	g.engine.SetString("headless", lua.Bool(c.Headless))
}

// call calls a callback unless an earlier one failed.
func (g *Game) call(fn lua.Value, args ...lua.Value) {
	if g.err != nil || fn.IsNil() {
		return
	}
	if _, err := g.vm.Call(fn, args...); err != nil {
		g.err = err
	}
}

// Init calls game.init.
func (g *Game) Init(ctx *veduta.Context) error {
	g.syncEngine()
	g.call(g.game.GetString("init"))
	return nil
}

// Update calls game.update.
func (g *Game) Update(ctx *veduta.Context, in veduta.Input) {
	g.in = in
	g.syncEngine()
	g.pruneEntities()
	g.call(g.game.GetString("update"))
}

// Draw calls game.draw with the hud functions drawing over the frame.
func (g *Game) Draw(ctx *veduta.Context, dl *gfx.DrawList) {
	g.syncEngine()
	fn := g.game.GetString("draw")
	if fn.IsNil() || g.err != nil {
		return
	}
	g.hud = ctx.HUD(dl)
	g.call(fn)
	g.hud.End()
	g.hud = nil
}

// Err returns the first error a script raised in this run.
func (g *Game) Err() error { return g.err }

// Kinds lists the kinds the scripts define.
func (g *Game) Kinds() []string {
	var out []string
	g.kinds.ForEach(func(k, v lua.Value) bool {
		if s, ok := k.Str(); ok && v.Table() != nil {
			out = append(out, s)
		}
		return true
	})
	sort.Strings(out)
	return out
}

// Kind returns the constructor of a kind the scripts define, or nil.
func (g *Game) Kind(name string) func(*scene.Entity) veduta.Behaviour {
	def := g.kinds.GetString(name).Table()
	if def == nil {
		return nil
	}
	return func(e *scene.Entity) veduta.Behaviour {
		if _, ok := e.State.(*State); !ok {
			e.State = &State{Table: lua.NewTable(0, 4)}
		}
		g.call(def.GetString("init"), g.entity(e))
		return behaviour{g: g, def: def}
	}
}

type behaviour struct {
	g   *Game
	def *lua.Table
}

func (b behaviour) Update(ctx *veduta.Context, e *scene.Entity, in veduta.Input) {
	b.g.in = in
	b.g.call(b.def.GetString("update"), b.g.entity(e))
}

// entity returns the Lua value of an entity, the same value every time.
func (g *Game) entity(e *scene.Entity) lua.Value {
	if e == nil {
		return lua.Nil
	}
	if u, ok := g.entities[e]; ok {
		return lua.UserdataValue(u)
	}
	u := &lua.Userdata{Data: e, Meta: g.entityMeta}
	g.entities[e] = u
	return lua.UserdataValue(u)
}

// pruneEntities forgets the values of entities that are gone, once there are many.
func (g *Game) pruneEntities() {
	if len(g.entities) < 256 || len(g.entities) < 2*g.ctx.Scene.Len() {
		return
	}
	for e := range g.entities {
		if !e.Alive() || g.ctx.Scene.Get(e.ID) != e {
			delete(g.entities, e)
		}
	}
}

// State is the state of an entity of a Lua kind: a table the trace records as
// state.<key>.
type State struct {
	Table *lua.Table
}

// CanonicalValue converts the table for the trace: a sequence becomes an array, any other
// table an object whose keys are the table's keys as strings.
func (s *State) CanonicalValue() any { return toGo(lua.TableValue(s.Table), 0) }

func toGo(v lua.Value, depth int) any {
	switch v.Type() {
	case lua.TypeNil:
		return nil
	case lua.TypeBoolean:
		b, _ := v.Bool()
		return b
	case lua.TypeNumber:
		if i, ok := v.Int(); ok {
			return i
		}
		f, _ := v.Float()
		return f
	case lua.TypeString:
		s, _ := v.Str()
		return s
	case lua.TypeTable:
		if depth >= 16 {
			return "table"
		}
		t := v.Table()
		n, count := int64(t.Len()), int64(0)
		t.ForEach(func(k, _ lua.Value) bool { count++; return true })
		if n > 0 && n == count {
			out := make([]any, n)
			for i := range out {
				out[i] = toGo(t.GetInt(int64(i)+1), depth+1)
			}
			return out
		}
		out := map[string]any{}
		t.ForEach(func(k, val lua.Value) bool {
			out[k.String()] = toGo(val, depth+1)
			return true
		})
		return out
	case lua.TypeUserdata:
		if e, ok := v.Userdata().Data.(*scene.Entity); ok {
			return "entity " + e.Name
		}
	}
	return v.Type().String()
}

// moduleFile maps a module name ("enemies.slime") to its file ("enemies/slime.lua"),
// relative to the directory of the main script.
func (g *Game) moduleFile(name string) string {
	return path.Join(path.Dir(g.main), strings.ReplaceAll(name, ".", "/")+".lua")
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// loadDir reads the project at dir.
func loadDir(dir string) (*Game, error) {
	p, err := cook.ReadProject(dir)
	if err != nil {
		return nil, err
	}
	return Load(dir, p, io.Discard)
}
