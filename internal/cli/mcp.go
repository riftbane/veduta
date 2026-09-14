package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/riftbane/veduta/asset/cook"
	"github.com/riftbane/veduta/docs"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/inspect"
	"github.com/riftbane/veduta/internal/sheet"
	"github.com/riftbane/veduta/mcp"
)

// Image size limits of MCP results (spec §11), sized after the 320×240 console panel.
const (
	// A render without width/height is one panel frame; a render asking for one side
	// gets the other at the same 4:3 aspect.
	mcpDefaultW, mcpDefaultH = 320, 240
	// The largest render: two panel pixels per image pixel.
	mcpMaxW, mcpMaxH = 640, 480
	// Sheets (simulate contact sheets, inspect sheets) tile several views into one
	// image: they keep the render width limit, so their tiles stay legible, but may be
	// taller than a render, because 4:3 tiles stack higher than 640×480 (a 2×2 grid is
	// 640×482, a 3×3 or 4×4 grid 640×484, a texture summary 524×524, a scene ids view
	// with its legend 640×520). mcpSheetMaxH must stay >= 482. Larger sheets are scaled
	// down to fit.
	mcpSheetMaxW, mcpSheetMaxH = mcpMaxW, 720
)

// mcpAspect is the aspect ratio of mcpDefaultW×mcpDefaultH in lowest terms ("4:3").
func mcpAspect() string {
	a, b := mcpDefaultW, mcpDefaultH
	for b != 0 {
		a, b = b, a%b
	}
	return fmt.Sprintf("%d:%d", mcpDefaultW/a, mcpDefaultH/a)
}

// mcpServer holds the state of one `veduta mcp` process.
type mcpServer struct {
	env        *Env
	projectDir string
	mu         sync.Mutex
	s          *Session // nil when not in a project
	sessErr    error
	lastBuild  *BuildReport
	extraTools []mcp.Tool
}

// session returns the project session, re-reading veduta.json so edits are seen.
func (m *mcpServer) session() (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := OpenSession(m.projectDir, m.env)
	if err != nil {
		return nil, fmt.Errorf("%w (the veduta MCP tools need a project: start the server in a directory with veduta.json)", err)
	}
	m.s = s
	return s, nil
}

// schema builds a JSON Schema object with the given properties (all optional unless
// listed in required) and no additional properties.
func schema(props map[string]any, required ...string) json.RawMessage {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	b, _ := json.Marshal(s)
	return b
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func strList(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}
func enum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": desc}
}

// textResult is a tool result holding the report as JSON text plus images.
func textResult(report any, images ...[]byte) *mcp.Result {
	r := &mcp.Result{Content: []mcp.Content{mcp.Text(string(marshal(report, false)))}}
	for _, img := range images {
		if img != nil {
			r.Content = append(r.Content, mcp.PNG(img))
		}
	}
	return r
}

// errResult turns an error into an isError result; located errors are included as JSON.
func errResult(err error) *mcp.Result {
	out := map[string]any{"ok": false, "error": err.Error()}
	if errs := sourceErrors(err); errs != nil {
		out["errors"] = errs
	}
	var bf *BuildFailed
	if as(err, &bf) {
		out["build"] = bf.Report
	}
	return &mcp.Result{Content: []mcp.Content{mcp.Text(string(marshal(out, false)))}, IsError: true}
}

