// Package dap is the debug adapter of Lua games: the Debug Adapter Protocol over a stream
// (veduta dap, on stdin and stdout), for VS Code and any editor that speaks it. It runs the
// game itself, in the player or in a headless scenario, with a debugger on its Lua VM:
// breakpoints, stepping over, into and out of calls, pausing, the call stack, the locals,
// upvalues and globals of every call, tables and entities opened up, and simple
// expressions (a name and fields: e.state.score) evaluated for hovers and watches.
//
// The game runs on its own goroutine and stops inside the VM's statement hook, where it
// waits: requests that read the stopped program are handed to that goroutine, so the VM is
// only ever touched by the goroutine that runs it.
package dap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	veduta "github.com/riftbane/veduta/v2"
	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/lua"
	"github.com/riftbane/veduta/v2/script"
)

// LaunchArgs are the launch request's arguments.
type LaunchArgs struct {
	Project     string `json:"project"`     // the game's folder
	Mode        string `json:"mode"`        // "play" (the simulator, or the framebuffer on Linux) or "scenario"
	Scenario    string `json:"scenario"`    // with mode scenario: its name or file
	Out         string `json:"out"`         // with mode scenario: where the run's files go (default the project's out)
	StopOnEntry bool   `json:"stopOnEntry"` // stop before the first statement
	NoDebug     bool   `json:"noDebug"`     // run without the debugger (Run Without Debugging)
}

type message struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    bool            `json:"success,omitempty"`
	Message    string          `json:"message,omitempty"`
	Event      string          `json:"event,omitempty"`
	Body       any             `json:"body,omitempty"`
}

type step int

const (
	stepNone step = iota
	stepIn
	stepOver
	stepOut
)

// Session is one debugging session.
type Session struct {
	in    *bufio.Reader
	out   io.Writer
	outMu sync.Mutex
	seq   int

	// Exit ends the process when the client disconnects (os.Exit; tests replace it).
	Exit func(code int)

	launch     *LaunchArgs
	configured bool
	started    bool

	breakpoints atomic.Pointer[map[string]map[int]bool] // cleaned absolute path → lines
	pause       atomic.Bool
	stopped     atomic.Bool
	work        chan func() bool // for the stopped game: true resumes it
	game        chan func()      // the game's run, for Serve's goroutine
	done        chan struct{}    // closed when the client's stream ends

	// The game goroutine's own state.
	entry     bool
	step      step
	stepDepth int
	vm        *lua.VM
	refs      []func() []variable
}

// NewSession returns a session reading requests from in and writing to out.
func NewSession(in io.Reader, out io.Writer) *Session {
	s := &Session{in: bufio.NewReader(in), out: out, Exit: os.Exit, work: make(chan func() bool), game: make(chan func(), 1), done: make(chan struct{})}
	empty := map[string]map[int]bool{}
	s.breakpoints.Store(&empty)
	return s
}

// Serve handles requests until the stream ends. The game runs on the goroutine that calls
// Serve, which for veduta dap is the main one: the Windows simulator's window must live on
// the main thread.
func (s *Session) Serve() error {
	var readErr error
	go func() {
		defer close(s.done)
		for {
			m, err := s.read()
			if err != nil {
				if err != io.EOF {
					readErr = err
				}
				return
			}
			if m.Type == "request" {
				s.handle(m)
			}
		}
	}()
	for {
		select {
		case run := <-s.game:
			run()
		case <-s.done:
			return readErr
		}
	}
}

func (s *Session) read() (*message, error) {
	tp := textproto.NewReader(s.in)
	h, err := tp.ReadMIMEHeader()
	if err != nil {
		if err == io.EOF || strings.Contains(err.Error(), "EOF") {
			return nil, io.EOF
		}
		return nil, err
	}
	n, err := strconv.Atoi(h.Get("Content-Length"))
	if err != nil {
		return nil, fmt.Errorf("dap: bad Content-Length %q", h.Get("Content-Length"))
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(s.in, body); err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("dap: %w", err)
	}
	return &m, nil
}

func (s *Session) send(m message) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	s.seq++
	m.Seq = s.seq
	b, _ := json.Marshal(m)
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(b), b)
}

func (s *Session) respond(req *message, body any) {
	s.send(message{Type: "response", RequestSeq: req.Seq, Command: req.Command, Success: true, Body: body})
}

func (s *Session) fail(req *message, format string, args ...any) {
	s.send(message{Type: "response", RequestSeq: req.Seq, Command: req.Command, Message: fmt.Sprintf(format, args...)})
}

func (s *Session) event(name string, body any) {
	s.send(message{Type: "event", Event: name, Body: body})
}

