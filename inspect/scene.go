package inspect

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/internal/sheet"
	"github.com/riftbane/veduta/scene"
)

// SceneSheets lists the sheet kinds of Scene in the order they are written. Options.Sheets
// also accepts "all" and "none".
var SceneSheets = []string{"summary", "camera", "top", "ids"}

// SceneCodes lists the issue codes Scene reports (spec §9.3). Options.Focus must be one
// of them (or empty).
var SceneCodes = []string{
	scnMissing, scnOutside, scnOverlap, scnSeesNothing, scnOffscreen, scnUnlit, scnZFight,
}

const (
	scnMissing     = "SCENE_MISSING_ASSET"
	scnOutside     = "SCENE_ENTITY_OUTSIDE_BOUNDS"
	scnOverlap     = "SCENE_OVERLAP"
	scnSeesNothing = "SCENE_CAMERA_SEES_NOTHING"
	scnOffscreen   = "SCENE_ENTITY_OFFSCREEN"
	scnUnlit       = "SCENE_UNLIT"
	scnZFight      = "SCENE_ZFIGHT_RISK"

	scnImportant = "important" // tag checked by SCENE_ENTITY_OFFSCREEN
)

// Thresholds of the scene checks (documented on Scene).
const (
	scnOverlapMin = 1e-3   // m: SCENE_OVERLAP needs the AABB intersection deeper than this on every axis
	scnZDist      = 1e-3   // m: overlap vertices within this of the other face's plane are coplanar
	scnZCos       = 0.9999 // |n1·n2| at least this (≈ 0.81°): parallel faces
	scnZAreaRel   = 1e-3   // overlap area above this × the smaller triangle's area counts
	scnZOffset    = 0.01   // m: offset the z-fight hint recommends
	scnZBudget    = 1 << 24
	scnZLines     = 256 // z-fight overlap outlines drawn on sheets
	scnDarkLuma   = 32  // mean Rec. 601 luminance (0–255) of entity pixels below this is dark
	scnBlackLight = 0.05
	scnDimLight   = 0.5
	scnIssueMax   = 16 // issues listed per code; the rest are summarized in one more issue
	scnListMax    = 8  // names or indices listed in an issue's where
	scnEntityMax  = 64 // entities listed in the entity_pixels metric
	scnLabelMax   = 64 // entity names drawn on the top view
	scnLegendRows = 32 // rows of the standalone ids legend
	scnPad        = 2
)

// Sheet colors.
const (
	scnColorStatic  = 0xff8090a0 // static entity AABB (top view)
	scnColorDynamic = 0xff40ff40 // other entity AABB (top view)
	scnColorIssue   = 0xffff3b30 // overlap boxes, entities outside the bounds
	scnColorHidden  = 0xffff9f0a // important entities without a visible pixel
	scnColorZFight  = 0xffff2dd4 // coplanar overlap outlines
	scnColorCamera  = 0xffffd60a // scene camera frustum (top view)
	scnColorBounds  = 0xff5e9bff // project bounds (top view)
)

// Scene inspects scene name of ir.Lib (spec §9.3) and returns its report, subject
// "scene:<name>". The scene is loaded with scene.Load and the library's model bounds; the
// project (ir.Lib.Project, defaults when nil) supplies the bounds and the analysis
// resolution (inspect_resolution, 640×360 by default). Pixel numbers come from one render
// of the scene camera at that resolution in which every drawn part writes the ID buffer
// (alpha-blended parts too, so a pane of glass counts as covering what is behind it).
// Entity index i below is the entity's position in the scene file (its id is i+1).
//
// Issues:
//
//   - SCENE_MISSING_ASSET (error): an entity's model or material, a material named by a
//     part of an entity's model, or the texture of a material an entity uses is not in
//     the library. One issue per missing (kind, file, name); count = entities affected;
//     where: entity (first), entities, kind, file, field, name.
//   - SCENE_ENTITY_OUTSIDE_BOUNDS (warning): the world position or the AABB of an entity
//     is not inside the project bounds (boundary included; non-finite values are
//     outside). One per entity; where: position, aabb, bounds, sides, outside_by.
//   - SCENE_OVERLAP (warning): the AABBs of two "static" entities intersect by more than
//     1 mm on every axis. Touching boxes (a crate on the ground), parent/descendant pairs
//     and pairs whose models are both drawn only with alpha-blended materials (water and
//     mist stacked by layer) do not count; visibility is ignored (AABBs are collision
//     volumes). One per pair; where: a, b, ids, overlap (depth per axis) and box (the
//     intersection).
//   - SCENE_CAMERA_SEES_NOTHING (error): no pixel of the camera render belongs to an
//     entity. The where explains why (distance to the drawn entities vs near/far, angle
//     off the view direction, entities whose AABB contains the camera).
//   - SCENE_ENTITY_OFFSCREEN (warning): an entity tagged "important" has no visible
//     pixel. The entity is rendered alone to tell "occluded" (projected pixels > 0, the
//     occluders are listed) from "outside_view"; "hidden" (visible: false), "no_model" and
//     "missing_model" need no render.
//   - SCENE_UNLIT: warning when light.color and light.ambient have every channel ≤ 0.05
//     while lit materials are drawn, or (otherwise) when the mean luminance of entity
//     pixels is below 32/255; info when every drawn part uses an unlit material (the
//     light has no effect).
//   - SCENE_ZFIGHT_RISK (warning): two different drawn entities have triangles whose
//     planes are parallel (|n1·n2| ≥ 0.9999) and that overlap in the plane over more than
//     0.1% of the smaller triangle, with every vertex of the overlap within 1 mm of both
//     planes. Faces must face the same way, or either must be double-sided (cull
//     "none"): a crate resting on the ground (bottom face down, ground face up) is not a
//     risk. Two alpha-blended triangles are not a risk either: neither writes depth, so
//     the draw order (layer, then depth) decides which covers which. Only entity pairs
//     whose AABBs touch are compared, triangles are bucketed by plane; at most 2^24
//     triangle pairs are tested (metric zfight_truncated). One issue per entity pair;
//     count = overlapping triangle pairs; where: normal, point, area.
//
// At most 16 issues are listed per code; one more issue with where.omitted counts the
// rest.
//
// Sheets (SceneSheets): camera (the scene camera, color, issue overlays: red overlap
// boxes and out-of-bounds AABBs, orange AABBs of hidden important entities, magenta
// z-fight outlines), top (orthographic top view, -Z up, every entity AABB, entity names,
// the camera frustum in yellow and the project bounds in blue), ids (entity id false
// color with a legend of every visible entity by id) and summary (default: camera, top,
// ids and legend in one 640×482 grid). Views are framed 4:3 like the console panel:
// single views default to 640×480 (the ids view adds its legend below), summary tiles to
// 317×238; opt.Width/Height override both. The camera view keeps the scene camera's
// vertical field of view, so its horizontal field follows the aspect, and the top view
// draws the frustum at that same aspect, not at the analysis resolution. An unknown
// scene, sheet kind or focus code is an error.
func Scene(ir *Renderer, name string, opt Options) (*Report, error) {
	if ir == nil || ir.Lib == nil {
		return nil, errors.New("inspect scene: no library")
	}
	for _, s := range opt.Sheets {
		if s != "all" && s != "none" && !slices.Contains(SceneSheets, s) {
			return nil, fmt.Errorf("inspect scene %s: unknown sheet %q (valid: %s, all, none)", name, s, strings.Join(SceneSheets, ", "))
		}
	}
	if opt.Focus != "" && !slices.Contains(SceneCodes, opt.Focus) {
		return nil, fmt.Errorf("inspect scene %s: unknown focus %q (valid: %s)", name, opt.Focus, strings.Join(SceneCodes, ", "))
	}
	src := ir.Lib.Scenes[name]
	if src == nil {
		return nil, fmt.Errorf("inspect scene: unknown scene %q (have: %s)", name, strings.Join(asset.Names(ir.Lib.Scenes), ", "))
	}
	if ir.r == nil || ir.res == nil {
		return nil, errors.New("inspect scene: renderer has no backend (use NewRenderer)")
	}
	a, err := scnAnalyze(ir, name, src)
	if err != nil {
		return nil, fmt.Errorf("inspect scene %s: %w", name, err)
	}
	rep := &Report{Subject: "scene:" + name, Metrics: map[string]any{}}
	a.flush(rep)
	a.metrics(rep.Metrics)
	for _, kind := range SceneSheets {
		if !opt.wants(kind, kind == "summary") {
			continue
		}
		img, err := a.sheet(kind, opt)
		if err != nil {
			return nil, fmt.Errorf("inspect scene %s: sheet %s: %w", name, kind, err)
		}
		p := opt.sheetPath(rep.Subject, kind)
		if err := scnWritePNG(p, img); err != nil {
			return nil, fmt.Errorf("inspect scene %s: sheet %s: %w", name, kind, err)
		}
		rep.Sheets = append(rep.Sheets, p)
	}
	rep.Finish(opt.Focus)
	return rep, nil
}

// scnAnalysis holds a loaded scene and everything measured on it.
type scnAnalysis struct {
	ir     *Renderer
	lib    *asset.Library
	src    *asset.Scene
	s      *scene.Scene
	ents   []*scene.Entity // id order = scene-file order
	bounds gmath.AABB      // project bounds
	assets string          // assets directory, for source paths in hints
	file   string          // scene source path
	w, h   int             // analysis resolution

	drawn      []bool // visible, with a model in the library
	glass      []bool // every part of the model is alpha-blended (writes no depth)
	drawnCount int
	triangles  int
	litParts   int
	unlitParts int
	unlitMats  []string

	fb           *gfx.Framebuffer // scene camera render (color, every part writes ids)
	pixels       []int            // visible pixels per entity index
	bbox         [][4]int         // screen bounding box of those pixels
	entityPixels int
	lumaSum      int64 // Σ luminance × 1000 over entity pixels
	lumaEnt      []int64
	darkPixels   int

	issues   []Issue
	outside  []int
	overlaps []scnPair
	hidden   []int
	zfights  []*scnZPair
	zTests   int
	zTrunc   bool
	tris     [][]scnTri
	trisDone []bool
}

type scnPair struct {
	i, j int
	box  gmath.AABB
}

