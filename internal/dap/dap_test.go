package dap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// client drives a session as an editor would.
type client struct {
	t    *testing.T
	w    io.Writer
	r    *bufio.Reader
	seq  int
	msgs chan map[string]any
	out  strings.Builder
	kept []map[string]any // messages read while waiting for another
}

func newClient(t *testing.T) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := NewSession(inR, outW)
	s.Exit = func(int) {}
	go s.Serve()
	c := &client{t: t, w: inW, r: bufio.NewReader(outR), msgs: make(chan map[string]any, 100)}
	go func() {
		for {
			var n int
			for {
				line, err := c.r.ReadString('\n')
				if err != nil {
					close(c.msgs)
					return
				}
				line = strings.TrimSpace(line)
				if line == "" {
					break
				}
				fmt.Sscanf(line, "Content-Length: %d", &n)
			}
			body := make([]byte, n)
			if _, err := io.ReadFull(c.r, body); err != nil {
				close(c.msgs)
				return
			}
			var m map[string]any
			json.Unmarshal(body, &m)
			c.msgs <- m
		}
	}()
	t.Cleanup(func() { inW.Close() })
	return c
}

func (c *client) request(command string, args any) int {
	c.seq++
	b, _ := json.Marshal(map[string]any{"seq": c.seq, "type": "request", "command": command, "arguments": args})
	fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n%s", len(b), b)
	return c.seq
}

// wait returns the first message that matches, in the order they came; the others are kept
// for later waits.
func (c *client) wait(what string, match func(map[string]any) bool) map[string]any {
	c.t.Helper()
	for i, m := range c.kept {
		if match(m) {
			c.kept = append(c.kept[:i], c.kept[i+1:]...)
			return m
		}
	}
	timeout := time.After(60 * time.Second)
	for {
		select {
		case m, ok := <-c.msgs:
			if !ok {
				c.t.Fatalf("the session ended waiting for %s", what)
			}
			if m["event"] == "output" {
				c.out.WriteString(m["body"].(map[string]any)["output"].(string))
				continue
			}
			if match(m) {
				return m
			}
			c.kept = append(c.kept, m)
		case <-timeout:
			c.t.Fatalf("timed out waiting for %s (output: %s)", what, c.out.String())
		}
	}
}

// call sends a request and returns its response's body, failing on an error response.
func (c *client) call(command string, args any) map[string]any {
	c.t.Helper()
	seq := c.request(command, args)
	m := c.wait(command, func(m map[string]any) bool { return m["type"] == "response" && m["request_seq"] == float64(seq) })
	if m["success"] != true {
		c.t.Fatalf("%s failed: %v", command, m["message"])
	}
	body, _ := m["body"].(map[string]any)
	return body
}

func (c *client) stopped(reason string) {
	c.t.Helper()
	m := c.wait("stopped", func(m map[string]any) bool {
		if m["event"] == "terminated" {
			c.t.Fatalf("the game ended instead of stopping: %s", c.out.String())
		}
		return m["event"] == "stopped"
	})
	if got := m["body"].(map[string]any)["reason"]; got != reason {
		c.t.Fatalf("stopped for %v, want %s", got, reason)
	}
}

// top returns the innermost frame's file (relative to the game) and line.
func (c *client) top(game string) (string, int) {
	c.t.Helper()
	frames := c.call("stackTrace", map[string]any{"threadId": 1})["stackFrames"].([]any)
	f := frames[0].(map[string]any)
	rel, _ := filepath.Rel(game, f["source"].(map[string]any)["path"].(string))
	return filepath.ToSlash(rel), int(f["line"].(float64))
}

func (c *client) variables(ref any) map[string]map[string]any {
	c.t.Helper()
	out := map[string]map[string]any{}
	for _, v := range c.call("variables", map[string]any{"variablesReference": ref})["variables"].([]any) {
		row := v.(map[string]any)
		out[row["name"].(string)] = row
	}
	return out
}