func (s *Session) handle(req *message) {
	switch req.Command {
	case "initialize":
		s.respond(req, map[string]any{
			"supportsConfigurationDoneRequest": true,
			"supportsEvaluateForHovers":        true,
			"supportsTerminateRequest":         true,
		})
		s.event("initialized", nil)
	case "launch":
		var a LaunchArgs
		if err := json.Unmarshal(req.Arguments, &a); err != nil {
			s.fail(req, "launch: %v", err)
			return
		}
		if err := s.checkLaunch(&a); err != nil {
			s.fail(req, "%v", err)
			return
		}
		s.launch = &a
		s.respond(req, nil)
		s.start()
	case "configurationDone":
		s.configured = true
		s.respond(req, nil)
		s.start()
	case "setBreakpoints":
		s.setBreakpoints(req)
	case "setExceptionBreakpoints":
		s.respond(req, map[string]any{"breakpoints": []any{}})
	case "threads":
		s.respond(req, map[string]any{"threads": []any{map[string]any{"id": 1, "name": "game"}}})
	case "stackTrace", "scopes", "variables", "evaluate":
		if !s.stopped.Load() {
			s.fail(req, "the game is running: pause it first")
			return
		}
		done := make(chan struct{})
		s.work <- func() bool {
			s.inspect(req)
			close(done)
			return false
		}
		<-done
	case "continue", "next", "stepIn", "stepOut":
		if !s.stopped.Load() {
			s.fail(req, "the game is running")
			return
		}
		kind := map[string]step{"continue": stepNone, "next": stepOver, "stepIn": stepIn, "stepOut": stepOut}[req.Command]
		s.work <- func() bool {
			s.step = kind
			return true
		}
		if req.Command == "continue" {
			s.respond(req, map[string]any{"allThreadsContinued": true})
		} else {
			s.respond(req, nil)
		}
	case "pause":
		s.pause.Store(true)
		s.respond(req, nil)
	case "disconnect", "terminate":
		s.respond(req, nil)
		s.Exit(0)
	default:
		s.fail(req, "%s is not supported", req.Command)
	}
}

func (s *Session) checkLaunch(a *LaunchArgs) error {
	if a.Project == "" {
		return fmt.Errorf("launch: no project (the game's folder)")
	}
	abs, err := filepath.Abs(a.Project)
	if err != nil {
		return err
	}
	a.Project = abs
	p, err := cook.ReadProject(abs)
	if err != nil {
		return fmt.Errorf("launch: %v", err)
	}
	if p.Script == "" {
		return fmt.Errorf("launch: %s is a Go game; the debugger is for Lua games", abs)
	}
	switch a.Mode {
	case "", "play":
		a.Mode = "play"
	case "scenario":
		if a.Scenario == "" {
			return fmt.Errorf("launch: mode scenario needs a scenario")
		}
		// A name, as veduta simulate --scenario takes it, is a file of tests/scenarios.
		if !strings.HasSuffix(a.Scenario, ".json") {
			a.Scenario = filepath.Join(abs, "tests", "scenarios", a.Scenario+".scenario.json")
		}
		if _, err := os.Stat(a.Scenario); err != nil {
			return fmt.Errorf("launch: %v", err)
		}
	default:
		return fmt.Errorf("launch: unknown mode %q (play or scenario)", a.Mode)
	}
	return nil
}

// start runs the game once it is launched and configured.
func (s *Session) start() {
	if s.started || s.launch == nil || !s.configured {
		return
	}
	s.started = true
	a := s.launch
	s.entry = a.StopOnEntry && !a.NoDebug
	s.game <- func() {
		stdout := &outputWriter{s: s, category: "stdout"}
		stderr := &outputWriter{s: s, category: "stderr"}
		code := s.run(a, stdout, stderr)
		stdout.flush()
		stderr.flush()
		s.event("exited", map[string]any{"exitCode": code})
		s.event("terminated", nil)
	}
}