func scnAnalyze(ir *Renderer, name string, src *asset.Scene) (*scnAnalysis, error) {
	s, err := scene.Load(src, ir.Lib.ModelBounds)
	if err != nil {
		return nil, err
	}
	p := ir.Lib.Project
	if p == nil {
		d := asset.DefaultProject
		p = &d
	}
	a := &scnAnalysis{ir: ir, lib: ir.Lib, src: src, s: s, ents: s.Entities(), bounds: p.Bounds, assets: p.Assets,
		w: p.InspectResolution[0], h: p.InspectResolution[1]}
	if a.bounds == (gmath.AABB{}) {
		a.bounds = asset.DefaultProject.Bounds
	}
	if a.assets == "" {
		a.assets = asset.DefaultProject.Assets
	}
	if a.w <= 0 || a.h <= 0 || a.w > asset.MaxResolution || a.h > asset.MaxResolution {
		a.w, a.h = asset.DefaultProject.InspectResolution[0], asset.DefaultProject.InspectResolution[1]
	}
	a.file = path.Join(a.assets, "scenes", name+".scene.json")
	n := len(a.ents)
	a.drawn = make([]bool, n)
	a.glass = make([]bool, n)
	a.pixels = make([]int, n)
	a.bbox = make([][4]int, n)
	a.lumaEnt = make([]int64, n)
	a.tris = make([][]scnTri, n)
	a.trisDone = make([]bool, n)
	a.prepare()
	if err := a.render(); err != nil {
		return nil, err
	}
	a.checkMissing()
	a.checkOutside()
	a.checkOverlap()
	a.checkSeesNothing()
	if err := a.checkOffscreen(); err != nil {
		return nil, err
	}
	a.checkUnlit()
	a.checkZFight()
	return a, nil
}

func (a *scnAnalysis) idx(id uint32) int {
	k := int(id) - 1
	if k >= 0 && k < len(a.ents) && a.ents[k].ID == id {
		return k
	}
	return -1
}

func (a *scnAnalysis) add(sev, code string, count int, where map[string]any, hint string) {
	a.issues = append(a.issues, Issue{Severity: sev, Code: code, Count: count, Where: where, Hint: hint})
}

// flush copies the issues into r, at most scnIssueMax per code plus one issue counting
// the omitted ones.
func (a *scnAnalysis) flush(r *Report) {
	listed := map[string]int{} // lookups only
	type omit struct {
		n, count int
		sev      string
	}
	omitted := map[string]*omit{} // lookups only; read back in SceneCodes order
	for _, is := range a.issues {
		if listed[is.Code] < scnIssueMax {
			listed[is.Code]++
			r.Add(is.Severity, is.Code, is.Count, is.Where, is.Hint)
			continue
		}
		o := omitted[is.Code]
		if o == nil {
			o = &omit{sev: is.Severity}
			omitted[is.Code] = o
		}
		o.n++
		o.count += is.Count
		if severityRank(is.Severity) < severityRank(o.sev) {
			o.sev = is.Severity
		}
	}
	for _, code := range SceneCodes {
		if o := omitted[code]; o != nil {
			r.Add(o.sev, code, o.count, map[string]any{"omitted": o.n},
				fmt.Sprintf("%d more %s issues are not listed (the first %d are). Fix those and inspect again, or inspect with focus %s.", o.n, code, scnIssueMax, code))
		}
	}
}

// ---------------------------------------------------------------------------------
// Measurements.

func scnPartMaterial(m *asset.Model, part gfx.MeshPart) string {
	if part.Material >= 0 && part.Material < len(m.Materials) {
		return m.Materials[part.Material]
	}
	return ""
}

func (a *scnAnalysis) model(e *scene.Entity) *asset.Model {
	if e.Model == "" || a.lib.Models[e.Model] == nil {
		return nil
	}
	if mr := a.ir.res.Models[e.Model]; mr != nil {
		return mr.Model
	}
	return nil
}

func (a *scnAnalysis) prepare() {
	for k, e := range a.ents {
		m := a.model(e)
		if m != nil {
			blended, other := 0, 0
			for _, part := range m.Mesh.Parts {
				switch {
				case part.Count <= 0:
				case a.ir.res.Material(scnPartMaterial(m, part), e.Material).Alpha == "blend":
					blended++
				default:
					other++
				}
			}
			a.glass[k] = blended > 0 && other == 0
		}
		if !e.Visible || m == nil {
			continue
		}
		a.drawn[k] = true
		a.drawnCount++
		for _, part := range m.Mesh.Parts {
			if part.Count <= 0 {
				continue
			}
			a.triangles += part.Count / 3
			mat := a.ir.res.Material(scnPartMaterial(m, part), e.Material)
			if mat.Unlit {
				a.unlitParts++
				if !slices.Contains(a.unlitMats, mat.Name) {
					a.unlitMats = append(a.unlitMats, mat.Name)
				}
			} else {
				a.litParts++
			}
		}
	}
	sort.Strings(a.unlitMats)
}

// scnCoverAll makes every draw command write depth and ids, so alpha-blended parts
// cover their pixels in the ID buffer.
func scnCoverAll(dl *gfx.DrawList, _ int) {
	for i := range dl.Cmds {
		dl.Cmds[i].State.DepthWrite = true
	}
}

func scnLuma(c uint32) int64 {
	return 299*int64(c>>16&0xff) + 587*int64(c>>8&0xff) + 114*int64(c&0xff)
}

func (a *scnAnalysis) render() error {
	fb, err := a.ir.RenderScene(a.s, a.s.Camera, a.w, a.h, gfx.ModeColor, false, scnCoverAll)
	if err != nil {
		return err
	}
	a.fb = fb
	for i, id := range fb.ID {
		k := a.idx(id)
		if id == 0 || k < 0 {
			continue
		}
		x, y := i%fb.W, i/fb.W
		if a.pixels[k] == 0 {
			a.bbox[k] = [4]int{x, y, x, y}
		} else {
			b := &a.bbox[k]
			b[0], b[1], b[2], b[3] = min(b[0], x), min(b[1], y), max(b[2], x), max(b[3], y)
		}
		a.pixels[k]++
		a.entityPixels++
		l := scnLuma(fb.Color[i])
		a.lumaSum += l
		a.lumaEnt[k] += l
		if l < scnDarkLuma*1000 {
			a.darkPixels++
		}
	}
	return nil
}

func (a *scnAnalysis) meanLuma() float64 {
	if a.entityPixels == 0 {
		return 0
	}
	return float64(a.lumaSum) / 1000 / float64(a.entityPixels)
}

// ---------------------------------------------------------------------------------
// SCENE_MISSING_ASSET

type scnRef struct {
	kind, file, field, name string
	owner                   string // model or material holding the reference ("" for the scene)
	parts                   []int
	ents                    []int
}

func (a *scnAnalysis) checkMissing() {
	var refs []*scnRef
	byKey := map[string]*scnRef{} // lookups only; refs keeps the order
	add := func(kind, file, field, name, owner string, ent, part int) {
		key := kind + "\x00" + file + "\x00" + name
		r := byKey[key]
		if r == nil {
			r = &scnRef{kind: kind, file: file, field: field, name: name, owner: owner}
			byKey[key] = r
			refs = append(refs, r)
		}
		if len(r.ents) == 0 || r.ents[len(r.ents)-1] != ent {
			r.ents = append(r.ents, ent)
		}
		if part >= 0 && !slices.Contains(r.parts, part) {
			r.parts = append(r.parts, part)
		}
	}
	for k, e := range a.ents {
		if e.Model != "" && a.lib.Models[e.Model] == nil {
			add("model", a.file, fmt.Sprintf("entities[%d].model", k), e.Model, "", k, -1)
		}
		if e.Material != "" && a.lib.Materials[e.Material] == nil {
			add("material", a.file, fmt.Sprintf("entities[%d].material", k), e.Material, "", k, -1)
		}
		var used []string
		entityMat := e.Material != "" && a.lib.Materials[e.Material] != nil
		if m := a.lib.Models[e.Model]; m != nil && e.Model != "" {
			mfile := path.Join(a.assets, "models", e.Model+".model.json")
			for pi, part := range m.Mesh.Parts {
				pm := scnPartMaterial(m, part)
				switch {
				case pm == "":
				case a.lib.Materials[pm] == nil:
					add("material", mfile, fmt.Sprintf("parts[%d].material", pi), pm, e.Model, k, pi)
				case !slices.Contains(used, pm):
					used = append(used, pm)
				}
			}
		}
		if entityMat && !slices.Contains(used, e.Material) {
			used = append(used, e.Material)
		}
		for _, mn := range used {
			if t := a.lib.Materials[mn].Texture; t != "" && a.lib.Textures[t] == nil {
				add("texture", path.Join(a.assets, "materials", mn+".mat.json"), "texture", t, mn, k, -1)
			}
		}
	}
	for _, r := range refs {
		names := make([]string, 0, min(len(r.ents), scnListMax))
		for _, k := range r.ents[:min(len(r.ents), scnListMax)] {
			names = append(names, a.ents[k].Name)
		}
		where := map[string]any{"entity": a.ents[r.ents[0]].Name, "entities": names, "kind": r.kind,
			"file": r.file, "field": r.field, "name": r.name}
		if len(r.parts) > 0 {
			where["parts"] = r.parts
		}
		users := a.entPhrase(r.ents)
		// Scene references: one field per entity.
		var fields []string
		if r.owner == "" {
			for _, k := range r.ents {
				fields = append(fields, fmt.Sprintf("entities[%d].%s", k, r.kind))
			}
			where["fields"] = fields[:min(len(fields), scnListMax)]
		}
		fieldText, change := r.field, "change "+r.field
		if len(fields) > 1 {
			fieldText = strings.Join(fields[:min(len(fields), 3)], ", ")
			if len(fields) > 3 {
				fieldText += fmt.Sprintf(" and %d more", len(fields)-3)
			}
			change = "change those fields"
		}
		var hint string
		switch {
		case r.kind == "model":
			hint = fmt.Sprintf("%s: model %q (%s in %s) is not in the library, so nothing is drawn. Create %s or %s%s.",
				scnCap(users), r.name, fieldText, r.file, path.Join(a.assets, "models", r.name+".model.json"), change, scnSuggest(r.name, asset.Names(a.lib.Models)))
		case r.kind == "material" && r.owner == "":
			hint = fmt.Sprintf("%s: material %q (%s in %s) is not in the library, so the default white material is used. Create %s or %s%s.",
				scnCap(users), r.name, fieldText, r.file, path.Join(a.assets, "materials", r.name+".mat.json"), change, scnSuggest(r.name, asset.Names(a.lib.Materials)))
		case r.kind == "material":
			hint = fmt.Sprintf("Model %q, used by %s, names material %q in %s of %s, which is not in the library (those parts render with the default white material). Create %s or change %s%s.",
				r.owner, users, r.name, r.field, r.file, path.Join(a.assets, "materials", r.name+".mat.json"), r.field, scnSuggest(r.name, asset.Names(a.lib.Materials)))
		default:
			hint = fmt.Sprintf("Material %q, used by %s, names texture %q in %s, which is not in the library (it renders untextured). Create %s or change \"texture\" in %s%s.",
				r.owner, users, r.name, r.file, path.Join(a.assets, "textures", r.name+".tex.json"), r.file, scnSuggest(r.name, asset.Names(a.lib.Textures)))
		}
		a.add(Error, scnMissing, len(r.ents), where, hint)
	}
}

