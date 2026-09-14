package veduta

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gfx/soft"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
	"github.com/riftbane/veduta/sim"
	"github.com/riftbane/veduta/sprite"
)

// Assets is every compiled asset of a project, by name.
type Assets = asset.Library

// engine runs one game deterministically. It is used by both the headless commands and
// the player loop.
type engine struct {
	game    Game
	project *asset.Project
	assets  *Assets
	ctx     Context

	behaviours map[uint32]Behaviour
	rec        *sim.Recorder
	monitor    *sim.Monitor
	contacts   *sim.Contacts
	custom     map[string]func() bool
	codec      StateCodec
	invSpecs   []string
	tick       uint64
	seed       uint64
	recording  bool

	renderer *soft.Renderer
	res      *scene.Resources
	dl       gfx.DrawList
	fb       *gfx.Framebuffer
	extra    func(dl *gfx.DrawList, view int)
}

func newEngine(g Game, p *asset.Project, a *Assets) *engine {
	e := &engine{game: g, project: p, assets: a, custom: map[string]func() bool{}}
	e.ctx = Context{
		DT:      1 / float32(p.TickRate),
		Project: p,
		Font:    sprite.DefaultFont(),
		eng:     e,
	}
	return e
}

// runOptions configures a simulation run.
type runOptions struct {
	Scene      string
	Seed       uint64
	Invariants []string  // invariant specs; nil means the project's list
	Trace      io.Writer // receives trace.jsonl (nil: hash only)
	Headless   bool
}

// start loads the scene, calls Init and records tick 0.
func (e *engine) start(opt runOptions) error {
	if err := e.prepare(opt); err != nil {
		return err
	}
	e.recording = true
	return e.endTick()
}

// prepare loads the scene and calls Init without recording anything.
func (e *engine) prepare(opt runOptions) error {
	e.ctx.RNG = sim.NewRNG(opt.Seed)
	e.seed = opt.Seed
	e.ctx.Headless = opt.Headless
	e.ctx.Tick = 0
	e.tick = 0
	e.rec = sim.NewRecorder(opt.Trace)
	e.contacts = sim.NewContacts()
	e.invSpecs = opt.Invariants
	if e.invSpecs == nil {
		e.invSpecs = e.project.Invariants
	}
	if err := e.loadScene(opt.Scene); err != nil {
		return err
	}
	e.tickSize()
	if err := e.game.Init(&e.ctx); err != nil {
		return fmt.Errorf("game Init: %w", err)
	}
	var list []sim.Invariant
	for _, spec := range e.invSpecs {
		inv, err := sim.ParseInvariant(spec, e.project.Bounds, e.custom)
		if err != nil {
			return err
		}
		list = append(list, inv)
	}
	e.monitor = sim.NewMonitor(list)
	e.ctx.Scene.Flush()
	e.ctx.Scene.Update()
	e.contacts.Step(e.ctx.Scene) // overlaps present at load are not collisions
	return nil
}

func (e *engine) loadScene(name string) error {
	src, ok := e.assets.Scenes[name]
	if !ok {
		return fmt.Errorf("unknown scene %q", name)
	}
	s, err := scene.Load(src, e.assets.ModelBounds)
	if err != nil {
		return err
	}
	s.OnEvent = e.emit
	e.ctx.Scene = s
	e.behaviours = map[uint32]Behaviour{}
	for _, ent := range s.Entities() {
		if err := e.attach(ent); err != nil {
			return err
		}
	}
	if e.contacts != nil {
		e.contacts = sim.NewContacts()
		e.contacts.Step(s)
	}
	e.emit(sim.EventSceneLoad, map[string]any{"scene": name, "entities": s.Len()})
	return nil
}

// attach instantiates the behaviour of a registered kind.
func (e *engine) attach(ent *scene.Entity) error {
	if isBuiltinKind(ent.Kind) {
		return nil
	}
	ctor := lookupKind(ent.Kind)
	if ctor == nil {
		return fmt.Errorf("entity %q: kind %q is not registered (known: %v)", ent.Name, ent.Kind, Kinds())
	}
	if b := ctor(ent); b != nil {
		e.behaviours[ent.ID] = b
	}
	return nil
}

func (e *engine) spawn(tmpl scene.Entity) *scene.Entity {
	ent := e.ctx.Scene.Spawn(tmpl)
	if err := e.attach(ent); err != nil {
		e.emit("error", map[string]any{"msg": err.Error()})
	}
	return ent
}

func (e *engine) emit(name string, fields map[string]any) {
	if e.rec != nil {
		e.rec.Emit(name, fields)
	}
}

