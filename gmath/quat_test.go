package gmath

import (
	"math"
	"testing"
)

func TestQuatIdent(t *testing.T) {
	q := QuatIdent()
	if q != (Quat{0, 0, 0, 1}) {
		t.Fatalf("QuatIdent = %v", q)
	}
	if q.Mat4() != Ident4() {
		t.Errorf("QuatIdent.Mat4 = %v", q.Mat4())
	}
	v := V3(1.5, -2, 3)
	if q.Rotate(v) != v {
		t.Errorf("QuatIdent.Rotate(%v) = %v", v, q.Rotate(v))
	}
	r := newRNG(31)
	for i := 0; i < 100; i++ {
		p := r.quat()
		if q.Mul(p) != p || p.Mul(q) != p {
			t.Fatalf("identity is not neutral for %v: %v %v", p, q.Mul(p), p.Mul(q))
		}
	}
}

func TestQuatPrincipalRotations(t *testing.T) {
	// The same convention table as the matrices: RotateY(+90°) maps +X to -Z,
	// RotateX(+90°) maps +Y to +Z, RotateZ(+90°) maps +X to +Y.
	q90 := float32(math.Pi / 2)
	tests := []struct {
		name    string
		axis    Vec3
		in, out Vec3
	}{
		{"Y:+X->-Z", UnitY, UnitX, Forward},
		{"Y:+Z->+X", UnitY, UnitZ, UnitX},
		{"Y:+Y fixed", UnitY, UnitY, UnitY},
		{"X:+Y->+Z", UnitX, UnitY, UnitZ},
		{"X:+Z->-Y", UnitX, UnitZ, UnitY.Neg()},
		{"X:+X fixed", UnitX, UnitX, UnitX},
		{"Z:+X->+Y", UnitZ, UnitX, UnitY},
		{"Z:+Y->-X", UnitZ, UnitY, UnitX.Neg()},
		{"Z:+Z fixed", UnitZ, UnitZ, UnitZ},
	}
	for _, tc := range tests {
		q := QuatAxisAngle(tc.axis, q90)
		checkV3(t, tc.name+" Rotate", q.Rotate(tc.in), tc.out, 1e-6)
		checkV3(t, tc.name+" Mat4", q.Mat4().MulDir(tc.in), tc.out, 1e-6)
	}
}

func TestQuatAxisAngleRotateMat4Agree(t *testing.T) {
	r := newRNG(37)
	for i := 0; i < 500; i++ {
		axis, angle := r.dir(), r.f(-2*Pi, 2*Pi)
		q := QuatAxisAngle(axis, angle)
		if !approx(q.Len(), 1, 3e-7) {
			t.Fatalf("|QuatAxisAngle| = %v", q.Len())
		}
		m := q.Mat4()
		for j := 0; j < 4; j++ {
			v := r.vec3(-5, 5)
			rv, mv := q.Rotate(v), m.MulDir(v)
			if !approxV3(rv, mv, 3e-6) {
				t.Fatalf("q=%v v=%v: Rotate %v, Mat4 %v", q, v, rv, mv)
			}
			if !approx(rv.Len(), v.Len(), 3e-6) {
				t.Fatalf("Rotate changed length: %v -> %v", v.Len(), rv.Len())
			}
		}
		// The axis is fixed by the rotation.
		checkV3(t, "axis fixed", q.Rotate(axis), axis, 2e-6)
		// Rotating a perpendicular vector turns it by exactly angle.
		p := axis.Cross(r.dir()).Normalize()
		rp := q.Rotate(p)
		cosA := float64(p.Dot(rp))
		sinA := float64(p.Cross(rp).Dot(axis))
		// Compare modulo 2π (so +π and -π agree) — never skip the check near ±π, which
		// would accept any rotation that happens to turn by π.
		if got := math.Atan2(sinA, cosA); math.Abs(math.Remainder(got-float64(angle), 2*math.Pi)) > 2e-5 {
			t.Fatalf("rotation about %v by %v turned a perpendicular by %v", axis, angle, got)
		}
		// Upper 3×3 is orthonormal with det +1, and the rest is the identity.
		if d := m.Det(); !approx(d, 1, 3e-6) {
			t.Fatalf("det(q.Mat4) = %v", d)
		}
		checkM4(t, "RᵀR", m.Transpose().Mul(m), Ident4(), 3e-6)
		if m[3] != 0 || m[7] != 0 || m[11] != 0 || m[12] != 0 || m[13] != 0 || m[14] != 0 || m[15] != 1 {
			t.Fatalf("q.Mat4 is not a pure rotation: %v", m)
		}
	}
	// Principal axes match the matrix constructors.
	for _, a := range []float32{-3, -1.2, 0, 0.3, 1, Pi / 2, 2.5, Pi} {
		checkM4(t, "QuatAxisAngle(X)", QuatAxisAngle(UnitX, a).Mat4(), RotateX(a), 2e-6)
		checkM4(t, "QuatAxisAngle(Y)", QuatAxisAngle(UnitY, a).Mat4(), RotateY(a), 2e-6)
		checkM4(t, "QuatAxisAngle(Z)", QuatAxisAngle(UnitZ, a).Mat4(), RotateZ(a), 2e-6)
	}
}

