package model

import (
	"math"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// smoothTolDeg is added to smooth_angle_deg so faces exactly at the threshold (a
// 12-segment cylinder at 30°) are smoothed despite rounding.
const smoothTolDeg = 1e-3

// corner is a final (model space, before the pivot) triangle corner.
type corner struct {
	pos gmath.Vec3
	uv  gmath.Vec2
}

// tri is a final triangle, counter-clockwise seen from the side it faces.
type tri [3]corner

// build turns a validated spec into the compiled model.
func build(name string, s *spec) *asset.Model {
	geo := make([][]tri, len(s.parts))
	for i := range s.parts {
		ps := &s.parts[i]
		if ps.shape == "mirror" {
			geo[i] = mirrorTris(geo[ps.of], ps.axis)
		} else {
			geo[i] = partTris(ps)
		}
	}

	m := &asset.Model{
		Name:           name,
		SmoothAngleDeg: s.smoothDeg,
		Symmetry:       s.symmetry,
		TriangleBudget: s.budget,
		Pivot:          s.pivot,
	}
	cosLim := gmath.Cos64((float64(s.smoothDeg) + smoothTolDeg) * gmath.Deg2Rad)
	for i, tris := range geo {
		ps := &s.parts[i]
		mat := indexOf(m.Materials, ps.material)
		if mat < 0 {
			mat = len(m.Materials)
			m.Materials = append(m.Materials, ps.material)
		}
		first := len(m.Mesh.Indices)
		appendPart(&m.Mesh, tris, smoothNormals(tris, cosLim))
		count := len(m.Mesh.Indices) - first
		m.Mesh.Parts = append(m.Mesh.Parts, gfx.MeshPart{First: first, Count: count, Material: mat})
		m.Parts = append(m.Parts, asset.PartInfo{
			Index: i, Shape: ps.shape, First: first, Count: count, Material: ps.material,
			UV: ps.uv, FlipNormals: ps.flip, Of: ps.of,
		})
	}
	applyPivot(m)
	return m
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// partTris builds a non-mirror part: shape → scale (winding restored when the scale
// mirrors) → UVs from the scaled geometry → rotation and translation → flip_normals.
func partTris(ps *partSpec) []tri {
	local := shapeTris(ps)
	sc := ps.scale
	mirrored := float64(sc[0]*sc[1])*sc[2] < 0
	scaled := make([]ltri, len(local))
	bounds := emptyBox()
	for i, t := range local {
		for k := range 3 {
			scaled[i][k] = t[k].mul(sc)
			bounds.extend(scaled[i][k])
		}
		if mirrored {
			scaled[i][1], scaled[i][2] = scaled[i][2], scaled[i][1]
		}
	}
	rot := rotationDeg(ps.rot)
	noRot := ps.rot == dvec{}
	pAxis := planarAxis(bounds)
	out := make([]tri, len(scaled))
	for i, t := range scaled {
		uv := mapUV(ps.uv, t, bounds, pAxis)
		for k := range 3 {
			p := t[k]
			if !noRot {
				p = rot.apply(p)
			}
			out[i][k] = corner{pos: p.add(ps.pos).vec32(), uv: uv[k]}
		}
		if ps.flip {
			out[i][1], out[i][2] = out[i][2], out[i][1]
		}
	}
	return out
}

// mirrorTris reflects src across the model-space plane through the origin perpendicular
// to axis and reverses the winding so the copy faces the same way (outwards).
func mirrorTris(src []tri, axis int) []tri {
	out := make([]tri, len(src))
	for i, t := range src {
		for k := range 3 {
			c := t[k]
			switch axis {
			case 0:
				c.pos.X = canon32(-c.pos.X)
			case 1:
				c.pos.Y = canon32(-c.pos.Y)
			default:
				c.pos.Z = canon32(-c.pos.Z)
			}
			out[i][k] = c
		}
		out[i][1], out[i][2] = out[i][2], out[i][1]
	}
	return out
}

// UV mapping. Coordinates are meters of the part's scaled geometry (before rotation and
// translation), so 1 UV unit = 1 m and textures follow the part. v grows downwards.

// planarAxis is the projection axis of "planar": the axis of the smallest extent of the
// part's scaled bounds; ties prefer Y, then Z, then X.
func planarAxis(b dbox) int {
	ext := b.max.sub(b.min)
	a := 1
	if ext[2] < ext[a] {
		a = 2
	}
	if ext[0] < ext[a] {
		a = 0
	}
	return a
}

// dominantAxis returns the axis of the largest |n| component (ties prefer Y, then Z,
// then X) and whether that component is positive.
func dominantAxis(n dvec) (int, bool) {
	a := 1
	if math.Abs(n[2]) > math.Abs(n[a]) {
		a = 2
	}
	if math.Abs(n[0]) > math.Abs(n[a]) {
		a = 0
	}
	return a, n[a] > 0
}

// projectUV is the box projection of p for a face whose outward normal is dominated by
// axis a in direction pos. Each face maps its top-left corner, seen from outside, to 0.
func projectUV(p dvec, a int, pos bool, b dbox) gmath.Vec2 {
	var u, v float64
	switch {
	case a == 0 && pos: // +X
		u, v = b.max[2]-p[2], b.max[1]-p[1]
	case a == 0: // -X
		u, v = p[2]-b.min[2], b.max[1]-p[1]
	case a == 1 && pos: // +Y
		u, v = p[0]-b.min[0], p[2]-b.min[2]
	case a == 1: // -Y
		u, v = p[0]-b.min[0], b.max[2]-p[2]
	case pos: // +Z
		u, v = p[0]-b.min[0], b.max[1]-p[1]
	default: // -Z
		u, v = b.max[0]-p[0], b.max[1]-p[1]
	}
	return gmath.V2(f32(u), f32(v))
}

// mapUV returns the UVs of the three corners of scaled triangle t.
func mapUV(mode string, t ltri, b dbox, pAxis int) [3]gmath.Vec2 {
	var out [3]gmath.Vec2
	n := t[1].sub(t[0]).cross(t[2].sub(t[0]))
	switch mode {
	case "planar":
		for k := range 3 {
			out[k] = projectUV(t[k], pAxis, true, b)
		}
	case "box":
		a, pos := dominantAxis(n)
		for k := range 3 {
			out[k] = projectUV(t[k], a, pos, b)
		}
	case "cylindrical":
		if a, pos := dominantAxis(n); a == 1 { // caps: box projection from ±Y
			for k := range 3 {
				out[k] = projectUV(t[k], a, pos, b)
			}
			return out
		}
		out = wrapUV(t, b, false)
	case "spherical":
		out = wrapUV(t, b, true)
	}
	return out
}

// wrapUV is the cylindrical (u = (θ+π)·ρ, v = maxY − y) or spherical (u = (θ+π)·r,
// v = φ·r) mapping, with θ = atan2(x, z) the angle around +Y (0 at +Z, growing towards
// +X), ρ the distance from the Y axis, r the distance from the part origin and φ the
// polar angle from +Y. θ is unwrapped per triangle to within π of the angle of its
// centroid, so the seam falls exactly at θ = ±π (the −Z side); corners on the Y axis
// take the centroid's angle.
func wrapUV(t ltri, b dbox, spherical bool) [3]gmath.Vec2 {
	c := t[0].add(t[1]).add(t[2]).scale(1.0 / 3)
	tc := 0.0
	if c[0] != 0 || c[2] != 0 {
		tc = gmath.Atan264(c[0], c[2])
	}
	var out [3]gmath.Vec2
	for k, p := range t {
		rho := math.Sqrt(float64(p[0]*p[0]) + float64(p[2]*p[2]))
		th := tc
		if rho > 0 {
			th = gmath.Atan264(p[0], p[2])
			if th-tc > math.Pi {
				th -= 2 * math.Pi
			} else if tc-th > math.Pi {
				th += 2 * math.Pi
			}
		}
		if !spherical {
			out[k] = gmath.V2(f32(float64((th+math.Pi)*rho)), f32(b.max[1]-p[1]))
			continue
		}
		r := p.len()
		phi := 0.0
		if r > 0 {
			phi = gmath.Acos64(max(-1, min(1, p[1]/r)))
		}
		out[k] = gmath.V2(f32(float64((th+math.Pi)*r)), f32(float64(phi*r)))
	}
	return out
}

// Normals and welding.

type posKey [3]uint32

func keyOf(p gmath.Vec3) posKey {
	return posKey{math.Float32bits(p.X), math.Float32bits(p.Y), math.Float32bits(p.Z)}
}

// smoothNormals returns the normal of every corner of tris (one part). Around each
// distinct position, the triangles touching it are grouped into smoothing fans: two of
// them join when they share an edge at that position and their face normals are within
// the smoothing angle (dot >= cosLim); joins are transitive. A corner's normal is the
// normalized sum of the unit face normals of its fan, each weighted by that triangle's
// interior angle at the position (angle-weighted normals do not depend on how quads
// are split into triangles). A sphere or a cylinder side (adjacent faces within the
// angle) is therefore smooth while box edges and cylinder caps stay sharp. Triangles of
// other parts never take part.
func smoothNormals(tris []tri, cosLim float64) [][3]gmath.Vec3 {
	unit := make([]dvec, len(tris))
	for i, t := range tris {
		a, b, c := dvec3(t[0].pos), dvec3(t[1].pos), dvec3(t[2].pos)
		unit[i] = b.sub(a).cross(c.sub(a)).normalize()
	}
	group := make(map[posKey]int32, len(tris))
	var members [][]int32             // triangles touching each distinct position, in order
	cg := make([][3]int32, len(tris)) // position group of every corner
	for i, t := range tris {
		for k := range 3 {
			key := keyOf(t[k].pos)
			g, ok := group[key]
			if !ok {
				g = int32(len(members))
				group[key] = g
				members = append(members, nil)
			}
			cg[i][k] = g
			if l := members[g]; len(l) == 0 || l[len(l)-1] != int32(i) {
				members[g] = append(l, int32(i))
			}
		}
	}
	// weighted returns triangle t's unit normal times its interior angle at group g.
	weighted := func(t int32, g int32) dvec {
		k := 0
		for cg[t][k] != g {
			k++
		}
		p := dvec3(tris[t][k].pos)
		e1, e2 := dvec3(tris[t][(k+1)%3].pos).sub(p), dvec3(tris[t][(k+2)%3].pos).sub(p)
		return unit[t].scale(gmath.Atan264(e1.cross(e2).len(), e1.dot(e2)))
	}
	out := make([][3]gmath.Vec3, len(tris))
	var parent []int
	var sums []dvec
	for g, mem := range members {
		n := len(mem)
		parent, sums = parent[:0], sums[:0]
		for a := 0; a < n; a++ {
			parent = append(parent, a)
			sums = append(sums, dvec{})
		}
		find := func(a int) int {
			for parent[a] != a {
				parent[a] = parent[parent[a]]
				a = parent[a]
			}
			return a
		}
		for a := 0; a < n; a++ {
			for b := a + 1; b < n; b++ {
				s, t := mem[a], mem[b]
				if unit[s].dot(unit[t]) < cosLim || !shareEdge(cg[s], cg[t], int32(g)) {
					continue
				}
				if ra, rb := find(a), find(b); ra < rb {
					parent[rb] = ra
				} else if rb < ra {
					parent[ra] = rb
				}
			}
		}
		for a := 0; a < n; a++ {
			r := find(a)
			sums[r] = sums[r].add(weighted(mem[a], int32(g)))
		}
		for a := 0; a < n; a++ {
			t := mem[a]
			nrm := sums[find(a)].normalize()
			if nrm == (dvec{}) {
				nrm = unit[t]
			}
			for k := range 3 {
				if cg[t][k] == int32(g) {
					out[t][k] = nrm.vec32()
				}
			}
		}
	}
	return out
}

// shareEdge reports whether two triangles (given by the position groups of their
// corners) that both touch position g also share a second position, i.e. an edge at g.
func shareEdge(a, b [3]int32, g int32) bool {
	for _, x := range a {
		if x == g {
			continue
		}
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

type vertexKey struct {
	pos, normal posKey
	u, v        uint32
}

// appendPart appends the triangles of one part to mesh, sharing vertices within the
// part that have identical position, normal and UV (first-use order).
func appendPart(mesh *gfx.MeshData, tris []tri, normals [][3]gmath.Vec3) {
	index := make(map[vertexKey]uint32, len(tris))
	for i, t := range tris {
		for k := range 3 {
			v := gfx.Vertex{Pos: t[k].pos, Normal: normals[i][k], UV: t[k].uv}
			key := vertexKey{keyOf(v.Pos), keyOf(v.Normal), math.Float32bits(v.UV.X), math.Float32bits(v.UV.Y)}
			idx, ok := index[key]
			if !ok {
				idx = uint32(len(mesh.Vertices))
				index[key] = idx
				mesh.Vertices = append(mesh.Vertices, v)
			}
			mesh.Indices = append(mesh.Indices, idx)
		}
	}
}

// applyPivot translates every vertex so the model's pivot point is at the origin,
// records the translation and sets the mesh bounds.
func applyPivot(m *asset.Model) {
	b := bounds(m.Mesh.Vertices)
	mid := func(k int) float32 {
		return f32((float64(b.Min.Get(k)) + float64(b.Max.Get(k))) / 2)
	}
	var pt gmath.Vec3
	switch m.Pivot {
	case "center":
		pt = gmath.V3(mid(0), mid(1), mid(2))
	case "bottom-center":
		pt = gmath.V3(mid(0), b.Min.Y, mid(2))
	}
	off := gmath.Zero3.Sub(pt)
	if off != gmath.Zero3 {
		for i := range m.Mesh.Vertices {
			p := m.Mesh.Vertices[i].Pos.Add(off)
			m.Mesh.Vertices[i].Pos = gmath.V3(canon32(p.X), canon32(p.Y), canon32(p.Z))
		}
		b = bounds(m.Mesh.Vertices)
	}
	m.PivotOffset = off
	m.Mesh.Bounds = b
}

func bounds(vs []gfx.Vertex) gmath.AABB {
	b := gmath.EmptyAABB()
	for _, v := range vs {
		b = b.Extend(v.Pos)
	}
	return b
}
