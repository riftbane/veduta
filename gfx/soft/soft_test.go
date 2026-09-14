package soft

import (
	"testing"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/internal/golden"
)

// cubeScene renders the lit textured cube used by the golden tests.
func cubeScene(t testing.TB, r *Renderer, w, h int, mode gfx.RenderMode) *gfx.Framebuffer {
	t.Helper()
	mesh, err := r.CreateMesh(cubeMesh())
	if err != nil {
		t.Fatal(err)
	}
	tex, err := r.CreateTexture(crateTexture())
	if err != nil {
		t.Fatal(err)
	}
	fb := gfx.NewFramebuffer(w, h, false)
	var dl gfx.DrawList
	dl.Clear = true
	dl.ClearColor = gfx.RGBA(0x20, 0x28, 0x30, 0xff)
	dl.Mode = mode
	dl.Light = gfx.DefaultLight
	v := dl.AddView(perspectiveView(gmath.V3(1.6, 1.3, 2.2), gmath.V3(0, 0, 0), 50, w, h))
	dl.Add(gfx.DrawCmd{
		View: v, Mesh: mesh, Model: gmath.RotateY(gmath.Radians(20)),
		Texture: tex, Color: white, State: gfx.StateOpaque, ID: 7,
	})
	if mode == gfx.ModeCollision {
		b := gmath.AABB{Min: gmath.V3(-0.5, -0.5, -0.5), Max: gmath.V3(0.5, 0.5, 0.5)}.Transform(gmath.RotateY(gmath.Radians(20)))
		addBox(&dl, v, b, 0xff40ff40)
	}
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

func addBox(dl *gfx.DrawList, view int, b gmath.AABB, color uint32) {
	c := b.Corners()
	edges := [12][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}, {0, 2}, {1, 3}, {4, 6}, {5, 7}, {0, 4}, {1, 5}, {2, 6}, {3, 7}}
	for _, e := range edges {
		dl.AddLine(gfx.DebugLine{A: c[e[0]], B: c[e[1]], Color: color, View: view})
	}
}

func TestGoldenLitTexturedCube(t *testing.T) {
	r := New(Options{})
	defer r.Close()
	fb := cubeScene(t, r, 320, 240, gfx.ModeColor)
	golden.Image(t, "soft_cube_lit_textured", fb.Image())

	// The cube is entity 7: its pixels must be in the ID buffer and nowhere else.
	covered := 0
	for i, id := range fb.ID {
		switch id {
		case 7:
			covered++
			if fb.Depth[i] >= 1 {
				t.Fatal("covered pixel without depth")
			}
		case 0:
		default:
			t.Fatalf("unexpected id %d", id)
		}
	}
	if covered < 5000 {
		t.Fatalf("cube covers only %d pixels", covered)
	}
}

func TestGoldenModes(t *testing.T) {
	for _, mode := range gfx.RenderModes {
		if mode == gfx.ModeColor {
			continue
		}
		t.Run(mode.String(), func(t *testing.T) {
			r := New(Options{})
			defer r.Close()
			fb := cubeScene(t, r, 160, 120, mode)
			golden.Image(t, "soft_cube_"+mode.String(), fb.Image())
		})
	}
}