func TestQuatAxisAngleNormalizesAxis(t *testing.T) {
	for _, a := range []float32{0.1, 1, -2} {
		if got, want := QuatAxisAngle(V3(0, 5, 0), a), QuatAxisAngle(UnitY, a); !sameRotation(got, want, 1e-7) {
			t.Errorf("axis (0,5,0): %v, want %v", got, want)
		}
		if got, want := QuatAxisAngle(V3(3, 0, 4), a), QuatAxisAngle(V3(0.6, 0, 0.8), a); !sameRotation(got, want, 1e-7) {
			t.Errorf("axis (3,0,4): %v, want %v", got, want)
		}
	}
	// A zero axis defines no rotation: the result must be the (unit) identity, not a
	// scaled quaternion that would corrupt later compositions.
	for _, a := range []float32{0, 1, math.Pi, -2} {
		if got := QuatAxisAngle(Vec3{}, a); got != QuatIdent() {
			t.Errorf("QuatAxisAngle(0, %v) = %v, want identity", a, got)
		}
	}
}

func TestQuatEulerOrder(t *testing.T) {
	// QuatEuler(x, y, z) is the rotation Ry·Rx·Rz: roll about Z first, then pitch about
	// X, then yaw about Y.
	angles := []float32{0, 0.3, -0.7, 1.2, math.Pi / 2, -math.Pi, 2.9}
	for _, x := range angles {
		for _, y := range angles {
			for _, z := range angles {
				got := QuatEuler(x, y, z).Mat4()
				want := RotateY(y).Mul(RotateX(x)).Mul(RotateZ(z))
				if !approxM4(got, want, 3e-6) {
					t.Fatalf("QuatEuler(%v,%v,%v).Mat4 =\n%v\nwant Ry·Rx·Rz =\n%v", x, y, z, got, want)
				}
			}
		}
	}
	// The check above must be able to fail: other orders give different matrices.
	x, y, z := float32(0.4), float32(1.1), float32(-0.8)
	e := QuatEuler(x, y, z).Mat4()
	for name, other := range map[string]Mat4{
		"Rx·Ry·Rz": RotateX(x).Mul(RotateY(y)).Mul(RotateZ(z)),
		"Rz·Rx·Ry": RotateZ(z).Mul(RotateX(x)).Mul(RotateY(y)),
		"Rz·Ry·Rx": RotateZ(z).Mul(RotateY(y)).Mul(RotateX(x)),
	} {
		if approxM4(e, other, 1e-3) {
			t.Errorf("QuatEuler also matches %s; the order test is vacuous", name)
		}
	}
	// Degrees wrapper and a readable case: yaw 90° turns +X to -Z.
	if got, want := QuatEulerDeg(V3(30, -45, 60)), QuatEuler(Radians(30), Radians(-45), Radians(60)); got != want {
		t.Errorf("QuatEulerDeg = %v, want %v", got, want)
	}
	checkV3(t, "yaw 90", QuatEulerDeg(V3(0, 90, 0)).Rotate(UnitX), Forward, 1e-6)
	checkV3(t, "pitch 90", QuatEulerDeg(V3(90, 0, 0)).Rotate(UnitY), UnitZ, 1e-6)
	checkV3(t, "roll 90", QuatEulerDeg(V3(0, 0, 90)).Rotate(UnitX), UnitY, 1e-6)
	// rotation_deg [0, 180, 0] (the player in the spec example) faces +Z.
	checkV3(t, "yaw 180 forward", QuatEulerDeg(V3(0, 180, 0)).Rotate(Forward), UnitZ, 1e-6)
}

