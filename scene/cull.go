package scene

import (
	"math"

	"github.com/riftbane/veduta/v2/gmath"
)

// frustum holds the six planes of a view volume in world space, each normalized so that
// a·x + b·y + c·z + d is the signed distance in meters (inside when >= 0): left, right,
// bottom, top, near, far, as the rasterizer clips.
type frustum [6][4]float64

// cullMargin is how far outside a plane, in meters, bounds must lie to be culled, so
// rounding never removes a triangle the rasterizer would draw.
const cullMargin = 1e-3

// newFrustum extracts the planes of the clip volume -w <= x, y, z <= w from a
// view-projection matrix (Gribb and Hartmann).
func newFrustum(vp gmath.Mat4) frustum {
	var rows [4][4]float64
	for i := range rows {
		rows[i] = [4]float64{float64(vp[i]), float64(vp[4+i]), float64(vp[8+i]), float64(vp[12+i])}
	}
	var f frustum
	for k := range 3 {
		for j := range 4 {
			f[2*k][j] = rows[3][j] + rows[k][j]
			f[2*k+1][j] = rows[3][j] - rows[k][j]
		}
	}
	for k := range f {
		p := &f[k]
		if n := math.Sqrt(float64(p[0]*p[0]) + float64(p[1]*p[1]) + float64(p[2]*p[2])); n > 0 {
			for j := range p {
				p[j] /= n
			}
		}
	}
	return f
}

// outside reports whether box lies wholly on the outer side of one plane.
func (f *frustum) outside(box gmath.AABB) bool {
	for k := range f {
		p := &f[k]
		// The corner farthest along the plane's normal.
		x, y, z := float64(box.Min.X), float64(box.Min.Y), float64(box.Min.Z)
		if p[0] >= 0 {
			x = float64(box.Max.X)
		}
		if p[1] >= 0 {
			y = float64(box.Max.Y)
		}
		if p[2] >= 0 {
			z = float64(box.Max.Z)
		}
		if float64(p[0]*x)+float64(p[1]*y)+float64(p[2]*z)+p[3] < -cullMargin {
			return true
		}
	}
	return false
}

// tan30 is tan(30°): levels of detail are measured as through a 60° lens.
const tan30 = 0.5773502691896257

// lodMeasure converts camera distances to the distances levels of detail and
// draw_distance are compared with (docs/model.md).
type lodMeasure struct {
	ortho bool
	fixed float32 // orthographic: every entity's distance
	scale float64 // perspective: tan(fov/2) / tan(30°)
}

func (c Camera) lodMeasure() lodMeasure {
	if c.Ortho {
		return lodMeasure{ortho: true, fixed: float32(float64(c.Size) * (0.5 / tan30))}
	}
	return lodMeasure{scale: gmath.Tan64(float64(gmath.Radians(c.FovDeg))/2) / tan30}
}

// distance returns the measured distance of box from eye: the nearest point of the box,
// or the orthographic constant.
func (l lodMeasure) distance(eye gmath.Vec3, box gmath.AABB) float32 {
	if l.ortho {
		return l.fixed
	}
	d := func(v, lo, hi float32) float64 { return float64(v - gmath.Clamp(v, lo, hi)) }
	dx, dy, dz := d(eye.X, box.Min.X, box.Max.X), d(eye.Y, box.Min.Y, box.Max.Y), d(eye.Z, box.Min.Z, box.Max.Z)
	return float32(math.Sqrt(float64(dx*dx)+float64(dy*dy)+float64(dz*dz)) * l.scale)
}
