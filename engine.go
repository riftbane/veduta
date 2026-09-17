package veduta

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gfx/soft"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/scene"
	"github.com/riftbane/veduta/v2/sim"
	"github.com/riftbane/veduta/v2/sprite"
	"github.com/riftbane/veduta/v2/world"
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
	world   *World                  // the loaded world, nil for a scene
	runtime map[string]*asset.Model // models the game built with Context.SetModel

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
	replay     *snapReplay // the run so far, for a Replayer's snapshots (headless runs only)

	renderer *soft.Renderer
	res      *scene.Resources
	dl       gfx.DrawList
	fb       *gfx.Framebuffer
	extra    func(dl *gfx.DrawList, view int)
}

func newEngine(g Game, p *asset.Project, a *Assets) *engine {
	e := &engine{game: g, project: p, assets: a, custom: map[string]func() bool{}, runtime: map[string]*asset.Model{}}
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
	World      string   // load this world instead of Scene
	At         [2]int32 // the world's start cell
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
	e.replay = nil
	if _, ok := e.game.(Replayer); ok && opt.Headless {
		e.replay = &snapReplay{Scene: opt.Scene, World: opt.World, At: opt.At, Seed: opt.Seed}
	}
	if st, ok := e.game.(Starter); ok {
		if err := st.Start(&e.ctx); err != nil {
			return fmt.Errorf("game Start: %w", err)
		}
	}
	var err error
	if opt.World != "" {
		err = e.loadWorld(opt.World, opt.At)
	} else {
		err = e.loadScene(opt.Scene)
	}
	if err != nil {
		return err
	}
	e.tickSize()
	if err := e.game.Init(&e.ctx); err != nil {
		return fmt.Errorf("game Init: %w", err)
	}
	if err := e.gameErr(); err != nil {
		return err
	}
	if err := e.buildMonitor(); err != nil {
		return err
	}
	e.ctx.Scene.Flush()
	e.ctx.Scene.Update()
	e.contacts.Step(e.ctx.Scene) // overlaps present at load are not collisions
	return nil
}

// buildMonitor parses the run's invariant specs against the current bounds (the
// world's extent when a world is loaded, else the project's) and the game's predicates.
func (e *engine) buildMonitor() error {
	bounds := e.project.Bounds
	if e.world != nil {
		bounds = e.world.worldBounds()
	}
	var list []sim.Invariant
	for _, spec := range e.invSpecs {
		inv, err := sim.ParseInvariant(spec, bounds, e.custom)
		if err != nil {
			return err
		}
		list = append(list, inv)
	}
	e.monitor = sim.NewMonitor(list)
	return nil
}

func (e *engine) loadScene(name string) error {
	src, ok := e.assets.Scenes[name]
	if !ok {
		return fmt.Errorf("unknown scene %q", name)
	}
	s, err := scene.Load(src, e.modelBounds)
	if err != nil {
		return err
	}
	e.world = nil
	if err := e.install(s); err != nil {
		return err
	}
	e.emit(sim.EventSceneLoad, map[string]any{"scene": name, "entities": s.Len()})
	return nil
}

// loadWorld replaces the scene with world name, streamed around its start cell at.
func (e *engine) loadWorld(name string, at [2]int32) error {
	src, ok := e.assets.Worlds[name]
	if !ok {
		return fmt.Errorf("unknown world %q", name)
	}
	w := newWorld(e, src, at)
	if len(w.gen.Missing) > 0 {
		return fmt.Errorf("world %q: missing prefabs %v", name, w.gen.Missing)
	}
	s, err := w.startScene()
	if err != nil {
		return err
	}
	e.world = w
	e.ctx.Scene = s
	e.behaviours = map[uint32]Behaviour{}
	for _, ent := range s.Entities() {
		if err := e.attach(ent); err != nil {
			return err
		}
	}
	w.stream()
	s.OnEvent = e.emit
	s.Update()
	if e.contacts != nil {
		e.contacts = sim.NewContacts()
		e.contacts.Step(s)
	}
	if e.monitor != nil { // loaded from Update: the bounds changed
		if err := e.buildMonitor(); err != nil {
			return err
		}
	}
	e.emit("world_load", map[string]any{"world": name, "at": at, "entities": s.Len()})
	return nil
}

