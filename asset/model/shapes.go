package model

import "github.com/riftbane/veduta/gmath"

// ltri is a triangle in a part's local, unscaled coordinates, counter-clockwise seen
// from outside (outward normal = cross(b-a, c-a)).
type ltri [3]dvec

// shapeTris generates the triangles of a non-mirror part.
func shapeTris(ps *partSpec) []ltri {
	switch ps.shape {
	case "box":
		return boxTris(ps.size.scale(0.5))
	case "plane":
		return planeTris(ps.size[0]/2, ps.size[2]/2)
	case "cylinder":
		r, h := ps.radius, ps.height/2
		return revolve([]dvec2{{0, -h}, {r, -h}, {r, h}, {0, h}}, ps.segments)
	case "sphere":
		return revolve(sphereProfile(ps.radius, ps.rings), ps.segments)
	case "extrude":
		return extrudeTris(ps.profile, ps.depth/2)
	case "lathe":
		return revolve(ps.profile, ps.segments)
	}
	panic("model: unknown shape " + ps.shape)
}

// boxFaces gives each box face's outward normal and in-plane axes u, v with
// cross(u, v) = n, so the corners c-u-v, c+u-v, c+u+v, c-u+v are counter-clockwise.
var boxFaces = [6][3]dvec{
	{{1, 0, 0}, {0, 0, -1}, {0, 1, 0}},
	{{-1, 0, 0}, {0, 0, 1}, {0, 1, 0}},
	{{0, 1, 0}, {1, 0, 0}, {0, 0, -1}},
	{{0, -1, 0}, {1, 0, 0}, {0, 0, 1}},
	{{0, 0, 1}, {1, 0, 0}, {0, 1, 0}},
	{{0, 0, -1}, {-1, 0, 0}, {0, 1, 0}},
}

// boxTris returns the 12 triangles of a box with half extents h.
func boxTris(h dvec) []ltri {
	out := make([]ltri, 0, 12)
	for _, f := range boxFaces {
		c, u, v := f[0].mul(h), f[1].mul(h), f[2].mul(h)
		p0, p1 := c.sub(u).sub(v), c.add(u).sub(v)
		p2, p3 := c.add(u).add(v), c.sub(u).add(v)
		out = append(out, ltri{p0, p1, p2}, ltri{p0, p2, p3})
	}
	return out
}

// planeTris returns a rectangle in the XZ plane facing +Y with half extents hx, hz.
func planeTris(hx, hz float64) []ltri {
	p0, p1 := dvec{-hx, 0, hz}, dvec{hx, 0, hz}
	p2, p3 := dvec{hx, 0, -hz}, dvec{-hx, 0, -hz}
	return []ltri{{p0, p1, p2}, {p0, p2, p3}}
}

// sphereProfile is the lathe profile of a UV sphere: rings+1 points from the south pole
// to the north pole, poles exactly on the axis.
func sphereProfile(r float64, rings int) []dvec2 {
	prof := make([]dvec2, rings+1)
	for k := 0; k <= rings; k++ {
		s, c := gmath.SinCos64(gmath.Pi * float64(k) / float64(rings))
		prof[k] = dvec2{float64(r * s), -float64(r * c)}
	}
	prof[0] = dvec2{0, -r}
	prof[rings] = dvec2{0, r}
	return prof
}

// revolve sweeps the [r, y] profile around +Y in n segments. Point (r, y) at segment j
// is (r·sin θj, y, r·cos θj) with θj = 2πj/n, so θ = 0 is +Z and θ grows towards +X.
// The surface faces the right-hand side of the profile's direction of travel in the
// (r, y) plane (r to the right, y up). Segments with one end on the axis become fans of
// single triangles; segments lying on the axis produce nothing.
func revolve(prof []dvec2, n int) []ltri {
	sin, cos := make([]float64, n), make([]float64, n)
	for j := 0; j < n; j++ {
		sin[j], cos[j] = gmath.SinCos64(2 * gmath.Pi * float64(j) / float64(n))
	}
	sin[0], cos[0] = 0, 1
	ring := func(p dvec2, j int) dvec {
		if p[0] == 0 {
			return dvec{0, p[1], 0}
		}
		j %= n
		return dvec{float64(p[0] * sin[j]), p[1], float64(p[0] * cos[j])}
	}
	var out []ltri
	for i := 0; i+1 < len(prof); i++ {
		a, b := prof[i], prof[i+1]
		if a[0] == 0 && b[0] == 0 {
			continue
		}
		for j := 0; j < n; j++ {
			a0, a1, b0, b1 := ring(a, j), ring(a, j+1), ring(b, j), ring(b, j+1)
			switch {
			case a[0] == 0:
				out = append(out, ltri{a0, b1, b0})
			case b[0] == 0:
				out = append(out, ltri{a0, a1, b0})
			default:
				out = append(out, ltri{a0, a1, b0}, ltri{a1, b1, b0})
			}
		}
	}
	return out
}

// extrudeTris extrudes the counter-clockwise XY polygon prof from z = -h to z = +h:
// ear-clipped caps facing ±Z and one quad per edge for the side walls.
func extrudeTris(prof []dvec2, h float64) []ltri {
	n := len(prof)
	front := func(i int) dvec { return dvec{prof[i][0], prof[i][1], h} }
	back := func(i int) dvec { return dvec{prof[i][0], prof[i][1], -h} }
	caps := earClip(prof)
	out := make([]ltri, 0, 2*len(caps)+2*n)
	for _, t := range caps {
		out = append(out, ltri{front(t[0]), front(t[1]), front(t[2])})
	}
	for _, t := range caps {
		out = append(out, ltri{back(t[0]), back(t[2]), back(t[1])})
	}
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		out = append(out, ltri{back(i), back(j), front(j)}, ltri{back(i), front(j), front(i)})
	}
	return out
}