// randomScene draws many overlapping random triangles with all blend modes.
func randomScene(t testing.TB, r *Renderer, w, h int) *gfx.Framebuffer {
	tex, err := r.CreateTexture(crateTexture())
	if err != nil {
		t.Fatal(err)
	}
	var dl gfx.DrawList
	dl.Clear = true
	dl.ClearColor = 0xff101010
	dl.Light = gfx.DefaultLight
	v := dl.AddView(perspectiveView(gmath.V3(0, 0, 6), gmath.V3(0, 0, 0), 60, w, h))
	g := lcg(1)
	for c := 0; c < 40; c++ {
		var verts []gfx.Vertex
		var idx []uint32
		for k := 0; k < 30; k++ {
			for j := 0; j < 3; j++ {
				// Products (g.float divides by 2^24) are rounded explicitly so arm64 cannot
				// fuse them into a multiply-add and move the golden geometry.
				p := gmath.V3(float32(g.float()*8)-4, float32(g.float()*6)-3, float32(g.float()*8)-6)
				verts = append(verts, gfx.Vertex{Pos: p, Normal: gmath.V3(float32(g.float())-0.5, float32(g.float())-0.5, 1).Normalize(), UV: gmath.V2(g.float()*3, g.float()*3)})
				idx = append(idx, uint32(len(idx)))
			}
		}
		first, count := dl.AddTransient(verts, idx)
		st := gfx.StateOpaque
		st.Cull = gfx.CullNone
		switch c % 4 {
		case 1:
			st = gfx.StateTransparent
			st.Cull = gfx.CullNone
		case 2:
			st.Blend = gfx.BlendAdd
			st.DepthWrite = false
		}
		dl.Add(gfx.DrawCmd{View: v, First: first, Count: count, Model: gmath.Ident4(), Texture: tex,
			Color: gmath.V4(g.float(), g.float(), g.float(), 0.3+float32(0.7*g.float())), State: st, ID: uint32(c + 1),
			Filter: gfx.Filter(c % 2)})
	}
	fb := gfx.NewFramebuffer(w, h, true)
	if err := r.Begin(fb); err != nil {
		t.Fatal(err)
	}
	if err := r.Draw(&dl); err != nil {
		t.Fatal(err)
	}
	r.End()
	return fb
}

func TestWorkerCountDoesNotChangeOutput(t *testing.T) {
	ref := New(Options{Workers: 1})
	defer ref.Close()
	want := randomScene(t, ref, 333, 211) // odd sizes exercise partial tiles
	for _, n := range []int{2, 3, 8} {
		r := New(Options{Workers: n})
		got := randomScene(t, r, 333, 211)
		r.Close()
		for i := range want.Color {
			if got.Color[i] != want.Color[i] || got.Depth[i] != want.Depth[i] || got.ID[i] != want.ID[i] || got.Normal[i] != want.Normal[i] {
				t.Fatalf("workers=%d: pixel %d differs", n, i)
			}
		}
	}
}