// install makes s the current scene: behaviours attached, contacts reset, events wired.
func (e *engine) install(s *scene.Scene) error {
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
	if e.monitor != nil {
		if err := e.buildMonitor(); err != nil {
			return err
		}
	}
	return nil
}

// gameErr returns the error a Failer game reports, if any.
func (e *engine) gameErr() error {
	if f, ok := e.game.(Failer); ok {
		return f.Err()
	}
	return nil
}

// attach instantiates the behaviour of a kind the game defines or registered.
func (e *engine) attach(ent *scene.Entity) error {
	if isBuiltinKind(ent.Kind) {
		return nil
	}
	var ctor func(*scene.Entity) Behaviour
	known := Kinds()
	if kp, ok := e.game.(KindProvider); ok {
		ctor = kp.Kind(ent.Kind)
		known = append(kp.Kinds(), known...)
	}
	if ctor == nil {
		ctor = lookupKind(ent.Kind)
	}
	if ctor == nil {
		return fmt.Errorf("entity %q: kind %q is not registered (known: %v)", ent.Name, ent.Kind, known)
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
	if e.replay != nil {
		e.replay.Inputs = append(e.replay.Inputs, in)
	}
	e.tick++
	e.ctx.Tick = e.tick
	e.tickSize()
	if w := e.world; w != nil && w.stream() {
		// Entities that appear overlapping are not collisions, as at load.
		e.ctx.Scene.Update()
		e.contacts.Step(e.ctx.Scene)
	}
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
	if err := e.gameErr(); err != nil {
		return err
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
	Draw   scene.DrawStats // entities culled, too far or drawn at a lower level of detail
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
	if err := e.syncModels(); err != nil {
		return nil, err
	}
	e.dl.Reset()
	var ds scene.DrawStats
	e.ctx.Scene.Draw(&e.dl, e.res, scene.DrawOptions{Camera: cam, Width: w, Height: h, Mode: mode, Stats: &ds})
	if e.extra != nil {
		e.extra(&e.dl, 0)
	}
	if mode == gfx.ModeColor && e.extra == nil {
		e.ctx.Width, e.ctx.Height = w, h
		e.game.Draw(&e.ctx, &e.dl)
		if err := e.gameErr(); err != nil {
			return nil, err
		}
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
	return &frame{FB: fb, Camera: cam, View: e.dl.Views[0], Mode: mode, Stats: e.renderer.Stats(), Draw: ds}, nil
}

// modelBounds is the scenes' BoundsFunc: a model the game built, else the library's.
func (e *engine) modelBounds(name string) (gmath.AABB, bool) {
	if m := e.runtime[name]; m != nil {
		return m.Mesh.Bounds, true
	}
	return e.assets.ModelBounds(name)
}

// syncModels uploads the runtime models (the loaded chunks' ground and flora, the game's
// own) that changed since the last frame and frees those that went away, so the renderer
// holds exactly the models entities can name. Runtime names contain ':', asset names
// never do.
func (e *engine) syncModels() error {
	want := e.runtime
	if e.world != nil {
		want = make(map[string]*asset.Model, len(e.runtime)+len(e.world.models))
		for k, m := range e.runtime {
			want[k] = m
		}
		for k, m := range e.world.models {
			want[k] = m
		}
		if e.res.Materials[world.WaterMaterial] == nil {
			e.res.Materials[world.WaterMaterial] = &world.DefaultWater
		}
	}
	for _, name := range sortedNames(e.res.Models) {
		if strings.Contains(name, ":") && want[name] == nil {
			e.res.Remove(name)
		}
	}
	for _, name := range sortedNames(want) {
		if cur := e.res.Models[name]; cur == nil || cur.Model != want[name] {
			if err := e.res.AddModel(e.renderer, name, want[name]); err != nil {
				return err
			}
		}
	}
	return nil
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
	// A world: its name, start and focus cells, and the streamed window.
	World   string
	Start   [2]int32
	Focus   [2]int32
	Chunks  []snapChunk
	Structs []snapStruct
	// A Replayer's snapshot holds only the run, and restoring plays it again.
	Replay *snapReplay
}

// snapReplay is a run from its start: what was loaded, the seed, the input of every tick,
// and the trace hash it reached, which the replay must reach too.
type snapReplay struct {
	Scene  string
	World  string
	At     [2]int32
	Seed   uint64
	Inputs []Input
	Hash   string
}

func (e *engine) snapshot() ([]byte, error) {
	s := e.ctx.Scene
	if e.replay != nil {
		r := *e.replay
		r.Inputs = append([]Input(nil), r.Inputs...)
		r.Hash = e.rec.Hash()
		var out bytes.Buffer
		if err := gob.NewEncoder(&out).Encode(&snapshot{Version: Version, Scene: s.Name, Tick: e.tick, Replay: &r}); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	snap := snapshot{Version: Version, Scene: s.Name, Tick: e.tick, RNG: e.ctx.RNG.State(), NextID: s.NextID(),
		Contacts: e.contacts.Active(), Failing: e.monitor.FailingState()}
	if w := e.world; w != nil {
		snap.World, snap.Start, snap.Focus = w.Name, w.Start, w.focus
		snap.Chunks, snap.Structs = w.snapshot()
	}
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
	if snap.Replay != nil {
		return e.restoreReplay(snap.Replay, snap.Tick, trace)
	}
	var s *scene.Scene
	e.world = nil
	if snap.World != "" {
		wd, ok := e.assets.Worlds[snap.World]
		if !ok {
			return fmt.Errorf("restore: unknown world %q", snap.World)
		}
		w := newWorld(e, wd, snap.Start)
		if len(w.gen.Missing) > 0 {
			return fmt.Errorf("restore: world %q: missing prefabs %v", snap.World, w.gen.Missing)
		}
		w.focus = snap.Focus
		w.restore(snap.Chunks, snap.Structs)
		start, err := w.startScene()
		if err != nil {
			return fmt.Errorf("restore: %w", err)
		}
		s = scene.New(snap.Scene, w.bounds)
		s.Camera, s.Light, s.Background = start.Camera, start.Light, start.Background
		e.world = w
	} else {
		src, ok := e.assets.Scenes[snap.Scene]
		if !ok {
			return fmt.Errorf("restore: unknown scene %q", snap.Scene)
		}
		s = scene.New(snap.Scene, e.modelBounds)
		s.Camera = scene.CameraFromAsset(src.Camera)
		s.Light, s.Background = src.Light, src.Background
	}
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
	if err := e.buildMonitor(); err != nil { // the bounds follow the snapshot's world or scene
		return fmt.Errorf("restore: %w", err)
	}
	e.monitor.SetFailingState(snap.Failing)
	e.rec = sim.NewRecorder(trace)
	e.recording = true
	return nil
}

// restoreReplay restores a Replayer's snapshot by playing its run again from the start,
// without a trace, and checks that the run reaches the same trace hash: a different game or
// project would not.
func (e *engine) restoreReplay(r *snapReplay, tick uint64, trace io.Writer) error {
	if uint64(len(r.Inputs)) != tick {
		return fmt.Errorf("restore: snapshot at tick %d holds %d ticks of input", tick, len(r.Inputs))
	}
	opt := runOptions{Scene: r.Scene, World: r.World, At: r.At, Seed: r.Seed, Invariants: e.invSpecs, Headless: true}
	if err := e.start(opt); err != nil {
		return fmt.Errorf("restore: replaying the run: %w", err)
	}
	for _, in := range r.Inputs {
		if err := e.step(in); err != nil {
			return fmt.Errorf("restore: replaying the run: %w", err)
		}
	}
	if h := e.rec.Hash(); h != r.Hash {
		return fmt.Errorf("restore: the replayed run reached trace hash %s, the snapshot %s (the game or its assets changed since)", h, r.Hash)
	}
	e.rec = sim.NewRecorder(trace)
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
