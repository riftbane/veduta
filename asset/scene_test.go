package asset

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gfx/soft"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/internal/golden"
)

// renderScene is a stand-in for the scene package: every entity with a model is drawn
// as a unit cube with its compiled material, opaque entities first, then blended ones,
// each pass in scene order. Entity ids are index+1.
func renderScene(t *testing.T, s *Scene, mats map[string]*Material, w, h int) *gfx.Framebuffer {
	t.Helper()
	r := soft.New(soft.Options{})
	defer r.Close()
	cube, err := r.CreateMesh(unitCube())
	if err != nil {
		t.Fatal(err)
	}
	cam := s.Camera
	aspect := float32(w) / float32(h)
	proj := gmath.Perspective(gmath.Radians(cam.FovDeg), aspect, cam.Near, cam.Far)
	if cam.Ortho {
		hw, hh := float32(cam.Size*aspect/2), float32(cam.Size/2)
		proj = gmath.Orthographic(-hw, hw, -hh, hh, cam.Near, cam.Far)
	}
	dl := gfx.DrawList{Clear: true, ClearColor: s.Background, Light: s.Light}
	v := dl.AddView(gfx.View{View: gmath.LookAt(cam.Position, cam.LookAt, gmath.Up), Proj: proj,
		Eye: cam.Position, Near: cam.Near, Far: cam.Far})
	index := map[string]int{}
	for i, e := range s.Entities {
		index[e.Name] = i
	}
	var world func(i int) gmath.Mat4
	world = func(i int) gmath.Mat4 {
		e := s.Entities[i]
		m := gmath.TRS(e.Position, gmath.QuatEulerDeg(e.RotationDeg), e.Scale)
		if e.Parent != "" {
			m = world(index[e.Parent]).Mul(m)
		}
		return m
	}
	for _, blend := range []bool{false, true} {
		for i, e := range s.Entities {
			m := &DefaultMaterial
			if mm, ok := mats[e.Material]; ok {
				m = mm
			}
			if e.Model == "" || !e.Visible || (m.Alpha == "blend") != blend {
				continue
			}
			dl.Add(gfx.DrawCmd{View: v, Mesh: cube, Model: world(i), Color: gfx.ColorVec4(m.Albedo), State: m.State(),
				Filter: m.Filter, Unlit: m.Unlit, Cutoff: m.AlphaCutoff(), ID: uint32(i + 1)})
		}
	}
	fb := gfx.NewFramebuffer(w, h, false)
	if err := r.Begin(fb); err != nil {
		t.Fatal(err)
	}
	if err := r.Draw(&dl); err != nil {
		t.Fatal(err)
	}
	if err := r.End(); err != nil {
		t.Fatal(err)
	}
	return fb
}

