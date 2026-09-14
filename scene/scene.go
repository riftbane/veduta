// Package scene holds the runtime world: entities with hierarchical transforms, models,
// materials, tags, visibility and game state, plus the camera and light. Scenes are
// loaded from compiled asset.Scene descriptions; entity ids are assigned sequentially in
// scene-file order, then in spawn order, and every iteration is in id order.
package scene

import (
	"fmt"
	"strconv"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// Built-in entity kinds. Other kinds are game behaviours registered with
// veduta.RegisterKind.
const (
	KindStatic = "static"
	KindCamera = "camera"
	KindLight  = "light"
)

// Transform is a local transform relative to the parent: v' = T·R·S·v.
type Transform struct {
	Position gmath.Vec3
	Rotation gmath.Quat
	Scale    gmath.Vec3
}

// Identity returns the identity transform.
func Identity() Transform { return Transform{Rotation: gmath.QuatIdent(), Scale: gmath.One3} }

// Matrix returns T·R·S.
func (t Transform) Matrix() gmath.Mat4 { return gmath.TRS(t.Position, t.Rotation, t.Scale) }

// Forward returns the local -Z axis rotated by the transform's rotation.
func (t Transform) Forward() gmath.Vec3 { return t.Rotation.Rotate(gmath.Forward) }

// Entity is one object of the world. Game code may change Transform, Visible, Tags,
// Model, Material, Hitbox and State freely during Update; world matrices and AABBs are
// recomputed by Scene.Update after every tick.
type Entity struct {
	ID        uint32
	Name      string
	Kind      string
	Transform Transform // relative to Parent
	Model     string    // model name, "" for none
	Material  string    // entity material, used by parts without their own
	Tags      []string
	AABB      gmath.AABB // world-space bounds of the hitbox, else of the model; empty without either
	Visible   bool
	State     any    // game state; exported fields appear in the trace as state.<field>
	Parent    uint32 // 0 for none
	// Hitbox, when set, is a box in the entity's local space that replaces the model's
	// bounds as the source of AABB (collisions, Overlapping, no_overlap and the trace's
	// aabb). An entity without a model gets an AABB from its hitbox alone.
	Hitbox *gmath.AABB
	// Layer is the first key of the draw order: lower layers are drawn first. Within a
	// layer, opaque parts are drawn in id order, then blended parts back to front. Drawing
	// order never overrides the depth test: it decides which blended surface covers
	// which, not which opaque surface is in front.
	Layer int

	world gmath.Mat4
	stamp uint32
	dead  bool
}

// World returns the world matrix computed by the last Scene.Update.
func (e *Entity) World() gmath.Mat4 { return e.world }

// WorldPosition returns the world-space origin of the entity.
func (e *Entity) WorldPosition() gmath.Vec3 { return e.world.Translation() }

// HasTag reports whether the entity carries tag t.
func (e *Entity) HasTag(t string) bool {
	for _, x := range e.Tags {
		if x == t {
			return true
		}
	}
	return false
}

// Alive reports whether the entity has not been despawned.
func (e *Entity) Alive() bool { return !e.dead }

// BoundsFunc returns the local bounds of a model by name.
type BoundsFunc func(model string) (gmath.AABB, bool)

// Scene is the world. It is not safe for concurrent use.
type Scene struct {
	Name       string
	Camera     Camera
	Light      gfx.Light
	Background uint32

	// OnEvent, when set, receives the built-in events spawn and despawn with their
	// fields; the engine forwards them into the trace.
	OnEvent func(name string, fields map[string]any)

	entities []*Entity // id order; despawned entities are removed by Flush
	byName   map[string]*Entity
	byID     map[uint32]*Entity
	nextID   uint32
	bounds   BoundsFunc
	stamp    uint32
}

// New returns an empty scene. bounds supplies model bounds for AABBs (nil: no AABBs).
func New(name string, bounds BoundsFunc) *Scene {
	return &Scene{
		Name:       name,
		Camera:     DefaultCamera(),
		Light:      gfx.DefaultLight,
		Background: 0xff202830,
		byName:     map[string]*Entity{},
		byID:       map[uint32]*Entity{},
		nextID:     1,
		bounds:     bounds,
	}
}

// Load builds a scene from a compiled description. Entities get ids 1..n in file order.
// Spawn events are not emitted for them (the engine records a scene_load event).
func Load(src *asset.Scene, bounds BoundsFunc) (*Scene, error) {
	s := New(src.Name, bounds)
	s.Camera = CameraFromAsset(src.Camera)
	s.Light = src.Light
	s.Background = src.Background
	for i := range src.Entities {
		a := &src.Entities[i]
		if _, dup := s.byName[a.Name]; dup {
			return nil, fmt.Errorf("scene %s: duplicate entity %q", src.Name, a.Name)
		}
		e := &Entity{
			ID:   s.nextID,
			Name: a.Name,
			Kind: a.Kind,
			Transform: Transform{
				Position: a.Position,
				Rotation: gmath.QuatEulerDeg(a.RotationDeg),
				Scale:    a.Scale,
			},
			Model:    a.Model,
			Material: a.Material,
			Tags:     append([]string(nil), a.Tags...),
			Visible:  a.Visible,
			Hitbox:   cloneBox(a.Hitbox),
			Layer:    a.Layer,
		}
		s.nextID++
		s.add(e)
	}
	for i := range src.Entities {
		a := &src.Entities[i]
		if a.Parent == "" {
			continue
		}
		p, ok := s.byName[a.Parent]
		if !ok {
			return nil, fmt.Errorf("scene %s: entity %q: unknown parent %q", src.Name, a.Name, a.Parent)
		}
		s.byName[a.Name].Parent = p.ID
	}
	if err := s.checkCycles(); err != nil {
		return nil, fmt.Errorf("scene %s: %w", src.Name, err)
	}
	s.Update()
	return s, nil
}

func (s *Scene) add(e *Entity) {
	s.entities = append(s.entities, e)
	s.byName[e.Name] = e
	s.byID[e.ID] = e
}

func (s *Scene) checkCycles() error {
	for _, e := range s.entities {
		seen := 0
		for p := e.Parent; p != 0; p = s.byID[p].Parent {
			if seen++; seen > len(s.entities) {
				return fmt.Errorf("entity %q: parent cycle", e.Name)
			}
		}
	}
	return nil
}

// Spawn adds a copy of tmpl to the scene with the next id and returns it. An empty name
// becomes "<kind>_<id>"; a name already in use gets "#<id>" appended. Zero scale and
// rotation default to identity. Tags and Hitbox are copied, so changing them on the
// template afterwards does not change the spawned entity.
func (s *Scene) Spawn(tmpl Entity) *Entity {
	e := tmpl
	e.ID = s.nextID
	s.nextID++
	e.dead = false
	e.stamp = 0
	if e.Name == "" {
		e.Name = e.Kind + "_" + strconv.FormatUint(uint64(e.ID), 10)
	}
	if _, dup := s.byName[e.Name]; dup {
		e.Name += "#" + strconv.FormatUint(uint64(e.ID), 10)
	}
	if e.Transform.Scale == (gmath.Vec3{}) {
		e.Transform.Scale = gmath.One3
	}
	if e.Transform.Rotation == (gmath.Quat{}) {
		e.Transform.Rotation = gmath.QuatIdent()
	}
	e.Tags = append([]string(nil), tmpl.Tags...)
	e.Hitbox = cloneBox(tmpl.Hitbox)
	if e.Parent != 0 && s.Get(e.Parent) == nil {
		e.Parent = 0
	}
	p := &e
	s.add(p)
	s.updateEntity(p)
	s.emit("spawn", map[string]any{"id": p.ID, "name": p.Name, "kind": p.Kind})
	return p
}

// Restore replaces all entities with ents (ids and names preserved, in id order) and sets
// the next id to spawn. It is used by snapshots; no events are emitted.
func (s *Scene) Restore(ents []Entity, nextID uint32) error {
	s.entities = nil
	s.byName = map[string]*Entity{}
	s.byID = map[uint32]*Entity{}
	var prev uint32
	for i := range ents {
		e := ents[i]
		if e.ID == 0 || e.ID <= prev || e.ID >= nextID {
			return fmt.Errorf("restore: entity %q has id %d out of order", e.Name, e.ID)
		}
		if _, dup := s.byName[e.Name]; dup {
			return fmt.Errorf("restore: duplicate entity name %q", e.Name)
		}
		prev = e.ID
		e.dead, e.stamp = false, 0
		e.Tags = append([]string(nil), e.Tags...)
		e.Hitbox = cloneBox(e.Hitbox)
		s.add(&e)
	}
	for _, e := range s.entities {
		if e.Parent != 0 && s.byID[e.Parent] == nil {
			return fmt.Errorf("restore: entity %q has unknown parent %d", e.Name, e.Parent)
		}
	}
	if err := s.checkCycles(); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	s.nextID = nextID
	s.Update()
	return nil
}

// Despawn marks e (and its descendants) as dead. They stay reachable until Flush, which
// the engine calls at the end of the tick, so iteration order is never disturbed.
func (s *Scene) Despawn(e *Entity) {
	if e == nil || e.dead {
		return
	}
	e.dead = true
	s.emit("despawn", map[string]any{"id": e.ID, "name": e.Name, "kind": e.Kind})
	for _, c := range s.entities {
		if c.Parent == e.ID {
			s.Despawn(c)
		}
	}
}

// Flush removes despawned entities.
func (s *Scene) Flush() {
	out := s.entities[:0]
	for _, e := range s.entities {
		if e.dead {
			delete(s.byName, e.Name)
			delete(s.byID, e.ID)
			continue
		}
		out = append(out, e)
	}
	for i := len(out); i < len(s.entities); i++ {
		s.entities[i] = nil
	}
	s.entities = out
}

func (s *Scene) emit(name string, fields map[string]any) {
	if s.OnEvent != nil {
		s.OnEvent(name, fields)
	}
}

// Entities returns the entities in id order, including ones despawned this tick (check
// Alive). The slice is owned by the scene: do not modify or keep it across ticks.
func (s *Scene) Entities() []*Entity { return s.entities }

// Len returns the number of live entities.
func (s *Scene) Len() int {
	n := 0
	for _, e := range s.entities {
		if !e.dead {
			n++
		}
	}
	return n
}

// NextID returns the id the next spawned entity will get.
func (s *Scene) NextID() uint32 { return s.nextID }

// Find returns the live entity named name, or nil.
func (s *Scene) Find(name string) *Entity {
	if e := s.byName[name]; e != nil && !e.dead {
		return e
	}
	return nil
}

// Get returns the live entity with id, or nil.
func (s *Scene) Get(id uint32) *Entity {
	if e := s.byID[id]; e != nil && !e.dead {
		return e
	}
	return nil
}

// Tagged returns the live entities carrying tag, in id order.
func (s *Scene) Tagged(tag string) []*Entity {
	var out []*Entity
	for _, e := range s.entities {
		if !e.dead && e.HasTag(tag) {
			out = append(out, e)
		}
	}
	return out
}

// Update recomputes world matrices (parents before children) and world AABBs.
func (s *Scene) Update() {
	s.stamp++
	for _, e := range s.entities {
		s.updateEntity(e)
	}
}

func (s *Scene) updateEntity(e *Entity) {
	if e.stamp == s.stamp && s.stamp != 0 {
		return
	}
	local := e.Transform.Matrix()
	if p := s.byID[e.Parent]; p != nil && e.Parent != 0 {
		s.updateEntity(p)
		e.world = p.world.Mul(local)
	} else {
		e.world = local
	}
	e.stamp = s.stamp
	e.AABB = gmath.EmptyAABB()
	switch {
	case e.Hitbox != nil:
		e.AABB = e.Hitbox.Transform(e.world) // an empty (inverted) hitbox stays empty
	case e.Model != "" && s.bounds != nil:
		if b, ok := s.bounds(e.Model); ok && !b.IsEmpty() {
			e.AABB = b.Transform(e.world)
		}
	}
}

// cloneBox returns a copy of *b, or nil.
func cloneBox(b *gmath.AABB) *gmath.AABB {
	if b == nil {
		return nil
	}
	c := *b
	return &c
}

// Bounds returns the union of all live entity AABBs and positions (entities without an
// AABB contribute their position), or an empty box for an empty scene.
func (s *Scene) Bounds() gmath.AABB {
	b := gmath.EmptyAABB()
	for _, e := range s.entities {
		if e.dead || e.Kind == KindCamera || e.Kind == KindLight {
			continue
		}
		if !e.AABB.IsEmpty() {
			b = b.Union(e.AABB)
		} else {
			b = b.Extend(e.WorldPosition())
		}
	}
	return b
}
