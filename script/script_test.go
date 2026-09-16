package script

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	for _, lib := range []string{"input", "scene", "camera", "world", "hud"} {
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