// TestFillRuleWatertight renders a jittered triangulated grid in overdraw mode: every
// pixel inside the grid must be covered exactly once (no cracks, no double hits).
// TestTinyTrianglesInterpolate draws triangles of a pixel or less, flat in depth and in
// color, and requires every pixel they cover to carry exactly that depth and color. The
// top-left fill rule biases the edge functions by one 1/256 px² unit; weighting vertices
// with the biased values made the weights sum to less than one, which for a sub-pixel
// triangle pulled its depth toward the camera (13 m reported as 6 m at a sphere's
// silhouette) and darkened its color.
func TestTinyTrianglesInterpolate(t *testing.T) {
	const W, H = 64, 48
	for _, fast := range []bool{false, true} {
		r := New(Options{Workers: 2})
		g := lcg(7)
		var verts []gfx.Vertex
		var idx []uint32
		for n := 0; n < 400; n++ {
			// Products are rounded explicitly so arm64 cannot fuse them into a multiply-add.
			x := 2 + float32(float32(g.float())*(W-4))
			y := 2 + float32(float32(g.float())*(H-4))
			base := uint32(len(verts))
			for k := 0; k < 3; k++ {
				dx := float32((float32(g.float()) - 0.5) * 1.6)
				dy := float32((float32(g.float()) - 0.5) * 1.6)
				verts = append(verts, gfx.Vertex{Pos: gmath.V3(x+dx, y+dy, 0.25), UV: gmath.V2(0.5, 0.5)})
			}
			idx = append(idx, base, base+1, base+2)
		}
		var dl gfx.DrawList
		dl.Clear = true
		v := dl.AddView(orthoPixelView(W, H))
		first, count := dl.AddTransient(verts, idx)
		cmd := gfx.DrawCmd{View: v, First: first, Count: count, Model: gmath.Ident4(), Color: gmath.V4(0.8, 0.6, 0.4, 1),
			Unlit: true, State: gfx.PipelineState{DepthTest: true, DepthWrite: true, Cull: gfx.CullNone}, ID: 3}
		if fast {
			img := gfx.NewImage(4, 4)
			for i := range img.Pix {
				img.Pix[i] = 0xffffffff
			}
			tex, err := r.CreateTexture(&gfx.TextureData{Levels: []*gfx.Image{img}})
			if err != nil {
				t.Fatal(err)
			}
			cmd.Texture, cmd.State = tex, gfx.StateOpaque
			cmd.State.Cull = gfx.CullNone
		}
		dl.Add(cmd)
		fb := gfx.NewFramebuffer(W, H, false)
		r.Begin(fb)
		if err := r.Draw(&dl); err != nil {
			t.Fatal(err)
		}
		r.End()
		r.Close()
		// The plane's depth: z = 0.25 in an orthographic view from -1 to 1.
		want := float32(0.5 - 0.25*0.5)
		covered, color := 0, uint32(0)
		for i, id := range fb.ID {
			if id != 3 {
				continue
			}
			if covered++; covered == 1 {
				color = fb.Color[i]
			}
			if d := fb.Depth[i] - want; d > 1e-6 || d < -1e-6 {
				t.Fatalf("fast=%v: pixel %d depth %v, want %v (the flat plane's depth everywhere)", fast, i, fb.Depth[i], want)
			}
			if fb.Color[i] != color {
				t.Fatalf("fast=%v: pixel %d color %#08x differs from %#08x: a flat color darkened", fast, i, fb.Color[i], color)
			}
		}
		if covered < 50 {
			t.Fatalf("fast=%v: only %d pixels covered; the test draws too little", fast, covered)
		}
	}
}

func TestFillRuleWatertight(t *testing.T) {
	const W, H, N = 200, 160, 12
	r := New(Options{Workers: 4})
	defer r.Close()
	g := lcg(42)
	var verts []gfx.Vertex
	for j := 0; j <= N; j++ {
		for i := 0; i <= N; i++ {
			// Products are rounded explicitly so arm64 cannot fuse them into a multiply-add.
			x := 20 + float32(float32(i)*(160.0/N))
			y := 10 + float32(float32(j)*(140.0/N))
			if i > 0 && i < N && j > 0 && j < N {
				x += float32((float32(g.float()) - 0.5) * 9)
				y += float32((float32(g.float()) - 0.5) * 9)
			}
			verts = append(verts, gfx.Vertex{Pos: gmath.V3(x, y, 0)})
		}
	}
	var idx []uint32
	for j := 0; j < N; j++ {
		for i := 0; i < N; i++ {
			a := uint32(j*(N+1) + i)
			b, c, d := a+1, a+N+1, a+N+2
			if (i+j)%2 == 0 {
				idx = append(idx, a, c, b, b, c, d)
			} else {
				idx = append(idx, a, c, d, a, d, b)
			}
		}
	}
	var dl gfx.DrawList
	dl.Clear = true
	dl.Mode = gfx.ModeOverdraw
	v := dl.AddView(orthoPixelView(W, H))
	first, count := dl.AddTransient(verts, idx)
	dl.Add(gfx.DrawCmd{View: v, First: first, Count: count, Model: gmath.Ident4(), Color: white, State: gfx.PipelineState{Cull: gfx.CullNone}})
	fb := gfx.NewFramebuffer(W, H, false)
	r.Begin(fb)
	if err := r.Draw(&dl); err != nil {
		t.Fatal(err)
	}
	r.End()
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			n := r.overdraw[y*W+x]
			inside := x >= 20 && x < 180 && y >= 10 && y < 150
			if inside && n != 1 || !inside && n != 0 {
				t.Fatalf("pixel (%d,%d) covered %d times (inside=%v)", x, y, n, inside)
			}
		}
	}
}

