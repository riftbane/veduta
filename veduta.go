package veduta

import (
	"encoding/gob"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/scene"
	"github.com/riftbane/veduta/sim"
	"github.com/riftbane/veduta/sprite"
)

// Version is the engine version.
const Version = "v1.4.0"

// Input is everything the player did during one tick: pressed, held and released keys
// (W3C KeyboardEvent.code names such as KeyW, Space, ArrowLeft), the mouse position and
// buttons, and typed text. It is identical whether it comes from the console or a scenario
// script.
type Input = sim.Input

// Game is implemented by every Veduta game.
type Game interface {
	// Init is called once after the first scene is loaded. Register invariants and the
	// state codec here, look up entities, spawn more.
	Init(ctx *Context) error
	// Update is called exactly once per tick (20 Hz by default), before entity
	// behaviours.
	Update(ctx *Context, in Input)
	// Draw is called once per rendered frame, after the engine has added the scene to
	// dl; use it for the HUD (see Context.HUD). It is skipped when no frame is rendered.
	Draw(ctx *Context, dl *gfx.DrawList)
}

// Behaviour is the per-entity logic of a kind, updated once per tick in entity id order.
// Keep behaviour values stateless: store per-entity state in Entity.State, which the
// trace records and snapshots save.
type Behaviour interface {
	Update(ctx *Context, e *scene.Entity, in Input)
}

// BehaviourFunc adapts a function to Behaviour.
type BehaviourFunc func(ctx *Context, e *scene.Entity, in Input)

// Update calls f.
func (f BehaviourFunc) Update(ctx *Context, e *scene.Entity, in Input) { f(ctx, e, in) }

// StateCodec saves and restores a game's own state (everything outside the scene) for
// snapshots. Games register one in Init with Context.RegisterState.
type StateCodec interface {
	SaveState(enc *gob.Encoder) error
	LoadState(dec *gob.Decoder) error
}

var registry = struct {
	sync.Mutex
	kinds map[string]func(*scene.Entity) Behaviour
}{kinds: map[string]func(*scene.Entity) Behaviour{}}

// RegisterKind associates a scene entity kind with the constructor of its behaviour.
// The constructor runs when an entity of that kind is loaded or spawned; it may
// initialize e.State. Call it from an init function or before Run. It panics on invalid
// or duplicate names and on the built-in kinds static, camera and light.
func RegisterKind(name string, ctor func(*scene.Entity) Behaviour) {
	if err := asset.ValidName(name); err != nil {
		panic("veduta.RegisterKind: " + err.Error())
	}
	if isBuiltinKind(name) {
		panic(fmt.Sprintf("veduta.RegisterKind: %q is a built-in kind", name))
	}
	if ctor == nil {
		panic("veduta.RegisterKind: nil constructor for " + name)
	}
	registry.Lock()
	defer registry.Unlock()
	if _, dup := registry.kinds[name]; dup {
		panic(fmt.Sprintf("veduta.RegisterKind: kind %q registered twice", name))
	}
	registry.kinds[name] = ctor
}