func (e *engine) registerInvariant(name string, pred func() bool) {
	if err := asset.ValidName(name); err != nil {
		panic("veduta: Invariant: " + err.Error())
	}
	e.custom[name] = pred
}

// tickSize sets ctx.Width and ctx.Height for Init and Update: the project resolution, in
// every mode. It is the frame the game is designed for (spec §5.2), and no panel, render
// or screenshot size can change it, so game logic that reads it gives the same trace in
// the player, in simulate and before render --tick. Draw then sees the size of the frame
// being drawn: in the player, the panel's size divided by VEDUTA_SCALE.
func (e *engine) tickSize() {
	e.ctx.Width, e.ctx.Height = e.project.Resolution[0], e.project.Resolution[1]
}

// step runs one tick with the given input.
func (e *engine) step(in Input) error {
	e.tick++
	e.ctx.Tick = e.tick
	e.tickSize()
	e.game.Update(&e.ctx, in)
	s := e.ctx.Scene
	ents := s.Entities()
	n := len(ents)
	for i := 0; i < n && i < len(s.Entities()); i++ {
		ent := s.Entities()[i]
		if !ent.Alive() {
			continue
		}
		if b := e.behaviours[ent.ID]; b != nil {
			b.Update(&e.ctx, ent, in)
		}
		if s != e.ctx.Scene { // a behaviour loaded another scene
			break
		}
	}
	return e.endTick()
}

// endTick flushes despawns, updates transforms, detects collisions, checks invariants
// and records the tick.
func (e *engine) endTick() error {
	s := e.ctx.Scene
	for _, ent := range s.Entities() {
		if !ent.Alive() {
			delete(e.behaviours, ent.ID)
		}
	}
	s.Flush()
	s.Update()
	for _, p := range e.contacts.Step(s) {
		a, b := s.Get(p.A), s.Get(p.B)
		e.emit(sim.EventCollision, map[string]any{"a": a.Name, "b": b.Name, "a_id": p.A, "b_id": p.B})
	}
	for _, v := range e.monitor.Check(e.tick, s) {
		e.emit(sim.EventInvariantViolation, map[string]any{"name": v.Name, "detail": v.Detail})
	}
	if !e.recording {
		e.rec = sim.NewRecorder(nil)
		return nil
	}
	return e.rec.EndTick(e.tick, sim.Summaries(s))
}

// frame renders the current state.
type frame struct {
	FB     *gfx.Framebuffer
	Camera scene.Camera
	View   gfx.View
	Mode   gfx.RenderMode
	Stats  gfx.FrameStats
}

// render draws the current scene (and the game's HUD in color mode) with cam.
func (e *engine) render(cam scene.Camera, w, h int, mode gfx.RenderMode, normals bool) (*frame, error) {
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 {
		return nil, fmt.Errorf("invalid frame size %dx%d", w, h)
	}
	if e.renderer == nil {
		e.renderer = soft.New(soft.Options{})
		res, err := scene.Upload(e.renderer, e.assets.Models, e.assets.Textures, e.assets.Materials)
		if err != nil {
			return nil, err
		}
		e.res = res
		ft, err := e.renderer.CreateTexture(e.ctx.Font.TextureData())
		if err != nil {
			return nil, err
		}
		e.ctx.FontTexture = ft
	}
	// Reuse the framebuffer across frames of the same size: the player renders 60 of them
	// a second. Callers use a frame before the next render (or clone it).
	fb := e.fb
	if fb == nil || fb.W != w || fb.H != h || (fb.Normal != nil) != normals {
		fb = gfx.NewFramebuffer(w, h, normals)
		e.fb = fb
	}
	e.dl.Reset()
	e.ctx.Scene.Draw(&e.dl, e.res, scene.DrawOptions{Camera: cam, Width: w, Height: h, Mode: mode})
	if e.extra != nil {
		e.extra(&e.dl, 0)
	}
	if mode == gfx.ModeColor && e.extra == nil {
		e.ctx.Width, e.ctx.Height = w, h
		e.game.Draw(&e.ctx, &e.dl)
	}
	if err := e.renderer.Begin(fb); err != nil {
		return nil, err
	}
	if err := e.renderer.Draw(&e.dl); err != nil {
		return nil, err
	}
	if err := e.renderer.End(); err != nil {
		return nil, err
	}
	return &frame{FB: fb, Camera: cam, View: e.dl.Views[0], Mode: mode, Stats: e.renderer.Stats()}, nil
}