func TestTopLeftRule(t *testing.T) {
	// A 4×4 pixel-aligned square split along its diagonal: exactly 16 pixels, each once.
	r := New(Options{Workers: 1})
	defer r.Close()
	var dl gfx.DrawList
	dl.Clear = true
	dl.Mode = gfx.ModeOverdraw
	v := dl.AddView(orthoPixelView(8, 8))
	verts := []gfx.Vertex{{Pos: gmath.V3(2, 2, 0)}, {Pos: gmath.V3(6, 2, 0)}, {Pos: gmath.V3(6, 6, 0)}, {Pos: gmath.V3(2, 6, 0)}}
	first, count := dl.AddTransient(verts, []uint32{0, 1, 2, 0, 2, 3})
	dl.Add(gfx.DrawCmd{View: v, First: first, Count: count, Model: gmath.Ident4(), Color: white, State: gfx.PipelineState{Cull: gfx.CullNone}})
	fb := gfx.NewFramebuffer(8, 8, false)
	r.Begin(fb)
	r.Draw(&dl)
	r.End()
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			want := uint16(0)
			if x >= 2 && x < 6 && y >= 2 && y < 6 {
				want = 1
			}
			if got := r.overdraw[y*8+x]; got != want {
				t.Errorf("pixel (%d,%d): %d fragments, want %d", x, y, got, want)
			}
		}
	}
}

func TestBackfaceCulling(t *testing.T) {
	r := New(Options{Workers: 1})
	defer r.Close()
	mesh, _ := r.CreateMesh(quadMesh(1))
	render := func(model gmath.Mat4, cull gfx.CullMode) int {
		var dl gfx.DrawList
		dl.Clear = true
		v := dl.AddView(perspectiveView(gmath.V3(0, 0, 3), gmath.V3(0, 0, 0), 60, 32, 32))
		dl.Add(gfx.DrawCmd{View: v, Mesh: mesh, Model: model, Color: white, State: gfx.PipelineState{DepthTest: true, DepthWrite: true, Cull: cull}, ID: 1})
		fb := gfx.NewFramebuffer(32, 32, false)
		r.Begin(fb)
		r.Draw(&dl)
		r.End()
		n := 0
		for _, id := range fb.ID {
			if id == 1 {
				n++
			}
		}
		return n
	}
	front := render(gmath.Ident4(), gfx.CullBack)
	back := render(gmath.RotateY(gmath.Pi), gfx.CullBack)
	backNone := render(gmath.RotateY(gmath.Pi), gfx.CullNone)
	if front == 0 || back != 0 || backNone != front {
		t.Fatalf("front=%d back(culled)=%d back(cull none)=%d", front, back, backNone)
	}
}

func TestDepthTestOrderIndependent(t *testing.T) {
	r := New(Options{Workers: 2})
	defer r.Close()
	mesh, _ := r.CreateMesh(quadMesh(1))
	render := func(nearFirst bool) *gfx.Framebuffer {
		var dl gfx.DrawList
		dl.Clear = true
		dl.Light = gfx.Light{Ambient: gmath.One3}
		v := dl.AddView(perspectiveView(gmath.V3(0, 0, 3), gmath.V3(0, 0, 0), 60, 64, 64))
		near := gfx.DrawCmd{View: v, Mesh: mesh, Model: gmath.Translate(gmath.V3(0.3, 0, 0.5)), Color: gmath.V4(1, 0, 0, 1), State: gfx.StateOpaque, ID: 1}
		far := gfx.DrawCmd{View: v, Mesh: mesh, Model: gmath.Translate(gmath.V3(-0.3, 0, 0)), Color: gmath.V4(0, 0, 1, 1), State: gfx.StateOpaque, ID: 2}
		if nearFirst {
			dl.Add(near)
			dl.Add(far)
		} else {
			dl.Add(far)
			dl.Add(near)
		}
		fb := gfx.NewFramebuffer(64, 64, false)
		r.Begin(fb)
		r.Draw(&dl)
		r.End()
		return fb
	}
	a, b := render(true), render(false)
	for i := range a.Color {
		if a.Color[i] != b.Color[i] || a.ID[i] != b.ID[i] {
			t.Fatalf("pixel %d depends on draw order", i)
		}
	}
	if a.ID[32*64+36] != 1 {
		t.Fatalf("center-right pixel should show the near quad, got id %d", a.ID[32*64+36])
	}
}

