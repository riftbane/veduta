package model

import (
	"math"

	"github.com/riftbane/veduta/gmath"
)

// Setup math is float64. Products are wrapped in explicit float64 conversions so the
// compiler cannot fuse multiply-adds: results are the same on every architecture.

// dvec is a float64 3-vector.
type dvec [3]float64

// dvec2 is a float64 2-vector (profile point).
type dvec2 [2]float64

func dvec3(v gmath.Vec3) dvec { return dvec{float64(v.X), float64(v.Y), float64(v.Z)} }

func (a dvec) add(b dvec) dvec { return dvec{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a dvec) sub(b dvec) dvec { return dvec{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a dvec) mul(b dvec) dvec {
	return dvec{float64(a[0] * b[0]), float64(a[1] * b[1]), float64(a[2] * b[2])}
}

func (a dvec) scale(s float64) dvec {
	return dvec{float64(a[0] * s), float64(a[1] * s), float64(a[2] * s)}
}

func (a dvec) dot(b dvec) float64 {
	return float64(a[0]*b[0]) + float64(a[1]*b[1]) + float64(a[2]*b[2])
}

func (a dvec) cross(b dvec) dvec {
	return dvec{
		float64(a[1]*b[2]) - float64(a[2]*b[1]),
		float64(a[2]*b[0]) - float64(a[0]*b[2]),
		float64(a[0]*b[1]) - float64(a[1]*b[0]),
	}
}

func (a dvec) len() float64 { return math.Sqrt(a.dot(a)) }

// normalize returns a/|a|, or the zero vector for a zero (or non-finite) length.
func (a dvec) normalize() dvec {
	l := a.len()
	if !(l > 0) || math.IsInf(l, 0) {
		return dvec{}
	}
	return dvec{a[0] / l, a[1] / l, a[2] / l}
}

// vec32 rounds to float32 storage; negative zeros become positive zeros so equal
// positions always have equal bits.
func (a dvec) vec32() gmath.Vec3 { return gmath.V3(f32(a[0]), f32(a[1]), f32(a[2])) }

func f32(x float64) float32 {
	v := float32(x)
	if v == 0 {
		return 0
	}
	return v
}

// canon32 turns a negative zero into a positive zero.
func canon32(x float32) float32 {
	if x == 0 {
		return 0
	}
	return x
}

// mat3 is a row-major float64 rotation matrix.
type mat3 [3]dvec

// rotationDeg returns R = Ry·Rx·Rz for Euler angles in degrees (gmath.QuatEulerDeg).
func rotationDeg(d dvec) mat3 {
	sx, cx := gmath.SinCos64(d[0] * gmath.Deg2Rad)
	sy, cy := gmath.SinCos64(d[1] * gmath.Deg2Rad)
	sz, cz := gmath.SinCos64(d[2] * gmath.Deg2Rad)
	rx := mat3{{1, 0, 0}, {0, cx, -sx}, {0, sx, cx}}
	ry := mat3{{cy, 0, sy}, {0, 1, 0}, {-sy, 0, cy}}
	rz := mat3{{cz, -sz, 0}, {sz, cz, 0}, {0, 0, 1}}
	return ry.mulMat(rx).mulMat(rz)
}

func (m mat3) mulMat(b mat3) mat3 {
	var out mat3
	for r := range 3 {
		for c := range 3 {
			out[r][c] = float64(m[r][0]*b[0][c]) + float64(m[r][1]*b[1][c]) + float64(m[r][2]*b[2][c])
		}
	}
	return out
}

func (m mat3) apply(v dvec) dvec { return dvec{m[0].dot(v), m[1].dot(v), m[2].dot(v)} }

// dbox is a float64 axis-aligned box.
type dbox struct{ min, max dvec }

func emptyBox() dbox {
	inf := math.Inf(1)
	return dbox{dvec{inf, inf, inf}, dvec{-inf, -inf, -inf}}
}

func (b *dbox) extend(p dvec) {
	for k := range 3 {
		b.min[k] = min(b.min[k], p[k])
		b.max[k] = max(b.max[k], p[k])
	}
}

// Polygon helpers (profiles, float64).

func cross2(o, a, b dvec2) float64 {
	return float64((a[0]-o[0])*(b[1]-o[1])) - float64((a[1]-o[1])*(b[0]-o[0]))
}

// signedArea returns twice the signed area of the closed polygon pts (positive when
// counter-clockwise).
func signedArea(pts []dvec2) float64 {
	var s float64
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		s += float64(p[0]*q[1]) - float64(q[0]*p[1])
	}
	return s
}

// onSegment reports whether p, known to be collinear with a-b, lies within the segment.
func onSegment(a, b, p dvec2) bool {
	return min(a[0], b[0]) <= p[0] && p[0] <= max(a[0], b[0]) &&
		min(a[1], b[1]) <= p[1] && p[1] <= max(a[1], b[1])
}

// segmentsTouch reports whether the closed segments a-b and c-d share a point.
func segmentsTouch(a, b, c, d dvec2) bool {
	d1, d2 := cross2(a, b, c), cross2(a, b, d)
	d3, d4 := cross2(c, d, a), cross2(c, d, b)
	if ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0)) {
		return true
	}
	return (d1 == 0 && onSegment(a, b, c)) || (d2 == 0 && onSegment(a, b, d)) ||
		(d3 == 0 && onSegment(c, d, a)) || (d4 == 0 && onSegment(c, d, b))
}