// entPhrase names entities by name and scene-file index, the first few only.
func (a *scnAnalysis) entPhrase(ks []int) string {
	if len(ks) == 1 {
		return "entity " + a.ref(ks[0])
	}
	parts := make([]string, 0, 3)
	for _, k := range ks[:min(len(ks), 3)] {
		parts = append(parts, a.ref(k))
	}
	s := "entities " + strings.Join(parts, ", ")
	if len(ks) > 3 {
		s += fmt.Sprintf(" and %d more", len(ks)-3)
	}
	return s
}

func (a *scnAnalysis) ref(k int) string { return fmt.Sprintf("%q (entities[%d])", a.ents[k].Name, k) }

// scnCap upper-cases the first letter of an ASCII phrase.
func scnCap(s string) string {
	if s != "" && s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}

// parentNote explains that a position is relative to the parent, when there is one.
func (a *scnAnalysis) parentNote(k int) string {
	if p := a.idx(a.ents[k].Parent); p >= 0 {
		return fmt.Sprintf(" (relative to parent %q)", a.ents[p].Name)
	}
	return ""
}

// scnSuggest returns " (did you mean \"x\"?)" for the closest name within edit
// distance max(1, len/3), else the list of available names.
func scnSuggest(name string, have []string) string {
	if len(have) == 0 {
		return " (the library has none)"
	}
	best, bestD := "", max(1, len(name)/3)+1
	for _, h := range have {
		if d := scnEditDistance(name, h); d < bestD {
			best, bestD = h, d
		}
	}
	if best != "" {
		return fmt.Sprintf(" (did you mean %q?)", best)
	}
	list := have[:min(len(have), scnListMax)]
	s := " (available: " + strings.Join(list, ", ")
	if len(have) > len(list) {
		s += ", …"
	}
	return s + ")"
}

func scnEditDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			c := prev[j-1]
			if ra[i-1] != rb[j-1] {
				c++
			}
			cur[j] = min(c, prev[j]+1, cur[j-1]+1)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// ---------------------------------------------------------------------------------
// SCENE_ENTITY_OUTSIDE_BOUNDS

func (a *scnAnalysis) checkOutside() {
	b := a.bounds
	for k, e := range a.ents {
		p := e.WorldPosition()
		box := e.AABB
		hasBox := !box.IsEmpty()
		posOut := !b.Contains(p)
		if !posOut && (!hasBox || b.ContainsBox(box)) {
			continue
		}
		a.outside = append(a.outside, k)
		ext := gmath.AABB{Min: p, Max: p}
		if hasBox {
			ext = box.Extend(p)
		}
		finite := ext.Min.IsFinite() && ext.Max.IsFinite()
		var sides []string
		excess := 0.0
		for ax := 0; ax < 3; ax++ {
			name := "xyz"[ax : ax+1]
			if d := float64(b.Min.Get(ax)) - float64(ext.Min.Get(ax)); d > 0 {
				sides = append(sides, "-"+name)
				excess = max(excess, d)
			}
			if d := float64(ext.Max.Get(ax)) - float64(b.Max.Get(ax)); d > 0 {
				sides = append(sides, "+"+name)
				excess = max(excess, d)
			}
		}
		where := map[string]any{"entity": e.Name, "id": e.ID, "position": scnVec(p), "bounds": scnBox(b), "position_outside": posOut}
		if hasBox {
			where["aabb"] = scnBox(box)
		}
		field := fmt.Sprintf("entities[%d].position", k)
		var hint string
		if !finite {
			hint = fmt.Sprintf("Entity %s has a non-finite position or AABB (position %s): set %s%s to finite numbers inside the project bounds %s.",
				a.ref(k), scnFV(p), field, a.parentNote(k), scnFBox(b))
		} else {
			where["sides"] = sides
			where["outside_by"] = scnNum(excess)
			what := "Its AABB"
			if posOut {
				what = "It"
			}
			hint = fmt.Sprintf("Entity %s at %s: %s extends %s m past the project bounds %s on %s. Move it inside by changing %s%s, or enlarge \"bounds\" in %s.",
				a.ref(k), scnFV(p), strings.ToLower(what[:1])+what[1:], scnF(excess), scnFBox(b), strings.Join(sides, ", "), field, a.parentNote(k), asset.ProjectFile)
			if posOut {
				hint += " Its position itself is outside, so the within_bounds invariant fails."
			}
		}
		a.add(Warning, scnOutside, 1, where, hint)
	}
}

// ---------------------------------------------------------------------------------
// SCENE_OVERLAP

func scnFiniteBox(b gmath.AABB) bool {
	return !b.IsEmpty() && b.Min.IsFinite() && b.Max.IsFinite()
}

// ancestor reports whether entity index anc is an ancestor of entity index k.
func (a *scnAnalysis) ancestor(anc, k int) bool {
	id := a.ents[anc].ID
	p := a.ents[k].Parent
	for n := 0; p != 0 && n <= len(a.ents); n++ {
		if p == id {
			return true
		}
		q := a.idx(p)
		if q < 0 {
			break
		}
		p = a.ents[q].Parent
	}
	return false
}

func (a *scnAnalysis) related(i, j int) bool { return a.ancestor(i, j) || a.ancestor(j, i) }

func (a *scnAnalysis) checkOverlap() {
	var st []int
	for k, e := range a.ents {
		if e.Kind == scene.KindStatic && scnFiniteBox(e.AABB) {
			st = append(st, k)
		}
	}
	sort.SliceStable(st, func(x, y int) bool { return a.ents[st[x]].AABB.Min.X < a.ents[st[y]].AABB.Min.X })
	var pairs []scnPair
	for x := range st {
		A := a.ents[st[x]].AABB
		for y := x + 1; y < len(st); y++ {
			B := a.ents[st[y]].AABB
			if float64(B.Min.X) >= float64(A.Max.X)-scnOverlapMin {
				break
			}
			d := scnDepth(A, B)
			if d.X > scnOverlapMin && d.Y > scnOverlapMin && d.Z > scnOverlapMin {
				i, j := min(st[x], st[y]), max(st[x], st[y])
				if !a.related(i, j) && !(a.glass[i] && a.glass[j]) {
					pairs = append(pairs, scnPair{i: i, j: j, box: gmath.AABB{Min: A.Min.Max(B.Min), Max: A.Max.Min(B.Max)}})
				}
			}
		}
	}
	sort.Slice(pairs, func(x, y int) bool {
		if pairs[x].i != pairs[y].i {
			return pairs[x].i < pairs[y].i
		}
		return pairs[x].j < pairs[y].j
	})
	a.overlaps = pairs
	for _, pr := range pairs {
		ei, ej := a.ents[pr.i], a.ents[pr.j]
		d := scnDepth(ei.AABB, ej.AABB)
		ax := 0
		for k := 1; k < 3; k++ {
			if d.Get(k) < d.Get(ax) {
				ax = k
			}
		}
		dir := float32(1)
		if ej.AABB.Center().Get(ax) < ei.AABB.Center().Get(ax) {
			dir = -1
		}
		amount := math.Ceil(scnRound(float64(d.Get(ax)))*100) / 100 // cm, without float32 noise
		axis := "xyz"[ax : ax+1]
		move := fmt.Sprintf("Move %q by %s%s m along %s (entities[%d].position%s", ej.Name, scnSign(dir), scnF(amount), axis, pr.j, a.parentNote(pr.j))
		if a.idx(ej.Parent) < 0 {
			np := ej.Transform.Position
			np = np.With(ax, float32(float64(np.Get(ax))+float64(float64(dir)*amount)))
			move += fmt.Sprintf(" %s → %s", scnFV(ej.Transform.Position), scnFV(np))
		}
		move += ")"
		where := map[string]any{"a": ei.Name, "b": ej.Name, "ids": []uint32{ei.ID, ej.ID}, "overlap": scnVec(d), "box": scnBox(pr.box)}
		hint := fmt.Sprintf("Static entities %s and %s interpenetrate: their AABBs overlap by %s m (x, y, z). %s, or move or scale one of them.",
			a.ref(pr.i), a.ref(pr.j), scnFV(d), move)
		a.add(Warning, scnOverlap, 1, where, hint)
	}
}

// scnDepth returns the depth of the intersection of a and b on each axis (negative when
// they are apart).
func scnDepth(a, b gmath.AABB) gmath.Vec3 {
	return a.Max.Min(b.Max).Sub(a.Min.Max(b.Min))
}

func scnSign(f float32) string {
	if f < 0 {
		return "-"
	}
	return "+"
}

// ---------------------------------------------------------------------------------
// SCENE_CAMERA_SEES_NOTHING

func (a *scnAnalysis) drawnBox() gmath.AABB {
	b := gmath.EmptyAABB()
	for k, e := range a.ents {
		if a.drawn[k] && scnFiniteBox(e.AABB) {
			b = b.Union(e.AABB)
		}
	}
	return b
}

// scnBoxDist returns the distance from p to the nearest and the farthest point of b.
func scnBoxDist(p gmath.Vec3, b gmath.AABB) (near, far float64) {
	q := p.Max(b.Min).Min(b.Max)
	near = float64(p.Dist(q))
	for _, c := range b.Corners() {
		far = max(far, float64(p.Dist(c)))
	}
	return near, far
}