func TestAlphaBlendExact(t *testing.T) {
	r := New(Options{Workers: 1})
	defer r.Close()
	var dl gfx.DrawList
	dl.Clear = true
	dl.ClearColor = gfx.RGBA(0, 0, 200, 255)
	v := dl.AddView(orthoPixelView(4, 4))
	verts := []gfx.Vertex{{Pos: gmath.V3(0, 0, 0)}, {Pos: gmath.V3(4, 0, 0)}, {Pos: gmath.V3(4, 4, 0)}, {Pos: gmath.V3(0, 4, 0)}}
	first, count := dl.AddTransient(verts, []uint32{0, 1, 2, 0, 2, 3})
	dl.Add(gfx.DrawCmd{View: v, First: first, Count: count, Model: gmath.Ident4(), Color: gmath.V4(1, 0, 0, 0.5), State: gfx.State2D, Unlit: true})
	fb := gfx.NewFramebuffer(4, 4, false)
	r.Begin(fb)
	r.Draw(&dl)
	r.End()
	// a = round(0.5*255) = 128: r = (255*128+127)/255 = 128, b = (200*127+127)/255 = 100.
	if got, want := fb.Color[5], gfx.RGBA(128, 0, 100, 255); got != want {
		t.Fatalf("blended pixel = %08x, want %08x", got, want)
	}
}