func readPNG(s *Session, p string) []byte {
	if p == "" {
		return nil
	}
	if !filepath.IsAbs(p) && s != nil {
		p = filepath.Join(s.Root, filepath.FromSlash(p))
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return data
}

// fitPNG re-encodes a PNG scaled down to at most w×h (unchanged when it already fits).
func fitPNG(data []byte, w, h int) []byte {
	if data == nil {
		return nil
	}
	img, err := gfx.DecodePNG(strings.NewReader(string(data)))
	if err != nil || img.W <= w && img.H <= h {
		return data
	}
	var b strings.Builder
	if sheet.Fit(img, w, h).EncodePNG(&b) != nil {
		return data
	}
	return []byte(b.String())
}

// tools returns every MCP tool of spec §11.
func (m *mcpServer) tools() []mcp.Tool {
	withSession := func(f func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error)) func(context.Context, json.RawMessage) (*mcp.Result, error) {
		return func(ctx context.Context, args json.RawMessage) (*mcp.Result, error) {
			s, err := m.session()
			if err != nil {
				return errResult(err), nil
			}
			res, err := f(ctx, s, args)
			if err != nil {
				return errResult(err), nil
			}
			return res, nil
		}
	}
	tools := []mcp.Tool{
		{
			Name:        "status",
			Description: "Versions (tool, engine, project), project summary, the console target the game plays on, the engine check (a warning when project and tool differ by a minor version or more), cooked and stale assets, the last build result of this session and update availability on the configured release channel. Call it first.",
			InputSchema: schema(map[string]any{}),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				var a struct{}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				return textResult(m.status(s)), nil
			}),
		},
		{
			Name:        "build",
			Description: "Cook stale assets and compile the game (CGO_ENABLED=0). Compile and vet errors come back as a structured list {file,line,col,msg}; asset errors as cook_errors.",
			InputSchema: schema(map[string]any{"vet": boolean("also run go vet ./...")}),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					Vet bool `json:"vet"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				r, err := s.Build(a.Vet)
				if err != nil {
					return nil, err
				}
				m.mu.Lock()
				m.lastBuild = r
				m.mu.Unlock()
				res := textResult(r)
				res.IsError = !r.OK
				return res, nil
			}),
		},
		{
			Name:        "cook",
			Description: "Compile changed asset sources to .vda. Reports compiled assets, located source errors and dangling references.",
			InputSchema: schema(map[string]any{"force": boolean("recompile everything")}),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					Force bool `json:"force"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				r, err := s.Cook(a.Force)
				if err != nil {
					return nil, err
				}
				// Errors only, to keep the result small (spec §11).
				out := map[string]any{"compiled": r.Compiled, "fresh": r.Fresh, "failed": r.Failed, "removed": r.Removed, "warnings": r.Warnings, "errors": r.Errors()}
				var compiled []string
				for _, it := range r.Items {
					if it.Status == cook.StatusCompiled {
						compiled = append(compiled, string(it.Kind)+"/"+it.Name)
					}
				}
				out["compiled_assets"] = compiled
				res := textResult(out)
				res.IsError = r.Failed > 0
				return res, nil
			}),
		},
		{
			Name: "render",
			Description: fmt.Sprintf("Render one frame headless through the game at a tick. Returns the image (%d×%d, the console panel, unless width/height are given; max %d×%d; one side alone gets the other at %s) and camera/frame metadata with per-entity visible pixels. bundle writes a .vframe for query/diff.",
				mcpDefaultW, mcpDefaultH, mcpMaxW, mcpMaxH, mcpAspect()),
			InputSchema: schema(map[string]any{
				"scene":  str("scene name (default: the project's default scene)"),
				"tick":   num("ticks to simulate before the frame (default 0)"),
				"seed":   num("RNG seed (default: the project's)"),
				"camera": str("camera preset: scene, top, front, back, left, right, iso, orbit:<deg>, or a camera entity name"),
				"mode":   enum("render mode", modeList()...),
				"width":  num(fmt.Sprintf("image width (default %d, max %d)", mcpDefaultW, mcpMaxW)),
				"height": num(fmt.Sprintf("image height (default %d, max %d)", mcpDefaultH, mcpMaxH)),
				"bundle": boolean("also write a frame bundle (.vframe) for query and diff"),
			}),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					Scene  string `json:"scene"`
					Tick   int    `json:"tick"`
					Seed   uint64 `json:"seed"`
					Camera string `json:"camera"`
					Mode   string `json:"mode"`
					Width  int    `json:"width"`
					Height int    `json:"height"`
					Bundle bool   `json:"bundle"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				w, h := clampSize(a.Width, a.Height)
				rep, err := s.Render(RenderOptions{Scene: a.Scene, Tick: a.Tick, Seed: a.Seed, Camera: a.Camera, Mode: a.Mode, Width: w, Height: h, Bundle: a.Bundle})
				if err != nil {
					return nil, err
				}
				return textResult(rep, readPNG(s, fmt.Sprint(rep["out"]))), nil
			}),
		},
		{
			Name:        "simulate",
			Description: "Run ticks through the game: a scenario file, or a scene with ticks/seed/inputs. Returns the verdict, the expectations table, invariant violations with their tick, trace event counts, the run_id (for trace) and exactly one contact sheet (screenshots + trajectories: top-down, or in the XY plane for a 2D game whose scene camera is orthographic looking along -Z).",
			InputSchema: schema(map[string]any{
				"scenario":    str("scenario name (\"move\" = tests/scenarios/move.scenario.json) or file path"),
				"scene":       str("scene (without scenario)"),
				"ticks":       num("ticks to simulate (without scenario, default 200)"),
				"seed":        num("RNG seed (without scenario)"),
				"inputs":      map[string]any{"type": "array", "description": "input events as in scenario files: {tick, press[], release[], mouse{x,y}, buttons[], text}", "items": map[string]any{"type": "object"}},
				"screenshots": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "ticks to capture"},
				"invariants":  strList("invariants to check (default: the scenario's, else the project's)"),
			}),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					Scenario    string          `json:"scenario"`
					Scene       string          `json:"scene"`
					Ticks       int             `json:"ticks"`
					Seed        uint64          `json:"seed"`
					Inputs      json.RawMessage `json:"inputs"`
					Screenshots []int           `json:"screenshots"`
					Invariants  []string        `json:"invariants"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				scenario := a.Scenario
				if scenario != "" {
					scenario = s.scenarioPath(scenario)
				}
				if scenario != "" && !filepath.IsAbs(scenario) {
					scenario = filepath.Join(s.Root, filepath.FromSlash(scenario))
				}
				rep, err := s.Simulate(SimulateOptions{Scenario: scenario, Scene: a.Scene, Ticks: a.Ticks, Seed: a.Seed, Inputs: a.Inputs, Screenshots: a.Screenshots, Invariants: a.Invariants})
				if err != nil {
					return nil, err
				}
				// Keep the text small: drop per-run paths the agent does not need.
				delete(rep, "trace")
				delete(rep, "out")
				res := textResult(rep, fitPNG(readPNG(s, fmt.Sprint(rep["sheet"])), mcpSheetMaxW, mcpSheetMaxH))
				return res, nil
			}),
		},
		{
			Name:        "trace",
			Description: "A slice of the trace of a previous simulate (run_id from its result): at most 200 ticks per call; with events, only ticks containing those events (and only the events).",
			InputSchema: schema(map[string]any{
				"run_id":    str("run id returned by simulate"),
				"from_tick": num("first tick"),
				"to_tick":   num("last tick (at most from_tick+199)"),
				"events":    strList("event names to keep, e.g. collision, gem_collected"),
			}, "run_id", "from_tick", "to_tick"),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					RunID    string   `json:"run_id"`
					FromTick int      `json:"from_tick"`
					ToTick   int      `json:"to_tick"`
					Events   []string `json:"events"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				t, err := s.Trace(a.RunID, a.FromTick, a.ToTick, a.Events)
				if err != nil {
					return nil, err
				}
				return textResult(t), nil
			}),
		},
		{
			Name:        "query",
			Description: "Ask a frame bundle (.vframe from render with bundle=true) what is at a pixel (entity, depth, distance, world position, normal) or for per-entity coverage (visible pixels, screen bounds, occlusion ratio). Returns the answer and a small image marking it.",
			InputSchema: schema(map[string]any{
				"frame":    str("frame bundle path, e.g. out/render_main_t0_color.vframe"),
				"at":       str("pixel x,y"),
				"coverage": boolean("per-entity coverage"),
			}, "frame"),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					Frame    string `json:"frame"`
					At       string `json:"at"`
					Coverage bool   `json:"coverage"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				frame := a.Frame
				if !filepath.IsAbs(frame) {
					frame = filepath.Join(s.Root, filepath.FromSlash(frame))
				}
				rep, err := Query(frame, a.At, a.Coverage)
				if err != nil {
					return nil, err
				}
				rep["frame"] = a.Frame
				return textResult(rep, queryImage(frame, a.At, a.Coverage)), nil
			}),
		},
		{
			Name:        "diff",
			Description: "Compare two images (PNG or .vframe): changed pixels, ratio, bounding box, max delta, and one a|b|heat sheet. Use it to confirm that only the intended change happened.",
			InputSchema: schema(map[string]any{
				"a": str("first image"), "b": str("second image"),
				"threshold": num("ignore channel differences up to this value (default 0)"),
			}, "a", "b"),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					A         string `json:"a"`
					B         string `json:"b"`
					Threshold int    `json:"threshold"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				abs := func(p string) string {
					if filepath.IsAbs(p) {
						return p
					}
					return filepath.Join(s.Root, filepath.FromSlash(p))
				}
				base := func(p string) string { return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) }
				out := s.Out("diff_" + base(a.A) + "_vs_" + base(a.B) + ".png")
				r, err := Diff(abs(a.A), abs(a.B), out, a.Threshold)
				if err != nil {
					return nil, err
				}
				r.Sheet = s.Rel(out)
				return textResult(r, readPNG(s, out)), nil
			}),
		},
		{
			Name:        "test",
			Description: "go test ./... plus every scenario in tests/scenarios and their goldens in tests/golden. Returns results and, for failed scenarios, the first failing expectation.",
			InputSchema: schema(map[string]any{"update_golden": boolean("rewrite golden hashes and sheets")}),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					UpdateGolden bool `json:"update_golden"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				r, err := s.Test(a.UpdateGolden)
				if err != nil {
					return nil, err
				}
				res := textResult(r)
				res.IsError = !r.OK
				return res, nil
			}),
		},
		{
			Name:        "fuzz",
			Description: "Play random-input games looking for invariant violations; a minimized repro is written to tests/scenarios/fuzz_<hash>.scenario.json.",
			InputSchema: schema(map[string]any{
				"scene": str("scene (default: the project's)"),
				"games": num("number of games (default 200)"),
				"ticks": num("ticks per game (default 200)"),
				"seed":  num("seed of the random players (default 1)"),
			}),
			Handler: withSession(func(ctx context.Context, s *Session, args json.RawMessage) (*mcp.Result, error) {
				a := struct {
					Scene string `json:"scene"`
					Games int    `json:"games"`
					Ticks int    `json:"ticks"`
					Seed  uint64 `json:"seed"`
				}{Games: 200, Ticks: 200, Seed: 1}
				if err := mcp.Strict(args, &a); err != nil {
					return nil, err
				}
				r, err := s.Fuzz(FuzzOptions{Scene: a.Scene, Games: a.Games, Ticks: a.Ticks, Seed: a.Seed})
				if err != nil {
					return nil, err
				}
				return textResult(r), nil
			}),
		},
		{
			Name:        "docs",
			Description: "Format reference for writing sources: model, texture, material, scene, scenario, api (also project, vda, inspect, config, 2d).",
			InputSchema: schema(map[string]any{"topic": enum("topic", docs.All()...)}, "topic"),
			Handler: func(ctx context.Context, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					Topic string `json:"topic"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return errResult(err), nil
				}
				text, err := docs.Get(a.Topic)
				if err != nil {
					return errResult(err), nil
				}
				return &mcp.Result{Content: []mcp.Content{mcp.Text(text)}}, nil
			},
		},
	}
	return append(tools, m.extraTools...)
}

func as[T any](err error, target *T) bool {
	for e := err; e != nil; {
		if t, ok := e.(T); ok {
			*target = t
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

func modeList() []string {
	var out []string
	for _, md := range gfx.RenderModes {
		out = append(out, md.String())
	}
	return out
}

// clampSize returns the render size for the requested width and height (≤ 0 = not
// given): mcpDefaultW×mcpDefaultH when neither is given, the missing side at the default
// aspect (4:3) when one is, and each side clamped to [16, mcpMaxW] and [16, mcpMaxH].
func clampSize(w, h int) (int, int) {
	if w <= 0 && h <= 0 {
		return mcpDefaultW, mcpDefaultH
	}
	if w <= 0 {
		w = h * mcpDefaultW / mcpDefaultH
	}
	if h <= 0 {
		h = w * mcpDefaultH / mcpDefaultW
	}
	return min(max(w, 16), mcpMaxW), min(max(h, 16), mcpMaxH)
}

// queryMark is the color of the crosshair on query images (magenta).
const queryMark = 0xffff00ff

// queryImage returns a PNG illustrating a query on the frame bundle at path frame (see
// queryPicture), or nil when the bundle cannot be read.
func queryImage(frame, at string, coverage bool) []byte {
	f, err := inspect.ReadFrame(frame)
	if err != nil {
		return nil
	}
	var b strings.Builder
	if queryPicture(f, at, coverage).EncodePNG(&b) != nil {
		return nil
	}
	return []byte(b.String())
}

// queryPicture draws the image of a query on f: the color buffer with a crosshair on the
// queried pixel, or the ID buffer colored per entity for coverage. The frame is fitted
// inside mcpMaxW×mcpMaxH, so any frame an MCP render produces comes back unscaled; the
// crosshair is drawn after the fit, at the scaled position, so it stays a sharp 17-pixel
// mark on a larger bundle too.
func queryPicture(f *inspect.Frame, at string, coverage bool) *gfx.Image {
	img := gfx.NewImage(f.Width, f.Height)
	if coverage {
		for i, id := range f.ID {
			img.Pix[i] = gfx.IDColor(id)
		}
		return sheet.Fit(img, mcpMaxW, mcpMaxH)
	}
	copy(img.Pix, f.Color)
	img = sheet.Fit(img, mcpMaxW, mcpMaxH)
	var x, y int
	if _, err := fmt.Sscanf(strings.ReplaceAll(at, " ", ""), "%d,%d", &x, &y); err != nil || x < 0 || y < 0 || x >= f.Width || y >= f.Height {
		return img
	}
	x, y = x*img.W/f.Width, y*img.H/f.Height
	for d := -8; d <= 8; d++ {
		if d > -2 && d < 2 {
			continue
		}
		img.Set(x+d, y, queryMark)
		img.Set(x, y+d, queryMark)
	}
	return img
}

// status assembles the status report.
func (m *mcpServer) status(s *Session) map[string]any {
	out := map[string]any{
		"tool":    versionInfo(m.env),
		"project": map[string]any{"name": s.Project.Name, "root": s.Root, "engine": s.Project.Engine, "entry": s.Project.Entry, "default_scene": s.Project.DefaultScene, "tick_rate": s.Project.TickRate},
		"target":  targetLine(),
	}
	if c := s.engineCheck(m.env); true {
		out["engine_check"] = c
	}
	if cr, err := cook.Run(cook.Options{Root: s.Root, Project: s.Project, DryRun: true}); err == nil {
		var stale, failing []string
		for _, it := range cr.Items {
			switch it.Status {
			case cook.StatusStale:
				stale = append(stale, string(it.Kind)+"/"+it.Name)
			case cook.StatusError:
				failing = append(failing, string(it.Kind)+"/"+it.Name)
			}
		}
		out["assets"] = map[string]any{"fresh": cr.Fresh, "stale": stale, "failing": failing, "warnings": cr.Warnings}
	}
	m.mu.Lock()
	if m.lastBuild != nil {
		out["last_build"] = map[string]any{"ok": m.lastBuild.OK, "errors": len(m.lastBuild.Errors), "ms": m.lastBuild.Millis}
	} else {
		out["last_build"] = nil
	}
	m.mu.Unlock()
	out["update"] = updateCheck(m.env)
	return out
}

// ServeMCP runs the MCP server on stdin/stdout.
func ServeMCP(ctx context.Context, env *Env, projectDir string) error {
	m := &mcpServer{env: env, projectDir: projectDir}
	m.extraTools = mcpExtraTools(m)
	srv := &mcp.Server{Name: "veduta", Version: env.Version, Tools: m.tools(), Log: env.Stderr}
	return srv.Serve(ctx, env.Stdin, env.Stdout)
}

// mcpExtraTools adds tools implemented elsewhere (inspect, release).
var mcpExtraTools = func(m *mcpServer) []mcp.Tool { return nil }

func init() {
	register(command{
		name:    "mcp",
		usage:   "mcp",
		summary: "serve the MCP server on stdio (JSON-RPC 2.0) for AI agents such as Claude Code",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			if len(args) > 0 {
				return nil, usagef("mcp takes no arguments (use --project DIR before mcp)")
			}
			// auto_update: apply before serving, never mid-session, then run the new binary.
			if res, err := autoUpdate(env); err != nil {
				fmt.Fprintln(env.Stderr, "veduta mcp: update skipped:", err)
			} else if res != nil {
				fmt.Fprintf(env.Stderr, "veduta mcp: updated %s → %s on the %s channel, restarting\n", res.Result.From, res.Result.To, res.Channel)
				if err := execSelf(); err != nil {
					fmt.Fprintln(env.Stderr, "veduta mcp: restart failed, serving with the old binary:", err)
				}
			}
			return nil, ServeMCP(context.Background(), env, mcpProjectDir)
		},
	})
}

// mcpProjectDir is set by Main from --project.
var mcpProjectDir string