// scnAngle returns the angle in degrees between directions u and v (0 when either is
// zero).
func scnAngle(u, v gmath.Vec3) float64 {
	u, v = u.Normalize(), v.Normalize()
	if u == (gmath.Vec3{}) || v == (gmath.Vec3{}) {
		return 0
	}
	c := max(-1, min(1, float64(u.Dot(v))))
	return gmath.Acos64(c) * gmath.Rad2Deg
}

// inView reports whether p projects inside the scene camera's view volume.
func (a *scnAnalysis) inView(p gmath.Vec3) bool {
	cam := a.s.Camera
	vp := cam.Proj(float32(a.w) / float32(a.h)).Mul(cam.View())
	c := vp.MulVec4(p.Vec4(1))
	return c.W > 0 && gmath.Abs(c.X) <= c.W && gmath.Abs(c.Y) <= c.W && gmath.Abs(c.Z) <= c.W
}

func (a *scnAnalysis) checkSeesNothing() {
	if a.entityPixels > 0 {
		return
	}
	cam := a.s.Camera
	where := map[string]any{"entity_pixels": 0, "pixels": a.w * a.h, "drawn_entities": a.drawnCount}
	box := a.drawnBox()
	if a.drawnCount == 0 || box.IsEmpty() {
		hint := fmt.Sprintf("No entity is drawn: none of the %d entities is visible with a model in the library. Give entities a model (entities[i].model) and keep them visible (entities[i].visible) in %s.", len(a.ents), a.file)
		if len(a.ents) == 0 {
			hint = fmt.Sprintf("The scene has no entities: add entities with a model to \"entities\" in %s.", a.file)
		}
		a.add(Error, scnSeesNothing, 1, where, hint)
		return
	}
	c := box.Center()
	fwd := cam.Target.Sub(cam.Position)
	angle := scnAngle(fwd, c.Sub(cam.Position))
	dNear, dFar := scnBoxDist(cam.Position, box)
	where["content_bounds"] = scnBox(box)
	where["content_center"] = scnVec(c)
	where["angle_deg"] = scnNum(angle)
	where["distance"] = []any{scnNum(dNear), scnNum(dFar)}
	var inside []string
	for k, e := range a.ents {
		if a.drawn[k] && e.AABB.Contains(cam.Position) {
			inside = append(inside, e.Name)
		}
	}
	if len(inside) > 0 {
		where["inside"] = inside[:min(len(inside), scnListMax)]
	}
	at := fmt.Sprintf("The scene camera at camera.position %s looking at camera.look_at %s sees no entity", scnFV(cam.Position), scnFV(cam.Target))
	var hint string
	switch {
	case dNear > float64(cam.Far):
		hint = fmt.Sprintf("%s: the drawn entities are %s–%s m away, beyond camera.far (%s). Raise camera.far to at least %s or move camera.position closer to %s.",
			at, scnF(dNear), scnF(dFar), scnF(float64(cam.Far)), scnF(math.Ceil(dFar*1.1)), scnFV(c))
	case dFar < float64(cam.Near):
		hint = fmt.Sprintf("%s: the drawn entities are within %s m, closer than camera.near (%s). Lower camera.near below %s or move camera.position away.",
			at, scnF(dFar), scnF(float64(cam.Near)), scnF(dNear))
	case !a.inView(c):
		hint = fmt.Sprintf("%s: the drawn entities (center %s) are %s° off its view direction. Set camera.look_at to %s.", at, scnFV(c), scnF(math.Round(angle)), scnFV(c))
	case len(inside) > 0:
		hint = fmt.Sprintf("%s: it is inside the AABB of %q, whose faces point away from it and are culled. Move camera.position out of it, towards %s.", at, inside[0], scnFV(c))
	default:
		hint = fmt.Sprintf("%s although their center %s is in view: they are cut by camera.near (%s) / camera.far (%s) or too small. Set camera.look_at to %s and move camera.position towards it.",
			at, scnFV(c), scnF(float64(cam.Near)), scnF(float64(cam.Far)), scnFV(c))
	}
	a.add(Error, scnSeesNothing, 1, where, hint)
}

// ---------------------------------------------------------------------------------
// SCENE_ENTITY_OFFSCREEN

func (a *scnAnalysis) checkOffscreen() error {
	rendered := 0
	for k, e := range a.ents {
		if !e.HasTag(scnImportant) || a.pixels[k] > 0 {
			continue
		}
		a.hidden = append(a.hidden, k)
		tagNote := fmt.Sprintf(" (or remove %q from entities[%d].tags)", scnImportant, k)
		where := map[string]any{"entity": e.Name, "id": e.ID, "visible_pixels": 0}
		var hint string
		switch {
		case e.Model == "":
			where["reason"] = "no_model"
			hint = fmt.Sprintf("Entity %s is tagged %q but has no model, so it has no pixel: set entities[%d].model%s.", a.ref(k), scnImportant, k, tagNote)
		case a.model(e) == nil:
			where["reason"] = "missing_model"
			hint = fmt.Sprintf("Entity %s is tagged %q but its model %q is not in the library (see SCENE_MISSING_ASSET): fix entities[%d].model.", a.ref(k), scnImportant, e.Model, k)
		case !e.Visible:
			where["reason"] = "hidden"
			hint = fmt.Sprintf("Entity %s is tagged %q but not drawn: set entities[%d].visible to true%s.", a.ref(k), scnImportant, k, tagNote)
		default:
			if rendered >= scnIssueMax {
				where["reason"] = "not_measured"
				hint = fmt.Sprintf("Entity %s is tagged %q and has no visible pixel from the scene camera.", a.ref(k), scnImportant)
				break
			}
			rendered++
			var err error
			if hint, err = a.offscreenDetail(k, where); err != nil {
				return err
			}
		}
		a.add(Warning, scnOffscreen, 1, where, hint)
	}
	return nil
}

// offscreenDetail renders entity k alone to count its projected pixels and fills where
// with the reason and occluders; it returns the hint.
func (a *scnAnalysis) offscreenDetail(k int, where map[string]any) (string, error) {
	e := a.ents[k]
	vis := make([]bool, len(a.ents))
	for i, o := range a.ents {
		vis[i] = o.Visible
		o.Visible = i == k
	}
	fb, err := a.ir.RenderScene(a.s, a.s.Camera, a.w, a.h, gfx.ModeIDs, false, scnCoverAll)
	for i, o := range a.ents {
		o.Visible = vis[i]
	}
	if err != nil {
		return "", err
	}
	projected := 0
	occ := make([]int, len(a.ents))
	for i, id := range fb.ID {
		if id != e.ID {
			continue
		}
		projected++
		if o := a.idx(a.fb.ID[i]); o >= 0 {
			occ[o]++
		}
	}
	where["projected_pixels"] = projected
	cam := a.s.Camera
	center := e.AABB.Center()
	angle := scnAngle(cam.Target.Sub(cam.Position), center.Sub(cam.Position))
	dNear, _ := scnBoxDist(cam.Position, e.AABB)
	where["angle_deg"] = scnNum(angle)
	where["distance"] = scnNum(float64(cam.Position.Dist(center)))
	if projected == 0 {
		if dNear <= float64(cam.Far) && a.inView(center) {
			where["reason"] = "no_pixels"
			return fmt.Sprintf("Entity %s is tagged %q and its center %s is in view, but it covers no pixel at %d×%d: it is too small %s m away (raise entities[%d].scale or move camera.position closer) or its material's alpha cuts it out entirely.",
				a.ref(k), scnImportant, scnFV(center), a.w, a.h, scnF(float64(cam.Position.Dist(center))), k), nil
		}
		where["reason"] = "outside_view"
		why := "outside the camera's field of view"
		switch {
		case dNear > float64(cam.Far):
			why = fmt.Sprintf("beyond camera.far (%s m; it is %s m away)", scnF(float64(cam.Far)), scnF(dNear))
		case angle > 90:
			why = "behind the camera"
		}
		return fmt.Sprintf("Entity %s is tagged %q but projects to no pixel from the scene camera: it is %s, %s° off the view direction. Point camera.look_at towards %s or move entities[%d].position%s into view.",
			a.ref(k), scnImportant, why, scnF(math.Round(angle)), scnFV(center), k, a.parentNote(k)), nil
	}
	where["reason"] = "occluded"
	var order []int
	for o, n := range occ {
		if n > 0 {
			order = append(order, o)
		}
	}
	sort.SliceStable(order, func(x, y int) bool { return occ[order[x]] > occ[order[y]] })
	type occluder struct {
		Entity string `json:"entity"`
		ID     uint32 `json:"id"`
		Pixels int    `json:"pixels"`
	}
	var list []occluder
	var names []string
	for _, o := range order[:min(len(order), 3)] {
		list = append(list, occluder{a.ents[o].Name, a.ents[o].ID, occ[o]})
		names = append(names, fmt.Sprintf("%s, %d px", a.ref(o), occ[o]))
	}
	where["occluders"] = list
	hint := fmt.Sprintf("Entity %s is tagged %q and projects to %d px from the scene camera, but all of them are covered by %s. Move camera.position (now %s) so the view clears it, move the occluder (entities[i].position), or move entities[%d].position.",
		a.ref(k), scnImportant, projected, strings.Join(names, "; "), scnFV(cam.Position), k)
	if len(order) > 0 {
		hint = strings.Replace(hint, "entities[i].position", fmt.Sprintf("entities[%d].position", order[0]), 1)
	}
	return hint, nil
}

// ---------------------------------------------------------------------------------
// SCENE_UNLIT

func scnMaxC(v gmath.Vec3) float32 { return max(v.X, v.Y, v.Z) }

func scnColor(v gmath.Vec3) string { return gfx.FormatColor(gfx.Vec4Color(v.Vec4(1))) }

