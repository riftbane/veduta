package scene

import (
	"fmt"
	"sort"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// Resources maps asset names to backend handles for drawing.
type Resources struct {
	Models    map[string]*ModelRes
	Materials map[string]*asset.Material // a material without a grid takes its texture's
	Textures  map[string]gfx.TextureID
	Plays     map[string]*asset.Clip // by texture name: the clip it plays when nothing picks a frame

	free []gfx.MeshID // handles of removed models, reused by AddModel
}

// ModelRes is an uploaded model.
type ModelRes struct {
	Mesh  gfx.MeshID
	Model *asset.Model
	LODs  []gfx.MeshID // parallel to Model.LODs; 0 for a level that draws another model
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
	r := &Resources{Models: map[string]*ModelRes{}, Materials: map[string]*asset.Material{}, Textures: map[string]gfx.TextureID{},
		Plays: map[string]*asset.Clip{}}
	for _, name := range sortedKeys(textures) {
		if err := r.AddTexture(b, name, textures[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range sortedKeys(models) {
		if err := r.AddModel(b, name, models[name]); err != nil {
			return nil, err
		}
	}
	for name, m := range materials {
		r.AddMaterial(name, m, textures[m.Texture])
	}
	return r, nil
}

// AddTexture uploads a texture under name and keeps its play clip.
func (r *Resources) AddTexture(b gfx.Backend, name string, t *asset.Texture) error {
	id, err := b.CreateTexture(&t.Data)
	if err != nil {
		return fmt.Errorf("upload texture %s: %w", name, err)
	}
	r.Textures[name] = id
	if c := t.Clip(t.Play); c != nil {
		r.Plays[name] = c
	}
	return nil
}

// AddMaterial adds a material drawn with texture t (nil when it has none). A material
// without a grid of its own cuts t into t's frames.
func (r *Resources) AddMaterial(name string, m *asset.Material, t *asset.Texture) {
	if t != nil && m.Grid == [2]int{} && t.Grid != [2]int{} {
		c := *m
		c.Grid = t.Grid
		m = &c
	}
	r.Materials[name] = m
}

// AddModel uploads a model and its levels of detail under name. A model created at
// runtime (a world chunk's ground) replaces the model of that name, reusing its mesh
// handles and then those of removed models.
func (r *Resources) AddModel(b gfx.Backend, name string, m *asset.Model) error {
	var reuse []gfx.MeshID
	if old := r.Models[name]; old != nil {
		reuse = old.handles()
	}
	upload := func(md *gfx.MeshData) (gfx.MeshID, error) {
		var id gfx.MeshID
		switch {
		case len(reuse) > 0:
			id, reuse = reuse[0], reuse[1:]
		case len(r.free) > 0:
			id, r.free = r.free[len(r.free)-1], r.free[:len(r.free)-1]
		default:
			return b.CreateMesh(md)
		}
		return id, b.UpdateMesh(id, md)
	}
	res := &ModelRes{Model: m}
	var err error
	if res.Mesh, err = upload(&m.Mesh); err != nil {
		return fmt.Errorf("upload model %s: %w", name, err)
	}
	for i := range m.LODs {
		var id gfx.MeshID
		if m.LODs[i].Model == "" {
			if id, err = upload(&m.LODs[i].Mesh); err != nil {
				return fmt.Errorf("upload model %s level %d: %w", name, i+1, err)
			}
		}
		res.LODs = append(res.LODs, id)
	}
	r.free = append(r.free, reuse...)
	r.Models[name] = res
	return nil
}

// handles returns the mesh handles of the model's base mesh and levels.
func (m *ModelRes) handles() []gfx.MeshID {
	out := []gfx.MeshID{m.Mesh}
	for _, id := range m.LODs {
		if id != 0 {
			out = append(out, id)
		}
	}
	return out
}

// Remove forgets the model called name and keeps its mesh handles for the next AddModel.
func (r *Resources) Remove(name string) {
	if m := r.Models[name]; m != nil {
		r.free = append(r.free, m.handles()...)
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
	Stats  *DrawStats // when set, receives what Draw left out
	// Tick and Rate (ticks per second) time the clips textures play when nothing picks
	// their frame.
	Tick uint64
	Rate int
	// Statics are drawn with the entities.
	Statics []Static
}

// A Static is a model drawn like an entity that is not one: it has no id, is in no trace
// and collides with nothing. Tile maps are drawn with statics.
type Static struct {
	Model string
	World gmath.Mat4
	Layer int
}

// DrawStats counts the entities with a model that Draw considered and what it did with
// them.
type DrawStats struct {
	Entities int `json:"entities"` // visible entities with an uploaded model
	Culled   int `json:"culled"`   // drawn bounds wholly outside the camera's view volume
	Distant  int `json:"distant"`  // farther than their model's draw_distance
	Reduced  int `json:"reduced"`  // drawn at a level of detail above 0
}

type pending struct {
	cmd    gfx.DrawCmd
	layer  int
	blend  bool
	dist   float32
	id     uint32
	static int // statics have id 0 and are ordered by their index
	part   int
}

// Draw appends the scene to dl: clear to the background, one view for the camera, one
// command per visible model part, and in collision mode the AABB of every entity as debug
// lines. An entity whose drawn bounds lie wholly outside the view volume is skipped (it
// would add no pixel); a model with levels of detail is drawn at the level its distance
// selects, and not at all beyond its draw_distance (docs/model.md). Parts are ordered by
// entity Layer (lower first); within a layer opaque parts come first in id order, then
// blended parts back to front by the depth of their bounds' center along the camera's
// view axis.
func (s *Scene) Draw(dl *gfx.DrawList, res *Resources, opt DrawOptions) {
	dl.Clear = true
	dl.ClearColor = s.Background
	dl.Mode = opt.Mode
	dl.Light = s.Light
	gv := opt.Camera.GfxView(opt.Width, opt.Height)
	view := dl.AddView(gv)
	fr := newFrustum(gv.Proj.Mul(gv.View))
	lod := opt.Camera.lodMeasure()
	var st DrawStats
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
		st.Entities++
		if !box.IsEmpty() && fr.outside(box) {
			st.Culled++
			continue
		}
		m := mr.Model
		mesh, md, materials := mr.Mesh, &m.Mesh, m.Materials
		if m.DrawDistance > 0 || len(m.LODs) > 0 {
			d := lod.distance(opt.Camera.Position, box)
			if m.DrawDistance > 0 && d > m.DrawDistance {
				st.Distant++
				continue
			}
			level := 0
			for i := range m.LODs {
				if d >= m.LODs[i].Distance {
					level = i + 1
				}
			}
			if level > 0 {
				switch l := &m.LODs[level-1]; {
				case l.Model == "" && level <= len(mr.LODs):
					mesh, md = mr.LODs[level-1], &l.Mesh
					st.Reduced++
				case l.Model != "" && res.Models[l.Model] != nil:
					o := res.Models[l.Model]
					mesh, md, materials = o.Mesh, &o.Model.Mesh, o.Model.Materials
					st.Reduced++
				}
			}
		}
		dist := box.Center().Sub(opt.Camera.Position).Dot(forward)
		cmds = opt.parts(cmds, res, view, e, mesh, md, materials, e.world, pending{layer: e.Layer, dist: dist, id: e.ID})
	}
	for i := range opt.Statics {
		st := &opt.Statics[i]
		mr, ok := res.Models[st.Model]
		if !ok {
			continue
		}
		box := mr.Model.Mesh.Bounds.Transform(st.World)
		if box.IsEmpty() || fr.outside(box) {
			continue
		}
		dist := box.Center().Sub(opt.Camera.Position).Dot(forward)
		cmds = opt.parts(cmds, res, view, nil, mr.Mesh, &mr.Model.Mesh, mr.Model.Materials, st.World, pending{layer: st.Layer, dist: dist, static: i})
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
		if a.static != b.static {
			return a.static < b.static
		}
		return a.part < b.part
	})
	for i := range cmds {
		dl.Add(cmds[i].cmd)
	}
	if opt.Stats != nil {
		*opt.Stats = st
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

// parts appends a command for every part of mesh md (uploaded as mesh) drawn with world
// matrix w by entity e (nil for a static), each a copy of p with its part, blend and command.
func (opt *DrawOptions) parts(cmds []pending, res *Resources, view int, e *Entity, mesh gfx.MeshID, md *gfx.MeshData, materials []string, w gmath.Mat4, p pending) []pending {
	entMat := ""
	if e != nil {
		entMat = e.Material
	}
	for pi, part := range md.Parts {
		if part.Count == 0 {
			continue
		}
		partMat := ""
		if part.Material >= 0 && part.Material < len(materials) {
			partMat = materials[part.Material]
		}
		mat := res.Material(partMat, entMat)
		cmd := gfx.DrawCmd{
			View:   view,
			Mesh:   mesh,
			First:  part.First,
			Count:  part.Count,
			Model:  w,
			Color:  gfx.ColorVec4(mat.Albedo),
			State:  mat.State(),
			Filter: mat.Filter,
			Unlit:  mat.Unlit,
			Cutoff: mat.AlphaCutoff(),
			ID:     p.id,
		}
		if mat.Texture != "" {
			cmd.Texture = res.Textures[mat.Texture]
		}
		if g := mat.Grid; g[0] > 0 && g[1] > 0 {
			cmd.UVScale, cmd.UVOffset = FrameUV(g, opt.frame(res, mat, e))
		}
		p.cmd, p.part, p.blend = cmd, pi, mat.Alpha == "blend"
		cmds = append(cmds, p)
	}
	return cmds
}

// frame returns the frame of mat's grid an entity shows (nil for a static): its own while it
// plays a clip, else the texture's play clip's, else its own.
func (opt *DrawOptions) frame(res *Resources, mat *asset.Material, e *Entity) int {
	if e != nil && e.Anim != "" {
		return e.Frame
	}
	if c := res.Plays[mat.Texture]; c != nil && opt.Rate > 0 {
		f, _ := c.Frame(int(opt.Tick), opt.Rate)
		return f
	}
	if e != nil {
		return e.Frame
	}
	return 0
}

// FrameUV returns the texture coordinate scale and offset that show frame f of a grid of
// frames (columns, rows), counted left to right, top to bottom, from 0 and wrapping
// around, negative frames included.
func FrameUV(grid [2]int, f int) (scale, offset gmath.Vec2) {
	n := grid[0] * grid[1]
	if f %= n; f < 0 {
		f += n
	}
	sx, sy := 1/float32(grid[0]), 1/float32(grid[1])
	return gmath.V2(sx, sy), gmath.V2(float32(f%grid[0])*sx, float32(f/grid[0])*sy)
}

// AddBoxLines appends the 12 edges of box b as debug lines (not depth tested).
func AddBoxLines(dl *gfx.DrawList, view int, b gmath.AABB, color uint32) {
	c := b.Corners()
	edges := [12][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}, {0, 2}, {1, 3}, {4, 6}, {5, 7}, {0, 4}, {1, 5}, {2, 6}, {3, 7}}
	for _, ed := range edges {
		dl.AddLine(gfx.DebugLine{A: c[ed[0]], B: c[ed[1]], Color: color, View: view})
	}
}