func (s *Session) run(a *LaunchArgs, stdout, stderr io.Writer) int {
	p, err := cook.ReadProject(a.Project)
	var g *script.Game
	if err == nil {
		g, err = script.Load(a.Project, p, stderr)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !a.NoDebug {
		g.Debugger = s
	}
	args := []string{"-project", a.Project}
	if a.Mode == "scenario" {
		args = append(args, "-headless", "simulate", "--scenario", a.Scenario)
		if a.Out != "" {
			args = append(args, "--out", a.Out)
		}
	}
	return veduta.RunArgs(g, args, stdout, stderr)
}

func (s *Session) setBreakpoints(req *message) {
	var a struct {
		Source struct {
			Path string `json:"path"`
		} `json:"source"`
		Breakpoints []struct {
			Line int `json:"line"`
		} `json:"breakpoints"`
	}
	if err := json.Unmarshal(req.Arguments, &a); err != nil {
		s.fail(req, "setBreakpoints: %v", err)
		return
	}
	old := *s.breakpoints.Load()
	next := make(map[string]map[int]bool, len(old)+1)
	for k, v := range old {
		next[k] = v
	}
	lines := map[int]bool{}
	var out []any
	for _, b := range a.Breakpoints {
		lines[b.Line] = true
		out = append(out, map[string]any{"verified": true, "line": b.Line})
	}
	key := pathKey(a.Source.Path)
	if len(lines) == 0 {
		delete(next, key)
	} else {
		next[key] = lines
	}
	s.breakpoints.Store(&next)
	if out == nil {
		out = []any{}
	}
	s.respond(req, map[string]any{"breakpoints": out})
}

// pathKey is how breakpoints are found by file: cleaned, and without case on Windows,
// where the editor and the file system may spell a path differently.
func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// Statement is the VM's hook: it decides whether the game stops before this statement,
// and while stopped serves the requests that read it.
func (s *Session) Statement(vm *lua.VM, chunk string, line, depth int) {
	reason := ""
	switch {
	case s.entry:
		reason, s.entry = "entry", false
	case s.pause.Swap(false):
		reason = "pause"
	case s.step == stepIn,
		s.step == stepOver && depth <= s.stepDepth,
		s.step == stepOut && depth < s.stepDepth:
		reason = "step"
	default:
		bps := *s.breakpoints.Load()
		if len(bps) == 0 {
			return
		}
		if lines := bps[pathKey(filepath.Join(s.launch.Project, filepath.FromSlash(chunk)))]; !lines[line] {
			return
		}
		reason = "breakpoint"
	}
	s.step, s.stepDepth = stepNone, depth
	s.vm, s.refs = vm, nil
	s.stopped.Store(true)
	s.event("stopped", map[string]any{"reason": reason, "threadId": 1, "allThreadsStopped": true})
	for resume := false; !resume; {
		resume = (<-s.work)()
	}
	s.stopped.Store(false)
	s.vm, s.refs = nil, nil
}

// variable is one row of the variables view.
type variable struct {
	name  string
	value lua.Value
}

// ref gives a value that opens up (a table, an entity) or a scope its reference.
func (s *Session) ref(list func() []variable) int {
	s.refs = append(s.refs, list)
	return len(s.refs)
}

// inspect answers a request that reads the stopped game, on the game's goroutine.
func (s *Session) inspect(req *message) {
	vm := s.vm
	switch req.Command {
	case "stackTrace":
		var frames []any
		for i, e := range vm.Stack() {
			frames = append(frames, map[string]any{
				"id": i + 1, "name": e.Name, "line": e.Line, "column": 1,
				"source": map[string]any{"name": filepath.Base(e.Chunk), "path": filepath.Join(s.launch.Project, filepath.FromSlash(e.Chunk))},
			})
		}
		if frames == nil {
			frames = []any{}
		}
		s.respond(req, map[string]any{"stackFrames": frames, "totalFrames": len(frames)})
	case "scopes":
		var a struct {
			FrameID int `json:"frameId"`
		}
		json.Unmarshal(req.Arguments, &a)
		level := a.FrameID - 1
		wrap := func(vs []lua.Variable) []variable {
			out := make([]variable, len(vs))
			for i, v := range vs {
				out[i] = variable{v.Name, v.Value}
			}
			return out
		}
		locals := s.ref(func() []variable { return wrap(vm.Locals(level)) })
		upvals := s.ref(func() []variable { return wrap(vm.Upvalues(level)) })
		globals := s.ref(func() []variable { return s.tableRows(vm.Globals()) })
		s.respond(req, map[string]any{"scopes": []any{
			map[string]any{"name": "Locals", "variablesReference": locals},
			map[string]any{"name": "Upvalues", "variablesReference": upvals},
			map[string]any{"name": "Globals", "variablesReference": globals, "expensive": true},
		}})
	case "variables":
		var a struct {
			Ref int `json:"variablesReference"`
		}
		json.Unmarshal(req.Arguments, &a)
		if a.Ref < 1 || a.Ref > len(s.refs) {
			s.respond(req, map[string]any{"variables": []any{}})
			return
		}
		rows := []any{}
		for _, v := range s.refs[a.Ref-1]() {
			rows = append(rows, s.row(v))
		}
		s.respond(req, map[string]any{"variables": rows})
	case "evaluate":
		var a struct {
			Expression string `json:"expression"`
			FrameID    int    `json:"frameId"`
		}
		json.Unmarshal(req.Arguments, &a)
		v, err := s.evaluate(strings.TrimSpace(a.Expression), max(a.FrameID-1, 0))
		if err != nil {
			s.fail(req, "%v", err)
			return
		}
		r := s.row(variable{a.Expression, v})
		s.respond(req, map[string]any{"result": r["value"], "type": r["type"], "variablesReference": r["variablesReference"]})
	}
}

// row describes a value for the variables view, with a reference when it opens up.
func (s *Session) row(v variable) map[string]any {
	ref := 0
	switch {
	case v.value.Table() != nil:
		t := v.value.Table()
		ref = s.ref(func() []variable { return s.tableRows(t) })
	case s.isEntity(v.value):
		e := v.value
		ref = s.ref(func() []variable { return s.entityRows(e) })
	}
	return map[string]any{"name": v.name, "value": s.display(v.value), "type": v.value.Type().String(), "variablesReference": ref}
}

// maxRows bounds what a table shows.
const maxRows = 500

// tableRows lists a table's fields: its keys as a script would write them, sorted so the
// view does not move between stops.
func (s *Session) tableRows(t *lua.Table) []variable {
	var rows []variable
	t.ForEach(func(k, v lua.Value) bool {
		name := k.String()
		if _, ok := k.Str(); !ok {
			name = "[" + name + "]"
		}
		rows = append(rows, variable{name, v})
		return len(rows) < maxRows
	})
	sort.SliceStable(rows, func(i, j int) bool {
		ni, ei := strconv.Atoi(strings.Trim(rows[i].name, "[]"))
		nj, ej := strconv.Atoi(strings.Trim(rows[j].name, "[]"))
		if ei == nil && ej == nil {
			return ni < nj
		}
		if (ei == nil) != (ej == nil) {
			return ei == nil
		}
		return rows[i].name < rows[j].name
	})
	return rows
}

func (s *Session) isEntity(v lua.Value) bool {
	u := v.Userdata()
	if u == nil || u.Meta == nil {
		return false
	}
	name, _ := u.Meta.GetString("__name").Str()
	return name == "entity"
}

func (s *Session) entityRows(e lua.Value) []variable {
	var rows []variable
	for _, f := range script.EntityFields {
		if v, err := s.index(e, lua.String(f)); err == nil {
			rows = append(rows, variable{f, v})
		}
	}
	return rows
}

// index reads obj[key] as the script would, metatables included.
func (s *Session) index(obj, key lua.Value) (v lua.Value, err error) {
	res, err := s.vm.Call(lua.FunctionValue(lua.NewFunction("debugger", func(vm *lua.VM, _ []lua.Value) []lua.Value {
		return vm.Ret(vm.Index(obj, key))
	})))
	if err != nil || len(res) == 0 {
		return lua.Nil, err
	}
	return res[0], nil
}

func (s *Session) display(v lua.Value) string {
	switch {
	case v.Table() != nil:
		n := 0
		v.Table().ForEach(func(_, _ lua.Value) bool { n++; return true })
		return fmt.Sprintf("table (%d)", n)
	case s.isEntity(v):
		name, _ := s.index(v, lua.String("name"))
		kind, _ := s.index(v, lua.String("kind"))
		return fmt.Sprintf("entity %s (%s)", name.String(), kind.String())
	}
	if str, ok := v.Str(); ok {
		return strconv.Quote(str)
	}
	return v.String()
}

// evaluate reads a name and its fields (hero.state.score, t[1] is not supported): a local of
// the frame, else an upvalue, else a global.
func (s *Session) evaluate(expr string, level int) (lua.Value, error) {
	parts := strings.Split(expr, ".")
	for _, p := range parts {
		if !isName(p) {
			return lua.Nil, fmt.Errorf("only a name and its fields can be evaluated (e.state.score)")
		}
	}
	v, found := lua.Nil, false
	locals := s.vm.Locals(level)
	for i := len(locals) - 1; i >= 0 && !found; i-- {
		if locals[i].Name == parts[0] {
			v, found = locals[i].Value, true
		}
	}
	for _, u := range s.vm.Upvalues(level) {
		if !found && u.Name == parts[0] {
			v, found = u.Value, true
		}
	}
	if !found {
		v = s.vm.Global(parts[0])
	}
	for _, p := range parts[1:] {
		next, err := s.index(v, lua.String(p))
		if err != nil {
			return lua.Nil, err
		}
		v = next
	}
	return v, nil
}

func isName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// outputWriter sends what the game writes to the client, a line at a time.
type outputWriter struct {
	s        *Session
	category string
	mu       sync.Mutex
	buf      []byte
}

func (w *outputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if i := strings.LastIndexByte(string(w.buf), '\n'); i >= 0 {
		w.s.event("output", map[string]any{"category": w.category, "output": string(w.buf[:i+1])})
		w.buf = append(w.buf[:0], w.buf[i+1:]...)
	}
	return len(p), nil
}

func (w *outputWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.s.event("output", map[string]any{"category": w.category, "output": string(w.buf)})
		w.buf = nil
	}
}