func TestQuatMulOrder(t *testing.T) {
	y90 := QuatAxisAngle(UnitY, math.Pi/2)
	x90 := QuatAxisAngle(UnitX, math.Pi/2)
	// a.Mul(b) applies b first. x90 maps +Y to +Z, then y90 maps +Z to +X.
	checkV3(t, "y90·x90 (+Y)", y90.Mul(x90).Rotate(UnitY), UnitX, 1e-6)
	// Reversed: y90 leaves +Y alone, then x90 maps it to +Z.
	checkV3(t, "x90·y90 (+Y)", x90.Mul(y90).Rotate(UnitY), UnitZ, 1e-6)

	r := newRNG(41)
	for i := 0; i < 300; i++ {
		a, b, c := r.quat(), r.quat(), r.quat()
		v := r.vec3(-3, 3)
		checkV3(t, "a·b rotate", a.Mul(b).Rotate(v), a.Rotate(b.Rotate(v)), 5e-6)
		checkM4(t, "(a·b).Mat4", a.Mul(b).Mat4(), a.Mat4().Mul(b.Mat4()), 5e-6)
		if l, r := a.Mul(b).Mul(c), a.Mul(b.Mul(c)); !approxV4(Vec4(l), Vec4(r), 5e-7) {
			t.Fatalf("Mul not associative: %v vs %v", l, r)
		}
		if l := a.Mul(b).Len(); !approx(l, 1, 1e-6) {
			t.Fatalf("|a·b| = %v", l)
		}
	}
}

func TestQuatConj(t *testing.T) {
	q := Quat{1, -2, 3, 4}
	if q.Conj() != (Quat{-1, 2, -3, 4}) {
		t.Errorf("Conj = %v", q.Conj())
	}
	if q.Conj().Conj() != q {
		t.Error("Conj is not an involution")
	}
	r := newRNG(43)
	for i := 0; i < 300; i++ {
		q := r.quat()
		v := r.vec3(-10, 10)
		if !sameRotation(q.Mul(q.Conj()), QuatIdent(), 1e-6) || !sameRotation(q.Conj().Mul(q), QuatIdent(), 1e-6) {
			t.Fatalf("q·q* = %v, want identity", q.Mul(q.Conj()))
		}
		checkV3(t, "q*(q(v))", q.Conj().Rotate(q.Rotate(v)), v, 3e-6)
		checkM4(t, "q*.Mat4", q.Conj().Mat4(), q.Mat4().Transpose(), 2e-6)
	}
}

func TestQuatDotLenNormalize(t *testing.T) {
	a, b := Quat{1, 2, 3, 4}, Quat{-1, 0.5, 2, 1}
	if got := a.Dot(b); got != -1+1+6+4 {
		t.Errorf("Dot = %v", got)
	}
	if got := (Quat{1, 2, 2, 4}).Len(); got != 5 {
		t.Errorf("Len = %v", got)
	}
	tests := []struct {
		name    string
		in, out Quat
	}{
		{"zero", Quat{}, QuatIdent()},
		{"scaledIdent", Quat{0, 0, 0, 2}, QuatIdent()},
		{"negIdent", Quat{0, 0, 0, -3}, Quat{0, 0, 0, -1}},
		{"axis", Quat{0, 4, 0, 0}, Quat{0, 1, 0, 0}},
		{"general", Quat{1, 2, 2, 4}, Quat{0.2, 0.4, 0.4, 0.8}},
	}
	for _, tc := range tests {
		got := tc.in.Normalize()
		if !approxV4(Vec4(got), Vec4(tc.out), 1e-7) {
			t.Errorf("Normalize(%s %v) = %v, want %v", tc.name, tc.in, got, tc.out)
		}
	}
	if QuatIdent().Normalize() != QuatIdent() {
		t.Error("Normalize(identity) changed it")
	}
}

