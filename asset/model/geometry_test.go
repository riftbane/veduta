package model

import (
	"fmt"
	"math"
	"testing"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gmath"
)

// tris returns the triangle positions of part i (every part when i < 0).
func tris(m *asset.Model, i int) [][3]gmath.Vec3 {
	first, count := 0, len(m.Mesh.Indices)
	if i >= 0 {
		first, count = m.Mesh.Parts[i].First, m.Mesh.Parts[i].Count
	}
	out := make([][3]gmath.Vec3, 0, count/3)
	for k := first; k < first+count; k += 3 {
		ix := m.Mesh.Indices[k : k+3]
		out = append(out, [3]gmath.Vec3{m.Mesh.Vertices[ix[0]].Pos, m.Mesh.Vertices[ix[1]].Pos, m.Mesh.Vertices[ix[2]].Pos})
	}
	return out
}

// corners returns the vertices of part i's triangles, three per triangle.
func corners(m *asset.Model, i int) [][3]int {
	p := m.Mesh.Parts[i]
	var out [][3]int
	for k := p.First; k < p.First+p.Count; k += 3 {
		ix := m.Mesh.Indices[k : k+3]
		out = append(out, [3]int{int(ix[0]), int(ix[1]), int(ix[2])})
	}
	return out
}

func d3(v gmath.Vec3) [3]float64 { return [3]float64{float64(v.X), float64(v.Y), float64(v.Z)} }