func testGame(t *testing.T) string {
	abs, err := filepath.Abs(filepath.Join("..", "..", "script", "testdata", "game"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func (c *client) start(game string, launch map[string]any, breakpoints map[string][]int) {
	c.t.Helper()
	c.call("initialize", map[string]any{"adapterID": "veduta"})
	c.wait("initialized", func(m map[string]any) bool { return m["event"] == "initialized" })
	c.call("launch", launch)
	for file, lines := range breakpoints {
		var bps []any
		for _, l := range lines {
			bps = append(bps, map[string]any{"line": l})
		}
		body := c.call("setBreakpoints", map[string]any{"source": map[string]any{"path": filepath.Join(game, filepath.FromSlash(file))}, "breakpoints": bps})
		if n := len(body["breakpoints"].([]any)); n != len(lines) {
			c.t.Fatalf("%d breakpoints verified of %d", n, len(lines))
		}
	}
	c.call("configurationDone", nil)
}

// TestBreakpointAndInspection stops the test game's scenario where the hero collects a
// coin, reads the stack, the locals, an upvalue, an entity and expressions, steps, and runs
// it to its end.
func TestBreakpointAndInspection(t *testing.T) {
	game := testGame(t)
	c := newClient(t)
	c.start(game, map[string]any{"project": game, "mode": "scenario", "scenario": "collect", "out": t.TempDir()}, map[string][]int{"main.lua": {43}})
	c.stopped("breakpoint")
	if file, line := c.top(game); file != "main.lua" || line != 43 {
		t.Fatalf("stopped at %s:%d, want main.lua:43", file, line)
	}
	scopes := c.call("scopes", map[string]any{"frameId": 1})["scopes"].([]any)
	locals := c.variables(scopes[0].(map[string]any)["variablesReference"])
	for _, name := range []string{"e", "coin"} {
		if locals[name] == nil {
			t.Errorf("no local %s: %v", name, locals)
		}
	}
	if !strings.HasPrefix(locals["e"]["value"].(string), "entity hero") {
		t.Errorf("e shows as %v", locals["e"]["value"])
	}
	hero := c.variables(locals["e"]["variablesReference"])
	if hero["kind"]["value"] != `"hero"` || hero["state"]["variablesReference"].(float64) == 0 {
		t.Errorf("the hero's fields: %v", hero)
	}
	upvals := c.variables(scopes[1].(map[string]any)["variablesReference"])
	if upvals["score"]["value"] != "0" {
		t.Errorf("upvalue score: %v", upvals["score"])
	}
	if r := c.call("evaluate", map[string]any{"expression": "coin.name", "frameId": 1})["result"]; !strings.HasPrefix(r.(string), `"coin_`) {
		t.Errorf("coin.name is %v", r)
	}
	if r := c.call("evaluate", map[string]any{"expression": "e.state.steps", "frameId": 1})["result"]; r == "nil" {
		t.Errorf("e.state.steps is %v", r)
	}

	c.call("next", map[string]any{"threadId": 1})
	c.stopped("step")
	if _, line := c.top(game); line != 44 {
		t.Errorf("next went to line %d, want 44", line)
	}
	c.call("stepIn", map[string]any{"threadId": 1})
	c.stopped("step")
	if _, line := c.top(game); line != 45 {
		t.Errorf("stepping into a Go function went to line %d, want 45", line)
	}

	c.call("setBreakpoints", map[string]any{"source": map[string]any{"path": filepath.Join(game, "main.lua")}, "breakpoints": []any{}})
	c.call("continue", map[string]any{"threadId": 1})
	exited := c.wait("exited", func(m map[string]any) bool { return m["event"] == "exited" })
	if code := exited["body"].(map[string]any)["exitCode"]; code != float64(0) {
		t.Errorf("exit code %v", code)
	}
	c.wait("terminated", func(m map[string]any) bool { return m["event"] == "terminated" })
	if !strings.Contains(c.out.String(), `"verdict":"pass"`) {
		t.Errorf("the scenario's report did not come out as output: %s", c.out.String())
	}

	// Stopping, inspecting and stepping change nothing the game does.
	plain := newClient(t)
	plain.start(game, map[string]any{"project": game, "mode": "scenario", "scenario": "collect", "out": t.TempDir(), "noDebug": true}, map[string][]int{"main.lua": {43}})
	plain.wait("terminated", func(m map[string]any) bool {
		if m["event"] == "stopped" {
			t.Fatal("a run without debugging stopped")
		}
		return m["event"] == "terminated"
	})
	hash := regexp.MustCompile(`"trace_hash":"(\w+)"`)
	a, b := hash.FindStringSubmatch(c.out.String()), hash.FindStringSubmatch(plain.out.String())
	if a == nil || b == nil || a[1] != b[1] {
		t.Errorf("trace hash under the debugger %v, without %v", a, b)
	}
}

// TestStepIntoAModule stops on entry, steps into require's module and out again.
func TestStepIntoAModule(t *testing.T) {
	game := testGame(t)
	c := newClient(t)
	c.start(game, map[string]any{"project": game, "mode": "scenario", "scenario": "collect", "out": t.TempDir(), "stopOnEntry": true}, nil)
	c.stopped("entry")
	if file, line := c.top(game); file != "main.lua" || line != 3 {
		t.Fatalf("entry at %s:%d, want main.lua:3", file, line)
	}
	c.call("stepIn", map[string]any{"threadId": 1})
	c.stopped("step")
	if file, line := c.top(game); file != "lib/movement.lua" || line != 2 {
		t.Fatalf("stepped into %s:%d, want lib/movement.lua:2", file, line)
	}
	c.call("stepOut", map[string]any{"threadId": 1})
	c.stopped("step")
	if file, line := c.top(game); file != "main.lua" || line != 5 {
		t.Fatalf("stepped out to %s:%d, want main.lua:5", file, line)
	}
	c.call("continue", map[string]any{"threadId": 1})
	c.wait("terminated", func(m map[string]any) bool { return m["event"] == "terminated" })
}

func TestLaunchRefuses(t *testing.T) {
	c := newClient(t)
	c.call("initialize", nil)
	seq := c.request("launch", map[string]any{"project": t.TempDir()})
	m := c.wait("launch", func(m map[string]any) bool { return m["type"] == "response" && m["request_seq"] == float64(seq) })
	if m["success"] == true {
		t.Error("a folder without a game was launched")
	}
	seq = c.request("stackTrace", map[string]any{"threadId": 1})
	m = c.wait("stackTrace", func(m map[string]any) bool { return m["type"] == "response" && m["request_seq"] == float64(seq) })
	if m["success"] == true {
		t.Error("a stack trace of a game that is not stopped")
	}
}

// TestLaunchGameArgs: a play launch passes where the game starts to the player, and a
// scenario launch refuses those fields.
func TestLaunchGameArgs(t *testing.T) {
	a := &LaunchArgs{Project: "p", Mode: "play", Scene: "level3", Seed: 5}
	if got := strings.Join(a.gameArgs(), " "); got != "-project p -scene level3 -seed 5" {
		t.Fatalf("play: %q", got)
	}
	a = &LaunchArgs{Project: "p", Mode: "play", World: "land", At: "1,2"}
	if got := strings.Join(a.gameArgs(), " "); got != "-project p -world land -at 1,2" {
		t.Fatalf("world: %q", got)
	}
	a = &LaunchArgs{Project: "p", Mode: "scenario", Scenario: "s.json", Out: "o"}
	if got := strings.Join(a.gameArgs(), " "); got != "-project p -headless simulate --scenario s.json --out o" {
		t.Fatalf("scenario: %q", got)
	}
	game := testGame(t)
	s := &Session{}
	for _, bad := range []*LaunchArgs{
		{Project: game, Mode: "scenario", Scenario: "collect", Scene: "x"},
		{Project: game, Mode: "play", Scene: "x", World: "y"},
		{Project: game, Mode: "play", At: "1,2"},
	} {
		if err := s.checkLaunch(bad); err == nil {
			t.Errorf("%+v: accepted", bad)
		}
	}
	if err := s.checkLaunch(&LaunchArgs{Project: game, Scene: "x"}); err != nil {
		t.Fatal(err)
	}
}