func (a *scnAnalysis) checkUnlit() {
	if a.litParts+a.unlitParts == 0 {
		return
	}
	l := a.s.Light
	if a.litParts == 0 {
		files := make([]string, 0, len(a.unlitMats))
		for _, m := range a.unlitMats[:min(len(a.unlitMats), scnListMax)] {
			files = append(files, path.Join(a.assets, "materials", m+".mat.json"))
		}
		a.add(Info, scnUnlit, a.unlitParts, map[string]any{"unlit_parts": a.unlitParts, "materials": a.unlitMats[:min(len(a.unlitMats), scnListMax)]},
			fmt.Sprintf("Every drawn part (%d) uses an unlit material, so light.direction, light.color and light.ambient have no effect. If shading is wanted, set \"unlit\": false in %s.", a.unlitParts, strings.Join(files, ", ")))
	}
	black := scnMaxC(l.Color) <= scnBlackLight && scnMaxC(l.Ambient) <= scnBlackLight
	mean := a.meanLuma()
	if black && a.litParts > 0 {
		a.add(Warning, scnUnlit, a.litParts, map[string]any{"light_color": scnColor(l.Color), "ambient": scnColor(l.Ambient),
			"lit_parts": a.litParts, "mean_luminance": scnNum(mean)},
			fmt.Sprintf("light.color (%s) and light.ambient (%s) are both near black (every channel ≤ %s/255), so the %d lit parts render black. Set light.color to \"#ffffff\" and light.ambient to \"#404040\" (the defaults) in %s.",
				scnColor(l.Color), scnColor(l.Ambient), scnF(scnBlackLight*255), a.litParts, a.file))
		return
	}
	if a.entityPixels == 0 || mean >= scnDarkLuma {
		return
	}
	where := map[string]any{"mean_luminance": scnNum(mean), "threshold": scnDarkLuma, "entity_pixels": a.entityPixels,
		"dark_pixels": a.darkPixels, "light_color": scnColor(l.Color), "ambient": scnColor(l.Ambient)}
	// The darkest entities covering at least 1% of the entity pixels.
	var cand []int
	for k, n := range a.pixels {
		if n > 0 && n*100 >= a.entityPixels {
			cand = append(cand, k)
		}
	}
	lum := func(k int) float64 { return float64(a.lumaEnt[k]) / 1000 / float64(a.pixels[k]) }
	sort.SliceStable(cand, func(x, y int) bool { return lum(cand[x]) < lum(cand[y]) })
	type dark struct {
		Entity    string  `json:"entity"`
		Luminance float64 `json:"luminance"`
		Pixels    int     `json:"pixels"`
	}
	var darkest []dark
	var mats []string
	for _, k := range cand[:min(len(cand), 3)] {
		darkest = append(darkest, dark{a.ents[k].Name, scnRound(lum(k)), a.pixels[k]})
		if m := a.model(a.ents[k]); m != nil {
			for _, part := range m.Mesh.Parts {
				if part.Count <= 0 {
					continue
				}
				if mat := a.ir.res.Material(scnPartMaterial(m, part), a.ents[k].Material); mat.Name != "" && !slices.Contains(mats, mat.Name) {
					mats = append(mats, mat.Name)
				}
			}
		}
	}
	where["darkest"] = darkest
	msg := fmt.Sprintf("Entity pixels average luminance %s/255 (below %d) from the scene camera.", scnF(mean), scnDarkLuma)
	var fix []string
	if a.litParts > 0 && l.Dir.Normalize().Y > 0 {
		fix = append(fix, fmt.Sprintf("light.direction %s points up (its y is positive), so upward faces get only ambient light: make its y negative", scnFV(l.Dir)))
	}
	if a.litParts > 0 && scnMaxC(l.Color) < scnDimLight && scnMaxC(l.Ambient) < scnDimLight {
		fix = append(fix, fmt.Sprintf("raise light.color (now %s) and light.ambient (now %s), for example to \"#ffffff\" and \"#404040\"", scnColor(l.Color), scnColor(l.Ambient)))
	}
	if len(fix) == 0 {
		files := make([]string, 0, len(mats))
		for _, m := range mats {
			files = append(files, path.Join(a.assets, "materials", m+".mat.json"))
		}
		what := "the materials of the darkest entities"
		if len(files) > 0 {
			what = strings.Join(files, ", ")
		}
		fix = append(fix, fmt.Sprintf("the light is not the cause: raise \"albedo\" (or brighten the texture) in %s", what))
	}
	a.add(Warning, scnUnlit, a.darkPixels, where, msg+" To fix: "+strings.Join(fix, "; ")+".")
}

// ---------------------------------------------------------------------------------
// SCENE_ZFIGHT_RISK

// scnV is a float64 vector. Each product that feeds an addition or subtraction is rounded
// explicitly so arm64 cannot fuse it into a multiply-add (see gmath.m32); the helpers are
// inlined, so rounding inside them also protects their callers.
type scnV [3]float64

func (p scnV) add(q scnV) scnV { return scnV{p[0] + q[0], p[1] + q[1], p[2] + q[2]} }
func (p scnV) sub(q scnV) scnV { return scnV{p[0] - q[0], p[1] - q[1], p[2] - q[2]} }
func (p scnV) mul(s float64) scnV {
	return scnV{float64(p[0] * s), float64(p[1] * s), float64(p[2] * s)}
}
func (p scnV) dot(q scnV) float64 {
	return float64(p[0]*q[0]) + float64(p[1]*q[1]) + float64(p[2]*q[2])
}
func (p scnV) length() float64  { return math.Sqrt(p.dot(p)) }
func (p scnV) vec3() gmath.Vec3 { return gmath.V3(float32(p[0]), float32(p[1]), float32(p[2])) }
func (p scnV) cross(q scnV) scnV {
	return scnV{
		float64(p[1]*q[2]) - float64(p[2]*q[1]),
		float64(p[2]*q[0]) - float64(p[0]*q[2]),
		float64(p[0]*q[1]) - float64(p[1]*q[0]),
	}
}
func (p scnV) finite() bool { return !math.IsNaN(p.dot(p)) && !math.IsInf(p.dot(p), 0) }
func scnXform(m gmath.Mat4, v gmath.Vec3) scnV {
	x, y, z := float64(v.X), float64(v.Y), float64(v.Z)
	return scnV{
		float64(float64(m[0])*x) + float64(float64(m[4])*y) + float64(float64(m[8])*z) + float64(m[12]),
		float64(float64(m[1])*x) + float64(float64(m[5])*y) + float64(float64(m[9])*z) + float64(m[13]),
		float64(float64(m[2])*x) + float64(float64(m[6])*y) + float64(float64(m[10])*z) + float64(m[14]),
	}
}

// scnTri is a world-space triangle of a drawn entity.
type scnTri struct {
	p        [3]scnV
	n        scnV // unit normal of the winding (front side)
	d        float64
	area     float64
	lo, hi   scnV
	tri      int // mesh triangle index (Mesh.Indices[3·tri:])
	part     int
	twoSided bool
	blend    bool // alpha-blended: depth tested but never writes depth
}

func (a *scnAnalysis) worldTris(k int) []scnTri {
	if a.trisDone[k] {
		return a.tris[k]
	}
	a.trisDone[k] = true
	e := a.ents[k]
	m := a.model(e)
	if m == nil {
		return nil
	}
	w := e.World()
	mesh := &m.Mesh
	var out []scnTri
	for pi, part := range mesh.Parts {
		mat := a.ir.res.Material(scnPartMaterial(m, part), e.Material)
		for o := part.First; o+2 < part.First+part.Count && o+2 < len(mesh.Indices); o += 3 {
			var t scnTri
			ok := true
			for c := 0; c < 3; c++ {
				vi := int(mesh.Indices[o+c])
				if vi >= len(mesh.Vertices) {
					ok = false
					break
				}
				t.p[c] = scnXform(w, mesh.Vertices[vi].Pos)
			}
			if !ok {
				continue
			}
			n := t.p[1].sub(t.p[0]).cross(t.p[2].sub(t.p[0]))
			l := n.length()
			if !(l > 0) || math.IsInf(l, 0) || !t.p[0].finite() {
				continue
			}
			t.n = n.mul(1 / l)
			t.area = float64(l / 2)
			t.d = t.n.dot(t.p[0])
			t.tri, t.part, t.twoSided, t.blend = o/3, pi, mat.Cull == gfx.CullNone, mat.Alpha == "blend"
			for c := 0; c < 3; c++ {
				t.lo[c] = min(t.p[0][c], t.p[1][c], t.p[2][c])
				t.hi[c] = max(t.p[0][c], t.p[1][c], t.p[2][c])
			}
			out = append(out, t)
		}
	}
	a.tris[k] = out
	return out
}

// scnZPair is the z-fight finding of one entity pair.
type scnZPair struct {
	i, j         int // entity indices, i < j
	pairs        int
	area         float64
	best         float64
	normal       scnV // of the best pair's triangle of i
	nj           scnV // of the best pair's triangle of j
	point        scnV
	same         bool // some overlapping pair faces the same way
	twoMat       string
	tI, tJ       []int
	outlines     [][]scnV
	outlineCount int
}

func (a *scnAnalysis) checkZFight() {
	var cand []int
	for k, e := range a.ents {
		if a.drawn[k] && scnFiniteBox(e.AABB) {
			cand = append(cand, k)
		}
	}
	sort.SliceStable(cand, func(x, y int) bool { return a.ents[cand[x]].AABB.Min.X < a.ents[cand[y]].AABB.Min.X })
	var pairs [][2]int
	for x := range cand {
		A := a.ents[cand[x]].AABB
		for y := x + 1; y < len(cand); y++ {
			B := a.ents[cand[y]].AABB
			if float64(B.Min.X) > float64(A.Max.X)+scnZDist {
				break
			}
			d := scnDepth(A, B)
			if float64(d.Y) >= -scnZDist && float64(d.Z) >= -scnZDist {
				pairs = append(pairs, [2]int{min(cand[x], cand[y]), max(cand[x], cand[y])})
			}
		}
	}
	sort.Slice(pairs, func(x, y int) bool {
		if pairs[x][0] != pairs[y][0] {
			return pairs[x][0] < pairs[y][0]
		}
		return pairs[x][1] < pairs[y][1]
	})
	for _, pr := range pairs {
		if a.zTrunc {
			break
		}
		if z := a.zPair(pr[0], pr[1]); z != nil {
			a.zfights = append(a.zfights, z)
		}
	}
	outlines := 0
	for _, z := range a.zfights {
		if outlines+len(z.outlines) > scnZLines {
			z.outlines = z.outlines[:max(0, scnZLines-outlines)]
		}
		outlines += len(z.outlines)
		a.zIssue(z)
	}
}

type scnZKey [4]int64