func sub3(a, b [3]float64) [3]float64 { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

func cross3(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

func dot3(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

// faceNormal returns cross(b-a, c-a) in float64.
func faceNormal(t [3]gmath.Vec3) [3]float64 {
	a := d3(t[0])
	return cross3(sub3(d3(t[1]), a), sub3(d3(t[2]), a))
}

func unit3(v [3]float64) [3]float64 {
	l := math.Sqrt(dot3(v, v))
	return [3]float64{v[0] / l, v[1] / l, v[2] / l}
}

func signedVolume(ts [][3]gmath.Vec3) float64 {
	var v float64
	for _, t := range ts {
		v += dot3(d3(t[0]), cross3(d3(t[1]), d3(t[2]))) / 6
	}
	return v
}

// checkClosed verifies that ts, welded by exact position, is watertight and
// consistently wound: every undirected edge is used exactly twice, once each way.
func checkClosed(t *testing.T, ts [][3]gmath.Vec3) {
	t.Helper()
	ids := map[gmath.Vec3]int{}
	id := func(p gmath.Vec3) int {
		v, ok := ids[p]
		if !ok {
			v = len(ids)
			ids[p] = v
		}
		return v
	}
	type edge [2]int
	count := map[edge]int{}
	var all []edge
	for _, tr := range ts {
		v := [3]int{id(tr[0]), id(tr[1]), id(tr[2])}
		for k := range 3 {
			e := edge{v[k], v[(k+1)%3]}
			count[e]++
			all = append(all, e)
		}
	}
	for _, e := range all {
		if f, b := count[e], count[edge{e[1], e[0]}]; f != 1 || b != 1 {
			t.Fatalf("edge %d-%d used %d times forward and %d backward (not watertight or mixed winding)", e[0], e[1], f, b)
		}
	}
}

func checkNoDegenerate(t *testing.T, ts [][3]gmath.Vec3) {
	t.Helper()
	for i, tr := range ts {
		n := faceNormal(tr)
		if a := math.Sqrt(dot3(n, n)) / 2; !(a > 1e-9) {
			t.Fatalf("triangle %d %v is degenerate (area %g)", i, tr, a)
		}
	}
}

// checkNormalsAgree verifies that every vertex normal is unit length and on the side
// its triangle faces.
func checkNormalsAgree(t *testing.T, m *asset.Model, part int) {
	t.Helper()
	for _, c := range corners(m, part) {
		fn := faceNormal([3]gmath.Vec3{m.Mesh.Vertices[c[0]].Pos, m.Mesh.Vertices[c[1]].Pos, m.Mesh.Vertices[c[2]].Pos})
		for _, v := range c {
			n := m.Mesh.Vertices[v].Normal
			if l := n.Len(); math.Abs(float64(l)-1) > 1e-5 {
				t.Fatalf("normal %v has length %v", n, l)
			}
			if dot3(d3(n), fn) <= 0 {
				t.Fatalf("vertex normal %v disagrees with face normal %v", n, fn)
			}
		}
	}
}

// checkOutward verifies that every face of a convex part faces away from center.
func checkOutward(t *testing.T, ts [][3]gmath.Vec3, center gmath.Vec3) {
	t.Helper()
	c := d3(center)
	for i, tr := range ts {
		g := d3(tr[0].Add(tr[1]).Add(tr[2]).Scale(1.0 / 3))
		if dot3(faceNormal(tr), sub3(g, c)) <= 0 {
			t.Fatalf("triangle %d %v faces inwards", i, tr)
		}
	}
}

// ngonArea is the area of a regular n-gon with circumradius r.
func ngonArea(r float64, n int) float64 {
	return float64(n) / 2 * r * r * math.Sin(2*math.Pi/float64(n))
}

// latheVolume is the exact volume of a closed counter-clockwise profile revolved in n
// planar segments: each band is a prismatoid (Simpson's rule is exact).
func latheVolume(prof [][2]float64, n int) float64 {
	var v float64
	for i := 0; i+1 < len(prof); i++ {
		a, b := prof[i], prof[i+1]
		v += (b[1] - a[1]) / 6 * (ngonArea(a[0], n) + ngonArea(b[0], n) + 4*ngonArea((a[0]+b[0])/2, n))
	}
	return v
}

func sphereProf(r float64, rings int) [][2]float64 {
	var p [][2]float64
	for k := 0; k <= rings; k++ {
		a := math.Pi * float64(k) / float64(rings)
		p = append(p, [2]float64{r * math.Sin(a), -r * math.Cos(a)})
	}
	return p
}

func TestClosedShapes(t *testing.T) {
	cases := []struct {
		part   string
		volume float64
		convex bool
	}{
		{`{"shape": "box", "size": [1, 2, 3]}`, 6, true},
		{`{"shape": "box", "size": [1, 1, 1], "scale": [2, 1, 0.5], "rotation_deg": [10, 20, 30], "position": [1, 2, 3]}`, 1, true},
		{`{"shape": "cylinder", "radius": 0.5, "height": 2, "segments": 12}`, 2 * ngonArea(0.5, 12), true},
		{`{"shape": "cylinder", "radius": 0.3, "height": 1, "segments": 3, "rotation_deg": [90, 0, 0]}`, ngonArea(0.3, 3), true},
		{`{"shape": "sphere", "radius": 1}`, latheVolume(sphereProf(1, 8), 16), true},
		{`{"shape": "sphere", "radius": 0.5, "segments": 7, "rings": 3}`, latheVolume(sphereProf(0.5, 3), 7), true},
		{`{"shape": "sphere", "radius": 2, "segments": 256, "rings": 128}`, latheVolume(sphereProf(2, 128), 256), true},
		// concave, clockwise arrow (winding normalized)
		{`{"shape": "extrude", "profile": [[-1, 0.2], [0.2, 0.2], [0.2, 0.5], [1, 0], [0.2, -0.5], [0.2, -0.2], [-1, -0.2]], "depth": 0.4}`,
			0.4 * (1.2*0.4 + 0.8*1.0/2), false},
		// L shape with a collinear point on the bottom edge
		{`{"shape": "extrude", "profile": [[0, 0], [1, 0], [2, 0], [2, 1], [1, 1], [1, 2], [0, 2]], "depth": 0.5}`, 1.5, false},
		// comb: many reflex vertices
		{`{"shape": "extrude", "profile": [[0, 0], [5, 0], [5, 2], [4, 2], [4, 1], [3, 1], [3, 2], [2, 2], [2, 1], [1, 1], [1, 2], [0, 2]], "depth": 1}`, 8, false},
		{`{"shape": "lathe", "profile": [[0, 0], [0.5, 0], [0.4, 1], [0, 1.2]]}`, latheVolume([][2]float64{{0, 0}, {0.5, 0}, {0.4, 1}, {0, 1.2}}, 16), false},
		// the same profile listed top to bottom (clockwise): orientation normalized
		{`{"shape": "lathe", "profile": [[0, 1.2], [0.4, 1], [0.5, 0], [0, 0]]}`, latheVolume([][2]float64{{0, 0}, {0.5, 0}, {0.4, 1}, {0, 1.2}}, 16), false},
		// a ring (closed loop, first point repeated)
		{`{"shape": "lathe", "profile": [[1, 0], [1.5, 0], [1.5, 1], [1, 1], [1, 0]], "segments": 8}`,
			latheVolume([][2]float64{{1, 0}, {1.5, 0}, {1.5, 1}, {1, 1}, {1, 0}}, 8), false},
		// negative scale (one and three axes) keeps the part outward facing
		{`{"shape": "box", "size": [1, 2, 3], "scale": [-1, 1, 1], "rotation_deg": [0, 30, 0]}`, 6, true},
		{`{"shape": "box", "size": [1, 2, 3], "scale": [-1, -1, 1]}`, 6, true},
		{`{"shape": "cylinder", "radius": 0.5, "height": 1, "segments": 9, "scale": [-1, -2, -1]}`, 2 * ngonArea(0.5, 9), true},
		{`{"shape": "extrude", "profile": [[0, 0], [1, 0], [2, 0], [2, 1], [1, 1], [1, 2], [0, 2]], "depth": 0.5, "scale": [1, 1, -2]}`, 3, false},
	}
	for i, c := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			m := compile(t, model(c.part))
			ts := tris(m, 0)
			checkNoDegenerate(t, ts)
			checkClosed(t, ts)
			checkNormalsAgree(t, m, 0)
			if v := signedVolume(ts); math.Abs(v-c.volume) > 1e-5*c.volume {
				t.Errorf("%s: volume %.7f, want %.7f", c.part, v, c.volume)
			}
			if c.convex {
				checkOutward(t, ts, m.Mesh.Bounds.Center())
			}
		})
	}
}

