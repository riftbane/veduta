package scene

import (
	"testing"

	"github.com/riftbane/veduta/gmath"
)

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
