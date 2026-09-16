package sprite

import (
	"image"
	"testing"

	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

func near(a, b float32) bool { return gmath.Abs(a-b) < 1e-5 }

func TestBeginAddsOverlayView(t *testing.T) {
	var dl gfx.DrawList
	dl.AddView(gfx.View{}) // a 3D camera added first
	b := Begin(&dl, 320, 200)
	if b.View() != 1 || len(dl.Views) != 2 {
		t.Fatalf("view index %d, %d views", b.View(), len(dl.Views))
	}
	v := dl.Views[1]
	if !v.Overlay || v.Near != -1 || v.Far != 1 || v.View != gmath.Ident4() {
		t.Fatalf("view = %+v", v)
	}
	if v.Proj != gmath.Orthographic(0, 320, 200, 0, -1, 1) {
		t.Fatalf("proj = %v", v.Proj)
	}
	for _, c := range []struct{ px, py, nx, ny float32 }{
		{0, 0, -1, 1}, {320, 200, 1, -1}, {160, 100, 0, 0}, {320, 0, 1, 1},
	} {
		p := v.Proj.Project(gmath.V3(c.px, c.py, 0))
		if !near(p.X, c.nx) || !near(p.Y, c.ny) || !near(p.Z, 0) {
			t.Errorf("pixel (%v,%v) → NDC %v, want (%v,%v,0)", c.px, c.py, p, c.nx, c.ny)
		}
	}
	b.End()
	if len(dl.Cmds) != 0 || len(dl.Verts) != 0 {
		t.Fatal("empty batch emitted geometry")
	}
}

// checkCmd verifies the fields every sprite command shares.
func checkCmd(t *testing.T, dl *gfx.DrawList, i, view int) gfx.DrawCmd {
	t.Helper()
	c := dl.Cmds[i]
	if c.View != view || c.Mesh != 0 || c.Model != gmath.Ident4() || c.State != gfx.State2D ||
		!c.Unlit || c.ID != 0 || c.Cutoff != 0 {
		t.Errorf("cmd %d = %+v", i, c)
	}
	if c.State.DepthTest || c.State.DepthWrite || c.State.Blend != gfx.BlendAlpha || c.State.Cull != gfx.CullNone {
		t.Errorf("cmd %d state = %+v", i, c.State)
	}
	return c
}

func TestBatchMergesAndFlushes(t *testing.T) {
	const red, blue, white, gray = 0xFFFF0000, 0xFF0000FF, 0xFFFFFFFF, 0x80808080
	src := image.Rect(0, 0, 4, 4)
	var dl gfx.DrawList
	b := Begin(&dl, 100, 100)
	b.Rect(gmath.R(0, 0, 10, 10), red)
	b.Rect(gmath.R(10, 0, 10, 10), red)                                      // merges
	b.Image(0, 16, 16, src, gmath.R(20, 0, 10, 10), red, gfx.FilterBilinear) // untextured: merges too
	b.Rect(gmath.R(30, 0, 10, 10), blue)                                     // tint change
	b.Image(7, 16, 16, src, gmath.R(0, 20, 8, 8), white, gfx.FilterNearest)
	b.Image(7, 16, 16, src, gmath.R(8, 20, 8, 8), white, gfx.FilterNearest)   // merges
	b.Image(7, 16, 16, src, gmath.R(16, 20, 8, 8), white, gfx.FilterBilinear) // filter change
	b.Image(8, 16, 16, src, gmath.R(24, 20, 8, 8), white, gfx.FilterBilinear) // texture change
	b.Image(8, 16, 16, src, gmath.R(32, 20, 8, 8), gray, gfx.FilterBilinear)  // tint change
	b.Image(8, 16, 16, src, gmath.R(40, 20, 8, 8), gray, gfx.FilterBilinear)  // merges
	b.End()

	want := []struct {
		tex    gfx.TextureID
		tint   uint32
		filter gfx.Filter
		quads  int
	}{
		{0, red, gfx.FilterNearest, 3},
		{0, blue, gfx.FilterNearest, 1},
		{7, white, gfx.FilterNearest, 2},
		{7, white, gfx.FilterBilinear, 1},
		{8, white, gfx.FilterBilinear, 1},
		{8, gray, gfx.FilterBilinear, 2},
	}
	if len(dl.Cmds) != len(want) {
		t.Fatalf("%d commands, want %d: %+v", len(dl.Cmds), len(want), dl.Cmds)
	}
	next := 0
	for i, w := range want {
		c := checkCmd(t, &dl, i, b.View())
		if c.Texture != w.tex || c.Color != gfx.ColorVec4(w.tint) || c.Filter != w.filter ||
			c.First != next || c.Count != 6*w.quads {
			t.Errorf("cmd %d: tex %d color %v filter %v range [%d,+%d), want tex %d tint %#x filter %v range [%d,+%d)",
				i, c.Texture, c.Color, c.Filter, c.First, c.Count, w.tex, w.tint, w.filter, next, 6*w.quads)
		}
		next += 6 * w.quads
	}
	if len(dl.Indices) != next || len(dl.Verts) != 4*next/6 {
		t.Fatalf("%d indices, %d verts; want %d, %d", len(dl.Indices), len(dl.Verts), next, 4*next/6)
	}
	for _, ix := range dl.Indices {
		if int(ix) >= len(dl.Verts) {
			t.Fatalf("index %d out of range", ix)
		}
	}
}

func TestBatchKeepsOrderWithForeignCommands(t *testing.T) {
	var dl gfx.DrawList
	b := Begin(&dl, 64, 64)
	b.Rect(gmath.R(0, 0, 4, 4), 0xFFFFFFFF)
	dl.Add(gfx.DrawCmd{Mesh: 5, State: gfx.StateOpaque})
	b.Rect(gmath.R(4, 0, 4, 4), 0xFFFFFFFF) // same key, but a foreign command intervened
	dl.AddTransient([]gfx.Vertex{{}, {}, {}}, []uint32{0, 1, 2})
	b.Rect(gmath.R(8, 0, 4, 4), 0xFFFFFFFF) // foreign transient geometry intervened
	b.Rect(gmath.R(12, 0, 4, 4), 0xFFFFFFFF)
	b.End()
	if len(dl.Cmds) != 4 || dl.Cmds[1].Mesh != 5 {
		t.Fatalf("commands = %+v", dl.Cmds)
	}
	for _, i := range []int{0, 2, 3} {
		checkCmd(t, &dl, i, b.View())
	}
	if dl.Cmds[0].Count != 6 || dl.Cmds[2].Count != 6 || dl.Cmds[3].Count != 12 || dl.Cmds[3].First != 15 {
		t.Fatalf("ranges: %+v", dl.Cmds)
	}
}

func TestQuadVertices(t *testing.T) {
	var dl gfx.DrawList
	b := Begin(&dl, 640, 360)
	b.Image(3, 32, 64, image.Rect(4, 8, 12, 24), gmath.R(10, 20, 30, 40), 0xFFFFFFFF, gfx.FilterNearest)
	b.End()
	want := []gfx.Vertex{
		{Pos: gmath.V3(10, 20, 0), UV: gmath.V2(4.0/32, 8.0/64)},
		{Pos: gmath.V3(40, 20, 0), UV: gmath.V2(12.0/32, 8.0/64)},
		{Pos: gmath.V3(40, 60, 0), UV: gmath.V2(12.0/32, 24.0/64)},
		{Pos: gmath.V3(10, 60, 0), UV: gmath.V2(4.0/32, 24.0/64)},
	}
	if len(dl.Verts) != 4 {
		t.Fatalf("%d verts", len(dl.Verts))
	}
	for i, w := range want {
		w.Normal = gmath.UnitZ
		if dl.Verts[i] != w {
			t.Errorf("vert %d = %+v, want %+v", i, dl.Verts[i], w)
		}
	}
	proj := dl.Views[b.View()].Proj
	for tri := 0; tri < 2; tri++ {
		var p, q [3]gmath.Vec2
		for k := 0; k < 3; k++ {
			v := dl.Verts[dl.Indices[dl.Cmds[0].First+3*tri+k]].Pos
			p[k] = v.XY()
			q[k] = proj.Project(v).XY()
		}
		if a := p[1].Sub(p[0]).Cross(p[2].Sub(p[0])); a >= 0 {
			t.Errorf("triangle %d: signed area %v in pixel space, want < 0 (y down)", tri, a)
		}
		if a := q[1].Sub(q[0]).Cross(q[2].Sub(q[0])); a <= 0 {
			t.Errorf("triangle %d: signed area %v in NDC, want counter-clockwise (front face)", tri, a)
		}
	}
}

func TestNineSlice(t *testing.T) {
	const tw, th = 64, 64
	src := image.Rect(16, 16, 48, 40) // 32×24 texels
	inset := [4]int{4, 6, 8, 2}       // left, top, right, bottom
	sx := [4]int{16, 20, 40, 48}
	sy := [4]int{16, 22, 38, 40}

	check := func(t *testing.T, dl *gfx.DrawList, dx, dy [4]float32, cells [][2]int) {
		t.Helper()
		if len(dl.Cmds) != 1 || dl.Cmds[0].Count != 6*len(cells) || len(dl.Verts) != 4*len(cells) {
			t.Fatalf("%d cmds, %d verts, want 1 cmd with %d quads", len(dl.Cmds), len(dl.Verts), len(cells))
		}
		for k, c := range cells {
			i, j := c[0], c[1]
			tl, br := dl.Verts[4*k], dl.Verts[4*k+2]
			wantTL := gfx.Vertex{Pos: gmath.V3(dx[i], dy[j], 0), Normal: gmath.UnitZ, UV: gmath.V2(float32(sx[i])/tw, float32(sy[j])/th)}
			wantBR := gfx.Vertex{Pos: gmath.V3(dx[i+1], dy[j+1], 0), Normal: gmath.UnitZ, UV: gmath.V2(float32(sx[i+1])/tw, float32(sy[j+1])/th)}
			if tl != wantTL || br != wantBR {
				t.Errorf("slice (%d,%d): TL %+v BR %+v, want %+v %+v", i, j, tl, br, wantTL, wantBR)
			}
		}
	}
	all := [][2]int{{0, 0}, {1, 0}, {2, 0}, {0, 1}, {1, 1}, {2, 1}, {0, 2}, {1, 2}, {2, 2}}

	t.Run("regular", func(t *testing.T) {
		var dl gfx.DrawList
		b := Begin(&dl, 640, 360)
		b.NineSlice(5, tw, th, src, inset, gmath.R(100, 50, 200, 80), 0xFFFFFFFF, gfx.FilterNearest)
		b.End()
		// Corners keep their texel size; edges and center stretch.
		check(t, &dl, [4]float32{100, 104, 292, 300}, [4]float32{50, 56, 128, 130}, all)
		c := checkCmd(t, &dl, 0, b.View())
		if c.Texture != 5 || c.Filter != gfx.FilterNearest {
			t.Errorf("cmd = %+v", c)
		}
	})
	t.Run("narrow", func(t *testing.T) {
		var dl gfx.DrawList
		b := Begin(&dl, 640, 360)
		// 6 px wide < left+right = 12: corners shrink to 2 and 4 px, middle column vanishes.
		b.NineSlice(5, tw, th, src, inset, gmath.R(0, 0, 6, 40), 0xFFFFFFFF, gfx.FilterNearest)
		b.End()
		check(t, &dl, [4]float32{0, 2, 2, 6}, [4]float32{0, 6, 38, 40},
			[][2]int{{0, 0}, {2, 0}, {0, 1}, {2, 1}, {0, 2}, {2, 2}})
	})
	t.Run("tiny", func(t *testing.T) {
		var dl gfx.DrawList
		b := Begin(&dl, 640, 360)
		b.NineSlice(5, tw, th, src, inset, gmath.R(0, 0, 3, 4), 0xFFFFFFFF, gfx.FilterNearest)
		b.End()
		check(t, &dl, [4]float32{0, 1, 1, 3}, [4]float32{0, 3, 3, 4},
			[][2]int{{0, 0}, {2, 0}, {0, 2}, {2, 2}})
	})
	t.Run("insets clamped to source", func(t *testing.T) {
		var dl gfx.DrawList
		b := Begin(&dl, 640, 360)
		b.NineSlice(5, tw, th, src, [4]int{100, -3, 100, 0}, gmath.R(100, 50, 200, 80), 0xFFFFFFFF, gfx.FilterNearest)
		b.End()
		// left = 32 (whole source), right = 0, top = bottom = 0: a single slice remains.
		if len(dl.Verts) != 4 {
			t.Fatalf("%d verts, want one quad", len(dl.Verts))
		}
		want := []gfx.Vertex{
			{Pos: gmath.V3(100, 50, 0), Normal: gmath.UnitZ, UV: gmath.V2(16.0/tw, 16.0/th)},
			{Pos: gmath.V3(132, 50, 0), Normal: gmath.UnitZ, UV: gmath.V2(48.0/tw, 16.0/th)},
			{Pos: gmath.V3(132, 130, 0), Normal: gmath.UnitZ, UV: gmath.V2(48.0/tw, 40.0/th)},
			{Pos: gmath.V3(100, 130, 0), Normal: gmath.UnitZ, UV: gmath.V2(16.0/tw, 40.0/th)},
		}
		for i, w := range want {
			if dl.Verts[i] != w {
				t.Errorf("vert %d = %+v, want %+v", i, dl.Verts[i], w)
			}
		}
	})
	t.Run("empty", func(t *testing.T) {
		var dl gfx.DrawList
		b := Begin(&dl, 640, 360)
		b.NineSlice(5, tw, th, src, inset, gmath.R(10, 10, 0, 20), 0xFFFFFFFF, gfx.FilterNearest)
		b.NineSlice(5, 0, th, src, inset, gmath.R(10, 10, 20, 20), 0xFFFFFFFF, gfx.FilterNearest)
		b.End()
		if len(dl.Cmds) != 0 || len(dl.Verts) != 0 {
			t.Fatalf("empty nine-slice drew %d cmds", len(dl.Cmds))
		}
	})
}

// TestFrameDoesNotAllocate draws a HUD frame into a reused DrawList, the way a game does
// every tick: once the list has grown, Begin, drawing and End allocate nothing.
func TestFrameDoesNotAllocate(t *testing.T) {
	f := DefaultFont()
	var dl gfx.DrawList
	frame := func() {
		dl.Reset()
		b := Begin(&dl, 640, 360)
		b.Rect(gmath.R(4, 4, 200, 40), 0xC0000000)
		b.NineSlice(2, 32, 32, image.Rect(0, 0, 32, 32), [4]int{8, 8, 8, 8}, gmath.R(10, 60, 120, 80), 0xFFFFFFFF, gfx.FilterNearest)
		b.Text(f, 1, 8, 8, 2, "SCORE 12\nTICK 0340 é", 0xFFFFFFFF)
		b.Image(3, 64, 64, image.Rect(0, 0, 16, 16), gmath.R(300, 10, 32, 32), 0xFFFFFFFF, gfx.FilterBilinear)
		b.End()
	}
	frame()
	if n := testing.AllocsPerRun(20, frame); n != 0 {
		t.Fatalf("%v allocations per frame, want 0", n)
	}
}

func TestResetMidBatchStartsNewRun(t *testing.T) {
	var dl gfx.DrawList
	b := Begin(&dl, 64, 64)
	b.Rect(gmath.R(0, 0, 4, 4), 0xFFFFFFFF)
	b.Rect(gmath.R(4, 0, 4, 4), 0xFFFFFFFF)
	dl.Reset() // misuse, but must not index a stale command
	b.Rect(gmath.R(8, 0, 4, 4), 0xFFFFFFFF)
	b.End()
	if len(dl.Cmds) != 1 || dl.Cmds[0].First != 0 || dl.Cmds[0].Count != 6 {
		t.Fatalf("commands = %+v", dl.Cmds)
	}
}

func TestUseAfterEndPanics(t *testing.T) {
	var dl gfx.DrawList
	b := Begin(&dl, 8, 8)
	b.End()
	defer func() {
		if recover() == nil {
			t.Fatal("drawing after End did not panic")
		}
	}()
	b.Rect(gmath.R(0, 0, 1, 1), 0xFFFFFFFF)
}
