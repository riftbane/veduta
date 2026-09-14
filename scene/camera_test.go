package scene

import (
	"testing"

	"github.com/riftbane/veduta/gmath"
)

// Camera2D frames height units vertically around its center, +X right and +Y up, and sees
// the z range from -100 to just in front of itself.
func TestCamera2DFraming(t *testing.T) {
	c := Camera2D(gmath.V2(3, -2), 12)
	if !c.Ortho || c.Size != 12 || c.Near != 0.1 || c.Far != 200 {
		t.Fatalf("camera %+v", c)
	}
	v := c.GfxView(320, 240) // aspect 4:3: 16 units across
	vp := v.Proj.Mul(v.View)
	const eps = 1e-5
	for _, tc := range []struct {
		p    gmath.Vec3
		x, y float32
	}{
		{gmath.V3(3, -2, 0), 0, 0},
		{gmath.V3(3, 4, 0), 0, 1},    // top edge
		{gmath.V3(3, -8, 0), 0, -1},  // bottom edge
		{gmath.V3(11, -2, 0), 1, 0},  // right edge
		{gmath.V3(-5, -2, 5), -1, 0}, // left edge, nearer the camera
		{gmath.V3(7, 1, -40), 0.5, 0.5},
	} {
		q := vp.Project(tc.p)
		if gmath.Abs(q.X-tc.x) > eps || gmath.Abs(q.Y-tc.y) > eps {
			t.Errorf("%v projects to %v, want (%g, %g)", tc.p, q, tc.x, tc.y)
		}
	}
	for _, z := range []float32{-99.9, 0, 99.8} {
		if q := vp.Project(gmath.V3(3, -2, z)); q.Z <= -1 || q.Z >= 1 {
			t.Errorf("z = %g is clipped (ndc z %g)", z, q.Z)
		}
	}
	for _, z := range []float32{-100.5, 99.95} {
		if q := vp.Project(gmath.V3(3, -2, z)); q.Z > -1 && q.Z < 1 {
			t.Errorf("z = %g is not clipped (ndc z %g)", z, q.Z)
		}
	}
	if a, b := vp.Project(gmath.V3(0, 0, 1)), vp.Project(gmath.V3(0, 0, 2)); !(b.Z < a.Z) {
		t.Errorf("a larger z must be nearer the camera: ndc z %g at z=1, %g at z=2", a.Z, b.Z)
	}
}

func TestCameraLookFrom(t *testing.T) {
	base := Camera{FovDeg: 70, Near: 0.2, Far: 300, Position: gmath.V3(9, 9, 9), Target: gmath.V3(1, 1, 1)}
	eye := gmath.V3(2, 1, -3)
	const eps = 1e-5
	for _, tc := range []struct {
		yaw, pitch float32
		want       gmath.Vec3 // viewing direction
	}{
		{0, 0, gmath.V3(0, 0, -1)},   // yaw 0 looks forward
		{90, 0, gmath.V3(-1, 0, 0)},  // counter-clockwise from above
		{-90, 0, gmath.V3(1, 0, 0)},  //
		{180, 0, gmath.V3(0, 0, 1)},  //
		{0, 90, gmath.V3(0, 1, 0)},   // positive pitch looks up
		{0, -90, gmath.V3(0, -1, 0)}, //
	} {
		c := base.LookFrom(eye, tc.yaw, tc.pitch)
		if c.Position != eye {
			t.Errorf("yaw %g pitch %g: position %v, want %v", tc.yaw, tc.pitch, c.Position, eye)
		}
		if c.FovDeg != base.FovDeg || c.Near != base.Near || c.Far != base.Far {
			t.Errorf("yaw %g pitch %g: projection changed: %+v", tc.yaw, tc.pitch, c)
		}
		dir := c.Target.Sub(c.Position)
		if d := dir.Sub(tc.want).Len(); d > eps {
			t.Errorf("yaw %g pitch %g: direction %v, want %v", tc.yaw, tc.pitch, dir, tc.want)
		}
		if l := dir.Len(); l < 1-eps || l > 1+eps {
			t.Errorf("yaw %g pitch %g: direction length %g, want 1", tc.yaw, tc.pitch, l)
		}
	}
}
