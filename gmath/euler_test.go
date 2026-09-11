package gmath

import "testing"

func TestEulerDegRoundTrip(t *testing.T) {
	cases := []Vec3{
		{0, 0, 0}, {0, 90, 0}, {0, 180, 0}, {30, 45, 60}, {-80, 170, -10},
		{10, -120, 5}, {89, 20, 0}, {-45, 0, 135},
	}
	for _, d := range cases {
		q := QuatEulerDeg(d)
		back := q.EulerDeg()
		q2 := QuatEulerDeg(back)
		// Same rotation (q and -q are equal rotations).
		if dot := Abs(q.Dot(q2)); dot < 1-1e-5 {
			t.Errorf("EulerDeg(%v) = %v: rotation differs (|dot| = %v)", d, back, dot)
		}
		if d.X > -89 && d.X < 89 && d.Y > -179 && d.Y < 179 && back.Sub(d).Len() > 1e-3 {
			t.Errorf("EulerDeg(%v) = %v, want the same angles", d, back)
		}
	}
	// Gimbal lock: pitch ±90 folds roll into yaw but keeps the rotation.
	for _, d := range []Vec3{{90, 30, 20}, {-90, 10, 40}} {
		q := QuatEulerDeg(d)
		if dot := Abs(q.Dot(QuatEulerDeg(q.EulerDeg()))); dot < 1-1e-5 {
			t.Errorf("gimbal %v: rotation lost", d)
		}
	}
}