func TestQuatSlerp(t *testing.T) {
	r := newRNG(47)
	for i := 0; i < 300; i++ {
		a, b := r.quat(), r.quat()
		if s := a.Slerp(b, 0); !sameRotation(s, a, 2e-6) {
			t.Fatalf("Slerp(t=0) = %v, want %v", s, a)
		}
		if s := a.Slerp(b, 1); !sameRotation(s, b, 2e-6) {
			t.Fatalf("Slerp(t=1) = %v, want %v", s, b)
		}
		// b and -b are the same rotation, so the path is the same (shortest arc).
		for _, tt := range []float32{0.1, 0.5, 0.9} {
			s1 := a.Slerp(b, tt)
			s2 := a.Slerp(Quat{-b.X, -b.Y, -b.Z, -b.W}, tt)
			if !sameRotation(s1, s2, 2e-6) {
				t.Fatalf("Slerp depends on the sign of b: %v vs %v", s1, s2)
			}
			if !approx(s1.Len(), 1, 1e-6) {
				t.Fatalf("|Slerp| = %v", s1.Len())
			}
		}
		// Constant angular velocity: the angle from a grows linearly with t.
		total := angleBetween(a, b)
		for _, tt := range []float32{0.25, 0.5, 0.75} {
			if got := angleBetween(a, a.Slerp(b, tt)); math.Abs(got-float64(float64(tt)*total)) > 2e-5 {
				t.Fatalf("angle(a, Slerp(%v)) = %v, want %v", tt, got, float64(tt)*total)
			}
		}
	}

	// Midpoint between identity and a rotation by θ is the rotation by θ/2.
	for _, th := range []float32{0.001, 0.02, 0.5, 1, 2, 3, 3.1} {
		axis := V3(1, 2, -2).Normalize()
		mid := QuatIdent().Slerp(QuatAxisAngle(axis, th), 0.5)
		if want := QuatAxisAngle(axis, th/2); !sameRotation(mid, want, 2e-6) {
			t.Errorf("Slerp midpoint for θ=%v: %v, want %v", th, mid, want)
		}
	}
	// Endpoints and midpoint when the inputs are nearly equal (the lerp branch).
	a := QuatAxisAngle(UnitY, 0.5)
	b := QuatAxisAngle(UnitY, 0.52)
	if got := a.Slerp(b, 0); !sameRotation(got, a, 1e-6) {
		t.Errorf("near Slerp(0) = %v", got)
	}
	if got := a.Slerp(b, 1); !sameRotation(got, b, 1e-6) {
		t.Errorf("near Slerp(1) = %v", got)
	}
	if got, want := a.Slerp(b, 0.5), QuatAxisAngle(UnitY, 0.51); !sameRotation(got, want, 1e-6) {
		t.Errorf("near Slerp(0.5) = %v, want %v", got, want)
	}
	if got := a.Slerp(a, 0.3); !sameRotation(got, a, 1e-7) {
		t.Errorf("Slerp(a, a) = %v", got)
	}
}

// angleBetween returns the rotation angle of a⁻¹·b in [0, pi].
func angleBetween(a, b Quat) float64 {
	d := math.Abs(float64(a.Dot(b)))
	if d > 1 {
		d = 1
	}
	return 2 * math.Acos(d)
}

func TestTRS(t *testing.T) {
	r := newRNG(53)
	for i := 0; i < 200; i++ {
		tr, q, s := r.vec3(-10, 10), r.quat(), r.scale()
		m := TRS(tr, q, s)
		checkM4(t, "TRS", m, Translate(tr).Mul(q.Mat4()).Mul(Scaling(s)), 2e-6)
		p := r.vec3(-3, 3)
		checkV3(t, "TRS point", m.MulPoint(p), tr.Add(q.Rotate(s.Mul(p))), 1e-5)
		if m.Translation() != tr {
			t.Fatalf("TRS translation = %v, want %v", m.Translation(), tr)
		}
	}
	if TRS(Vec3{}, QuatIdent(), One3) != Ident4() {
		t.Error("TRS(0, I, 1) is not the identity")
	}
}
