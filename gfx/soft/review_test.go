package soft

import (
	"runtime"
	"testing"

	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// Regression tests for the findings of the rasterizer review.

func renderOnce(t *testing.T, r *Renderer, dl *gfx.DrawList, w, h int) *gfx.Framebuffer {
	t.Helper()
	fb := gfx.NewFramebuffer(w, h, false)
	if err := r.Begin(fb); err != nil {
		t.Fatal(err)
	}
	if err := r.Draw(dl); err != nil {
		t.Fatal(err)
	}
	r.End()
	return fb
}

// Near surfaces are white and far ones dark, for perspective and orthographic views.
func TestDepthModePolarity(t *testing.T) {
	for _, ortho := range []bool{false, true} {
		r := New(Options{Workers: 2})
		mesh, _ := r.CreateMesh(quadMesh(1))
		var dl gfx.DrawList
		dl.Clear, dl.Mode = true, gfx.ModeDepth
		v := perspectiveView(gmath.V3(0, 0, 5), gmath.V3(0, 0, 0), 60, 64, 64)
		if ortho {
			v.Proj = gmath.Orthographic(-4, 4, -4, 4, 0.1, 100)
		}
		view := dl.AddView(v)
		dl.Add(gfx.DrawCmd{View: view, Mesh: mesh, Model: gmath.Translate(gmath.V3(0, 0, 1)).Mul(gmath.Scaling(gmath.V3(0.3, 0.3, 1))), Color: white, State: gfx.StateOpaque, ID: 1})
		dl.Add(gfx.DrawCmd{View: view, Mesh: mesh, Model: gmath.Translate(gmath.V3(0, 0, -3)).Mul(gmath.Scaling(gmath.V3(3, 3, 1))), Color: white, State: gfx.StateOpaque, ID: 2})
		fb := renderOnce(t, r, &dl, 64, 64)
		r.Close()
		n, f := 32*64+32, 16*64+16
		near, far := fb.Color[n]&0xff, fb.Color[f]&0xff
		if fb.ID[n] != 1 || fb.ID[f] != 2 || near != 255 || far >= near {
			t.Errorf("ortho=%v: near gray %d (id %d), far gray %d (id %d)", ortho, near, fb.ID[n], far, fb.ID[f])
		}
	}
}

// ModeNormals hatches back-facing triangles and normals that point away from the viewer.
func TestNormalsModeFlagsFlippedGeometry(t *testing.T) {
	r := New(Options{Workers: 1})
	defer r.Close()
	good := quadMesh(1)
	flippedNormals := quadMesh(1)
	for i := range flippedNormals.Vertices {
		flippedNormals.Vertices[i].Normal = gmath.V3(0, 0, -1)
	}
	insideOut := quadMesh(1)
	insideOut.Indices = []uint32{0, 2, 1, 0, 3, 2}
	count := func(m *gfx.MeshData) (flagged, plain int) {
		id, _ := r.CreateMesh(m)
		var dl gfx.DrawList
		dl.Clear, dl.Mode = true, gfx.ModeNormals
		v := dl.AddView(perspectiveView(gmath.V3(0, 0, 3), gmath.V3(0, 0, 0), 60, 48, 48))
		dl.Add(gfx.DrawCmd{View: v, Mesh: id, Model: gmath.Ident4(), Color: white, State: gfx.StateOpaque, ID: 1})
		fb := renderOnce(t, r, &dl, 48, 48)
		for i, c := range fb.Color {
			if fb.ID[i] == 0 {
				continue
			}
			if c == hatchBack || c == hatchAway || c == hatchDark {
				flagged++
			} else {
				plain++
			}
		}
		return
	}
	if f, p := count(good); f != 0 || p == 0 {
		t.Errorf("correct quad: %d flagged, %d plain", f, p)
	}
	if f, p := count(flippedNormals); f == 0 || p != 0 {
		t.Errorf("flipped normals: %d flagged, %d plain", f, p)
	}
	if f, p := count(insideOut); f == 0 || p != 0 {
		t.Errorf("inside-out quad: %d flagged, %d plain", f, p)
	}
}

// A mirroring model matrix keeps front faces visible.
func TestNegativeScaleIsNotInsideOut(t *testing.T) {
	r := New(Options{Workers: 1})
	defer r.Close()
	mesh, _ := r.CreateMesh(cubeMesh())
	pixels := func(model gmath.Mat4) int {
		var dl gfx.DrawList
		dl.Clear = true
		v := dl.AddView(perspectiveView(gmath.V3(1.5, 1.2, 2.5), gmath.V3(0, 0, 0), 50, 64, 64))
		dl.Add(gfx.DrawCmd{View: v, Mesh: mesh, Model: model, Color: white, State: gfx.StateOpaque, ID: 1})
		fb := renderOnce(t, r, &dl, 64, 64)
		// The visible faces are the near ones: their depth is below the cube center's.
		n := 0
		for i, id := range fb.ID {
			if id == 1 && fb.Depth[i] < 1 {
				n++
			}
		}
		return n
	}
	a, b := pixels(gmath.Ident4()), pixels(gmath.Scaling(gmath.V3(-1, 1, 1)))
	if a == 0 || a != b {
		t.Fatalf("identity covers %d px, mirrored %d px", a, b)
	}
}

// A kilometre-long triangle crossing the near plane has no holes after clipping.
func TestClippedHugeTriangleHasNoHoles(t *testing.T) {
	const W, H = 320, 180
	eye := []gmath.Vec3{{X: -0.627, Y: 6.5e-5, Z: 1.64}, {X: -725.5, Y: -114.7, Z: -622.4}, {X: 944.9, Y: 132.9, Z: 332.0}}
	proj := gmath.Perspective(gmath.Radians(60), float32(W)/H, 0.01, 1e4)
	cover := func(tris [][3]gmath.Vec3) int {
		r := New(Options{Workers: 2})
		defer r.Close()
		var dl gfx.DrawList
		dl.Clear = true
		v := dl.AddView(gfx.View{View: gmath.Ident4(), Proj: proj, Near: 0.01, Far: 1e4})
		var verts []gfx.Vertex
		var idx []uint32
		for _, tr := range tris {
			for _, p := range tr {
				idx = append(idx, uint32(len(verts)))
				verts = append(verts, gfx.Vertex{Pos: p})
			}
		}
		first, count := dl.AddTransient(verts, idx)
		dl.Add(gfx.DrawCmd{View: v, First: first, Count: count, Model: gmath.Ident4(), Color: white, State: gfx.PipelineState{DepthTest: true, DepthWrite: true, Cull: gfx.CullNone}, ID: 1})
		fb := renderOnce(t, r, &dl, W, H)
		n := 0
		for _, id := range fb.ID {
			if id == 1 {
				n++
			}
		}
		return n
	}
	whole := cover([][3]gmath.Vec3{{eye[0], eye[1], eye[2]}})
	// Subdivide into 16 pieces: every piece is small enough to clip accurately.
	var parts [][3]gmath.Vec3
	var split func(a, b, c gmath.Vec3, d int)
	split = func(a, b, c gmath.Vec3, d int) {
		if d == 0 {
			parts = append(parts, [3]gmath.Vec3{a, b, c})
			return
		}
		ab, bc, ca := a.Lerp(b, 0.5), b.Lerp(c, 0.5), c.Lerp(a, 0.5)
		split(a, ab, ca, d-1)
		split(ab, b, bc, d-1)
		split(ca, bc, c, d-1)
		split(ab, bc, ca, d-1)
	}
	split(eye[0], eye[1], eye[2], 2)
	sub := cover(parts)
	if whole == 0 || float64(whole) < 0.99*float64(sub) {
		t.Fatalf("whole triangle covers %d px, subdivided %d px", whole, sub)
	}
}

// Alternating target sizes does not reallocate bins.
func TestTargetSwitchDoesNotAllocate(t *testing.T) {
	r := New(Options{Workers: 4})
	defer r.Close()
	dl, big := benchScene(t, r)
	small := gfx.NewFramebuffer(320, 180, false)
	frame := func() {
		for _, fb := range []*gfx.Framebuffer{big, small} {
			r.Begin(fb)
			r.Draw(dl)
			r.End()
		}
	}
	frame()
	if n := testing.AllocsPerRun(5, frame); n != 0 {
		t.Fatalf("switching targets allocates %v times per frame", n)
	}
}

// Culled counts submitted triangles, never more.
func TestCulledNeverExceedsTriangles(t *testing.T) {
	r := New(Options{Workers: 1})
	defer r.Close()
	mesh, _ := r.CreateMesh(quadMesh(10))
	var dl gfx.DrawList
	dl.Clear = true
	v := dl.AddView(perspectiveView(gmath.V3(0, 0, 3), gmath.V3(0, 0, 0), 60, 32, 32))
	dl.Add(gfx.DrawCmd{View: v, Mesh: mesh, Model: gmath.RotateY(gmath.Pi), Color: white, State: gfx.StateOpaque})
	renderOnce(t, r, &dl, 32, 32)
	if st := r.Stats(); st.Culled != 2 || st.Triangles != 2 {
		t.Fatalf("back-facing clipped quad: %+v", st)
	}
	dl.Cmds[0].Model = gmath.Ident4()
	renderOnce(t, r, &dl, 32, 32)
	if st := r.Stats(); st.Culled != 0 || st.Drawn == 0 {
		t.Fatalf("front-facing clipped quad: %+v", st)
	}
}

// Using a closed renderer is an error, whatever the worker count.
func TestDrawAfterClose(t *testing.T) {
	for _, n := range []int{1, 4} {
		r := New(Options{Workers: n})
		fb := gfx.NewFramebuffer(8, 8, false)
		r.Begin(fb)
		r.Close()
		if err := r.Draw(&gfx.DrawList{}); err == nil {
			t.Errorf("workers=%d: Draw after Close succeeded", n)
		}
		if err := r.Begin(fb); err == nil {
			t.Errorf("workers=%d: Begin after Close succeeded", n)
		}
	}
}

// An abandoned renderer used for a single frame never panics (the cleanup must not stop
// the workers while Draw runs).
func TestAbandonedRendererOneShot(t *testing.T) {
	for i := 0; i < 20; i++ {
		func() {
			r := New(Options{Workers: 8})
			dl, fb := benchScene(t, r)
			r.Begin(fb)
			runtime.GC()
			if err := r.Draw(dl); err != nil {
				t.Fatal(err)
			}
		}()
		runtime.GC()
	}
}

// Rotating a clipped triangle's indices does not change its pixels (one mip level per
// source triangle, not per fan piece).
func TestClippedTriangleMipIsPerSourceTriangle(t *testing.T) {
	r := New(Options{Workers: 1})
	defer r.Close()
	tex, _ := r.CreateTexture(crateTexture())
	p := []gfx.Vertex{
		{Pos: gmath.V3(-50, 0, 20), Normal: gmath.Up, UV: gmath.V2(0, 0)},
		{Pos: gmath.V3(50, 0, 20), Normal: gmath.Up, UV: gmath.V2(50, 0)},
		{Pos: gmath.V3(0, 0, -80), Normal: gmath.Up, UV: gmath.V2(25, 50)},
	}
	render := func(order []uint32) []uint32 {
		var dl gfx.DrawList
		dl.Clear = true
		dl.Light = gfx.DefaultLight
		v := dl.AddView(perspectiveView(gmath.V3(0, 1, 0), gmath.V3(0, 0.3, -3), 70, 160, 120))
		first, count := dl.AddTransient(p, order)
		dl.Add(gfx.DrawCmd{View: v, First: first, Count: count, Model: gmath.Ident4(), Texture: tex, Color: white, State: gfx.PipelineState{DepthTest: true, DepthWrite: true, Cull: gfx.CullNone}})
		return append([]uint32(nil), renderOnce(t, r, &dl, 160, 120).Color...)
	}
	// Different first vertices give different fan pieces, so interpolation rounding may
	// move a few low bits; a per-piece mip level would instead show hard seams.
	a, b := render([]uint32{0, 1, 2}), render([]uint32{2, 0, 1})
	large := 0
	for i := range a {
		if channelDelta(a[i], b[i]) > 24 {
			large++
		}
	}
	if large*200 > len(a) {
		t.Fatalf("%d of %d pixels differ strongly when the triangle's first vertex changes", large, len(a))
	}
}

func channelDelta(a, b uint32) uint32 {
	var d uint32
	for s := 0; s < 32; s += 8 {
		x, y := a>>s&0xff, b>>s&0xff
		if x > y {
			d = max(d, x-y)
		} else {
			d = max(d, y-x)
		}
	}
	return d
}

// WrapClamp clamps huge coordinates to the last texel.
func TestClampHugeUV(t *testing.T) {
	img := gfx.NewImage(4, 4)
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, uint32(0xff000000|x))
		}
	}
	tex := &texture{wrap: gfx.WrapClamp, levels: []level{newLevel(img)}}
	for _, u := range []float32{1e6, 1e9, 1e10, 3e38} {
		if got := tex.nearest(0, u, 0.1) & 0xff; got != 3 {
			t.Errorf("nearest u=%g: texel %d, want 3", u, got)
		}
		if got := tex.bilinear(0, u, 0.1) & 0xff; got != 3 {
			t.Errorf("bilinear u=%g: texel %d, want 3", u, got)
		}
	}
	if got := tex.nearest(0, -1e10, 0.1) & 0xff; got != 0 {
		t.Errorf("nearest u=-1e10: texel %d, want 0", got)
	}
}

func TestIDColorsDistinct(t *testing.T) {
	seen := map[uint32]uint32{}
	for id := uint32(1); id <= 500; id++ {
		c := gfx.IDColor(id)
		if prev, dup := seen[c]; dup {
			t.Fatalf("ids %d and %d share color %08x", prev, id, c)
		}
		seen[c] = id
	}
}