// zPair compares the triangles of entities i and j near the intersection of their AABBs.
func (a *scnAnalysis) zPair(i, j int) *scnZPair {
	box := gmath.AABB{Min: a.ents[i].AABB.Min.Max(a.ents[j].AABB.Min), Max: a.ents[i].AABB.Max.Min(a.ents[j].AABB.Max)}
	lo := scnV{float64(box.Min.X) - scnZDist, float64(box.Min.Y) - scnZDist, float64(box.Min.Z) - scnZDist}
	hi := scnV{float64(box.Max.X) + scnZDist, float64(box.Max.Y) + scnZDist, float64(box.Max.Z) + scnZDist}
	near := func(tris []scnTri) []int {
		var out []int
		for t := range tris {
			if scnBoxesTouch(tris[t].lo, tris[t].hi, lo, hi, 0) {
				out = append(out, t)
			}
		}
		return out
	}
	ti, tj := a.worldTris(i), a.worldTris(j)
	ci, cj := near(ti), near(tj)
	if len(ci) == 0 || len(cj) == 0 {
		return nil
	}
	radius := 0.0
	for _, t := range ci {
		for c := 0; c < 3; c++ {
			radius = max(radius, ti[t].p[c].length())
		}
	}
	for _, t := range cj {
		for c := 0; c < 3; c++ {
			radius = max(radius, tj[t].p[c].length())
		}
	}
	cell := scnZDist + float64(0.0142*radius) + 1e-9
	if math.IsInf(cell, 0) || math.IsNaN(cell) {
		return nil
	}
	key := func(n scnV, d float64) scnZKey {
		return scnZKey{int64(math.Round(n[0] * 32)), int64(math.Round(n[1] * 32)), int64(math.Round(n[2] * 32)), int64(math.Floor(d / cell))}
	}
	buckets := map[scnZKey][]int{} // lookups only
	anyTwo := false
	for _, t := range cj {
		k := key(tj[t].n, tj[t].d)
		buckets[k] = append(buckets[k], t)
		anyTwo = anyTwo || tj[t].twoSided
	}
	var z *scnZPair
	var found []int
	for _, t := range ci {
		A := &ti[t]
		found = found[:0]
		for _, sgn := range [2]float64{1, -1} {
			if sgn < 0 && !A.twoSided && !anyTwo {
				continue
			}
			base := key(A.n.mul(sgn), A.d*sgn)
			for d0 := int64(-1); d0 <= 1; d0++ {
				for d1 := int64(-1); d1 <= 1; d1++ {
					for d2 := int64(-1); d2 <= 1; d2++ {
						for d3 := int64(-1); d3 <= 1; d3++ {
							found = append(found, buckets[scnZKey{base[0] + d0, base[1] + d1, base[2] + d2, base[3] + d3}]...)
						}
					}
				}
			}
		}
		slices.Sort(found)
		found = slices.Compact(found)
		for _, u := range found {
			B := &tj[u]
			if A.blend && B.blend { // neither writes depth: the draw order decides, not the depth test
				continue
			}
			if a.zTests >= scnZBudget {
				a.zTrunc = true
				return z
			}
			a.zTests++
			area, poly, same, ok := scnCoplanarOverlap(A, B)
			if !ok {
				continue
			}
			if z == nil {
				z = &scnZPair{i: i, j: j}
			}
			z.pairs++
			z.area += area
			if !same {
				if A.twoSided {
					z.twoMat = a.partMatName(i, A.part)
				} else {
					z.twoMat = a.partMatName(j, B.part)
				}
			}
			z.same = z.same || same
			if area > z.best {
				z.best = area
				z.normal, z.nj = A.n, B.n
				var c scnV
				for _, p := range poly {
					c = c.add(p)
				}
				z.point = c.mul(1 / float64(len(poly)))
			}
			if !slices.Contains(z.tI, A.tri) {
				z.tI = append(z.tI, A.tri)
			}
			if !slices.Contains(z.tJ, B.tri) {
				z.tJ = append(z.tJ, B.tri)
			}
			if len(z.outlines) < scnZLines {
				z.outlines = append(z.outlines, poly)
			}
		}
	}
	if z != nil {
		slices.Sort(z.tI)
		slices.Sort(z.tJ)
	}
	return z
}

func (a *scnAnalysis) partMatName(k, part int) string {
	m := a.model(a.ents[k])
	if m == nil || part < 0 || part >= len(m.Mesh.Parts) {
		return ""
	}
	return a.ir.res.Material(scnPartMaterial(m, m.Mesh.Parts[part]), a.ents[k].Material).Name
}

func scnBoxesTouch(alo, ahi, blo, bhi scnV, eps float64) bool {
	for c := 0; c < 3; c++ {
		if alo[c] > bhi[c]+eps || blo[c] > ahi[c]+eps {
			return false
		}
	}
	return true
}

type scnP2 [2]float64

func scnCross2(p, q, r scnP2) float64 {
	return float64((q[0]-p[0])*(r[1]-p[1])) - float64((q[1]-p[1])*(r[0]-p[0]))
}

// scnCoplanarOverlap tests two triangles for a z-fight risk: parallel planes, facing the
// same way (or either double-sided), overlapping in the plane over more than scnZAreaRel
// of the smaller triangle, and every vertex of the overlap within scnZDist of both
// planes. It returns the overlap area and outline (on A's plane).
func scnCoplanarOverlap(A, B *scnTri) (area float64, poly []scnV, same, ok bool) {
	c := A.n.dot(B.n)
	same = c >= scnZCos
	if !same && (c > -scnZCos || !(A.twoSided || B.twoSided)) {
		return 0, nil, false, false
	}
	if !scnBoxesTouch(A.lo, A.hi, B.lo, B.hi, scnZDist) {
		return 0, nil, false, false
	}
	// B entirely on one side of A's plane: no overlap within tolerance.
	above, below := 0, 0
	for _, p := range B.p {
		s := A.n.dot(p) - A.d
		if s > scnZDist {
			above++
		} else if s < -scnZDist {
			below++
		}
	}
	if above == 3 || below == 3 {
		return 0, nil, false, false
	}
	// 2D basis of A's plane.
	ax := scnV{1, 0, 0}
	if math.Abs(A.n[1]) < math.Abs(A.n[0]) && math.Abs(A.n[1]) <= math.Abs(A.n[2]) {
		ax = scnV{0, 1, 0}
	} else if math.Abs(A.n[2]) < math.Abs(A.n[0]) && math.Abs(A.n[2]) < math.Abs(A.n[1]) {
		ax = scnV{0, 0, 1}
	}
	u := A.n.cross(ax)
	u = u.mul(1 / u.length())
	v := A.n.cross(u)
	var pa, pb [3]scnP2
	for i := 0; i < 3; i++ {
		pa[i] = scnP2{A.p[i].dot(u), A.p[i].dot(v)}
		pb[i] = scnP2{B.p[i].dot(u), B.p[i].dot(v)}
	}
	clip := scnClipTri(pa, pb)
	if len(clip) < 3 {
		return 0, nil, false, false
	}
	for i := range clip {
		q := clip[(i+1)%len(clip)]
		area += float64(clip[i][0]*q[1]) - float64(q[0]*clip[i][1])
	}
	area = float64(math.Abs(area) / 2)
	if !(area > scnZAreaRel*min(A.area, B.area)) {
		return 0, nil, false, false
	}
	poly = make([]scnV, len(clip))
	for i, q := range clip {
		p := u.mul(q[0]).add(v.mul(q[1])).add(A.n.mul(A.d))
		if math.Abs(B.n.dot(p)-B.d) > scnZDist {
			return 0, nil, false, false
		}
		poly[i] = p
	}
	return area, poly, same, true
}

// scnClipTri clips triangle a by triangle b (Sutherland–Hodgman) and returns the
// intersection polygon.
func scnClipTri(a, b [3]scnP2) []scnP2 {
	if scnCross2(b[0], b[1], b[2]) < 0 {
		b[1], b[2] = b[2], b[1]
	}
	poly := []scnP2{a[0], a[1], a[2]}
	for e := 0; e < 3 && len(poly) > 0; e++ {
		p, q := b[e], b[(e+1)%3]
		out := make([]scnP2, 0, len(poly)+1)
		for i := range poly {
			cur, prev := poly[i], poly[(i+len(poly)-1)%len(poly)]
			dc, dp := scnCross2(p, q, cur), scnCross2(p, q, prev)
			if dc >= 0 {
				if dp < 0 {
					out = append(out, scnLerp2(prev, cur, dp/(dp-dc)))
				}
				out = append(out, cur)
			} else if dp >= 0 {
				out = append(out, scnLerp2(prev, cur, dp/(dp-dc)))
			}
		}
		poly = out
	}
	return poly
}

func scnLerp2(a, b scnP2, t float64) scnP2 {
	return scnP2{a[0] + float64((b[0]-a[0])*t), a[1] + float64((b[1]-a[1])*t)}
}

func scnSurface(b gmath.AABB) float64 {
	s := b.Size()
	x, y, z := float64(s.X), float64(s.Y), float64(s.Z)
	return float64(x*y) + float64(y*z) + float64(z*x)
}

func (a *scnAnalysis) zIssue(z *scnZPair) {
	ei, ej := a.ents[z.i], a.ents[z.j]
	mover, dir := z.j, z.nj
	if scnSurface(ei.AABB) < scnSurface(ej.AABB) {
		mover, dir = z.i, z.normal
	}
	facing := "same"
	if !z.same {
		facing = "opposite"
	}
	where := map[string]any{"a": ei.Name, "b": ej.Name, "ids": []uint32{ei.ID, ej.ID}, "normal": scnVec(z.normal.vec3()),
		"point": scnVec(z.point.vec3()), "area": scnNum(z.area), "facing": facing,
		"a_triangles": z.tI[:min(len(z.tI), scnListMax)], "b_triangles": z.tJ[:min(len(z.tJ), scnListMax)]}
	me := a.ents[mover]
	move := fmt.Sprintf("Move %q at least %s m along %s (entities[%d].position%s", me.Name, scnF(scnZOffset), scnFV(dir.vec3()), mover, a.parentNote(mover))
	if a.idx(me.Parent) < 0 {
		np := me.Transform.Position.Add(dir.vec3().Scale(scnZOffset))
		move += fmt.Sprintf(" %s → %s", scnFV(me.Transform.Position), scnFV(np))
	}
	move += ")"
	var hint string
	if z.same {
		hint = fmt.Sprintf("Faces of %s and %s are coplanar (within %s m) and face the same way over %s m² around %s (normal %s): the depth test cannot order them, so they flicker. %s, or remove the covered face.",
			a.ref(z.i), a.ref(z.j), scnF(scnZDist), scnF(z.area), scnFV(z.point.vec3()), scnFV(z.normal.vec3()), move)
	} else {
		hint = fmt.Sprintf("Faces of %s and %s are coplanar (within %s m) and back to back over %s m² around %s (normal %s), but material %q is double-sided (cull \"none\" in %s), so both are drawn and flicker. Set \"cull\": \"back\" there, or %s.",
			a.ref(z.i), a.ref(z.j), scnF(scnZDist), scnF(z.area), scnFV(z.point.vec3()), scnFV(z.normal.vec3()), z.twoMat,
			path.Join(a.assets, "materials", z.twoMat+".mat.json"), strings.ToLower(move[:1])+move[1:])
	}
	a.add(Warning, scnZFight, z.pairs, where, hint)
}

