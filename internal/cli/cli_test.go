package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/docs"
	"github.com/riftbane/veduta/v2/inspect"
	"github.com/riftbane/veduta/v2/internal/golden"
	"github.com/riftbane/veduta/v2/internal/testgame"
)

// The docs tool's description names every topic, so an agent reading the tool list learns
// that the extra topics (the 2d recipe among them) exist.
func TestMCPDocsToolNamesEveryTopic(t *testing.T) {
	for _, tl := range (&mcpServer{}).tools() {
		if tl.Name != "docs" {
			continue
		}
		words := map[string]bool{}
		for _, w := range strings.FieldsFunc(tl.Description, func(r rune) bool { return r == ' ' || r == ',' || r == '(' || r == ')' || r == '.' || r == ':' }) {
			words[w] = true
		}
		for _, topic := range docs.All() {
			if !words[topic] {
				t.Errorf("docs tool description %q does not name topic %q", tl.Description, topic)
			}
		}
		return
	}
	t.Fatal("no docs tool")
}

// newProject creates a project from the engine's test game with veduta init against this
// engine checkout.
func newProject(t *testing.T) (string, *Env) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a game; skipped in -short mode")
	}
	root, err := golden.Root()
	if err != nil {
		t.Fatal(err)
	}
	env := &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}
	dir := filepath.Join(t.TempDir(), "demo")
	r, err := Init(env, InitOptions{Dir: dir, EngineDir: root, game: testgame.FS, gamePackage: testgame.GamePackage})
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK || r.Name != "demo" || len(r.Warnings) != 0 {
		t.Fatalf("init: %+v", r)
	}
	return dir, env
}

func TestInitAndCommands(t *testing.T) {
	dir, env := newProject(t)
	for _, f := range []string{"go.mod", "go.sum", "CLAUDE.md", "CHANGELOG.md", ".mcp.json", ".gitignore", ".github/workflows/release.yml", "cmd/game/main.go", "game/kinds.go", "assets/scenes/main.scene.json", "tests/scenarios/move.scenario.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil && f != "go.sum" {
			t.Fatalf("missing %s: %v", f, err)
		}
	}
	// The project is ready for the console: a card description the console's frozen card/1
	// reader accepts, and a release that builds the board's architecture.
	var card map[string]string
	if data, err := os.ReadFile(filepath.Join(dir, "card.json")); err != nil || json.Unmarshal(data, &card) != nil {
		t.Fatalf("card.json: %v\n%s", err, data)
	}
	if want := map[string]string{"veduta": "card/1", "title": "demo", "name": "demo", "exec": "demo"}; !reflect.DeepEqual(card, want) {
		t.Fatalf("card.json = %v, want %v", card, want)
	}
	if wf, _ := os.ReadFile(filepath.Join(dir, ".github", "workflows", "release.yml")); !strings.Contains(string(wf), "linux/arm64") || strings.Contains(string(wf), "windows") {
		t.Fatalf("release workflow does not build for the console only:\n%s", wf)
	}
	main, _ := os.ReadFile(filepath.Join(dir, "cmd", "game", "main.go"))
	if !strings.Contains(string(main), `"demo/game"`) || strings.Contains(string(main), "testgame/game") {
		t.Fatalf("import path not rewritten:\n%s", main)
	}
	s, err := OpenSession(filepath.Join(dir, "game"), env) // found from a subdirectory
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Build(true)
	if err != nil || !b.OK {
		t.Fatalf("build: %+v %v", b, err)
	}
	tr, err := s.Test(false)
	if err != nil || !tr.OK || len(tr.Scenarios) != 6 {
		t.Fatalf("test: %s %v", tr.Human(), err)
	}
	// Break the game: compile errors come back located.
	kinds := filepath.Join(dir, "game", "kinds.go")
	src, _ := os.ReadFile(kinds)
	os.WriteFile(kinds, bytes.Replace(src, []byte("st.VelY = JumpSpeed"), []byte("st.VelY = undefinedThing"), 1), 0o644)
	b, err = s.Build(false)
	if err != nil || b.OK || len(b.Errors) == 0 || b.Errors[0].File != "game/kinds.go" || b.Errors[0].Line == 0 {
		t.Fatalf("broken build: %+v %v", b, err)
	}
	if _, err := s.Render(RenderOptions{}); err == nil || !strings.Contains(err.Error(), "game/kinds.go") {
		t.Fatalf("render on a broken build: %v", err)
	}
	os.WriteFile(kinds, src, 0o644)
	// Exit codes through Main.
	var out bytes.Buffer
	code := Main([]string{"--project", dir, "--json", "simulate", "--scene", "main", "--ticks", "30"}, Env{Version: "dev", Stdout: &out, Stderr: io.Discard})
	if code != 0 || !strings.Contains(out.String(), `"verdict":"pass"`) {
		t.Fatalf("simulate via Main: %d %s", code, out.String())
	}
	if code := Main([]string{"bogus"}, Env{Stdout: io.Discard, Stderr: io.Discard}); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
}

