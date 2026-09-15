package inspect

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/internal/sheet"
	"github.com/riftbane/veduta/scene"
)

// Prefab issue codes.
const (
	pfMissing   = "PREFAB_MISSING_ASSET"
	pfFootprint = "PREFAB_FOOTPRINT_SMALL"
	pfOverlap   = "PREFAB_OVERLAP"
	pfKind      = "PREFAB_KIND_UNCHECKED"
)

// PrefabCodes are the issue codes of Prefab.
var PrefabCodes = []string{pfMissing, pfFootprint, pfOverlap, pfKind}

// PrefabSheets are the sheets Prefab can write.
var PrefabSheets = []string{"summary"}

// Prefab inspects a prefab: missing models and materials, entities reaching outside the
// footprint, entities overlapping each other, and kinds the tool cannot check; the
// summary sheet shows it from an isometric and a top view with its footprint.
func Prefab(ir *Renderer, name string, opt Options) (*Report, error) {
	if ir == nil || ir.Lib == nil {
		return nil, errors.New("inspect prefab: no library")
	}
	for _, s := range opt.Sheets {
		if s != "all" && s != "none" && !slices.Contains(PrefabSheets, s) {
			return nil, fmt.Errorf("inspect prefab %s: unknown sheet %q (valid: %s, all, none)", name, s, strings.Join(PrefabSheets, ", "))
		}
	}
	if opt.Focus != "" && !slices.Contains(PrefabCodes, opt.Focus) {
		return nil, fmt.Errorf("inspect prefab %s: unknown focus %q (valid: %s)", name, opt.Focus, strings.Join(PrefabCodes, ", "))
	}
	p := ir.Lib.Prefabs[name]
	if p == nil {
		return nil, fmt.Errorf("inspect prefab: unknown prefab %q (have: %s)", name, strings.Join(asset.Names(ir.Lib.Prefabs), ", "))
	}
	rep := &Report{Subject: "prefab:" + name, Metrics: map[string]any{}}
	src := &asset.Scene{Name: name, Camera: asset.Camera{FovDeg: 40, Near: 0.1, Far: 200, Position: gmath.V3(0, 5, 10)}, Light: gfx.DefaultLight, Background: 0xff2a2f38, Entities: p.Entities}
	s, err := scene.Load(src, ir.Lib.ModelBounds)
	if err != nil {
		return nil, fmt.Errorf("inspect prefab %s: %w", name, err)
	}
	foot := gmath.AABB{Min: gmath.Zero3, Max: gmath.V3(p.Footprint.X, 0, p.Footprint.Y)}
	tris := 0
	for i, e := range p.Entities {
		where := map[string]any{"entity": e.Name, "index": i}
		if e.Model != "" {
			if m := ir.Lib.Models[e.Model]; m == nil {
				rep.Add(Error, pfMissing, 1, where, fmt.Sprintf("model %q does not exist: create assets/models/%s.model.json or fix entities[%d].model", e.Model, e.Model, i))
			} else {
				tris += len(m.Mesh.Indices) / 3
			}
		}
		if e.Material != "" && ir.Lib.Materials[e.Material] == nil {
			rep.Add(Error, pfMissing, 1, where, fmt.Sprintf("material %q does not exist: create assets/materials/%s.mat.json or fix entities[%d].material", e.Material, e.Material, i))
		}
		if !slices.Contains(asset.BuiltinKinds, e.Kind) {
			rep.Add(Info, pfKind, 1, where, fmt.Sprintf("kind %q is checked when the game loads the world (the tool has no game)", e.Kind))
		}
	}
	ents := s.Entities()
	for i, e := range ents {
		if e.AABB.IsEmpty() {
			continue
		}
		over := gmath.V3(max(0, foot.Min.X-e.AABB.Min.X, e.AABB.Max.X-foot.Max.X), 0, max(0, foot.Min.Z-e.AABB.Min.Z, e.AABB.Max.Z-foot.Max.Z))
		if over.X > 0.01 || over.Z > 0.01 {
			rep.Add(Warning, pfFootprint, 1, map[string]any{"entity": e.Name, "index": i, "over_x": over.X, "over_z": over.Z},
				fmt.Sprintf("%q reaches %.2f m past the footprint along x and %.2f m along z: enlarge footprint [%v, %v] or move entities[%d].position", e.Name, over.X, over.Z, p.Footprint.X, p.Footprint.Y, i))
		}
		for j := i + 1; j < len(ents); j++ {
			o := ents[j]
			if o.AABB.IsEmpty() || e.Parent == o.ID || o.Parent == e.ID || !e.AABB.Overlaps(o.AABB) {
				continue
			}
			rep.Add(Warning, pfOverlap, 1, map[string]any{"a": e.Name, "b": o.Name},
				fmt.Sprintf("%q and %q overlap; move one, or parent one to the other if that is intended", e.Name, o.Name))
		}
	}
	rep.Metrics["entities"] = len(p.Entities)
	rep.Metrics["triangles"] = tris
	rep.Metrics["footprint"] = [2]float32{p.Footprint.X, p.Footprint.Y}
	rep.Metrics["tags"] = p.Tags
	if opt.wants("summary", true) {
		w, h := scnSize(opt, true)
		b := s.Bounds().Union(foot)
		box := func(dl *gfx.DrawList, view int) { scene.AddBoxLines(dl, view, foot, 0xffff8c1a) }
		iso, err := ir.RenderScene(s, scene.FramePerspective(b, gmath.V3(-1, -1, -1), 40, float32(w)/float32(h)), w, h, gfx.ModeColor, false, box)
		if err != nil {
			return nil, fmt.Errorf("inspect prefab %s: %w", name, err)
		}
		top, err := ir.RenderScene(s, scene.FrameOrtho(b, gmath.V3(0, -1, 0), float32(w)/float32(h)), w, h, gfx.ModeColor, false, box)
		if err != nil {
			return nil, fmt.Errorf("inspect prefab %s: %w", name, err)
		}
		img := sheet.Grid([]*gfx.Image{iso.Image(), top.Image()}, []string{"iso", "top (footprint in orange)"}, 2, scnPad)
		pth := opt.sheetPath(rep.Subject, "summary")
		if err := scnWritePNG(pth, img); err != nil {
			return nil, fmt.Errorf("inspect prefab %s: sheet: %w", name, err)
		}
		rep.Sheets = append(rep.Sheets, pth)
	}
	rep.Finish(opt.Focus)
	return rep, nil
}