func TestOpenShapes(t *testing.T) {
	// A plane faces +Y.
	m := compile(t, model(`{"shape": "plane", "size": [2, 3]}`))
	for _, tr := range tris(m, 0) {
		if n := unit3(faceNormal(tr)); n != [3]float64{0, 1, 0} {
			t.Fatalf("plane normal %v", n)
		}
	}
	if b := m.Mesh.Bounds; b.Min != gmath.V3(-1, 0, -1.5) || b.Max != gmath.V3(1, 0, 1.5) {
		t.Fatalf("plane bounds %v", b)
	}
	// An open lathe listed bottom to top faces away from the axis.
	m = compile(t, model(`{"shape": "lathe", "profile": [[1, 0], [1.2, 1], [0.8, 2]], "segments": 10}`))
	checkNoDegenerate(t, tris(m, 0))
	checkNormalsAgree(t, m, 0)
	for _, tr := range tris(m, 0) {
		g := tr[0].Add(tr[1]).Add(tr[2])
		if dot3(faceNormal(tr), [3]float64{float64(g.X), 0, float64(g.Z)}) <= 0 {
			t.Fatalf("open lathe triangle %v faces the axis", tr)
		}
	}
}

func TestFlipNormals(t *testing.T) {
	plain := compile(t, model(`{"shape": "sphere", "radius": 1, "position": [1, 0, 0]}`))
	flip := compile(t, model(`{"shape": "sphere", "radius": 1, "position": [1, 0, 0], "flip_normals": true}`))
	vp, vf := signedVolume(tris(plain, 0)), signedVolume(tris(flip, 0))
	if !(vp > 0) || math.Abs(vp+vf) > 1e-6 {
		t.Fatalf("volumes %v, %v", vp, vf)
	}
	checkClosed(t, tris(flip, 0))
	checkNormalsAgree(t, flip, 0) // vertex normals follow the reversed faces: inwards
	for _, v := range flip.Mesh.Vertices {
		if v.Normal.Dot(v.Pos.Sub(gmath.V3(1, 0, 0))) >= 0 {
			t.Fatalf("flipped normal %v at %v points outwards", v.Normal, v.Pos)
		}
	}
	if !flip.Parts[0].FlipNormals || plain.Parts[0].FlipNormals {
		t.Fatal("PartInfo.FlipNormals")
	}
	// UVs are unchanged by flipping.
	if len(plain.Mesh.Vertices) != len(flip.Mesh.Vertices) {
		t.Fatalf("vertex counts %d, %d", len(plain.Mesh.Vertices), len(flip.Mesh.Vertices))
	}
}