// mcpClient drives ServeMCP over pipes.
type mcpClient struct {
	t     *testing.T
	in    io.WriteCloser
	out   *bufio.Scanner
	id    int
	sizes []image.Point // sizes of the image blocks of the last tool call
}

func (c *mcpClient) call(method string, params any) map[string]any {
	c.t.Helper()
	c.id++
	req := map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method}
	if params != nil {
		req["params"] = params
	}
	b, _ := json.Marshal(req)
	c.in.Write(append(b, '\n'))
	if !c.out.Scan() {
		c.t.Fatalf("%s: no response", method)
	}
	var resp map[string]any
	if err := json.Unmarshal(c.out.Bytes(), &resp); err != nil {
		c.t.Fatal(err)
	}
	if resp["error"] != nil {
		c.t.Fatalf("%s: %v", method, resp["error"])
	}
	return resp["result"].(map[string]any)
}

// tool calls a tool and returns its JSON text and the number of image blocks.
func (c *mcpClient) tool(name string, args map[string]any) (map[string]any, int, bool) {
	c.t.Helper()
	res := c.call("tools/call", map[string]any{"name": name, "arguments": args})
	var text map[string]any
	images := 0
	c.sizes = nil
	for _, b := range res["content"].([]any) {
		m := b.(map[string]any)
		switch m["type"] {
		case "text":
			json.Unmarshal([]byte(m["text"].(string)), &text)
			if text == nil {
				text = map[string]any{"text": m["text"]}
			}
		case "image":
			if m["mimeType"] != "image/png" || len(m["data"].(string)) < 100 {
				c.t.Fatalf("%s: bad image block", name)
			}
			data, err := base64.StdEncoding.DecodeString(m["data"].(string))
			if err != nil {
				c.t.Fatalf("%s: image block: %v", name, err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				c.t.Fatalf("%s: image block: %v", name, err)
			}
			c.sizes = append(c.sizes, image.Pt(cfg.Width, cfg.Height))
			images++
		}
	}
	return text, images, res["isError"] == true
}

// TestMCPImageSizes pins the MCP image limits to the 320×240 console panel: renders
// default to one panel frame, at most two panel pixels per image pixel, a missing side
// follows at 4:3, and sheets are fitted inside 640 pixels wide without shrinking a
// two-row 4:3 contact sheet.
func TestMCPImageSizes(t *testing.T) {
	for _, c := range []struct{ w, h, wantW, wantH int }{
		{0, 0, 320, 240},
		{-5, 0, 320, 240},
		{640, 0, 640, 480},
		{0, 480, 640, 480},
		{200, 0, 200, 150},
		{0, 150, 200, 150},
		{0, 600, 640, 480},
		{2000, 0, 640, 480},
		{1280, 720, 640, 480},
		{240, 320, 240, 320},
		{400, 900, 400, 480},
		{4, 4, 16, 16},
	} {
		if w, h := clampSize(c.w, c.h); w != c.wantW || h != c.wantH {
			t.Errorf("clampSize(%d, %d) = %dx%d, want %dx%d", c.w, c.h, w, h, c.wantW, c.wantH)
		}
	}
	if mcpSheetMaxW != 640 || mcpSheetMaxH < 482 {
		t.Errorf("sheet limit %dx%d cannot hold a 640x482 contact sheet unscaled", mcpSheetMaxW, mcpSheetMaxH)
	}

	encode := func(w, h int) []byte {
		var b bytes.Buffer
		if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	size := func(data []byte) image.Point {
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return image.Pt(cfg.Width, cfg.Height)
	}
	for _, c := range []struct {
		w, h int
		want image.Point
	}{
		{640, 482, image.Pt(640, 482)},  // 2×2 simulate sheet or scene summary: unscaled
		{524, 524, image.Pt(524, 524)},  // texture summary: unscaled
		{640, 1440, image.Pt(320, 720)}, // too tall: scaled to fit
		{1280, 482, image.Pt(640, 241)}, // too wide: scaled to fit
	} {
		if got := size(fitPNG(encode(c.w, c.h), mcpSheetMaxW, mcpSheetMaxH)); got != c.want {
			t.Errorf("fitPNG %dx%d into the sheet limit = %v, want %v", c.w, c.h, got, c.want)
		}
	}
}

// TestQueryPicture checks the image the query tool returns: a frame an MCP render can
// produce (up to 640×480) comes back unscaled, a larger bundle (a CLI render at a 0.x
// project's 1280×720) is fitted inside 640×480, and in every case the crosshair is a
// sharp mark at the queried pixel, not blended by the scaling.
func TestQueryPicture(t *testing.T) {
	const bg = 0xff302820
	for _, c := range []struct {
		w, h, x, y int
		want       image.Point
	}{
		{320, 240, 160, 120, image.Pt(320, 240)},
		{640, 480, 100, 50, image.Pt(640, 480)},
		{1280, 720, 640, 360, image.Pt(640, 360)},
		{1280, 720, 5, 715, image.Pt(640, 360)},
	} {
		n := c.w * c.h
		f := &inspect.Frame{FrameHeader: inspect.FrameHeader{Width: c.w, Height: c.h},
			Color: make([]uint32, n), Depth: make([]float32, n), ID: make([]uint32, n)}
		for i := range f.Color {
			f.Color[i] = bg
		}
		img := queryPicture(f, fmt.Sprintf("%d, %d", c.x, c.y), false)
		if got := image.Pt(img.W, img.H); got != c.want {
			t.Errorf("%dx%d frame: query image is %v, want %v", c.w, c.h, got, c.want)
			continue
		}
		cx, cy := c.x*img.W/c.w, c.y*img.H/c.h
		for d := -8; d <= 8; d++ {
			want := uint32(queryMark)
			if d > -2 && d < 2 {
				want = bg
			}
			for _, p := range [2]image.Point{{cx + d, cy}, {cx, cy + d}} {
				if p.X < 0 || p.Y < 0 || p.X >= img.W || p.Y >= img.H {
					continue
				}
				if got := img.At(p.X, p.Y); got != want {
					t.Errorf("%dx%d frame, query at %d,%d: pixel %v is %#x, want %#x", c.w, c.h, c.x, c.y, p, got, want)
				}
			}
		}
		if got := img.At(cx+9, cy); cx+9 < img.W && got != bg {
			t.Errorf("%dx%d frame: crosshair longer than 8 pixels (%#x)", c.w, c.h, got)
		}
		if cov := queryPicture(f, "", true); image.Pt(cov.W, cov.H) != c.want {
			t.Errorf("%dx%d frame: coverage image is %dx%d, want %v", c.w, c.h, cov.W, cov.H, c.want)
		}
	}
}

func TestMCPEndToEnd(t *testing.T) {
	dir, _ := newProject(t)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	env := &Env{Version: "dev", Stdin: inR, Stdout: outW, Stderr: io.Discard}
	done := make(chan error, 1)
	go func() { done <- ServeMCP(context.Background(), env, dir); outW.Close() }()
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	c := &mcpClient{t: t, in: inW, out: sc}

	init := c.call("initialize", map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]any{"name": "test"}})
	if init["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize: %v", init)
	}
	list := c.call("tools/list", nil)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range list {
		m := tl.(map[string]any)
		names[m["name"].(string)] = true
		if m["inputSchema"].(map[string]any)["additionalProperties"] != false {
			t.Errorf("tool %s allows additional properties", m["name"])
		}
		if m["name"] == "render" {
			// The agent sizes its requests from these strings: they must state the
			// panel-sized default and the limit that clampSize enforces.
			props := m["inputSchema"].(map[string]any)["properties"].(map[string]any)
			desc := m["description"].(string)
			wDesc := props["width"].(map[string]any)["description"].(string)
			hDesc := props["height"].(map[string]any)["description"].(string)
			if !strings.Contains(desc, "320×240") || !strings.Contains(desc, "640×480") || !strings.Contains(desc, "4:3") ||
				!strings.Contains(wDesc, "max 640") || !strings.Contains(hDesc, "max 480") {
				t.Errorf("render tool does not state its image sizes: %q, width %q, height %q", desc, wDesc, hDesc)
			}
		}
	}
	for _, want := range []string{"status", "build", "cook", "render", "simulate", "trace", "query", "diff", "test", "fuzz", "docs", "inspect", "world_map", "world_query", "world_place", "world_remove", "world_terrain", "world_vegetation", "bench"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}

	st, _, isErr := c.tool("status", map[string]any{})
	if isErr || st["project"].(map[string]any)["name"] != "demo" {
		t.Fatalf("status: %v", st)
	}
	if b, _, isErr := c.tool("build", map[string]any{"vet": true}); isErr || b["ok"] != true {
		t.Fatalf("build: %v", b)
	}
	r, imgs, isErr := c.tool("render", map[string]any{"scene": "main", "tick": 10, "bundle": true})
	if isErr || imgs != 1 || r["width"].(float64) != 320 || r["height"].(float64) != 240 || c.sizes[0] != image.Pt(320, 240) {
		t.Fatalf("render: %v images=%d %v", r, imgs, c.sizes)
	}
	bundle := r["bundle"].(string)
	q, imgs, isErr := c.tool("query", map[string]any{"frame": bundle, "at": "160,120"})
	if isErr || imgs != 1 || q["pixel"] == nil || c.sizes[0] != image.Pt(320, 240) {
		t.Fatalf("query: %v %v", q, c.sizes)
	}
	big, imgs, isErr := c.tool("render", map[string]any{"scene": "main", "width": 2000, "bundle": true})
	if isErr || imgs != 1 || c.sizes[0] != image.Pt(640, 480) {
		t.Fatalf("render at the size limit: %v images=%d %v", big, imgs, c.sizes)
	}
	// The query image of the largest render comes back unscaled, so its crosshair is sharp.
	if q, imgs, isErr := c.tool("query", map[string]any{"frame": big["bundle"], "at": "320,240"}); isErr || imgs != 1 || c.sizes[0] != image.Pt(640, 480) {
		t.Fatalf("query at the size limit: %v %v", q, c.sizes)
	}
	// Sheets keep the render width limit, so a 4:3 summary (640×482) arrives unscaled.
	if in, imgs, isErr := c.tool("inspect", map[string]any{"kind": "scene", "name": "main"}); isErr || imgs != 1 || c.sizes[0] != image.Pt(640, 482) {
		t.Fatalf("inspect: %v images=%d %v", in, imgs, c.sizes)
	}
	if q, imgs, isErr := c.tool("query", map[string]any{"frame": bundle, "coverage": true}); isErr || imgs != 1 || q["coverage"] == nil {
		t.Fatalf("coverage: %v", q)
	}
	// The world tools: the map with one image, a cell, a dry-run placement, a render and
	// a simulation of the template's world through the game.
	if wm, imgs, isErr := c.tool("world_map", map[string]any{"world": "overworld", "radius": 32}); isErr || imgs != 1 || wm["cells"].(float64) != 65*65 {
		t.Fatalf("world_map: %v images=%d", wm, imgs)
	}
	if wq, _, isErr := c.tool("world_query", map[string]any{"world": "overworld", "cell": []int{3, -4}}); isErr || wq["biome"] == nil {
		t.Fatalf("world_query: %v", wq)
	}
	wp, _, isErr := c.tool("world_place", map[string]any{"world": "overworld", "prefab": "village", "name": "capital", "near": []int{0, 0}, "dry_run": true})
	if isErr || wp["valid"] != true || wp["written"] != false {
		t.Fatalf("world_place: %v", wp)
	}
	if bad, _, isErr := c.tool("world_place", map[string]any{"world": "overworld", "prefab": "village", "name": "far", "cell": []int{8190, 0}}); !isErr || bad["valid"] != false {
		t.Fatalf("world_place outside: %v", bad)
	}
	wt, imgs, isErr := c.tool("world_terrain", map[string]any{"world": "overworld", "name": "bay", "kind": "sea", "cell": []int{-60, 0}, "radius": 40, "depth": 5, "dry_run": true})
	if isErr || imgs != 1 || wt["written"] != false || wt["after"].(map[string]any)["water_cells"].(float64) == 0 {
		t.Fatalf("world_terrain: %v", wt)
	}
	if bad, _, isErr := c.tool("world_terrain", map[string]any{"world": "overworld", "name": "hill", "kind": "hill", "cell": []int{0, 0}, "radius": 5}); !isErr || !strings.Contains(fmt.Sprint(bad), "height") {
		t.Fatalf("world_terrain without height: %v", bad)
	}
	if wv, imgs, isErr := c.tool("world_vegetation", map[string]any{"world": "overworld", "name": "north_grove", "prefab": "tree", "density": 0.4, "cell": []int{0, -40}, "radius": 12, "dry_run": true}); isErr || imgs != 1 || wv["plants"].(float64) == 0 {
		t.Fatalf("world_vegetation: %v", wv)
	}
	if wr, imgs, isErr := c.tool("render", map[string]any{"world": "overworld", "at": []int{16, 16}}); isErr || imgs != 1 || wr["world"] != "overworld" {
		t.Fatalf("render world: %v", wr)
	}
	if ws, imgs, isErr := c.tool("simulate", map[string]any{"world": "overworld", "ticks": 30}); isErr || imgs != 1 || ws["verdict"] != "pass" || ws["world"] != "overworld" {
		t.Fatalf("simulate world: %v", ws)
	}
	if in, imgs, isErr := c.tool("inspect", map[string]any{"kind": "world", "name": "overworld"}); isErr || imgs != 1 || in["summary"].(map[string]any)["errors"].(float64) != 0 {
		t.Fatalf("inspect world: %v", in)
	}
	d, imgs, isErr := c.tool("diff", map[string]any{"a": r["out"], "b": bundle})
	if isErr || imgs != 1 || d["changed_pixels"].(float64) != 0 {
		t.Fatalf("diff: %v", d)
	}
	if bn, _, isErr := c.tool("bench", map[string]any{"scenario": "move", "cpus": 1}); isErr || bn["ticks"] != float64(60) || bn["frame_ms"].(map[string]any)["max"].(float64) <= 0 {
		t.Fatalf("bench: %v", bn)
	}
	sim, imgs, isErr := c.tool("simulate", map[string]any{"scenario": "collect"}) // a bare name resolves under tests/scenarios
	if isErr || imgs != 1 || sim["verdict"] != "pass" {
		t.Fatalf("simulate: %v images=%d", sim, imgs)
	}
	if sz := c.sizes[0]; sz.X != mcpSheetMaxW || sz.Y > mcpSheetMaxH {
		t.Errorf("simulate sheet is %v, want %d wide and at most %d tall", sz, mcpSheetMaxW, mcpSheetMaxH)
	}
	sim2, imgs, _ := c.tool("simulate", map[string]any{"scene": "main", "ticks": 90, "inputs": []any{map[string]any{"tick": 5, "press": []string{"up"}}}})
	if imgs != 1 || sim2["verdict"] != "pass" {
		t.Fatalf("simulate inline inputs: %v", sim2)
	}
	tr, _, isErr := c.tool("trace", map[string]any{"run_id": sim["run_id"], "from_tick": 0, "to_tick": 400, "events": []string{"gem_collected"}})
	if isErr || len(tr["ticks"].([]any)) == 0 || tr["to_tick"].(float64) != 199 {
		t.Fatalf("trace: %v", tr)
	}
	if _, _, isErr := c.tool("trace", map[string]any{"run_id": "../etc", "from_tick": 0, "to_tick": 1}); !isErr {
		t.Fatal("trace accepted a path as run id")
	}
	if f, _, isErr := c.tool("fuzz", map[string]any{"games": 4, "ticks": 120}); isErr || f["ok"] != true {
		t.Fatalf("fuzz: %v", f)
	}
	if doc, _, isErr := c.tool("docs", map[string]any{"topic": "scenario"}); isErr || !strings.Contains(doc["text"].(string), "expect") {
		t.Fatalf("docs: %v", doc)
	}
	if _, _, isErr := c.tool("render", map[string]any{"scene": "main", "bogus": 1}); !isErr {
		t.Fatal("unknown argument accepted")
	}
	if _, _, isErr := c.tool("render", map[string]any{"scene": "nope"}); !isErr {
		t.Fatal("unknown scene should be an error result")
	}
	inW.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestScriptProject: the tool runs a script game itself. build checks the scripts and
