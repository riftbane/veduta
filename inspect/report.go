package inspect

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gfx/soft"
	"github.com/riftbane/veduta/scene"
)

// Severities, from most to least severe.
const (
	Error   = "error"
	Warning = "warning"
	Info    = "info"
)

func severityRank(s string) int {
	switch s {
	case Error:
		return 0
	case Warning:
		return 1
	}
	return 2
}

// Report is what every inspector returns (spec §9). Agents read reports first; sheets
// only confirm.
type Report struct {
	Subject string         `json:"subject"` // "model:crate", "texture:grass", "scene:main"
	Summary Summary        `json:"summary"`
	Issues  []Issue        `json:"issues"`  // most severe first, then by code
	Metrics map[string]any `json:"metrics"` // encoded with sorted keys
	Sheets  []string       `json:"sheets"`  // PNG paths written
}

// Summary counts issues by severity.
type Summary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Info     int `json:"info"`
}

// Issue is one finding. Code is one of the spec's codes (MESH_FLIPPED_NORMALS, …); Count
// is how many elements are affected; Where locates them (part index, triangles, entity,
// layer, pixels…); Hint says what to change in the source.
type Issue struct {
	Severity string         `json:"severity"`
	Code     string         `json:"code"`
	Count    int            `json:"count"`
	Where    map[string]any `json:"where,omitempty"`
	Hint     string         `json:"hint"`
}

// Add appends an issue.
func (r *Report) Add(severity, code string, count int, where map[string]any, hint string) {
	r.Issues = append(r.Issues, Issue{Severity: severity, Code: code, Count: count, Where: where, Hint: hint})
}

// Finish sorts the issues (severity, then code, then insertion order), fills the summary,
// and applies focus: when focus is a code, only issues with that code are kept (the
// summary still counts all of them).
func (r *Report) Finish(focus string) {
	sort.SliceStable(r.Issues, func(i, j int) bool {
		a, b := r.Issues[i], r.Issues[j]
		if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
			return ra < rb
		}
		return a.Code < b.Code
	})
	r.Summary = Summary{}
	for _, is := range r.Issues {
		switch is.Severity {
		case Error:
			r.Summary.Errors++
		case Warning:
			r.Summary.Warnings++
		default:
			r.Summary.Info++
		}
	}
	if focus != "" {
		kept := r.Issues[:0]
		for _, is := range r.Issues {
			if is.Code == focus {
				kept = append(kept, is)
			}
		}
		r.Issues = kept
	}
	if r.Issues == nil {
		r.Issues = []Issue{}
	}
	if r.Metrics == nil {
		r.Metrics = map[string]any{}
	}
	if r.Sheets == nil {
		r.Sheets = []string{}
	}
}

// Has reports whether the report contains an issue with code.
func (r *Report) Has(code string) bool {
	for _, is := range r.Issues {
		if is.Code == code {
			return true
		}
	}
	return false
}

// Options configures an inspector.
type Options struct {
	OutDir string   // directory for sheets (default "out")
	Focus  string   // keep only issues with this code
	Sheets []string // sheets to write; nil = the inspector's default (one summary sheet); ["none"] = none
	Width  int      // tile width in pixels (default 640/2 for grids, see each inspector)
	Height int      // tile height (default Width*9/16)
}

func (o Options) outDir() string {
	if o.OutDir == "" {
		return "out"
	}
	return o.OutDir
}

// sheetPath returns the path of sheet kind for subject ("model:crate" + "turntable" →
// out/crate.turntable.png, with ':' in sheet names replaced by '_').
func (o Options) sheetPath(subject, kind string) string {
	name := subject
	if i := strings.IndexByte(subject, ':'); i >= 0 {
		name = subject[i+1:]
	}
	return filepath.Join(o.outDir(), name+"."+strings.ReplaceAll(kind, ":", "_")+".png")
}

// wants reports whether sheet kind was requested (or is the default when none were).
func (o Options) wants(kind string, isDefault bool) bool {
	if o.Sheets == nil {
		return isDefault
	}
	for _, s := range o.Sheets {
		if s == kind || s == "all" {
			return true
		}
		if strings.HasSuffix(s, ":") && strings.HasPrefix(kind, s) {
			return true
		}
	}
	return false
}

// Renderer renders assets of a library for inspection. It owns a software renderer; call
// Close when done.
type Renderer struct {
	Lib *asset.Library
	r   *soft.Renderer
	res *scene.Resources
}

// NewRenderer uploads the library's assets.
func NewRenderer(lib *asset.Library) (*Renderer, error) {
	r := soft.New(soft.Options{})
	res, err := scene.Upload(r, lib.Models, lib.Textures, lib.Materials)
	if err != nil {
		r.Close()
		return nil, err
	}
	return &Renderer{Lib: lib, r: r, res: res}, nil
}

// Close releases the renderer.
func (ir *Renderer) Close() { ir.r.Close() }

// Resources returns the uploaded asset handles.
func (ir *Renderer) Resources() *scene.Resources { return ir.res }

// Backend returns the software renderer (for custom draw lists).
func (ir *Renderer) Backend() *soft.Renderer { return ir.r }

// ModelView describes one inspection render of a single model.
type ModelView struct {
	Model    string
	Material string // entity material for parts without their own ("" = default)
	Camera   scene.Camera
	Mode     gfx.RenderMode
	W, H     int
	Light    gfx.Light // zero value: gfx.DefaultLight
	BG       uint32    // background (0: sheet background)
	Normals  bool      // also produce the normal buffer
	Extra    func(dl *gfx.DrawList, view int)
}

// RenderModel draws one model alone at the origin (entity id 1) and returns the
// framebuffer.
func (ir *Renderer) RenderModel(v ModelView) (*gfx.Framebuffer, error) {
	s := scene.New("inspect", ir.res.Bounds)
	if v.Light == (gfx.Light{}) {
		s.Light = gfx.DefaultLight
	} else {
		s.Light = v.Light
	}
	s.Background = v.BG
	if s.Background == 0 {
		s.Background = 0xff2a2f38
	}
	s.Spawn(scene.Entity{Name: "subject", Kind: scene.KindStatic, Model: v.Model, Material: v.Material, Visible: true})
	s.Update()
	return ir.drawScene(s, v.Camera, v.W, v.H, v.Mode, v.Normals, v.Extra)
}

// RenderScene draws scene s (already loaded) with cam.
func (ir *Renderer) RenderScene(s *scene.Scene, cam scene.Camera, w, h int, mode gfx.RenderMode, normals bool, extra func(dl *gfx.DrawList, view int)) (*gfx.Framebuffer, error) {
	return ir.drawScene(s, cam, w, h, mode, normals, extra)
}

func (ir *Renderer) drawScene(s *scene.Scene, cam scene.Camera, w, h int, mode gfx.RenderMode, normals bool, extra func(dl *gfx.DrawList, view int)) (*gfx.Framebuffer, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("inspect: bad size %dx%d", w, h)
	}
	var dl gfx.DrawList
	s.Draw(&dl, ir.res, scene.DrawOptions{Camera: cam, Width: w, Height: h, Mode: mode})
	if extra != nil {
		extra(&dl, 0)
	}
	fb := gfx.NewFramebuffer(w, h, normals)
	if err := ir.r.Begin(fb); err != nil {
		return nil, err
	}
	if err := ir.r.Draw(&dl); err != nil {
		return nil, err
	}
	if err := ir.r.End(); err != nil {
		return nil, err
	}
	return fb, nil
}
