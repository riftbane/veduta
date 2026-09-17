package script

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2"
	"github.com/riftbane/veduta/v2/internal/golden"
	"github.com/riftbane/veduta/v2/lua"
)

const testGame = "testdata/game"

type report struct {
	OK           bool   `json:"ok"`
	Verdict      string `json:"verdict"`
	FirstFailure string `json:"first_failure"`
	TraceHash    string `json:"trace_hash"`
	Error        string `json:"error"`
	Expect       []struct {
		Pass   bool   `json:"pass"`
		Error  string `json:"error"`
		Path   string `json:"path"`
		Actual any    `json:"actual"`
	} `json:"expectations"`
}

// run runs the test game (or a copy of it at dir) with args and decodes its report.
func run(t *testing.T, dir string, args ...string) (report, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"-project", dir, "-headless"}, args...), &stdout, &stderr)
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var r report
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &r); err != nil {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	return r, stderr.String(), code
}

// TestScenario: the scenario passes, twice with the same trace hash, and the state of Lua
// kinds is in the trace (the expectations read state.score, state.steps, state.value).
func TestScenario(t *testing.T) {
	scenario := filepath.Join(testGame, "tests", "scenarios", "collect.scenario.json")
	a, stderr, code := run(t, testGame, "simulate", "--scenario", scenario, "--out", t.TempDir())
	if code != 0 || a.Verdict != "pass" {
		t.Fatalf("exit %d, verdict %q: %s\n%+v\nstderr: %s", code, a.Verdict, a.FirstFailure, a, stderr)
	}
	b, _, _ := run(t, testGame, "simulate", "--scenario", scenario, "--out", t.TempDir())
	if a.TraceHash == "" || a.TraceHash != b.TraceHash {
		t.Fatalf("trace hashes %q and %q", a.TraceHash, b.TraceHash)
	}
	// The same on every machine: CI runs this on amd64, arm64 and Windows.
	golden.Text(t, "script_collect.hash", []byte(a.TraceHash+"\n"))
}

// TestRender draws the scene and the hud.
func TestRender(t *testing.T) {
	out := filepath.Join(t.TempDir(), "frame.png")
	r, stderr, code := run(t, testGame, "render", "--tick", "10", "--out", out)
	if code != 0 || !r.OK {
		t.Fatalf("exit %d: %+v\n%s", code, r, stderr)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Fatalf("no frame written: %v", err)
	}
}