// Kinds returns the registered kinds, sorted.
func Kinds() []string {
	registry.Lock()
	defer registry.Unlock()
	out := make([]string, 0, len(registry.kinds))
	for k := range registry.kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func lookupKind(name string) func(*scene.Entity) Behaviour {
	registry.Lock()
	defer registry.Unlock()
	return registry.kinds[name]
}

func isBuiltinKind(k string) bool {
	return k == scene.KindStatic || k == scene.KindCamera || k == scene.KindLight
}

// Context is the game's access to the engine during Init, Update and Draw.
type Context struct {
	Scene    *scene.Scene
	Tick     uint64   // 0 during Init, then 1, 2, … in Update
	RNG      *sim.RNG // the only allowed source of randomness
	DT       float32  // seconds per tick (1 / tick_rate)
	Headless bool     // true when run by the tool, false in the player
	Project  *asset.Project

	// Width and Height are a frame size in pixels, never zero once Init runs: in Draw, the
	// size of the frame being drawn; in Init and Update (and behaviours), the project's
	// resolution in every mode, so game logic that reads them is deterministic and HUD
	// layout can be computed during Update.
	Width, Height int
	// Font is the built-in 8×8 font and FontTexture its texture, for HUD text.
	Font        *sprite.Font
	FontTexture gfx.TextureID

	eng         *engine
	pointerLock bool
}

// LockPointer records a request to hide the cursor and keep it inside the frame, as mouse
// look would need. No player backend implements pointer lock: the console has no mouse, and
// its framebuffer player accepts the request and does nothing. Headless runs ignore it too.
// It is kept for API compatibility and because scenarios and games may still set it;
// PointerLocked reports the request.
func (c *Context) LockPointer(on bool) { c.pointerLock = on }

// PointerLocked reports the last LockPointer request. The player loop passes it on to the
// platform layer, where no backend acts on it.
func (c *Context) PointerLocked() bool { return c.pointerLock }

// Trace emits a structured game event into the current tick's trace. Field values must
// be JSON-encodable (numbers, strings, bools, vectors, slices, maps, structs).
func (c *Context) Trace(event string, fields map[string]any) { c.eng.emit(event, fields) }

// Invariant registers a named predicate evaluated after every tick when a scenario (or
// the project manifest) lists its name.
func (c *Context) Invariant(name string, pred func() bool) { c.eng.registerInvariant(name, pred) }

// RegisterState registers the codec that saves and restores the game's own state in
// snapshots.
func (c *Context) RegisterState(codec StateCodec) { c.eng.codec = codec }

// Spawn adds an entity to the scene (see scene.Scene.Spawn) and instantiates its
// behaviour when its kind is registered. Entities spawned during a tick are first
// updated on the next tick.
func (c *Context) Spawn(tmpl scene.Entity) *scene.Entity { return c.eng.spawn(tmpl) }

// Despawn removes e (and its children) at the end of the tick.
func (c *Context) Despawn(e *scene.Entity) { c.Scene.Despawn(e) }

// LoadScene replaces the current scene with a freshly loaded one (entity ids restart at
// 1) and emits a scene_load event. Use it to reset a level. It unloads a world.
func (c *Context) LoadScene(name string) error { return c.eng.loadScene(name) }

// LoadWorld replaces the current scene with world name (docs/world.md) streamed around
// the start cell at: the world's camera and persistent entities are placed relative to
// that cell's centre and the chunks around it are loaded. It emits a world_load event.
func (c *Context) LoadWorld(name string, at [2]int32) error { return c.eng.loadWorld(name, at) }

// SetModel adds or replaces a model the game builds at runtime, such as the mesh of a
// voxel chunk after a block changed. Entities name it in their Model like an asset model,
// with the same culling, levels of detail and draw distance; it is uploaded when the next
// frame is rendered. The name must contain ':' (asset names cannot) and must not start
// with "world:". The engine keeps m and re-uploads a name only when it is given another
// *asset.Model, so build a new model instead of changing one already set. Runtime models
// outlive LoadScene and are not saved in snapshots: rebuild them from the game's state.
func (c *Context) SetModel(name string, m *asset.Model) error {
	switch {
	case !strings.Contains(name, ":"):
		return fmt.Errorf("veduta: SetModel %q: a runtime model's name must contain ':' (asset names cannot)", name)
	case strings.HasPrefix(name, "world:"):
		return fmt.Errorf("veduta: SetModel %q: names starting with world: belong to worlds", name)
	case m == nil:
		return fmt.Errorf("veduta: SetModel %q: nil model", name)
	}
	check := func(level string, md *gfx.MeshData) error {
		if len(md.Indices)%3 != 0 {
			return fmt.Errorf("veduta: SetModel %q%s: %d indices is not whole triangles", name, level, len(md.Indices))
		}
		for i, idx := range md.Indices {
			if int(idx) >= len(md.Vertices) {
				return fmt.Errorf("veduta: SetModel %q%s: index %d is %d, but there are %d vertices", name, level, i, idx, len(md.Vertices))
			}
		}
		for i, p := range md.Parts {
			if p.First < 0 || p.Count < 0 || p.Count%3 != 0 || p.First+p.Count > len(md.Indices) {
				return fmt.Errorf("veduta: SetModel %q%s: part %d range [%d, +%d) is outside the %d indices", name, level, i, p.First, p.Count, len(md.Indices))
			}
		}
		return nil
	}
	if err := check("", &m.Mesh); err != nil {
		return err
	}
	for i := range m.LODs {
		l := &m.LODs[i]
		if l.Model == "" && len(l.Mesh.Parts) != len(m.Mesh.Parts) {
			return fmt.Errorf("veduta: SetModel %q level %d: %d parts, the base mesh has %d", name, i+1, len(l.Mesh.Parts), len(m.Mesh.Parts))
		}
		if err := check(fmt.Sprintf(" level %d", i+1), &l.Mesh); err != nil {
			return err
		}
	}
	c.eng.runtime[name] = m
	return nil
}

// RemoveModel forgets a model set with SetModel; entities that still name it draw nothing.
func (c *Context) RemoveModel(name string) { delete(c.eng.runtime, name) }

// Model returns the model set with SetModel under name, or nil.
func (c *Context) Model(name string) *asset.Model { return c.eng.runtime[name] }

// World returns the loaded world, or nil when the game is in a scene. Call its Focus
// every tick with the position the chunks should follow.
func (c *Context) World() *World { return c.eng.world }

// Overlapping returns the live entities whose AABB overlaps e's, in id order, using the
// bounds of the last completed tick.
func (c *Context) Overlapping(e *scene.Entity) []*scene.Entity {
	var out []*scene.Entity
	if e == nil || e.AABB.IsEmpty() {
		return nil
	}
	for _, o := range c.Scene.Entities() {
		if o != e && o.Alive() && !o.AABB.IsEmpty() && o.AABB.Overlaps(e.AABB) {
			out = append(out, o)
		}
	}
	return out
}

// HUD starts a sprite batch covering the frame; call End when done.
func (c *Context) HUD(dl *gfx.DrawList) *sprite.Batch { return sprite.Begin(dl, c.Width, c.Height) }

// Text draws s with the built-in font at pixel (x, y) with an integer scale.
func (c *Context) Text(b *sprite.Batch, x, y float32, scale int, s string, color uint32) float32 {
	return b.Text(c.Font, c.FontTexture, x, y, scale, s, color)
}