// ---------------------------------------------------------------------------------
// Metrics.

type scnEntityPixels struct {
	ID     uint32  `json:"id"`
	Name   string  `json:"name"`
	Pixels int     `json:"pixels"`
	BBox   *[4]int `json:"bbox,omitempty"` // x0, y0, x1, y1 (inclusive) at the analysis resolution
}

func (a *scnAnalysis) metrics(mt map[string]any) {
	cam := a.s.Camera
	cm := map[string]any{"position": scnVec(cam.Position), "look_at": scnVec(cam.Target), "near": scnNum(float64(cam.Near)), "far": scnNum(float64(cam.Far))}
	if cam.Ortho {
		cm["type"] = "orthographic"
		cm["size"] = scnNum(float64(cam.Size))
	} else {
		cm["type"] = "perspective"
		cm["fov_deg"] = scnNum(float64(cam.FovDeg))
	}
	l := a.s.Light
	visible, important := 0, 0
	var list []scnEntityPixels
	var imp []scnEntityPixels
	for k, e := range a.ents {
		if a.pixels[k] > 0 {
			visible++
		}
		ep := scnEntityPixels{ID: e.ID, Name: e.Name, Pixels: a.pixels[k]}
		if a.pixels[k] > 0 {
			b := a.bbox[k]
			ep.BBox = &b
		}
		if e.HasTag(scnImportant) {
			important++
			imp = append(imp, scnEntityPixels{ID: e.ID, Name: e.Name, Pixels: a.pixels[k]})
		}
		if a.drawn[k] {
			list = append(list, ep)
		}
	}
	if len(list) > scnEntityMax {
		mt["entity_pixels_omitted"] = len(list) - scnEntityMax
		list = list[:scnEntityMax]
	}
	if list == nil {
		list = []scnEntityPixels{}
	}
	var bounds any
	if b := a.allBox(); !b.IsEmpty() {
		bounds = scnBox(b)
	}
	mt["entities"] = len(a.ents)
	mt["drawn_entities"] = a.drawnCount
	mt["visible_entities"] = visible
	mt["important_entities"] = important
	if len(imp) > 0 {
		mt["important"] = imp[:min(len(imp), scnEntityMax)]
	}
	mt["triangles"] = a.triangles
	mt["bounds"] = bounds
	mt["project_bounds"] = scnBox(a.bounds)
	mt["camera"] = cm
	mt["light"] = map[string]any{"direction": scnVec(l.Dir), "color": scnColor(l.Color), "ambient": scnColor(l.Ambient)}
	mt["background"] = gfx.FormatColor(a.s.Background | 0xff000000)
	mt["resolution"] = []int{a.w, a.h}
	mt["background_ratio"] = scnRound(float64(a.w*a.h-a.entityPixels) / float64(a.w*a.h))
	mt["mean_luminance"] = scnRound(a.meanLuma())
	mt["entity_pixels"] = list
	mt["lit_parts"] = a.litParts
	mt["unlit_parts"] = a.unlitParts
	mt["overlap_pairs"] = len(a.overlaps)
	mt["zfight_pairs"] = len(a.zfights)
	mt["zfight_tests"] = a.zTests
	mt["zfight_truncated"] = a.zTrunc
}

// allBox is the union of every finite entity AABB.
func (a *scnAnalysis) allBox() gmath.AABB {
	b := gmath.EmptyAABB()
	for _, e := range a.ents {
		if scnFiniteBox(e.AABB) {
			b = b.Union(e.AABB)
		}
	}
	return b
}

// ---------------------------------------------------------------------------------
// Number formatting.

func scnRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	r := math.Round(x*1e4) / 1e4
	if r == 0 {
		return 0
	}
	return r
}

// scnNum returns x rounded to 4 decimals, or a string for non-finite values (JSON has
// no NaN or infinity).
func scnNum(x float64) any {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "+Inf"
	case math.IsInf(x, -1):
		return "-Inf"
	}
	return scnRound(x)
}

func scnVec(v gmath.Vec3) any {
	if !v.IsFinite() {
		return []any{scnNum(float64(v.X)), scnNum(float64(v.Y)), scnNum(float64(v.Z))}
	}
	return gmath.V3(float32(scnRound(float64(v.X))), float32(scnRound(float64(v.Y))), float32(scnRound(float64(v.Z))))
}

func scnBox(b gmath.AABB) any { return []any{scnVec(b.Min), scnVec(b.Max)} }

func scnF(x float64) string {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return fmt.Sprint(scnNum(x))
	}
	return strconv.FormatFloat(scnRound(x), 'f', -1, 64)
}

func scnFV(v gmath.Vec3) string {
	return "[" + scnF(float64(v.X)) + ", " + scnF(float64(v.Y)) + ", " + scnF(float64(v.Z)) + "]"
}

func scnFBox(b gmath.AABB) string { return "[" + scnFV(b.Min) + ", " + scnFV(b.Max) + "]" }

// ---------------------------------------------------------------------------------
// Sheets.

// scnSize returns the size of a rendered view, framed 4:3 like the console panel:
// summary tiles default to 317×238 (two columns and padding fill 640 pixels), single
// views to 640×480. opt.Width and opt.Height override both; widths are clamped to
// [64, 317] for tiles and [64, 640] for views, heights to [48, 640]. A width given alone
// is clamped first and the height follows it at 3/4, rounded, so the view stays 4:3.
func scnSize(opt Options, tile bool) (int, int) {
	w, h, maxW := 640, 480, 640
	if tile {
		w, h, maxW = 317, 238, (640-3*scnPad)/2
	}
	if opt.Width > 0 {
		w, h = min(max(opt.Width, 64), maxW), opt.Height
		if h <= 0 {
			h = (w*3 + 2) / 4
		}
	} else if opt.Height > 0 {
		h = opt.Height
	}
	return min(max(w, 64), maxW), min(max(h, 48), 640)
}

func (a *scnAnalysis) sheet(kind string, opt Options) (*gfx.Image, error) {
	switch kind {
	case "summary":
		w, h := scnSize(opt, true)
		cam, err := a.cameraView(w, h)
		if err != nil {
			return nil, err
		}
		top, err := a.topView(w, h)
		if err != nil {
			return nil, err
		}
		ids, fb, err := a.idsView(w, h)
		if err != nil {
			return nil, err
		}
		legend := a.legend(fb, w, (h-4)/12, 2, h)
		return sheet.Grid([]*gfx.Image{cam, top, ids, legend}, []string{"camera", "top", "ids", ""}, 2, scnPad), nil
	case "camera":
		return a.cameraView(scnSize(opt, false))
	case "top":
		return a.topView(scnSize(opt, false))
	case "ids":
		w, h := scnSize(opt, false)
		ids, fb, err := a.idsView(w, h)
		if err != nil {
			return nil, err
		}
		cols := max(1, w/160)
		n := len(scnLegendIDs(fb))
		rows := min(max((n+cols-1)/cols, 1), scnLegendRows)
		legend := a.legend(fb, w, rows, cols, rows*12+4)
		out := gfx.NewImage(w, h+legend.H)
		out.Blit(ids, 0, 0)
		out.Blit(legend, 0, h)
		return out, nil
	}
	return nil, fmt.Errorf("unknown sheet %q", kind)
}

func (a *scnAnalysis) cameraView(w, h int) (*gfx.Image, error) {
	fb, err := a.ir.RenderScene(a.s, a.s.Camera, w, h, gfx.ModeColor, false, a.issueLines)
	if err != nil {
		return nil, err
	}
	return fb.Image(), nil
}

func (a *scnAnalysis) idsView(w, h int) (*gfx.Image, *gfx.Framebuffer, error) {
	fb, err := a.ir.RenderScene(a.s, a.s.Camera, w, h, gfx.ModeIDs, false, scnCoverAll)
	if err != nil {
		return nil, nil, err
	}
	return fb.Image(), fb, nil
}

// issueLines draws the issue overlays, each over the previous: z-fight outlines
// (magenta), hidden important entities (orange), out-of-bounds entities and overlap
// boxes (red, last, because overlapping boxes often have coplanar faces too).
func (a *scnAnalysis) issueLines(dl *gfx.DrawList, view int) {
	for _, z := range a.zfights {
		for _, poly := range z.outlines {
			for i := range poly {
				dl.AddLine(gfx.DebugLine{A: poly[i].vec3(), B: poly[(i+1)%len(poly)].vec3(), Color: scnColorZFight, View: view})
			}
		}
	}
	for _, k := range a.hidden {
		a.markEntity(dl, view, k, scnColorHidden)
	}
	for _, k := range a.outside {
		a.markEntity(dl, view, k, scnColorIssue)
	}
	for _, p := range a.overlaps {
		scene.AddBoxLines(dl, view, p.box, scnColorIssue)
	}
}

// markEntity draws the AABB of entity k, or a cross at its position when it has none.
func (a *scnAnalysis) markEntity(dl *gfx.DrawList, view, k int, color uint32) {
	e := a.ents[k]
	if scnFiniteBox(e.AABB) {
		scene.AddBoxLines(dl, view, e.AABB, color)
		return
	}
	if p := e.WorldPosition(); p.IsFinite() {
		s := float32(0.25)
		for ax := 0; ax < 3; ax++ {
			d := gmath.Vec3{}.With(ax, s)
			dl.AddLine(gfx.DebugLine{A: p.Sub(d), B: p.Add(d), Color: color, View: view})
		}
	}
}