func TestMirror(t *testing.T) {
	m := compile(t, model(`
		{"shape": "box", "size": [0.5, 0.3, 0.2], "position": [1, 0.5, 0.2], "rotation_deg": [10, 20, 30], "material": "wood"},
		{"shape": "mirror", "axis": "x", "of": 0},
		{"shape": "mirror", "axis": "z", "of": 1, "material": "steel"},
		{"shape": "cylinder", "radius": 0.2, "height": 0.5, "position": [0.3, 1, 0], "flip_normals": true},
		{"shape": "mirror", "axis": "y", "of": 3}`))
	reflect := func(p gmath.Vec3, axis int) gmath.Vec3 {
		switch axis {
		case 0:
			p.X = -p.X
		case 1:
			p.Y = -p.Y
		default:
			p.Z = -p.Z
		}
		return p
	}
	check := func(dst, src, axis int) {
		a, b := tris(m, src), tris(m, dst)
		if len(a) != len(b) {
			t.Fatalf("mirror %d has %d triangles, source %d", dst, len(b), len(a))
		}
		for i := range a {
			want := [3]gmath.Vec3{reflect(a[i][0], axis), reflect(a[i][2], axis), reflect(a[i][1], axis)}
			if b[i] != want {
				t.Fatalf("mirror %d triangle %d = %v, want %v", dst, i, b[i], want)
			}
		}
		if va, vb := signedVolume(a), signedVolume(b); math.Abs(va-vb) > 1e-6 {
			t.Fatalf("mirror %d volume %v, source %v", dst, vb, va)
		}
		checkClosed(t, b)
	}
	check(1, 0, 0)
	check(2, 1, 2)
	check(4, 3, 1)
	if v := signedVolume(tris(m, 1)); !(v > 0) {
		t.Fatalf("mirror of an outward part has volume %v", v)
	}
	if v := signedVolume(tris(m, 4)); !(v < 0) {
		t.Fatalf("mirror of a flipped part has volume %v", v)
	}
	checkNormalsAgree(t, m, 1)
	want := []asset.PartInfo{
		{Index: 1, Shape: "mirror", Material: "wood", UV: "box", Of: 0},
		{Index: 2, Shape: "mirror", Material: "steel", UV: "box", Of: 1},
		{Index: 4, Shape: "mirror", Material: "", UV: "cylindrical", FlipNormals: true, Of: 3},
	}
	for _, w := range want {
		p := m.Parts[w.Index]
		w.First, w.Count = p.First, p.Count
		if p != w {
			t.Errorf("part %d info %+v, want %+v", w.Index, p, w)
		}
	}
}

