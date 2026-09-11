package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/internal/golden"
)

// newProject creates a demo project with veduta init against this engine checkout.
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
	r, err := Init(env, InitOptions{Dir: dir, EngineDir: root})
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
	for _, f := range []string{"go.mod", "go.sum", "CLAUDE.md", ".mcp.json", ".gitignore", ".github/workflows/release.yml", "cmd/game/main.go", "game/kinds.go", "assets/scenes/main.scene.json", "tests/scenarios/move.scenario.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil && f != "go.sum" {
			t.Fatalf("missing %s: %v", f, err)
		}
	}
	main, _ := os.ReadFile(filepath.Join(dir, "cmd", "game", "main.go"))
	if !strings.Contains(string(main), `"demo/game"`) || strings.Contains(string(main), "template/game") {
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
	if err != nil || !tr.OK || len(tr.Scenarios) != 3 {
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
	t   *testing.T
	in  io.WriteCloser
	out *bufio.Scanner
	id  int
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
			images++
		}
	}
	return text, images, res["isError"] == true
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
	}
	for _, want := range []string{"status", "build", "cook", "render", "simulate", "trace", "query", "diff", "test", "fuzz", "docs"} {
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
	if isErr || imgs != 1 || r["width"].(float64) != 640 {
		t.Fatalf("render: %v images=%d", r, imgs)
	}
	bundle := r["bundle"].(string)
	q, imgs, isErr := c.tool("query", map[string]any{"frame": bundle, "at": "320,180"})
	if isErr || imgs != 1 || q["pixel"] == nil {
		t.Fatalf("query: %v", q)
	}
	if q, imgs, isErr := c.tool("query", map[string]any{"frame": bundle, "coverage": true}); isErr || imgs != 1 || q["coverage"] == nil {
		t.Fatalf("coverage: %v", q)
	}
	d, imgs, isErr := c.tool("diff", map[string]any{"a": r["out"], "b": bundle})
	if isErr || imgs != 1 || d["changed_pixels"].(float64) != 0 {
		t.Fatalf("diff: %v", d)
	}
	sim, imgs, isErr := c.tool("simulate", map[string]any{"scenario": "tests/scenarios/collect.scenario.json"})
	if isErr || imgs != 1 || sim["verdict"] != "pass" {
		t.Fatalf("simulate: %v images=%d", sim, imgs)
	}
	sim2, imgs, _ := c.tool("simulate", map[string]any{"scene": "main", "ticks": 90, "inputs": []any{map[string]any{"tick": 5, "press": []string{"KeyW"}}}})
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