// copyGame copies the test game into a temporary directory and replaces files in it.
func copyGame(t *testing.T, replace map[string]string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(testGame, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(testGame, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range replace {
		file := filepath.Join(dst, name)
		if rest, ok := strings.CutPrefix(src, "+"); ok { // appended to the file
			old, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			src = string(old) + "\n" + rest
		}
		os.MkdirAll(filepath.Dir(file), 0o755)
		if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// TestScriptErrors: a Lua error in any callback fails the run with the script's position
// and traceback, a syntax error is reported with its file and line, and an endless loop is
// stopped.
func TestScriptErrors(t *testing.T) {
	for _, c := range []struct {
		name, file, src, want string
	}{
		{"update", "main.lua", "+function game.update() local t = nil; return t.x end",
			"main.lua:62: attempt to index a nil value (local 't')"},
		{"kind", "main.lua", "+kinds.hero.update = function(e) e.nope = 1 end",
			"main.lua:62: entity has no field 'nope'"},
		{"syntax", "main.lua", "function game.update(\n", "main.lua:2:"},
		{"module", "lib/movement.lua", "return {step = function() error('broken module') end}", "lib/movement.lua:1: broken module"},
		{"missing module", "main.lua", "+require('nowhere')", "module \"nowhere\" not found"},
		{"loop", "main.lua", "+function game.update() while true do end end", "without returning"},
		{"no scene yet", "main.lua", "+scene.find('hero')", "no scene yet"},
		{"hud outside draw", "main.lua", "+function game.update() hud.text(0, 0, 'x') end", "only in game.draw"},
		{"bad button", "main.lua", "+function game.update() input.down('start') end", "unknown button \"start\""},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := copyGame(t, map[string]string{c.file: c.src})
			r, _, code := run(t, dir, "simulate", "--scene", "main", "--ticks", "5", "--seed", "1", "--out", t.TempDir())
			if code == 0 || !strings.Contains(r.Error, c.want) {
				t.Fatalf("exit %d, error %q, want it to contain %q", code, r.Error, c.want)
			}
		})
	}
}

// TestSyntaxErrors lists every script that does not compile.
func TestSyntaxErrors(t *testing.T) {
	dir := copyGame(t, map[string]string{"main.lua": "x = = 1", "lib/movement.lua": "return {"})
	g, err := loadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	errs := g.SyntaxErrors()
	if len(errs) != 2 || errs[0].Chunk != "lib/movement.lua" || errs[1].Chunk != "main.lua" || errs[1].Line != 1 {
		t.Fatalf("syntax errors %v", errs)
	}
}

// TestAPIDocumented: every function of the API, and every entity method, is named in
// docs/lua.md.
func TestAPIDocumented(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "docs", "lua.md"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := loadDir(testGame)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := veduta.RunArgs(g, []string{"-project", testGame, "-headless", "simulate", "--scene", "main", "--ticks", "1", "--out", t.TempDir()}, &out, io.Discard); code != 0 {
		t.Fatalf("run: %s", out.String())
	}
	for _, lib := range []string{"input", "scene", "camera", "world", "hud", "mesh", "volume", "save"} {
		g.vm.Global(lib).Table().ForEach(func(k, _ lua.Value) bool {
			if name := lib + "." + k.String(); !strings.Contains(string(doc), "`"+name) {
				t.Errorf("docs/lua.md does not document %s", name)
			}
			return true
		})
	}
	for _, name := range []string{"trace", "invariant", "require"} {
		if !strings.Contains(string(doc), "`"+name+"(") {
			t.Errorf("docs/lua.md does not document %s", name)
		}
	}
	e := g.entity(g.ctx.Scene.Find("hero"))
	for _, m := range []string{"position", "set_position", "move", "world_position", "rotation", "set_rotation", "scale", "set_scale",
		"has_tag", "add_tag", "remove_tag", "tags", "overlapping", "bounds", "despawn"} {
		if g.vm.Index(e, lua.String(m)).IsNil() {
			t.Errorf("entities have no method %s", m)
		}
		if !strings.Contains(string(doc), "`e:"+m+"(") {
			t.Errorf("docs/lua.md does not document e:%s", m)
		}
	}
}

// TestTypesMatchAPI: the editor definitions (veduta.d.lua) name every function and entity
// method the runtime has, and nothing it does not.
func TestTypesMatchAPI(t *testing.T) {
	g, err := loadDir(testGame)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := veduta.RunArgs(g, []string{"-project", testGame, "-headless", "simulate", "--scene", "main", "--ticks", "1", "--out", t.TempDir()}, &out, io.Discard); code != 0 {
		t.Fatalf("run: %s", out.String())
	}
	declared := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^function ([\w.:]+)\(`).FindAllStringSubmatch(string(Types), -1) {
		declared[m[1]] = true
	}
	have := map[string]bool{"trace": true, "invariant": true, "require": true}
	for _, lib := range []string{"input", "scene", "camera", "world", "hud", "mesh", "volume", "save"} {
		g.vm.Global(lib).Table().ForEach(func(k, _ lua.Value) bool {
			have[lib+"."+k.String()] = true
			return true
		})
	}
	e := g.entity(g.ctx.Scene.Find("hero"))
	newMesh, _ := g.vm.Call(g.vm.Global("mesh").Table().GetString("new"))
	newVolume, _ := g.vm.Call(g.vm.Global("volume").Table().GetString("new"), lua.Int(1), lua.Int(1), lua.Int(1))
	objects := map[string]lua.Value{"Entity:": e, "Mesh:": newMesh[0], "Volume:": newVolume[0]}
	for name := range declared {
		for prefix, obj := range objects {
			if m, ok := strings.CutPrefix(name, prefix); ok {
				if g.vm.Index(obj, lua.String(m)).IsNil() {
					t.Errorf("veduta.d.lua declares %s, which the runtime does not have", name)
				}
				have[name] = true
			}
		}
	}
	for prefix, obj := range objects {
		if prefix == "Entity:" {
			continue // an entity's __index is a function: its methods are checked above
		}
		obj.Userdata().Meta.GetString("__index").Table().ForEach(func(k, _ lua.Value) bool {
			have[prefix+k.String()] = true
			return true
		})
	}
	for name := range have {
		if !declared[name] {
			t.Errorf("veduta.d.lua does not declare %s", name)
		}
	}
	for name := range declared {
		if !have[name] {
			t.Errorf("veduta.d.lua declares %s, which the runtime does not have", name)
		}
	}
	for _, field := range EntityFields {
		if !strings.Contains(string(Types), "---@field "+field+" ") {
			t.Errorf("veduta.d.lua does not declare the entity field %s", field)
		}
		if _, err := g.vm.Call(lua.FunctionValue(lua.NewFunction("get", func(vm *lua.VM, args []lua.Value) []lua.Value {
			return vm.Ret(vm.Index(e, lua.String(field)))
		}))); err != nil {
			t.Errorf("entities have no field %s: %v", field, err)
		}
	}
	for _, field := range []string{"tick", "dt", "width", "height", "headless", "name", "title", "api"} {
		if g.engine.Get(lua.String(field)).IsNil() || !strings.Contains(string(Types), "---@field "+field+" ") {
			t.Errorf("engine.%s: in the runtime or in veduta.d.lua but not both", field)
		}
	}
}

// TestReload: Reload reads the scripts again for the next run, and keeps the old ones when
// the new ones do not compile.
func TestReload(t *testing.T) {
	dir := copyGame(t, nil)
	g, err := loadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "lib", "extra.lua"), []byte("return 42\n"), 0o644)
	if err := g.Reload(); err != nil || g.sources["lib/extra.lua"] != "return 42\n" {
		t.Fatalf("reload: %v, sources %v", err, sortedKeys(g.sources))
	}
	os.WriteFile(filepath.Join(dir, "main.lua"), []byte("function game.update(\n"), 0o644)
	if err := g.Reload(); err == nil || !strings.Contains(err.Error(), "main.lua:2") {
		t.Fatalf("reload of a broken script: %v", err)
	}
	if !strings.Contains(g.sources["main.lua"], "kinds.hero") {
		t.Fatal("a failed reload replaced the scripts")
	}
}

// TestAPILevel: a game that needs a later API level is refused before it runs.
func TestAPILevel(t *testing.T) {
	dir := copyGame(t, map[string]string{"veduta.json": `{"veduta": "project/1", "name": "later", "engine": "v9.0.0", "script": "main.lua", "api": 99}`})
	r, _, code := run(t, dir, "simulate", "--scene", "main", "--ticks", "1", "--out", t.TempDir())
	if code == 0 || !strings.Contains(r.Error, "needs Lua API level 99") || !strings.Contains(r.Error, fmt.Sprintf("has level %d", APILevel)) {
		t.Fatalf("exit %d: %s", code, r.Error)
	}
}

// TestSnapshotReplay: a script game's snapshot is its run, and restoring it plays the run
// again, so the ticks after a restore are the ticks of an uninterrupted run: the interpreter's
// module variables (score) and the entities' state tables included.
func TestSnapshotReplay(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	os.WriteFile(input, []byte(`[
		{"tick": 1, "press": ["right"]}, {"tick": 20, "release": ["right"]},
		{"tick": 21, "press": ["b", "a"]}, {"tick": 22, "release": ["b", "a"]},
		{"tick": 30, "press": ["left"]}, {"tick": 49, "release": ["left"]}
	]`), 0o644)
	lines := func(file string) []string {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}

	snap := filepath.Join(dir, "s.snap")
	if r, stderr, code := run(t, testGame, "snapshot", "--tick", "25", "--input", input, "--out", snap); code != 0 || !r.OK {
		t.Fatalf("snapshot: exit %d %+v %s", code, r, stderr)
	}
	restored := filepath.Join(dir, "restored.jsonl")
	if r, stderr, code := run(t, testGame, "snapshot", "--restore", snap, "--ticks", "30", "--input", input, "--out", restored); code != 0 || !r.OK {
		t.Fatalf("restore: exit %d %+v %s", code, r, stderr)
	}
	whole := filepath.Join(dir, "whole")
	if _, stderr, code := run(t, testGame, "simulate", "--scene", "main", "--ticks", "55", "--input", input, "--out", whole); code != 0 {
		t.Fatalf("simulate: exit %d %s", code, stderr)
	}
	after, all := lines(restored), lines(filepath.Join(whole, "trace.jsonl"))
	if len(after) != 30 || len(all) != 56 {
		t.Fatalf("%d restored ticks and %d simulated, want 30 and 56", len(after), len(all))
	}
	for i, line := range after {
		if line != all[26+i] {
			t.Fatalf("tick %d after the restore differs from the uninterrupted run:\n%s\n%s", 26+i, line, all[26+i])
		}
	}
	if !strings.Contains(after[len(after)-1], `"score":1`) {
		t.Errorf("the last tick lost the score: %s", after[len(after)-1])
	}

	// A snapshot of another game does not restore.
	other := copyGame(t, nil)
	main := filepath.Join(other, "main.lua")
	src, _ := os.ReadFile(main)
	os.WriteFile(main, bytes.Replace(src, []byte("score = score + 1"), []byte("score = score + 2"), 1), 0o644)
	if r, _, code := run(t, other, "snapshot", "--restore", snap, "--ticks", "1"); code == 0 || !strings.Contains(r.Error, "replayed run") {
		t.Fatalf("a changed game restored: exit %d %+v", code, r)
	}
}

// TestEntityHierarchy: scripts give entities parents and hitboxes, when spawning them and
// later, and spawn prefabs placed and turned as a world places them.
func TestEntityHierarchy(t *testing.T) {
	dir := copyGame(t, map[string]string{
		"assets/prefabs/hut.prefab.json": `{
			"veduta": "prefab/1", "footprint": [4, 2],
			"entities": [
				{ "name": "roof", "kind": "static", "model": "quad", "parent": "walls", "position": [0, 2, 0] },
				{ "name": "walls", "kind": "static", "model": "quad", "position": [1, 0, 0.5], "tags": ["hut"] }
			]
		}`,
		"main.lua": `+
local problems = {}
local function check(ok, what) if not ok then problems[#problems + 1] = what end end

local init = game.init
function game.init()
  init()
  local hero = scene.find("hero")
  local cart = scene.spawn{name = "cart", model = "quad", position = {2, 0, 0}, parent = hero,
    hitbox = {{-1, -1, -1}, {1, 1, 1}}}
  local wheel = scene.spawn{name = "wheel", model = "quad", parent = "cart", position = {0, -1, 0}}
  check(cart.parent == hero and wheel.parent == cart, "spawn parents")
  check(cart.hitbox[2][1] == 1 and wheel.hitbox == nil, "spawn hitbox")
  check(#hero:children() == 1 and hero:children()[1] == cart, "children")
  check(not pcall(function() hero.parent = wheel end), "a cycle was accepted")
  check(not pcall(function() cart.hitbox = {{1, 0, 0}, {0, 1, 1}} end), "an inverted hitbox was accepted")
  wheel.parent = nil
  wheel.hitbox = {{-0.5, -0.5, -0.5}, {0.5, 0.5, 0.5}}
  check(wheel.parent == nil and #cart:children() == 0, "parent cleared")

  local hut = scene.spawn_prefab("hut", 10, 0, 20, 90, "hut_a")
  check(#hut == 2 and hut[1] == hut.roof and hut.walls.name == "hut_a_walls", "prefab table")
  check(hut.roof.parent == hut.walls and hut.walls:has_tag("hut"), "prefab parents and tags")
  local x, _, z = hut.walls:position()
  -- footprint 4 x 2 turned by 90: walls at (1, 0.5) from the corner end up at (0.5, 3)
  check(x == 10.5 and z == 23, "prefab placement " .. x .. " " .. z)
  check(not pcall(scene.spawn_prefab, "nope", 0, 0, 0), "an unknown prefab was accepted")
  check(not pcall(scene.spawn_prefab, "hut", 0, 0, 0, 45), "a 45 degree rotation was accepted")
  trace("hierarchy", {problems = table.concat(problems, "; ")})
end
`,
	})
	out := t.TempDir()
	r, stderr, code := run(t, dir, "simulate", "--scene", "main", "--ticks", "2", "--out", out)
	if code != 0 {
		t.Fatalf("exit %d %+v %s", code, r, stderr)
	}
	trace, _ := os.ReadFile(filepath.Join(out, "trace.jsonl"))
	if !strings.Contains(string(trace), `"problems":""`) {
		i := strings.Index(string(trace), `"hierarchy"`)
		t.Fatalf("checks failed: %s", string(trace)[i:min(len(trace), i+300)])
	}
	last := string(trace)[strings.LastIndex(strings.TrimSpace(string(trace)), "\n"):]
	for _, want := range []string{`"name":"cart"`, `"parent":"hero"`, `"name":"hut_a_roof"`, `"parent":"hut_a_walls"`} {
		if !strings.Contains(last, want) {
			t.Errorf("the trace lacks %s", want)
		}
	}
	// The cart's hitbox follows the hero: its world box is centred 2 units right of him.
	if !regexp.MustCompile(`\{"aabb":\{"max":\[3,1,1\],"min":\[1,-1,-1\]\},"id":\d+,"kind":"static","material":"","model":"quad","name":"cart"`).MatchString(last) {
		i := strings.Index(last, `"name":"cart"`)
		t.Errorf("cart's box: %s", last[i:min(len(last), i+300)])
	}
}

// TestHUDImages: hud.image draws a part of a texture where and as large as asked, and
// hud.panel keeps its corners while it stretches.
func TestHUDImages(t *testing.T) {
	dir := copyGame(t, map[string]string{
		"assets/textures/icons.tex.json": `{"veduta": "texture/1", "size": [8, 4], "mipmaps": false, "layers": [
			{"type": "rect", "xy": [0, 0], "size": [4, 4], "color": "#ff0000"},
			{"type": "rect", "xy": [4, 0], "size": [4, 4], "color": "#0000ff"}]}`,
		"assets/textures/frame.tex.json": `{"veduta": "texture/1", "size": [6, 6], "mipmaps": false, "layers": [
			{"type": "solid", "color": "#00ff00"},
			{"type": "rect", "xy": [0, 0], "size": [6, 6], "color": "#ffffff", "outline": 2}]}`,
		"main.lua": `+
function game.draw()
  hud.image("icons", 10, 10, {src = {4, 0, 4, 4}, w = 16, h = 16})
  hud.image("icons", 40, 10)
  hud.panel("frame", 100, 100, 60, 40, 2)
  local w, h = hud.image_size("icons")
  if w ~= 8 or h ~= 4 then error("image_size " .. w .. "x" .. h) end
end
`,
	})
	out := filepath.Join(t.TempDir(), "frame.png")
	if r, stderr, code := run(t, dir, "render", "--out", out); code != 0 {
		t.Fatalf("exit %d %+v %s", code, r, stderr)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) string {
		r, g, b, _ := img.At(x, y).RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	for _, c := range []struct {
		x, y int
		want string
	}{
		{10, 10, "#0000ff"}, {25, 25, "#0000ff"}, // the blue icon, 16 pixels wide
		{41, 11, "#ff0000"}, {46, 11, "#0000ff"}, // the whole sheet, at its size
		{100, 100, "#ffffff"}, {101, 139, "#ffffff"}, {158, 120, "#ffffff"}, // the frame's border, 2 pixels
		{103, 103, "#00ff00"}, {130, 120, "#00ff00"}, // its middle, stretched
	} {
		if got := at(c.x, c.y); got != c.want {
			t.Errorf("pixel (%d, %d) is %s, want %s", c.x, c.y, got, c.want)
		}
	}

	for name, src := range map[string]string{
		"no texture": `hud.image("nope", 0, 0)`,
		"bad src":    `hud.image("icons", 0, 0, {src = {6, 0, 4, 4}})`,
		"bad border": `hud.panel("frame", 0, 0, 10, 10, {1, 2})`,
	} {
		bad := copyGame(t, map[string]string{
			"assets/textures/icons.tex.json": `{"veduta": "texture/1", "size": [8, 4], "layers": [{"type": "solid", "color": "#ff0000"}]}`,
			"assets/textures/frame.tex.json": `{"veduta": "texture/1", "size": [6, 6], "layers": [{"type": "solid", "color": "#ff0000"}]}`,
			"main.lua":                       "+function game.draw() " + src + " end",
		})
		r, _, code := run(t, bad, "render", "--out", filepath.Join(t.TempDir(), "x.png"))
		if code == 0 || r.Error == "" {
			t.Errorf("%s: exit %d %+v", name, code, r)
		}
	}
}

// TestSaves: a script's save comes back with its types and nesting; a scenario's saves
// are there when the run starts; writes are trace events; what a save cannot hold is an
// error.
func TestSaves(t *testing.T) {
	dir := copyGame(t, map[string]string{
		"main.lua": `+
local problems = {}
local function check(ok, what) if not ok then problems[#problems + 1] = what end end

local init = game.init
function game.init()
  init()
  local old = save.read("slot1")
  check(old and old.gold == 120 and math.type(old.gold) == "integer", "scenario save")
  check(old and math.type(old.ratio) == "float" and old.ratio == 2.0, "float stays float")
  check(save.read("nothing") == nil, "a missing save")

  local data = {gold = 7, ratio = 0.5, name = "Mira", flags = {bridge = true}, party = {"mira", "tobi"}, big = 2^53}
  check(save.write("slot2", data) == true, "write")
  local back = save.read("slot2")
  check(back ~= data and back.party[2] == "tobi" and back.flags.bridge == true, "read back")
  check(math.type(back.gold) == "integer" and math.type(back.big) == "float", "number types")
  local keys = {}
  for k in pairs(back) do keys[#keys + 1] = k end
  check(table.concat(keys, ",") == "big,flags,gold,name,party,ratio", "sorted keys: " .. table.concat(keys, ","))
  check(table.concat(save.list(), ",") == "slot1,slot2", "list")
  check(save.remove("slot1") == true and save.read("slot1") == nil, "remove")

  local cyclic = {}
  cyclic.self = cyclic
  for what, bad in pairs({
    ["a function"] = function() save.write("x", {f = print}) end,
    ["an entity"] = function() save.write("x", {e = scene.find("hero")}) end,
    ["a float key"] = function() save.write("x", {[1.5] = 1}) end,
    ["a cycle"] = function() save.write("x", cyclic) end,
    ["nan"] = function() save.write("x", {n = 0/0}) end,
    ["a bad name"] = function() save.write("Slot 1", {}) end,
    ["not a table"] = function() save.write("x", 3) end,
  }) do
    check(not pcall(bad), what .. " was saved")
  end
  trace("saves", {problems = table.concat(problems, "; ")})
end
`,
		"tests/scenarios/saves.scenario.json": `{
			"veduta": "scenario/1", "scene": "main", "seed": 1, "ticks": 3,
			"saves": {"slot1": {"gold": 120, "ratio": 2.0}},
			"expect": [
				{"tick": 3, "trace": "save_write", "count_min": 1, "count_max": 1},
				{"tick": 3, "trace": "save_remove", "count_min": 1, "count_max": 1}
			]
		}`,
	})
	out := t.TempDir()
	r, stderr, code := run(t, dir, "simulate", "--scenario", filepath.Join(dir, "tests", "scenarios", "saves.scenario.json"), "--out", out)
	if code != 0 || r.Verdict != "pass" {
		t.Fatalf("exit %d %+v %s", code, r, stderr)
	}
	trace, _ := os.ReadFile(filepath.Join(out, "trace.jsonl"))
	if !strings.Contains(string(trace), `"problems":""`) {
		i := strings.Index(string(trace), `"event":"saves"`)
		t.Fatalf("checks failed: %s", string(trace)[max(0, i-400):min(len(trace), i+50)])
	}
	if _, err := os.Stat(filepath.Join(dir, "out", "saves")); err == nil {
		t.Error("a headless run wrote saves to disk")
	}
}