func (e *engine) close() {
	if e.renderer != nil {
		e.renderer.Close()
	}
}

// Snapshots.

type snapEntity struct {
	ID        uint32
	Name      string
	Kind      string
	Transform scene.Transform
	Model     string
	Material  string
	Tags      []string
	Visible   bool
	Parent    uint32
	Hitbox    *gmath.AABB
	Layer     int
	State     []byte
}

type snapshot struct {
	Version  string
	Scene    string
	Tick     uint64
	RNG      [4]uint64
	NextID   uint32
	Entities []snapEntity
	Contacts []sim.Pair
	Failing  []bool
	Game     []byte
}

func (e *engine) snapshot() ([]byte, error) {
	s := e.ctx.Scene
	snap := snapshot{Version: Version, Scene: s.Name, Tick: e.tick, RNG: e.ctx.RNG.State(), NextID: s.NextID(),
		Contacts: e.contacts.Active(), Failing: e.monitor.FailingState()}
	for _, ent := range s.Entities() {
		if !ent.Alive() {
			continue
		}
		se := snapEntity{ID: ent.ID, Name: ent.Name, Kind: ent.Kind, Transform: ent.Transform, Model: ent.Model,
			Material: ent.Material, Tags: ent.Tags, Visible: ent.Visible, Parent: ent.Parent, Hitbox: ent.Hitbox, Layer: ent.Layer}
		if ent.State != nil {
			var buf bytes.Buffer
			gob.Register(ent.State)
			if err := gob.NewEncoder(&buf).Encode(&ent.State); err != nil {
				return nil, fmt.Errorf("snapshot entity %s state: %w", ent.Name, err)
			}
			se.State = buf.Bytes()
		}
		snap.Entities = append(snap.Entities, se)
	}
	if e.codec != nil {
		var buf bytes.Buffer
		if err := e.codec.SaveState(gob.NewEncoder(&buf)); err != nil {
			return nil, fmt.Errorf("snapshot game state: %w", err)
		}
		snap.Game = buf.Bytes()
	}
	var out bytes.Buffer
	if err := gob.NewEncoder(&out).Encode(&snap); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// restore replaces the running state with a snapshot taken from the same game and
// project. prepare must have run (scene loaded, Init called).
func (e *engine) restore(data []byte, trace io.Writer) error {
	var snap snapshot
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snap); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	if snap.Version != Version {
		return fmt.Errorf("restore: snapshot is from engine %s, this is %s", snap.Version, Version)
	}
	src, ok := e.assets.Scenes[snap.Scene]
	if !ok {
		return fmt.Errorf("restore: unknown scene %q", snap.Scene)
	}
	s := scene.New(snap.Scene, e.assets.ModelBounds)
	s.Camera = scene.CameraFromAsset(src.Camera)
	s.Light, s.Background = src.Light, src.Background
	ents := make([]scene.Entity, len(snap.Entities))
	for i, se := range snap.Entities {
		ents[i] = scene.Entity{ID: se.ID, Name: se.Name, Kind: se.Kind, Transform: se.Transform, Model: se.Model,
			Material: se.Material, Tags: se.Tags, Visible: se.Visible, Parent: se.Parent, Hitbox: se.Hitbox, Layer: se.Layer}
	}
	if err := s.Restore(ents, snap.NextID); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	s.OnEvent = e.emit
	e.ctx.Scene = s
	e.behaviours = map[uint32]Behaviour{}
	for i, ent := range s.Entities() {
		if err := e.attach(ent); err != nil {
			return fmt.Errorf("restore: %w", err)
		}
		ent.State = nil
		if data := snap.Entities[i].State; data != nil {
			var st any
			if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&st); err != nil {
				return fmt.Errorf("restore entity %s state: %w", ent.Name, err)
			}
			ent.State = st
		}
	}
	if e.codec != nil {
		if snap.Game == nil {
			return errors.New("restore: snapshot has no game state but the game registered a codec")
		}
		if err := e.codec.LoadState(gob.NewDecoder(bytes.NewReader(snap.Game))); err != nil {
			return fmt.Errorf("restore game state: %w", err)
		}
	}
	e.tick = snap.Tick
	e.ctx.Tick = snap.Tick
	e.ctx.RNG.SetState(snap.RNG)
	s.Update()
	e.contacts = sim.NewContacts()
	e.contacts.SetActive(snap.Contacts)
	e.monitor.SetFailingState(snap.Failing)
	e.rec = sim.NewRecorder(trace)
	e.recording = true
	return nil
}

// sortedNames returns the keys of m in order.
func sortedNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