func TestNearPlaneClipping(t *testing.T) {
	// A ground plane that extends behind the camera must be clipped, not culled or NaN.
	r := New(Options{})
	defer r.Close()
	big := &gfx.MeshData{
		Vertices: []gfx.Vertex{
			{Pos: gmath.V3(-50, 0, -50), Normal: gmath.Up, UV: gmath.V2(0, 0)},
			{Pos: gmath.V3(-50, 0, 50), Normal: gmath.Up, UV: gmath.V2(0, 50)},
			{Pos: gmath.V3(50, 0, 50), Normal: gmath.Up, UV: gmath.V2(50, 50)},
			{Pos: gmath.V3(50, 0, -50), Normal: gmath.Up, UV: gmath.V2(50, 0)},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
	mesh, _ := r.CreateMesh(big)
	tex, _ := r.CreateTexture(crateTexture())
	var dl gfx.DrawList
	dl.Clear = true
	dl.Light = gfx.DefaultLight
	v := dl.AddView(perspectiveView(gmath.V3(0, 1, 0), gmath.V3(0, 0.5, -3), 70, 96, 64))
	dl.Add(gfx.DrawCmd{View: v, Mesh: mesh, Model: gmath.Ident4(), Texture: tex, Color: white, State: gfx.StateOpaque, ID: 3})
	fb := gfx.NewFramebuffer(96, 64, false)
	r.Begin(fb)
	if err := r.Draw(&dl); err != nil {
		t.Fatal(err)
	}
	r.End()
	st := r.Stats()
	if st.Clipped == 0 {
		t.Fatalf("expected clipping, stats %+v", st)
	}
	// Bottom row sees the ground, top row sees sky.
	for x := 0; x < 96; x++ {
		if fb.ID[63*96+x] != 3 {
			t.Fatalf("bottom row pixel %d not ground", x)
		}
		if fb.ID[x] != 0 {
			t.Fatalf("top row pixel %d is ground", x)
		}
	}
}

func TestErrors(t *testing.T) {
	r := New(Options{Workers: 1})
	defer r.Close()
	if err := r.Draw(&gfx.DrawList{}); err == nil {
		t.Error("Draw without Begin should fail")
	}
	if _, err := r.CreateMesh(&gfx.MeshData{Vertices: make([]gfx.Vertex, 2), Indices: []uint32{0, 1, 2}}); err == nil {
		t.Error("out-of-range index should fail")
	}
	bad := &gfx.TextureData{Levels: []*gfx.Image{gfx.NewImage(4, 4), gfx.NewImage(3, 2)}}
	if _, err := r.CreateTexture(bad); err == nil {
		t.Error("bad mip size should fail")
	}
	fb := gfx.NewFramebuffer(4, 4, false)
	r.Begin(fb)
	dl := gfx.DrawList{Cmds: []gfx.DrawCmd{{View: 3}}}
	if err := r.Draw(&dl); err == nil {
		t.Error("bad view index should fail")
	}
}

// benchScene builds 10k textured, lit triangles covering a 320×240 frame, the console
// panel: a 50×100 quad grid seen in perspective. That is several times the triangles a
// level for a Raspberry Pi Zero 2 W should submit, so it measures the rasterizer under
// load rather than a typical frame.
func benchScene(b testing.TB, r *Renderer) (*gfx.DrawList, *gfx.Framebuffer) {
	const nx, nz = 100, 50
	mesh := &gfx.MeshData{}
	for j := 0; j <= nz; j++ {
		for i := 0; i <= nx; i++ {
			x, z := float32(float32(i)/nx*16)-8, float32(float32(j)/nz*8)-8 // rounded: no fused multiply-sub
			y := 0.3 * gmath.Sin(x*0.9) * gmath.Cos(z*1.3)
			mesh.Vertices = append(mesh.Vertices, gfx.Vertex{Pos: gmath.V3(x, y, z), Normal: gmath.V3(-0.27*gmath.Cos(x*0.9), 1, 0.39*gmath.Sin(z*1.3)).Normalize(), UV: gmath.V2(float32(i)/4, float32(j)/4)})
		}
	}
	for j := 0; j < nz; j++ {
		for i := 0; i < nx; i++ {
			a := uint32(j*(nx+1) + i)
			mesh.Indices = append(mesh.Indices, a, a+nx+1, a+1, a+1, a+nx+1, a+nx+2)
		}
	}
	id, err := r.CreateMesh(mesh)
	if err != nil {
		b.Fatal(err)
	}
	tex, _ := r.CreateTexture(crateTexture())
	dl := &gfx.DrawList{Clear: true, ClearColor: 0xff202830, Light: gfx.DefaultLight}
	v := dl.AddView(perspectiveView(gmath.V3(0, 3.2, 1.5), gmath.V3(0, 0, -3.5), 60, 320, 240))
	dl.Add(gfx.DrawCmd{View: v, Mesh: id, Model: gmath.Ident4(), Texture: tex, Color: white, State: gfx.StateOpaque, ID: 1})
	return dl, gfx.NewFramebuffer(320, 240, false)
}

func TestDrawDoesNotAllocate(t *testing.T) {
	r := New(Options{Workers: 4})
	defer r.Close()
	dl, fb := benchScene(t, r)
	frame := func() {
		r.Begin(fb)
		if err := r.Draw(dl); err != nil {
			t.Fatal(err)
		}
		r.End()
	}
	frame() // warm-up grows scratch buffers
	if n := testing.AllocsPerRun(10, frame); n != 0 {
		t.Fatalf("Draw allocates %v times per frame in steady state", n)
	}
	if st := r.Stats(); st.Triangles != 10000 {
		t.Fatalf("bench scene has %d triangles, want 10000", st.Triangles)
	}
}

func BenchmarkDraw10kTriangles320x240(b *testing.B) {
	r := New(Options{})
	defer r.Close()
	dl, fb := benchScene(b, r)
	r.Begin(fb)
	r.Draw(dl)
	r.End()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Begin(fb)
		r.Draw(dl)
		r.End()
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/1000/float64(b.N), "ms/frame")
}