// reports syntax errors located, test runs the scenarios, simulate and bench go through
// the same paths as a Go game's, with no go command involved.
func TestScriptProject(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "..", "script", "testdata", "game")
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "") // no go command: nothing may need one
	env := &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}
	s, err := OpenSession(dir, env)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := s.Build(false); err != nil || !b.OK || b.Binary != "main.lua" {
		t.Fatalf("build: %+v %v", b, err)
	}
	tr, err := s.Test(false)
	if err != nil || !tr.OK || len(tr.Scenarios) != 1 {
		t.Fatalf("test: %s %v", tr.Human(), err)
	}
	sim, err := s.Simulate(SimulateOptions{Scene: "main", Ticks: 30})
	if err != nil || sim["verdict"] != "pass" {
		t.Fatalf("simulate: %+v %v", sim, err)
	}
	if bn, err := s.Bench(BenchOptions{Scenario: "collect"}); err != nil || bn["ticks"] != float64(80) {
		t.Fatalf("bench: %+v %v", bn, err)
	}
	os.WriteFile(filepath.Join(dir, "lib", "movement.lua"), []byte("local M = {}\nfunction M.step(e\nreturn M\n"), 0o644)
	b, err := s.Build(false)
	if err != nil || b.OK || len(b.Errors) != 1 || b.Errors[0].File != "lib/movement.lua" || b.Errors[0].Line != 3 {
		t.Fatalf("broken script: %+v %v", b, err)
	}
	if _, err := s.Simulate(SimulateOptions{Scene: "main", Ticks: 5}); err == nil || !strings.Contains(err.Error(), "lib/movement.lua:3") {
		t.Fatalf("simulate a broken script: %v", err)
	}
}