// topBox frames the top view: every entity AABB and position-only entity, plus the
// scene camera's position and look-at point when they are within three radii of the
// entities (+1 m), so the frustum shows without shrinking the scene.
func (a *scnAnalysis) topBox() gmath.AABB {
	b := a.allBox()
	for _, e := range a.ents {
		if e.AABB.IsEmpty() {
			if p := e.WorldPosition(); p.IsFinite() {
				b = b.Extend(p)
			}
		}
	}
	cam := a.s.Camera
	if b.IsEmpty() {
		b = gmath.AABB{Min: cam.Target.Sub(gmath.V3(5, 5, 5)), Max: cam.Target.Add(gmath.V3(5, 5, 5))}
	}
	c := b.Center()
	r := float32(b.Size().Len()/2) + 1
	for _, p := range [2]gmath.Vec3{cam.Position, cam.Target} {
		if p.IsFinite() && p.Dist(c) <= 3*r {
			b = b.Extend(p)
		}
	}
	return b
}

func (a *scnAnalysis) topView(w, h int) (*gfx.Image, error) {
	aspect := float32(w) / float32(h)
	box := a.topBox()
	cam := scene.FrameOrtho(box, gmath.V3(0, -1, 0), aspect)
	fb, err := a.ir.RenderScene(a.s, cam, w, h, gfx.ModeColor, false, func(dl *gfx.DrawList, view int) {
		a.topLines(dl, view, box, aspect)
		a.issueLines(dl, view)
	})
	if err != nil {
		return nil, err
	}
	img := fb.Image()
	a.topLabels(img, cam, aspect)
	return img, nil
}

// topLines draws the project bounds, every entity box and the scene camera's frustum,
// at aspect (width/height) of the camera view on the same sheet, which is the size of
// the top view itself, so the frustum outlines what that camera view shows.
func (a *scnAnalysis) topLines(dl *gfx.DrawList, view int, box gmath.AABB, aspect float32) {
	y := box.Center().Y
	b := a.bounds
	rect := [4]gmath.Vec3{gmath.V3(b.Min.X, y, b.Min.Z), gmath.V3(b.Max.X, y, b.Min.Z), gmath.V3(b.Max.X, y, b.Max.Z), gmath.V3(b.Min.X, y, b.Max.Z)}
	for i := range rect {
		dl.AddLine(gfx.DebugLine{A: rect[i], B: rect[(i+1)%4], Color: scnColorBounds, View: view})
	}
	for k, e := range a.ents {
		color := uint32(scnColorDynamic)
		if e.Kind == scene.KindStatic {
			color = scnColorStatic
		}
		a.markEntity(dl, view, k, color)
	}
	// Scene camera frustum, out to the farthest corner of the framed box (at most far).
	cam := a.s.Camera
	f := cam.Target.Sub(cam.Position).Normalize()
	if f == (gmath.Vec3{}) || !cam.Position.IsFinite() {
		return
	}
	up := gmath.Up
	if gmath.Abs(f.Dot(up)) > 0.9999 {
		up = gmath.V3(0, 0, -1)
		if gmath.Abs(f.Z) > 0.9999 {
			up = gmath.Up
		}
	}
	r := f.Cross(up).Normalize()
	u := r.Cross(f)
	_, reach := scnBoxDist(cam.Position, box)
	L := min(float32(reach), cam.Far)
	var near, far [4]gmath.Vec3
	signs := [4][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
	for i, sg := range signs {
		if cam.Ortho {
			hh := cam.Size / 2
			off := r.Scale(hh * aspect * sg[0]).Add(u.Scale(hh * sg[1]))
			near[i] = cam.Position.Add(off)
			far[i] = near[i].Add(f.Scale(L))
		} else {
			t := gmath.Tan(gmath.Radians(cam.FovDeg) / 2)
			near[i] = cam.Position
			far[i] = cam.Position.Add(f.Add(r.Scale(t * aspect * sg[0])).Add(u.Scale(t * sg[1])).Scale(L))
		}
	}
	line := func(p, q gmath.Vec3) {
		dl.AddLine(gfx.DebugLine{A: p, B: q, Color: scnColorCamera, View: view})
	}
	for i := 0; i < 4; i++ {
		line(near[i], far[i])
		line(far[i], far[(i+1)%4])
		if cam.Ortho {
			line(near[i], near[(i+1)%4])
		}
	}
	line(cam.Position, cam.Position.Add(f.Scale(L)))
	// A cross on the look_at point.
	s := float32(max(box.Size().X, box.Size().Z, 1) * 0.02)
	for _, d := range [2]gmath.Vec3{gmath.V3(s, 0, 0), gmath.V3(0, 0, s)} {
		line(cam.Target.Sub(d), cam.Target.Add(d))
	}
}

// issueEntities marks the entities named by the issues drawn on the sheets.
func (a *scnAnalysis) issueEntities() []bool {
	m := make([]bool, len(a.ents))
	for _, k := range a.outside {
		m[k] = true
	}
	for _, k := range a.hidden {
		m[k] = true
	}
	for _, p := range a.overlaps {
		m[p.i], m[p.j] = true, true
	}
	for _, z := range a.zfights {
		m[z.i], m[z.j] = true, true
	}
	return m
}

// topLabels writes entity names on the top view: right of the footprint (left when there
// is no room) so the box stays visible, or inside the top-left corner of footprints much
// larger than the label. Entities named by an issue are labeled first, then from the
// smallest footprint to the largest (then by id), so a ground plane never hides the names
// of what stands on it; a label that would overlap one already placed is skipped (at most
// scnLabelMax labels).
func (a *scnAnalysis) topLabels(img *gfx.Image, cam scene.Camera, aspect float32) {
	vp := cam.Proj(aspect).Mul(cam.View())
	type cand struct {
		k              int
		x0, y0, x1, y1 float64 // projected footprint in pixels
		area           float64
	}
	toPx := func(p gmath.Vec3) (float64, float64, bool) {
		c := vp.MulVec4(p.Vec4(1))
		if !(c.W > 0) || !p.IsFinite() {
			return 0, 0, false
		}
		return float64((float32(c.X/c.W*0.5) + 0.5) * float32(img.W)), float64((0.5 - float32(c.Y/c.W*0.5)) * float32(img.H)), true
	}
	var cands []cand
	for k, e := range a.ents {
		c := cand{k: k, x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)}
		pts := []gmath.Vec3{e.WorldPosition()}
		if scnFiniteBox(e.AABB) {
			cs := e.AABB.Corners()
			pts = cs[:]
		}
		ok := true
		for _, p := range pts {
			x, y, in := toPx(p)
			if !in {
				ok = false
				break
			}
			c.x0, c.y0, c.x1, c.y1 = min(c.x0, x), min(c.y0, y), max(c.x1, x), max(c.y1, y)
		}
		if !ok || c.x1 < 0 || c.y1 < 0 || c.x0 >= float64(img.W) || c.y0 >= float64(img.H) {
			continue
		}
		c.area = (c.x1 - c.x0) * (c.y1 - c.y0)
		cands = append(cands, c)
	}
	issue := a.issueEntities()
	sort.SliceStable(cands, func(i, j int) bool {
		if issue[cands[i].k] != issue[cands[j].k] {
			return issue[cands[i].k]
		}
		return cands[i].area < cands[j].area
	})
	var placed [][4]int
	for _, c := range cands {
		if len(placed) >= scnLabelMax {
			break
		}
		text := a.ents[c.k].Name
		if r := []rune(text); len(r) > 14 {
			text = string(r[:13]) + "…"
		}
		tw := sheet.TextWidth(text, 1)
		x, y := c.x1+3, float64((c.y0+c.y1)/2)-4
		if x+float64(tw) > float64(img.W-2) {
			x = c.x0 - 3 - float64(tw)
		}
		if c.x1-c.x0 > float64(3*tw) && c.y1-c.y0 > 36 {
			x, y = max(c.x0, 0)+4, max(c.y0, 0)+4
		}
		// A label at a non-finite place goes to the corner on every architecture (out of
		// range float to int conversions differ between amd64 and arm64).
		if !(x < 1<<62) {
			x = 2
		}
		if !(y < 1<<62) {
			y = 2
		}
		x0 := min(max(int(math.Floor(x)), 2), img.W-tw-2)
		y0 := min(max(int(math.Floor(y)), 2), img.H-10)
		box := [4]int{x0 - 2, y0 - 2, x0 + tw + 1, y0 + 9}
		clash := false
		for _, q := range placed {
			if box[0] <= q[2] && q[0] <= box[2] && box[1] <= q[3] && q[1] <= box[3] {
				clash = true
				break
			}
		}
		if clash {
			continue
		}
		placed = append(placed, box)
		sheet.Label(img, x0, y0, 1, text)
	}
}

// scnLegendIDs returns the entity ids present in fb, sorted.
func scnLegendIDs(fb *gfx.Framebuffer) []uint32 {
	var ids []uint32
	seen := map[uint32]bool{} // lookups only
	for _, id := range fb.ID {
		if id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// legend draws the id legend of fb: a swatch, the id and the name of every entity
// visible in fb, by id, column-major in cols columns of rows entries, in a w×h image.
func (a *scnAnalysis) legend(fb *gfx.Framebuffer, w, rows, cols, h int) *gfx.Image {
	img := gfx.NewImage(w, h)
	img.Fill(sheet.Background)
	ids := scnLegendIDs(fb)
	rows = max(rows, 1)
	if len(ids) == 0 {
		sheet.Text(img, 4, 4, 1, "no entity visible", sheet.LabelFG)
		return img
	}
	capacity := rows * cols
	show := ids
	more := 0
	if len(ids) > capacity {
		show = ids[:capacity-1]
		more = len(ids) - len(show)
	}
	colW := w / cols
	maxChars := max((colW-14)/8, 1)
	for i, id := range show {
		x := 4 + (i/rows)*colW
		y := 3 + (i%rows)*12
		sheet.FillRect(img, x, y, 8, 8, gfx.IDColor(id))
		name := "?"
		if k := a.idx(id); k >= 0 {
			name = a.ents[k].Name
		}
		text := []rune(fmt.Sprintf("%d %s", id, name))
		if len(text) > maxChars {
			text = append(text[:maxChars-1], '…')
		}
		sheet.Text(img, x+11, y, 1, string(text), sheet.LabelFG)
	}
	if more > 0 {
		i := len(show)
		sheet.Text(img, 4+(i/rows)*colW+11, 3+(i%rows)*12, 1, fmt.Sprintf("+%d more", more), sheet.LabelFG)
	}
	return img
}

func scnWritePNG(p string, img *gfx.Image) error {
	var buf bytes.Buffer
	if err := img.EncodePNG(&buf); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, buf.Bytes(), 0o644)
}
