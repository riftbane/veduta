package scene

import (
	"fmt"
	"sort"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// Resources maps asset names to backend handles for drawing.
type Resources struct {
	Models    map[string]*ModelRes
	Materials map[string]*asset.Material
	Textures  map[string]gfx.TextureID

	free []gfx.MeshID // handles of removed models, reused by AddModel
}

// ModelRes is an uploaded model.
type ModelRes struct {
	Mesh  gfx.MeshID
	Model *asset.Model
}

// Bounds returns the local bounds of a model, for use as a Scene BoundsFunc.
func (r *Resources) Bounds(model string) (gmath.AABB, bool) {
	if m, ok := r.Models[model]; ok {
		return m.Model.Mesh.Bounds, true
	}
	return gmath.AABB{}, false
}

// Upload creates backend meshes and textures for every asset, in name order so handles
// are deterministic.
func Upload(b gfx.Backend, models map[string]*asset.Model, textures map[string]*asset.Texture, materials map[string]*asset.Material) (*Resources, error) {
	r := &Resources{Models: map[string]*ModelRes{}, Materials: map[string]*asset.Material{}, Textures: map[string]gfx.TextureID{}}
	for _, name := range sortedKeys(textures) {
		id, err := b.CreateTexture(&textures[name].Data)
		if err != nil {
			return nil, fmt.Errorf("upload texture %s: %w", name, err)
		}
		r.Textures[name] = id
	}
	for _, name := range sortedKeys(models) {
		m := models[name]
		id, err := b.CreateMesh(&m.Mesh)
		if err != nil {
			return nil, fmt.Errorf("upload model %s: %w", name, err)
		}
		r.Models[name] = &ModelRes{Mesh: id, Model: m}
	}
	for name, m := range materials {
		r.Materials[name] = m
	}
	return r, nil
}

// AddModel uploads a model created at runtime (a world chunk's ground) under name,
// replacing a model of that name and reusing the handle of a removed model when one is
// free.
func (r *Resources) AddModel(b gfx.Backend, name string, m *asset.Model) error {
	if old := r.Models[name]; old != nil {
		if err := b.UpdateMesh(old.Mesh, &m.Mesh); err != nil {
			return fmt.Errorf("upload model %s: %w", name, err)
		}
		old.Model = m
		return nil
	}
	if n := len(r.free); n > 0 {
		id := r.free[n-1]
		if err := b.UpdateMesh(id, &m.Mesh); err != nil {
			return fmt.Errorf("upload model %s: %w", name, err)
		}
		r.free = r.free[:n-1]
		r.Models[name] = &ModelRes{Mesh: id, Model: m}
		return nil
	}
	id, err := b.CreateMesh(&m.Mesh)
	if err != nil {
		return fmt.Errorf("upload model %s: %w", name, err)
	}
	r.Models[name] = &ModelRes{Mesh: id, Model: m}
	return nil
}

// Remove forgets the model called name and keeps its mesh handle for the next AddModel.
func (r *Resources) Remove(name string) {
	if m := r.Models[name]; m != nil {
		r.free = append(r.free, m.Mesh)
		delete(r.Models, name)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Material resolves the material of a model part: the part's own, else the entity's,
// else asset.DefaultMaterial. Unknown names also fall back to the default (inspection
// reports them).
func (r *Resources) Material(part, entity string) *asset.Material {
	for _, n := range [2]string{part, entity} {
		if n == "" {
			continue
		}
		if m, ok := r.Materials[n]; ok {
			return m
		}
		break
	}
	return &asset.DefaultMaterial
}

// DrawOptions controls Scene.Draw.
type DrawOptions struct {
	Camera Camera
	Width  int
	Height int
	Mode   gfx.RenderMode
}

type pending struct {
	cmd   gfx.DrawCmd
	layer int
	blend bool
	dist  float32
	id    uint32
	part  int
}

// Draw appends the scene to dl: clear to the background, one view for the camera, one
// command per visible model part, and in collision mode the AABB of every entity as debug
// lines. Parts are ordered by entity Layer (lower first); within a layer opaque parts come
// first in id order, then blended parts back to front by the depth of their bounds' center
// along the camera's view axis.
func (s *Scene) Draw(dl *gfx.DrawList, res *Resources, opt DrawOptions) {
	dl.Clear = true
	dl.ClearColor = s.Background
	dl.Mode = opt.Mode
	dl.Light = s.Light
	view := dl.AddView(opt.Camera.GfxView(opt.Width, opt.Height))
	// Blended parts are sorted by depth along the view axis, not by distance from the eye:
	// under an orthographic camera every point of a plane facing it is equally deep, and a
	// sprite off to the side is no farther away than one in the middle.
	forward := opt.Camera.Target.Sub(opt.Camera.Position).Normalize()
	var cmds []pending
	for _, e := range s.entities {
		if e.dead || !e.Visible || e.Model == "" {
			continue
		}
		mr, ok := res.Models[e.Model]
		if !ok {
			continue
		}
		box := e.AABB
		if e.Hitbox != nil { // the drawing's bounds, not the collision box
			box = mr.Model.Mesh.Bounds.Transform(e.world)
		}
		dist := box.Center().Sub(opt.Camera.Position).Dot(forward)
		for pi, part := range mr.Model.Mesh.Parts {
			if part.Count == 0 {
				continue
			}
			partMat := ""
			if part.Material >= 0 && part.Material < len(mr.Model.Materials) {
				partMat = mr.Model.Materials[part.Material]
			}
			mat := res.Material(partMat, e.Material)
			cmd := gfx.DrawCmd{
				View:   view,
				Mesh:   mr.Mesh,
				First:  part.First,
				Count:  part.Count,
				Model:  e.world,
				Color:  gfx.ColorVec4(mat.Albedo),
				State:  mat.State(),
				Filter: mat.Filter,
				Unlit:  mat.Unlit,
				Cutoff: mat.AlphaCutoff(),
				ID:     e.ID,
			}
			if mat.Texture != "" {
				cmd.Texture = res.Textures[mat.Texture]
			}
			cmds = append(cmds, pending{cmd: cmd, layer: e.Layer, blend: mat.Alpha == "blend", dist: dist, id: e.ID, part: pi})
		}
	}
	sort.SliceStable(cmds, func(i, j int) bool {
		a, b := &cmds[i], &cmds[j]
		if a.layer != b.layer {
			return a.layer < b.layer
		}
		if a.blend != b.blend {
			return !a.blend
		}
		if a.blend && a.dist != b.dist {
			return a.dist > b.dist
		}
		if a.id != b.id {
			return a.id < b.id
		}
		return a.part < b.part
	})
	for i := range cmds {
		dl.Add(cmds[i].cmd)
	}
	if opt.Mode == gfx.ModeCollision {
		for _, e := range s.entities {
			if e.dead || e.AABB.IsEmpty() {
				continue
			}
			color := uint32(0xff40ff40)
			if e.Kind == KindStatic {
				color = 0xff8090a0
			}
			AddBoxLines(dl, view, e.AABB, color)
		}
	}
}

// AddBoxLines appends the 12 edges of box b as debug lines (not depth tested).
func AddBoxLines(dl *gfx.DrawList, view int, b gmath.AABB, color uint32) {
	c := b.Corners()
	edges := [12][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}, {0, 2}, {1, 3}, {4, 6}, {5, 7}, {0, 4}, {1, 5}, {2, 6}, {3, 7}}
	for _, ed := range edges {
		dl.AddLine(gfx.DebugLine{A: c[ed[0]], B: c[ed[1]], Color: color, View: view})
	}
}