func TestSmoothing(t *testing.T) {
	radial := func(p gmath.Vec3) gmath.Vec3 { return gmath.V3(p.X, 0, p.Z).Normalize() }
	// A 12-segment cylinder: sides 30° apart are smooth at 30°, caps stay sharp.
	for _, c := range []struct {
		angle  string
		smooth bool
	}{{"30", true}, {"29.99", false}} {
		m := compile(t, `{"veduta": "model/1", "smooth_angle_deg": `+c.angle+`,
			"parts": [{"shape": "cylinder", "radius": 1, "height": 2, "segments": 12}]}`)
		for _, cs := range corners(m, 0) {
			fn := unit3(faceNormal([3]gmath.Vec3{m.Mesh.Vertices[cs[0]].Pos, m.Mesh.Vertices[cs[1]].Pos, m.Mesh.Vertices[cs[2]].Pos}))
			for _, i := range cs {
				v := m.Mesh.Vertices[i]
				if math.Abs(fn[1]) > 0.5 { // cap: always the flat cap normal
					if v.Normal != gmath.V3(0, float32(fn[1]), 0) {
						t.Fatalf("%s°: cap normal %v", c.angle, v.Normal)
					}
					continue
				}
				r := radial(v.Pos)
				isRadial := v.Normal.Sub(r).Len() < 1e-5
				isFace := v.Normal.Sub(gmath.V3(float32(fn[0]), float32(fn[1]), float32(fn[2]))).Len() < 1e-5
				if c.smooth && !isRadial || !c.smooth && !isFace {
					t.Fatalf("%s°: side normal %v at %v (radial %v, face %v)", c.angle, v.Normal, v.Pos, r, fn)
				}
			}
		}
	}
	// A 16×8 sphere is smooth at 30°: one normal per position, close to radial.
	m := compile(t, model(`{"shape": "sphere", "radius": 1}`))
	byPos := map[gmath.Vec3]gmath.Vec3{}
	for _, v := range m.Mesh.Vertices {
		if n, ok := byPos[v.Pos]; ok && n != v.Normal {
			t.Fatalf("sphere position %v has normals %v and %v", v.Pos, n, v.Normal)
		}
		byPos[v.Pos] = v.Normal
		if d := v.Normal.Dot(v.Pos.Normalize()); d < 0.995 {
			t.Fatalf("sphere normal %v at %v: dot with radial %v", v.Normal, v.Pos, d)
		}
	}
	// 0°: fully faceted; box: always sharp with 24 vertices.
	m = compile(t, `{"veduta": "model/1", "smooth_angle_deg": 0, "parts": [{"shape": "sphere", "radius": 1}]}`)
	for _, cs := range corners(m, 0) {
		fn := unit3(faceNormal([3]gmath.Vec3{m.Mesh.Vertices[cs[0]].Pos, m.Mesh.Vertices[cs[1]].Pos, m.Mesh.Vertices[cs[2]].Pos}))
		for _, i := range cs {
			if n := d3(m.Mesh.Vertices[i].Normal); math.Abs(dot3(n, fn)-1) > 1e-6 {
				t.Fatalf("faceted normal %v, face %v", n, fn)
			}
		}
	}
	m = compile(t, model(`{"shape": "box", "size": [1, 1, 1]}`))
	for _, v := range m.Mesh.Vertices {
		if a := v.Normal.Abs(); a.X+a.Y+a.Z != 1 {
			t.Fatalf("box normal %v", v.Normal)
		}
	}
	// Never across parts: a second part touching the first changes nothing in it.
	one := compile(t, `{"veduta": "model/1", "smooth_angle_deg": 180, "parts": [{"shape": "box", "size": [1, 1, 1]}]}`)
	two := compile(t, `{"veduta": "model/1", "smooth_angle_deg": 180, "parts": [{"shape": "box", "size": [1, 1, 1]},
		{"shape": "box", "size": [1, 1, 1], "position": [1, 0, 0]}]}`)
	for i, v := range one.Mesh.Vertices {
		if two.Mesh.Vertices[i] != v {
			t.Fatalf("vertex %d changed by a neighbouring part: %+v vs %+v", i, v, two.Mesh.Vertices[i])
		}
	}
}

