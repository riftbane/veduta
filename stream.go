package veduta

import (
	"sort"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
	"github.com/riftbane/veduta/world"
)

// World is a loaded world (Context.World): the generator and the chunks streamed around
// the focus. Chunks within View chunks of the focus are loaded at the start of every
// tick, before Update; a chunk is unloaded once it is more than View+1 chunks away, so
// walking along a border does not reload it. Structures (sites and places) are spawned
// whole when a chunk their footprint touches loads and despawned when none is loaded.
type World struct {
	Name  string
	Start [2]int32 // the start cell: the camera and the persistent entities are relative to its centre

	gen     *world.Gen
	eng     *engine
	focus   [2]int32
	chunks  map[[2]int32]*loadedChunk
	structs map[string]*loadedStruct
	models  map[string]*asset.Model // ground models of the loaded chunks, by model name
	loaded  [][2]int32              // sorted keys of chunks
}

type loadedChunk struct {
	ids     []uint32
	structs []string
}

type loadedStruct struct {
	ids  []uint32
	refs int
}

// Gen returns the world's generator (biomes, cells, chunks).
func (w *World) Gen() *world.Gen { return w.gen }

// Focus keeps the loaded chunks around pos from the next tick on.
func (w *World) Focus(pos gmath.Vec3) {
	x, z := w.gen.CellOf(pos)
	w.focus = [2]int32{x, z}
}

// FocusCell is Focus with a cell.
func (w *World) FocusCell(cell [2]int32) { w.focus = cell }

// FocusedCell returns the current focus cell.
func (w *World) FocusedCell() [2]int32 { return w.focus }

// CellOf returns the cell holding pos.
func (w *World) CellOf(pos gmath.Vec3) [2]int32 {
	x, z := w.gen.CellOf(pos)
	return [2]int32{x, z}
}

// Loaded returns the loaded chunks, sorted by z then x.
func (w *World) Loaded() [][2]int32 { return w.loaded }

// bounds is the scene's BoundsFunc: a chunk ground's model, else the library's.
func (w *World) bounds(model string) (gmath.AABB, bool) {
	if m, ok := w.models[model]; ok {
		return m.Mesh.Bounds, true
	}
	return w.eng.assets.ModelBounds(model)
}

// worldBounds returns the within_bounds box of a world: its extent along x and z, the
// project's range along y.
func (w *World) worldBounds() gmath.AABB {
	wd := w.gen.W
	reach := float32(float64(wd.Extent) * float64(wd.Chunk) * float64(wd.Cell))
	b := w.eng.project.Bounds
	return gmath.AABB{Min: gmath.V3(-reach, b.Min.Y, -reach), Max: gmath.V3(reach, b.Max.Y, reach)}
}

// newWorld builds the streaming state of world wd starting at cell at.
func newWorld(e *engine, wd *asset.World, at [2]int32) *World {
	return &World{
		Name: wd.Name, Start: at, gen: world.New(wd, e.assets.Prefab), eng: e, focus: at,
		chunks: map[[2]int32]*loadedChunk{}, structs: map[string]*loadedStruct{}, models: map[string]*asset.Model{},
	}
}

// startScene builds the scene of the world: camera, light, background and the
// persistent entities, all relative to the centre of the start cell.
func (w *World) startScene() (*scene.Scene, error) {
	wd := w.gen.W
	off := w.gen.Center(w.Start[0], w.Start[1])
	src := &asset.Scene{Name: wd.Name, Camera: wd.Camera, Light: wd.Light, Background: wd.Background}
	src.Camera.Position = src.Camera.Position.Add(off)
	src.Camera.LookAt = src.Camera.LookAt.Add(off)
	for _, e := range wd.Entities {
		if e.Parent == "" {
			e.Position = e.Position.Add(off)
		}
		src.Entities = append(src.Entities, e)
	}
	return scene.Load(src, w.bounds)
}