// unitCube is a cube centered at the origin with side 1, counter-clockwise faces.
func unitCube() *gfx.MeshData {
	type face struct{ n, u, v gmath.Vec3 }
	faces := []face{
		{gmath.V3(1, 0, 0), gmath.V3(0, 0, -1), gmath.V3(0, 1, 0)},
		{gmath.V3(-1, 0, 0), gmath.V3(0, 0, 1), gmath.V3(0, 1, 0)},
		{gmath.V3(0, 1, 0), gmath.V3(1, 0, 0), gmath.V3(0, 0, -1)},
		{gmath.V3(0, -1, 0), gmath.V3(1, 0, 0), gmath.V3(0, 0, 1)},
		{gmath.V3(0, 0, 1), gmath.V3(1, 0, 0), gmath.V3(0, 1, 0)},
		{gmath.V3(0, 0, -1), gmath.V3(-1, 0, 0), gmath.V3(0, 1, 0)},
	}
	m := &gfx.MeshData{Bounds: gmath.AABB{Min: gmath.V3(-0.5, -0.5, -0.5), Max: gmath.V3(0.5, 0.5, 0.5)}}
	for _, f := range faces {
		c, u, v := f.n.Scale(0.5), f.u.Scale(0.5), f.v.Scale(0.5)
		base := uint32(len(m.Vertices))
		for _, p := range []gmath.Vec3{c.Sub(u).Sub(v), c.Add(u).Sub(v), c.Add(u).Add(v), c.Sub(u).Add(v)} {
			m.Vertices = append(m.Vertices, gfx.Vertex{Pos: p, Normal: f.n})
		}
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	m.Parts = []gfx.MeshPart{{First: 0, Count: len(m.Indices)}}
	return m
}

const renderSceneSrc = `{
  "veduta": "scene/1",
  "camera": {"position": [4, 3.5, 6], "look_at": [0, 0.4, 0], "fov_deg": 45},
  "light": {"direction": [-0.5, -1, -0.4]},
  "entities": [
    {"name": "ground", "kind": "static", "model": "cube", "material": "grass", "position": [0, -0.05, 0], "scale": [6, 0.1, 6]},
    {"name": "crate", "kind": "static", "model": "cube", "material": "crate", "position": [0, 0.5, 0], "rotation_deg": [0, 30, 0]},
    {"name": "lamp", "kind": "static", "model": "cube", "material": "lamp", "parent": "crate", "position": [0, 0.75, 0], "scale": [0.3, 0.5, 0.3]},
    {"name": "north", "kind": "static", "model": "cube", "material": "marker", "position": [0, 0.25, -2.5], "scale": [0.5, 0.5, 0.5]},
    {"name": "glass", "kind": "static", "model": "cube", "material": "glass", "position": [1.3, 0.6, 1.2], "scale": [0.8, 1.2, 0.2]},
    {"name": "ghost", "kind": "static", "model": "cube", "material": "crate", "position": [-1.5, 0.5, 1], "visible": false}
  ]
}`

var renderMaterials = [][2]string{
	{"grass", `{"veduta": "material/1", "albedo": "#4a8f3a"}`},
	{"crate", `{"veduta": "material/1", "albedo": "#b07a40"}`},
	{"lamp", `{"veduta": "material/1", "albedo": "#fff2a0", "unlit": true}`},
	{"marker", `{"veduta": "material/1", "albedo": "#d03030"}`},
	{"glass", `{"veduta": "material/1", "albedo": "#60a0ff80", "alpha": "blend", "cull": "none"}`},
}

// idStats returns the pixel count and the mean row of each entity id.
func idStats(fb *gfx.Framebuffer) (count map[uint32]int, meanY map[uint32]float64) {
	count, meanY = map[uint32]int{}, map[uint32]float64{}
	for i, id := range fb.ID {
		count[id]++
		meanY[id] += float64(i / fb.W)
	}
	for id, n := range count {
		meanY[id] /= float64(n)
	}
	return count, meanY
}

// A compiled scene and its materials render as expected: camera placement, light,
// background, parent transforms, unlit, blend and visibility.
func TestSceneRenderGolden(t *testing.T) {
	mats := map[string]*Material{}
	for _, m := range renderMaterials {
		mat, err := ParseMaterial(m[0]+".mat.json", []byte(m[1]))
		if err != nil {
			t.Fatal(err)
		}
		mats[m[0]] = mat
	}
	s, err := ParseScene("render.scene.json", []byte(renderSceneSrc))
	if err != nil {
		t.Fatal(err)
	}
	const w, h = 320, 180
	fb := renderScene(t, s, mats, w, h)
	golden.Image(t, "asset_scene_materials", fb.Image())
	if fb.Color[0] != s.Background {
		t.Errorf("corner pixel %08x, want background %08x", fb.Color[0], s.Background)
	}
	count, _ := idStats(fb)
	for i, e := range s.Entities {
		n := count[uint32(i+1)]
		if mats[e.Material] != nil && mats[e.Material].Alpha == "blend" {
			// Blended materials do not write depth, so they never own ID-buffer pixels.
			if n != 0 {
				t.Errorf("blended entity %s owns %d id pixels", e.Name, n)
			}
			continue
		}
		if e.Visible && n < 50 {
			t.Errorf("entity %s covers %d pixels", e.Name, n)
		}
		if !e.Visible && n != 0 {
			t.Errorf("hidden entity %s covers %d pixels", e.Name, n)
		}
	}
	// The unlit lamp keeps its exact albedo.
	lampID := uint32(3)
	for i, id := range fb.ID {
		if id == lampID && fb.Color[i] != mats["lamp"].Albedo {
			t.Fatalf("unlit lamp pixel %08x, want %08x", fb.Color[i], mats["lamp"].Albedo)
		}
	}

	// Top-down orthographic view: -Z (the "north" marker) is at the top of the image.
	top, err := ParseScene("top.scene.json", []byte(strings.Replace(renderSceneSrc,
		`"camera": {"position": [4, 3.5, 6], "look_at": [0, 0.4, 0], "fov_deg": 45}`,
		`"camera": {"type": "orthographic", "size": 8, "position": [0, 20, 0], "look_at": [0, 0, 0]}`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	fb = renderScene(t, top, mats, w, h)
	golden.Image(t, "asset_scene_ortho_top", fb.Image())
	count, meanY := idStats(fb)
	if north := uint32(4); count[north] == 0 || meanY[north] > h/2-20 {
		t.Errorf("north marker at mean row %.1f (%d px), want it in the top half", meanY[north], count[north])
	}
	// size 8 m over 180 px: the 6 m ground spans 6/8 of the height.
	if g := count[1]; g < 6*6*(h/8)*(h/8)*9/10 {
		t.Errorf("ground covers %d pixels in the top view", g)
	}
}

const minimalScene = `{"veduta": "scene/1", "camera": {"position": [0, 5, 10], "look_at": [0, 0, 0]}}`

func TestParseSceneDefaults(t *testing.T) {
	s, err := ParseScene("assets/scenes/empty.scene.json", []byte(minimalScene))
	if err != nil {
		t.Fatal(err)
	}
	want := &Scene{
		Name: "empty",
		Camera: Camera{FovDeg: 60, Near: 0.1, Far: 200,
			Position: gmath.V3(0, 5, 10), LookAt: gmath.Zero3},
		Light:      gfx.Light{Dir: gmath.V3(-0.4, -1, -0.3), Color: gmath.One3, Ambient: gfx.ColorVec3(0xff404040)},
		Background: 0xff202830,
	}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("got  %+v\nwant %+v", s, want)
	}
}

func TestParseSceneSample(t *testing.T) {
	s, err := ParseScene("scenes/main.scene.json", readTestdata(t, "scenes/main.scene.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "main" || len(s.Entities) != 3 {
		t.Fatalf("got %+v", s)
	}
	names := []string{s.Entities[0].Name, s.Entities[1].Name, s.Entities[2].Name}
	if !reflect.DeepEqual(names, []string{"ground", "player", "gem_1"}) {
		t.Fatalf("entity order %v", names)
	}
	p := s.Entities[1]
	want := Entity{Name: "player", Kind: "player", Model: "hero", Material: "hero",
		RotationDeg: gmath.V3(0, 180, 0), Scale: gmath.One3, Tags: []string{"player"}, Visible: true}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("player = %+v, want %+v", p, want)
	}
	if g := s.Entities[0]; g.Tags != nil || !g.Visible || g.Scale != gmath.One3 {
		t.Fatalf("ground defaults %+v", g)
	}
}

func TestParseSceneFull(t *testing.T) {
	src := `{
  "veduta": "scene/1",
  "camera": { "type": "orthographic", "size": 20, "near": 1, "far": 50, "position": [0, 30, 0], "look_at": [0, 0, 0] },
  "light": { "direction": [0, -1, 0], "color": "#ff0000", "ambient": "#000000" },
  "background": "#00000000",
  "entities": [
    { "name": "hand", "kind": "static", "parent": "arm", "scale": [-1, 2, 3], "visible": false, "tags": ["a", "b"] },
    { "name": "arm", "kind": "robot_arm", "position": [1, 2, 3] }
  ]
}`
	s, err := ParseScene("robot.scene.json", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if c := s.Camera; !c.Ortho || c.Size != 20 || c.FovDeg != 0 || c.Near != 1 || c.Far != 50 {
		t.Fatalf("camera %+v", c)
	}
	if s.Light.Color != gmath.V3(1, 0, 0) || s.Light.Ambient != gmath.Zero3 || s.Light.Dir != gmath.V3(0, -1, 0) {
		t.Fatalf("light %+v", s.Light)
	}
	if s.Background != 0 {
		t.Fatalf("background %08x", s.Background)
	}
	h := s.Entities[0]
	if h.Parent != "arm" || h.Visible || h.Scale != gmath.V3(-1, 2, 3) || !reflect.DeepEqual(h.Tags, []string{"a", "b"}) {
		t.Fatalf("hand %+v", h)
	}
}

// A hitbox compiles to a local-space box (a flat one, min == max on an axis, is allowed)
// and a layer to an int.
func TestParseSceneHitboxAndLayer(t *testing.T) {
	src := `{"veduta": "scene/1", "camera": {"position": [0, 0, 10], "look_at": [0, 0, 0]}, "entities": [
    {"name": "coin", "kind": "static", "model": "quad", "hitbox": [[-0.25, -0.25, -0.5], [0.25, 0.25, 0.5]], "layer": 2},
    {"name": "trigger", "kind": "static", "hitbox": [[0, 0, 0], [2, 1, 0]], "layer": -1000},
    {"name": "plain", "kind": "static", "model": "quad", "layer": 0}
  ]}`
	s, err := ParseScene("twod.scene.json", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if l := [3]int{s.Entities[0].Layer, s.Entities[1].Layer, s.Entities[2].Layer}; l != [3]int{2, -1000, 0} {
		t.Errorf("layers %v", l)
	}
	want := []*gmath.AABB{
		{Min: gmath.V3(-0.25, -0.25, -0.5), Max: gmath.V3(0.25, 0.25, 0.5)},
		{Min: gmath.V3(0, 0, 0), Max: gmath.V3(2, 1, 0)},
		nil,
	}
	for i, e := range s.Entities {
		if !reflect.DeepEqual(e.Hitbox, want[i]) {
			t.Errorf("%s: hitbox %v, want %v", e.Name, e.Hitbox, want[i])
		}
	}
}

func TestParseSceneErrors(t *testing.T) {
	cam := `"camera": {"position": [0, 5, 10], "look_at": [0, 0, 0]}`
	ents := func(e string) string { return `{"veduta": "scene/1", ` + cam + `, "entities": [` + e + `]}` }
	cases := []struct {
		name, src string
		wants     []wantErr
	}{
		{"camera type", `{"veduta": "scene/1", "camera": {"type": "fisheye", "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.type: unknown value "fisheye"`, `"fisheye"`}}},
		{"fov on ortho", `{"veduta": "scene/1", "camera": {"type": "orthographic", "size": 5, "fov_deg": 60, "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.fov_deg: only allowed for perspective`, `60`}}},
		{"ortho without size", `{"veduta": "scene/1", "camera": {"type": "orthographic", "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.size: is required for orthographic`, `{"type"`}}},
		{"size on perspective", `{"veduta": "scene/1", "camera": {"size": 5, "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.size: only allowed for orthographic`, `5,`}}},
		{"negative size", `{"veduta": "scene/1", "camera": {"type": "orthographic", "size": -5, "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.size: must be a positive number`, `-5`}}},
		{"fov too small", `{"veduta": "scene/1", "camera": {"fov_deg": 1, "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.fov_deg: 1 out of range (1, 179)`, `1,`}}},
		{"fov too large", `{"veduta": "scene/1", "camera": {"fov_deg": 179, "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.fov_deg: 179 out of range`, `179`}}},
		{"near >= far", `{"veduta": "scene/1", "camera": {"near": 10, "far": 5, "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.far: far (5) must be greater than near (10)`, `5,`}}},
		{"near >= default far", `{"veduta": "scene/1", "camera": {"near": 300, "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.near: far (200) must be greater than near (300)`, `300`}}},
		{"near zero", `{"veduta": "scene/1", "camera": {"near": 0, "position": [0, 5, 10], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.near: must be a positive number`, `0,`}}},
		{"missing camera", `{"veduta": "scene/1"}`,
			[]wantErr{{`camera.position: is required`, ""}, {`camera.look_at: is required`, ""}}},
		{"position length", `{"veduta": "scene/1", "camera": {"position": [0, 5], "look_at": [0, 0, 0]}}`,
			[]wantErr{{`camera.position: want 3 numbers, got 2`, `[0, 5]`}}},
		{"look_at equals position", `{"veduta": "scene/1", "camera": {"position": [1, 2, 3], "look_at": [1, 2, 3]}}`,
			[]wantErr{{`camera.look_at: must differ from camera.position`, `[1, 2, 3]}`}}},
		{"zero light", `{"veduta": "scene/1", ` + cam + `, "light": {"direction": [0, 0, 0]}}`,
			[]wantErr{{`light.direction: must not be the zero vector`, `[0, 0, 0]}}`}}},
		{"light alpha", `{"veduta": "scene/1", ` + cam + `, "light": {"color": "#ffffff80", "ambient": "#12"}}`,
			[]wantErr{{`light.color: light colors have no alpha`, `"#ffffff80"`}, {`light.ambient: color "#12"`, `"#12"`}}},
		{"background", `{"veduta": "scene/1", ` + cam + `, "background": "blue"}`,
			[]wantErr{{`background: color "blue"`, `"blue"`}}},
		{"entity name missing", ents(`{"kind": "static"}`),
			[]wantErr{{`entities[0].name: is required`, `{"kind"`}}},
		{"entity name invalid", ents(`{"name": "Player", "kind": "static"}`),
			[]wantErr{{`entities[0].name: name "Player"`, `"Player"`}}},
		{"entity name duplicate", ents(`{"name": "a", "kind": "static"}, {"name": "b", "kind": "static"}, {"name": "a", "kind": "static"}`),
			[]wantErr{{`entities[2].name: duplicate entity name "a" (first used by entities[0])`, `"a", "kind": "static"}]`}}},
		{"kind missing", ents(`{"name": "a"}`),
			[]wantErr{{`entities[0].kind: is required`, `{"name"`}}},
		{"kind invalid", ents(`{"name": "a", "kind": "Player"}`),
			[]wantErr{{`entities[0].kind: name "Player"`, `"Player"`}}},
		{"model and material invalid", ents(`{"name": "a", "kind": "static", "model": "a b", "material": "M"}`),
			[]wantErr{{`entities[0].model:`, `"a b"`}, {`entities[0].material:`, `"M"`}}},
		{"scale zero", ents(`{"name": "a", "kind": "static", "scale": [1, 0, 0]}`),
			[]wantErr{{`entities[0].scale[1]: must be non-zero`, `0, 0]}]`}, {`entities[0].scale[2]: must be non-zero`, `0]}]`}}},
		{"rotation length", ents(`{"name": "a", "kind": "static", "rotation_deg": [1, 2, 3, 4]}`),
			[]wantErr{{`entities[0].rotation_deg: want 3 numbers, got 4`, `[1, 2, 3, 4]`}}},
		{"tags", ents(`{"name": "a", "kind": "static", "tags": ["x", "Bad", "x"]}`),
			[]wantErr{{`entities[0].tags[1]: name "Bad"`, `"Bad"`}, {`entities[0].tags[2]: duplicate tag "x"`, `"x"]`}}},
		{"unknown parent", ents(`{"name": "a", "kind": "static", "parent": "ghost"}`),
			[]wantErr{{`entities[0].parent: no entity named "ghost"`, `"ghost"`}}},
		{"self parent", ents(`{"name": "a", "kind": "static", "parent": "a"}`),
			[]wantErr{{`entities[0].parent: an entity cannot be its own parent`, `"a"}`}}},
		{"cycle", ents(`{"name": "root", "kind": "static"}, {"name": "c", "kind": "static", "parent": "b"}, {"name": "b", "kind": "static", "parent": "c"}, {"name": "d", "kind": "static", "parent": "c"}`),
			[]wantErr{{`entities[1].parent: parent cycle: c -> b -> c`, `"b"}`}}},
		{"three cycle", ents(`{"name": "x", "kind": "static", "parent": "z"}, {"name": "y", "kind": "static", "parent": "x"}, {"name": "z", "kind": "static", "parent": "y"}`),
			[]wantErr{{`entities[0].parent: parent cycle: x -> z -> y -> x`, `"z"}`}}},
		{"unknown field", ents(`{"name": "a", "kind": "static", "rotation": [0, 0, 0]}`),
			[]wantErr{{`entities[0].rotation: unknown field`, `"rotation"`}}},
		{"hitbox one vector", ents(`{"name": "a", "kind": "static", "hitbox": [[0, 0, 0]]}`),
			[]wantErr{{`entities[0].hitbox: want [[minx, miny, minz], [maxx, maxy, maxz]], got 1 vectors`, `[[0, 0, 0]]`}}},
		{"hitbox empty", ents(`{"name": "a", "kind": "static", "hitbox": []}`),
			[]wantErr{{`entities[0].hitbox: want [[minx, miny, minz], [maxx, maxy, maxz]], got 0 vectors`, `[]}`}}},
		{"hitbox vector length", ents(`{"name": "a", "kind": "static", "hitbox": [[0, 0], [1, 1, 1, 1]]}`),
			[]wantErr{{`entities[0].hitbox[0]: want 3 numbers, got 2`, `[0, 0]`}, {`entities[0].hitbox[1]: want 3 numbers, got 4`, `[1, 1, 1, 1]`}}},
		{"hitbox null vector", ents(`{"name": "a", "kind": "static", "hitbox": [null, [1, 1, 1]]}`),
			[]wantErr{{`entities[0].hitbox[0]: is required`, `null`}}},
		{"hitbox min above max", ents(`{"name": "a", "kind": "static", "hitbox": [[-1, 2, 0.5], [1, 1, -0.5]]}`),
			[]wantErr{{`entities[0].hitbox[1][1]: max y (1) is less than min y (2)`, `1, -0.5]]`}, {`entities[0].hitbox[1][2]: max z (-0.5) is less than min z (0.5)`, `-0.5]]`}}},
		{"hitbox out of float range", ents(`{"name": "a", "kind": "static", "hitbox": [[0, 0, 0], [1e39, 1, 1]]}`),
			[]wantErr{{`entities[0].hitbox[1][0]:`, `1e39`}}},
		{"layer above range", ents(`{"name": "a", "kind": "static", "layer": 1001}`),
			[]wantErr{{`entities[0].layer: 1001 out of range [-1000, 1000]`, `1001`}}},
		{"layer below range", ents(`{"name": "a", "kind": "static", "layer": -5000}`),
			[]wantErr{{`entities[0].layer: -5000 out of range [-1000, 1000]`, `-5000`}}},
		{"layer not an integer", ents(`{"name": "a", "kind": "static", "layer": 1.5}`),
			[]wantErr{{`entities[0].layer: cannot use JSON number 1.5 as int`, `1.5`}}},
		{"hitbox wrong type", ents(`{"name": "a", "kind": "static", "hitbox": [0, 0, 0]}`),
			[]wantErr{{`entities[0].hitbox[0]: cannot use JSON number`, `0, 0, 0]}]`}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseScene("s.scene.json", []byte(c.src))
			checkErrs(t, c.src, err, c.wants...)
		})
	}
}