func TestBoxUV(t *testing.T) {
	// Box [2, 1, 1]: every face maps its top-left corner (seen from outside) to (0, 0),
	// 1 UV unit per meter, v downwards.
	want := func(p, n gmath.Vec3) gmath.Vec2 {
		switch n {
		case gmath.V3(1, 0, 0):
			return gmath.V2(0.5-p.Z, 0.5-p.Y)
		case gmath.V3(-1, 0, 0):
			return gmath.V2(p.Z+0.5, 0.5-p.Y)
		case gmath.V3(0, 1, 0):
			return gmath.V2(p.X+1, p.Z+0.5)
		case gmath.V3(0, -1, 0):
			return gmath.V2(p.X+1, 0.5-p.Z)
		case gmath.V3(0, 0, 1):
			return gmath.V2(p.X+1, 0.5-p.Y)
		case gmath.V3(0, 0, -1):
			return gmath.V2(1-p.X, 0.5-p.Y)
		}
		t.Fatalf("unexpected normal %v", n)
		return gmath.Vec2{}
	}
	m := compile(t, model(`{"shape": "box", "size": [2, 1, 1]}`))
	for _, v := range m.Mesh.Vertices {
		if w := want(v.Pos, v.Normal); v.UV != w {
			t.Errorf("vertex %v normal %v: uv %v, want %v", v.Pos, v.Normal, v.UV, w)
		}
	}
	// The front face spans u in [0, 2], v in [0, 1] with (0, 0) at its top-left corner.
	for _, v := range m.Mesh.Vertices {
		if v.Normal == gmath.V3(0, 0, 1) && v.Pos == gmath.V3(-1, 0.5, 0.5) && v.UV != gmath.V2(0, 0) {
			t.Errorf("front top-left uv %v", v.UV)
		}
		if v.Normal == gmath.V3(0, 0, 1) && v.Pos == gmath.V3(1, -0.5, 0.5) && v.UV != gmath.V2(2, 1) {
			t.Errorf("front bottom-right uv %v", v.UV)
		}
	}
	// UVs follow the part: rotation and translation do not change them; scale does.
	moved := compile(t, model(`{"shape": "box", "size": [2, 1, 1], "position": [5, 1, 2], "rotation_deg": [30, 40, 50]}`))
	scaled := compile(t, model(`{"shape": "box", "size": [1, 1, 1], "scale": [2, 1, 1]}`))
	for i, v := range m.Mesh.Vertices {
		if moved.Mesh.Vertices[i].UV != v.UV || scaled.Mesh.Vertices[i].UV != v.UV {
			t.Fatalf("vertex %d uv %v, moved %v, scaled %v", i, v.UV, moved.Mesh.Vertices[i].UV, scaled.Mesh.Vertices[i].UV)
		}
	}
}

func TestPlanarUV(t *testing.T) {
	m := compile(t, model(`{"shape": "plane", "size": [4, 2]}`))
	for _, v := range m.Mesh.Vertices {
		if w := gmath.V2(v.Pos.X+2, v.Pos.Z+1); v.UV != w {
			t.Errorf("plane %v uv %v, want %v", v.Pos, v.UV, w)
		}
	}
	// A box thinnest along Z projects along Z like its front face.
	m = compile(t, model(`{"shape": "box", "size": [2, 1, 0.1], "uv": "planar"}`))
	for _, v := range m.Mesh.Vertices {
		if w := gmath.V2(v.Pos.X+1, 0.5-v.Pos.Y); v.UV != w {
			t.Errorf("planar box %v uv %v, want %v", v.Pos, v.UV, w)
		}
	}
}