// stream loads and unloads chunks around the focus and returns whether anything changed.
func (w *World) stream() bool {
	e := w.eng
	s := e.ctx.Scene
	wd := w.gen.W
	fx, fz := w.gen.ChunkOf(w.focus[0], w.focus[1])
	view := int32(wd.View)
	extent := int32(wd.Extent)
	changed := false
	onEvent := s.OnEvent
	s.OnEvent = nil // streamed spawns and despawns are one chunk event each, not hundreds
	defer func() { s.OnEvent = onEvent }()
	// Unload chunks that fell out of the hysteresis band.
	for _, key := range w.loaded {
		if max(abs32(key[0]-fx), abs32(key[1]-fz)) <= view+1 {
			continue
		}
		c := w.chunks[key]
		for _, id := range c.ids {
			s.Despawn(s.Get(id))
		}
		for _, k := range c.structs {
			st := w.structs[k]
			st.refs--
			if st.refs == 0 {
				for _, id := range st.ids {
					s.Despawn(s.Get(id))
				}
				delete(w.structs, k)
			}
		}
		delete(w.chunks, key)
		delete(w.models, world.GroundName(key[0], key[1]))
		e.emit("chunk_unload", map[string]any{"x": key[0], "z": key[1]})
		changed = true
	}
	// Load the window.
	for cz := fz - view; cz <= fz+view; cz++ {
		for cx := fx - view; cx <= fx+view; cx++ {
			key := [2]int32{cx, cz}
			if cx < -extent || cx >= extent || cz < -extent || cz >= extent || w.chunks[key] != nil {
				continue
			}
			w.loadChunk(key)
			changed = true
		}
	}
	if changed {
		w.loaded = w.loaded[:0]
		for key := range w.chunks {
			w.loaded = append(w.loaded, key)
		}
		sort.Slice(w.loaded, func(i, j int) bool {
			if w.loaded[i][1] != w.loaded[j][1] {
				return w.loaded[i][1] < w.loaded[j][1]
			}
			return w.loaded[i][0] < w.loaded[j][0]
		})
	}
	return changed
}

func (w *World) loadChunk(key [2]int32) {
	e := w.eng
	c := w.gen.Chunk(key[0], key[1])
	w.models[c.Ground.Name] = c.Ground
	lc := &loadedChunk{}
	first := e.ctx.Scene.NextID()
	ground := w.gen.GroundEntity(c)
	lc.ids = append(lc.ids, w.spawn([]asset.Entity{ground})...)
	for i := range c.Scatter {
		lc.ids = append(lc.ids, w.spawn(w.gen.Instantiate(&c.Scatter[i]))...)
	}
	for _, st := range w.gen.Structures(key[0], key[1]) {
		ls := w.structs[st.Key]
		if ls == nil {
			ls = &loadedStruct{ids: w.spawn(w.gen.Instantiate(&st))}
			w.structs[st.Key] = ls
		}
		ls.refs++
		lc.structs = append(lc.structs, st.Key)
	}
	w.chunks[key] = lc
	n := len(lc.ids)
	for _, k := range lc.structs {
		n += len(w.structs[k].ids)
	}
	e.emit("chunk_load", map[string]any{"x": key[0], "z": key[1], "entities": n, "first_id": first})
}

// spawn adds entities in order, resolving parents by name, and returns their ids.
func (w *World) spawn(ents []asset.Entity) []uint32 {
	e := w.eng
	ids := make([]uint32, len(ents))
	byName := make(map[string]uint32, len(ents)) // lookup only
	for i, a := range ents {
		ent := e.spawn(scene.Entity{
			Name: a.Name, Kind: a.Kind,
			Transform: scene.Transform{Position: a.Position, Rotation: gmath.QuatEulerDeg(a.RotationDeg), Scale: a.Scale},
			Model:     a.Model, Material: a.Material, Tags: a.Tags, Visible: a.Visible, Hitbox: a.Hitbox, Layer: a.Layer,
		})
		ids[i] = ent.ID
		byName[a.Name] = ent.ID
	}
	for i, a := range ents {
		if a.Parent != "" {
			if id, ok := byName[a.Parent]; ok {
				e.ctx.Scene.Get(ids[i]).Parent = id
			}
		}
	}
	return ids
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// snapshot state of the streamer.
type snapChunk struct {
	X, Z    int32
	IDs     []uint32
	Structs []string
}

type snapStruct struct {
	Key  string
	IDs  []uint32
	Refs int
}

func (w *World) snapshot() ([]snapChunk, []snapStruct) {
	var chunks []snapChunk
	for _, key := range w.loaded {
		c := w.chunks[key]
		chunks = append(chunks, snapChunk{X: key[0], Z: key[1], IDs: c.ids, Structs: c.structs})
	}
	keys := make([]string, 0, len(w.structs))
	for k := range w.structs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var structs []snapStruct
	for _, k := range keys {
		structs = append(structs, snapStruct{Key: k, IDs: w.structs[k].ids, Refs: w.structs[k].refs})
	}
	return chunks, structs
}

// restore rebuilds the streaming maps (and the ground models) from a snapshot.
func (w *World) restore(chunks []snapChunk, structs []snapStruct) {
	for _, c := range chunks {
		key := [2]int32{c.X, c.Z}
		w.chunks[key] = &loadedChunk{ids: c.IDs, structs: c.Structs}
		w.models[world.GroundName(c.X, c.Z)] = w.gen.Chunk(c.X, c.Z).Ground
		w.loaded = append(w.loaded, key)
	}
	for _, s := range structs {
		w.structs[s.Key] = &loadedStruct{ids: s.IDs, refs: s.Refs}
	}
}