func TestParseSceneReportsAll(t *testing.T) {
	src := "{\n  \"veduta\": \"scene/1\",\n  \"camera\": { \"fov_deg\": 200, \"position\": [0, 0, 0], \"look_at\": [0, 0, 0] },\n  \"background\": \"#1\",\n  \"entities\": [\n    { \"name\": \"a\", \"kind\": \"static\", \"scale\": [0, 1, 1] },\n    { \"name\": \"a\", \"kind\": \"\" }\n  ]\n}"
	_, err := ParseScene("s.scene.json", []byte(src))
	es := sourceErrors(t, err)
	want := [][2]int{{3, 26}, {3, 65}, {4, 17}, {6, 48}, {7, 15}, {7, 28}}
	if len(es) != len(want) {
		t.Fatalf("got %d errors, want %d:\n%v", len(es), len(want), err)
	}
	for i, w := range want {
		if es[i].Line != w[0] || es[i].Col != w[1] {
			t.Errorf("error %d = %v, want at %d:%d", i, es[i], w[0], w[1])
		}
	}
}

func TestCompileSceneWithoutLocator(t *testing.T) {
	src := &SceneSource{
		Camera:   CameraSource{Position: []float32{0, 1, 2}, LookAt: []float32{0, 0, 0}},
		Entities: []EntitySource{{Name: "a", Kind: "static"}, {Name: "b", Kind: "static", Parent: "a"}},
	}
	s, err := CompileScene("demo", src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Entities[1].Parent != "a" {
		t.Fatalf("parent %q", s.Entities[1].Parent)
	}
	src.Entities[0].Parent = "b"
	if _, err := CompileScene("demo", src, nil); err == nil {
		t.Fatal("cycle accepted")
	}
}

func TestSceneDocExamples(t *testing.T) {
	for i, ex := range docJSON(t, "scene.md") {
		if _, err := ParseScene("example.scene.json", []byte(ex)); err != nil {
			t.Errorf("docs/scene.md example %d: %v", i, err)
		}
	}
}