// firstCrossing checks that the closed polygon pts is simple: non-adjacent edges share
// no point and adjacent edges do not fold back onto each other. It returns the first
// offending pair of edges (by start index) in scan order.
func firstCrossing(pts []dvec2) (int, int, bool) {
	n := len(pts)
	for i := 0; i < n; i++ {
		a, b := pts[i], pts[(i+1)%n]
		for j := i + 1; j < n; j++ {
			c, d := pts[j], pts[(j+1)%n]
			switch {
			case j == i+1: // edges meet at b == c: bad only if d folds back onto a-b
				if cross2(a, b, d) == 0 && float64((d[0]-b[0])*(a[0]-b[0]))+float64((d[1]-b[1])*(a[1]-b[1])) > 0 {
					return i, j, true
				}
			case i == 0 && j == n-1: // edges meet at a == d
				if cross2(c, a, b) == 0 && float64((c[0]-a[0])*(b[0]-a[0]))+float64((c[1]-a[1])*(b[1]-a[1])) > 0 {
					return i, j, true
				}
			default:
				if segmentsTouch(a, b, c, d) {
					return i, j, true
				}
			}
		}
	}
	return 0, 0, false
}

// earClip triangulates the simple counter-clockwise polygon pts by ear clipping and
// returns counter-clockwise index triples. The scan order is fixed, so the result is
// deterministic. Collinear vertices are kept (they are consumed by neighbouring ears),
// so the cap shares every boundary vertex with the side walls.
func earClip(pts []dvec2) [][3]int {
	idx := make([]int, len(pts))
	for i := range idx {
		idx[i] = i
	}
	out := make([][3]int, 0, len(pts)-2)
	i := 0
	for len(idx) > 3 {
		n := len(idx)
		found := -1
		for k := 0; k < n; k++ {
			c := (i + k) % n
			if isEar(pts, idx, c) {
				found = c
				break
			}
		}
		if found < 0 { // numerical trouble only: clip the most convex vertex
			best := -math.MaxFloat64
			for c := 0; c < n; c++ {
				if a := cross2(pts[idx[(c+n-1)%n]], pts[idx[c]], pts[idx[(c+1)%n]]); a > best {
					best, found = a, c
				}
			}
		}
		out = append(out, [3]int{idx[(found+n-1)%n], idx[found], idx[(found+1)%n]})
		idx = append(idx[:found], idx[found+1:]...)
		i = found % len(idx)
	}
	return append(out, [3]int{idx[0], idx[1], idx[2]})
}

// isEar reports whether vertex c of the remaining polygon idx is an ear: strictly convex
// and no other remaining vertex inside or on its triangle.
func isEar(pts []dvec2, idx []int, c int) bool {
	n := len(idx)
	ia, ib, ic := idx[(c+n-1)%n], idx[c], idx[(c+1)%n]
	a, b, cc := pts[ia], pts[ib], pts[ic]
	if cross2(a, b, cc) <= 0 {
		return false
	}
	for _, j := range idx {
		if j == ia || j == ib || j == ic {
			continue
		}
		p := pts[j]
		if p == a || p == b || p == cc {
			continue
		}
		if cross2(a, b, p) >= 0 && cross2(b, cc, p) >= 0 && cross2(cc, a, p) >= 0 {
			return false
		}
	}
	return true
}