func TestWrapUV(t *testing.T) {
	// Cylinder r = 0.5: u = (θ+π)·0.5 in [0, π] (π/2 at the front), v = 0.5 - y; caps use
	// the ±Y box rule.
	m := compile(t, model(`{"shape": "cylinder", "radius": 0.5, "height": 1, "segments": 16}`))
	step := float32(2 * math.Pi * 0.5 / 16)
	for _, cs := range corners(m, 0) {
		lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
		side := m.Mesh.Vertices[cs[0]].Normal.Y == 0
		for _, i := range cs {
			v := m.Mesh.Vertices[i]
			if !side {
				continue
			}
			lo, hi = min(lo, v.UV.X), max(hi, v.UV.X)
			if !near(v.UV.Y, 0.5-v.Pos.Y, 1e-6) || v.UV.X < -1e-6 || v.UV.X > math.Pi+1e-5 {
				t.Fatalf("side uv %v at %v", v.UV, v.Pos)
			}
			if v.Pos.X == 0 && v.Pos.Z > 0 && !near(v.UV.X, math.Pi/2, 1e-6) { // front: θ = 0
				t.Fatalf("front uv %v", v.UV)
			}
		}
		if side && hi-lo > step+1e-5 {
			t.Fatalf("side triangle spans u %v..%v (seam not unwrapped)", lo, hi)
		}
	}
	// Sphere r = 1: v = polar angle (0 at the top pole), u = (θ+π), no seam jumps.
	m = compile(t, model(`{"shape": "sphere", "radius": 1}`))
	for _, cs := range corners(m, 0) {
		lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
		for _, i := range cs {
			v := m.Mesh.Vertices[i]
			lo, hi = min(lo, v.UV.X), max(hi, v.UV.X)
			if want := float32(math.Acos(float64(v.Pos.Y) / float64(v.Pos.Len()))); !near(v.UV.Y, want, 1e-5) {
				t.Fatalf("sphere v %v at %v, want %v", v.UV.Y, v.Pos, want)
			}
		}
		if hi-lo > float32(2*math.Pi/16)+1e-5 {
			t.Fatalf("sphere triangle spans u %v..%v", lo, hi)
		}
	}
}

func TestRotationMatchesQuat(t *testing.T) {
	v := dvec{0.3, -1.2, 2.5}
	for _, a := range []gmath.Vec3{{X: 10, Y: 20, Z: 30}, {X: -90, Y: 45, Z: 180}, {X: 0, Y: 90, Z: 0}, {X: 33, Y: -170, Z: 5}} {
		got := rotationDeg(dvec3(a)).apply(v)
		want := gmath.QuatEulerDeg(a).Rotate(gmath.V3(0.3, -1.2, 2.5))
		if gmath.V3(float32(got[0]), float32(got[1]), float32(got[2])).Sub(want).Len() > 1e-5 {
			t.Errorf("rotation %v: %v, quat %v", a, got, want)
		}
	}
}

func TestEarClip(t *testing.T) {
	polys := [][]dvec2{
		{{0, 0}, {1, 0}, {0, 1}},
		{{0, 0}, {4, 0}, {4, 4}, {2, 1}, {0, 4}},
		{{0, 0}, {1, 0}, {2, 0}, {3, 0}, {3, 1}, {0, 1}},
	}
	// a 40-point star with alternating radii
	var star []dvec2
	for i := 0; i < 40; i++ {
		r := 1.0
		if i%2 == 1 {
			r = 0.35
		}
		a := 2 * math.Pi * float64(i) / 40
		star = append(star, dvec2{r * math.Cos(a), r * math.Sin(a)})
	}
	polys = append(polys, star)
	for _, p := range polys {
		ts := earClip(p)
		if len(ts) != len(p)-2 {
			t.Fatalf("%d triangles for %d points", len(ts), len(p))
		}
		var sum float64
		for _, tr := range ts {
			a := cross2(p[tr[0]], p[tr[1]], p[tr[2]])
			if !(a > 0) {
				t.Fatalf("triangle %v has area %v", tr, a)
			}
			sum += a
		}
		if want := signedArea(p); math.Abs(sum-want) > 1e-9 {
			t.Fatalf("triangles cover %v, polygon %v", sum, want)
		}
	}
}
