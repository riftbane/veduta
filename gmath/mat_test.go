package gmath

import (
	"math"
	"testing"
)

// seq is the matrix whose storage is 1..16, i.e. column c holds 4c+1..4c+4.
var seq = Mat4{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func TestMat4Layout(t *testing.T) {
	if Ident4() != (Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}) {
		t.Fatalf("Ident4 = %v", Ident4())
	}
	if Ident3() != (Mat3{1, 0, 0, 0, 1, 0, 0, 0, 1}) {
		t.Fatalf("Ident3 = %v", Ident3())
	}
	// Column-major: element (row r, column c) is m[c*4+r].
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			if got, want := seq.At(r, c), float32(c*4+r+1); got != want {
				t.Errorf("At(%d,%d) = %v, want %v", r, c, got, want)
			}
		}
	}
	if got := seq.Col(1); got != V4(5, 6, 7, 8) {
		t.Errorf("Col(1) = %v", got)
	}
	if got := seq.Row(1); got != V4(2, 6, 10, 14) {
		t.Errorf("Row(1) = %v", got)
	}
	// The translation lives in column 3 (elements 12, 13, 14).
	tr := Translate(V3(7, 8, 9))
	if tr[12] != 7 || tr[13] != 8 || tr[14] != 9 || tr.At(0, 3) != 7 || tr.Col(3) != V4(7, 8, 9, 1) {
		t.Errorf("Translate layout = %v", tr)
	}
	if got := tr.Translation(); got != V3(7, 8, 9) {
		t.Errorf("Translation = %v", got)
	}
	if got := seq.Translation(); got != V3(13, 14, 15) {
		t.Errorf("seq.Translation = %v", got)
	}
	if got := seq.Mat3(); got != (Mat3{1, 2, 3, 5, 6, 7, 9, 10, 11}) {
		t.Errorf("Mat3 = %v", got)
	}
	if got := Scaling(V3(2, 3, 4)); got != (Mat4{2, 0, 0, 0, 0, 3, 0, 0, 0, 0, 4, 0, 0, 0, 0, 1}) {
		t.Errorf("Scaling = %v", got)
	}
}

func TestMat4MulIdentityAndAssociativity(t *testing.T) {
	r := newRNG(71)
	for i := 0; i < 300; i++ {
		var a, b, c Mat4
		if i%2 == 0 {
			a, b, c = r.trs(), r.trs(), r.trs()
		} else {
			a, b, c = r.mat4(-2, 2), r.mat4(-2, 2), r.mat4(-2, 2)
		}
		if Ident4().Mul(a) != a || a.Mul(Ident4()) != a {
			t.Fatalf("identity not neutral for %v", a)
		}
		checkM4(t, "(AB)C vs A(BC)", a.Mul(b).Mul(c), a.Mul(b.Mul(c)), 1e-5)
		// Every element is a row of a dotted with a column of b.
		ab := a.Mul(b)
		for row := 0; row < 4; row++ {
			for col := 0; col < 4; col++ {
				if got, want := ab.At(row, col), a.Row(row).Dot(b.Col(col)); !approx(got, want, 1e-6) {
					t.Fatalf("(AB)[%d,%d] = %v, want %v", row, col, got, want)
				}
			}
		}
		// (AB)v = A(Bv).
		v := V4(r.f(-3, 3), r.f(-3, 3), r.f(-3, 3), 1)
		if l, rr := ab.MulVec4(v), a.MulVec4(b.MulVec4(v)); !approxV4(l, rr, 1e-5) {
			t.Fatalf("(AB)v = %v, A(Bv) = %v", l, rr)
		}
	}
	// Hand-computed product.
	a := Mat4{1, 0, 0, 0, 2, 1, 0, 0, 0, 0, 1, 0, 3, 4, 5, 1}
	b := Scaling(V3(2, 3, 4))
	if got, want := a.Mul(b), (Mat4{2, 0, 0, 0, 6, 3, 0, 0, 0, 0, 4, 0, 3, 4, 5, 1}); got != want {
		t.Errorf("a·b = %v, want %v", got, want)
	}
	if got, want := b.Mul(a), (Mat4{2, 0, 0, 0, 4, 3, 0, 0, 0, 0, 4, 0, 6, 12, 20, 1}); got != want {
		t.Errorf("b·a = %v, want %v", got, want)
	}
}

func TestMat4MulOrderApplies(t *testing.T) {
	// Vectors multiply on the right, so in T·R the rotation happens first.
	tr := Translate(V3(10, 0, 0))
	ry := RotateY(Radians(90))
	checkV3(t, "T·R·X", tr.Mul(ry).MulPoint(UnitX), V3(10, 0, -1), 1e-6)
	checkV3(t, "R·T·X", ry.Mul(tr).MulPoint(UnitX), V3(0, 0, -11), 1e-6)
}

func TestMat4MulVec4PointDir(t *testing.T) {
	if got := seq.MulVec4(V4(1, 0, 0, 0)); got != seq.Col(0) {
		t.Errorf("seq·e0 = %v, want column 0", got)
	}
	if got := seq.MulVec4(V4(1, 1, 1, 1)); got != V4(28, 32, 36, 40) {
		t.Errorf("seq·1 = %v", got)
	}
	if got := seq.MulPoint(V3(1, 1, 1)); got != V3(28, 32, 36) {
		t.Errorf("seq.MulPoint(1) = %v", got)
	}
	if got := seq.MulDir(V3(1, 1, 1)); got != V3(15, 18, 21) {
		t.Errorf("seq.MulDir(1) = %v", got)
	}
	r := newRNG(73)
	for i := 0; i < 500; i++ {
		m := r.mat4(-3, 3)
		if i%2 == 0 {
			m = r.trs()
		}
		p := r.vec3(-10, 10)
		v := m.MulVec4(p.Vec4(1))
		for row := 0; row < 4; row++ {
			if !approx([4]float32{v.X, v.Y, v.Z, v.W}[row], m.Row(row).Dot(p.Vec4(1)), 1e-6) {
				t.Fatalf("MulVec4 row %d disagrees with Row·v", row)
			}
		}
		checkV3(t, "MulPoint", m.MulPoint(p), v.XYZ(), 1e-6)
		checkV3(t, "MulDir", m.MulDir(p), m.MulVec4(p.Vec4(0)).XYZ(), 1e-6)
		checkV3(t, "Mat3·d", m.Mat3().MulVec3(p), m.MulDir(p), 1e-6)
		// A direction ignores translation; a point does not.
		tr := r.vec3(-5, 5)
		checkV3(t, "T.MulDir", Translate(tr).MulDir(p), p, 0)
		checkV3(t, "T.MulPoint", Translate(tr).MulPoint(p), p.Add(tr), 1e-6)
		// For affine matrices Project is MulPoint.
		if i%2 == 0 {
			checkV3(t, "Project(affine)", m.Project(p), m.MulPoint(p), 1e-6)
		}
	}
}

func TestMat4Transpose(t *testing.T) {
	st := seq.Transpose()
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			if st.At(r, c) != seq.At(c, r) {
				t.Fatalf("Transpose(%d,%d) = %v, want %v", r, c, st.At(r, c), seq.At(c, r))
			}
		}
		if st.Row(r) != seq.Col(r) || st.Col(r) != seq.Row(r) {
			t.Fatalf("rows and columns not swapped at %d", r)
		}
	}
	if st.Transpose() != seq {
		t.Error("Transpose is not an involution")
	}
	if Ident4().Transpose() != Ident4() {
		t.Error("Iᵀ != I")
	}
	r := newRNG(79)
	for i := 0; i < 200; i++ {
		a, b := r.mat4(-2, 2), r.mat4(-2, 2)
		checkM4(t, "(AB)ᵀ", a.Mul(b).Transpose(), b.Transpose().Mul(a.Transpose()), 1e-6)
	}
}

func TestMat4Inverse(t *testing.T) {
	// Well-conditioned affine matrices from a seeded generator.
	r := newRNG(83)
	for i := 0; i < 500; i++ {
		a := r.trs()
		inv, ok := a.Inverse()
		if !ok {
			t.Fatalf("TRS matrix reported singular: %v", a)
		}
		checkM4(t, "A·A⁻¹", a.Mul(inv), Ident4(), 2e-5)
		checkM4(t, "A⁻¹·A", inv.Mul(a), Ident4(), 2e-5)
		p := r.vec3(-10, 10)
		checkV3(t, "A⁻¹(A(p))", inv.MulPoint(a.MulPoint(p)), p, 2e-5)
	}
	// General (projective) matrices, diagonally dominant so they stay well conditioned.
	for i := 0; i < 300; i++ {
		a := r.mat4(-1, 1)
		for d := 0; d < 4; d++ {
			a[d*5] += 4
		}
		inv, ok := a.Inverse()
		if !ok {
			t.Fatalf("matrix reported singular: %v", a)
		}
		checkM4(t, "A·A⁻¹ (general)", a.Mul(inv), Ident4(), 2e-6)
		checkM4(t, "A⁻¹·A (general)", inv.Mul(a), Ident4(), 2e-6)
	}
	known := []struct {
		name   string
		m, inv Mat4
	}{
		{"identity", Ident4(), Ident4()},
		{"translate", Translate(V3(1, -2, 3)), Translate(V3(-1, 2, -3))},
		{"scale", Scaling(V3(2, -4, 0.5)), Scaling(V3(0.5, -0.25, 2))},
		{"rotateY", RotateY(0.7), RotateY(0.7).Transpose()},
		{"rotateX", RotateX(-1.3), RotateX(1.3)},
		{"rotateZ", RotateZ(2.2), RotateZ(-2.2)},
		{"perspective", Perspective(1, 1.5, 1, 10), perspectiveInverse(1, 1.5, 1, 10)},
	}
	for _, tc := range known {
		inv, ok := tc.m.Inverse()
		if !ok {
			t.Errorf("%s: reported singular", tc.name)
			continue
		}
		checkM4(t, tc.name+" inverse", inv, tc.inv, 2e-6)
	}
}

// perspectiveInverse is the closed-form inverse of Perspective.
func perspectiveInverse(fovY, aspect, n, f float32) Mat4 {
	g := float32(math.Tan(float64(fovY) / 2))
	return Mat4{
		g * aspect, 0, 0, 0,
		0, g, 0, 0,
		0, 0, 0, (n - f) / (2 * f * n),
		0, 0, -1, (f + n) / (2 * f * n),
	}
}

func TestMat4InverseSingular(t *testing.T) {
	tests := []struct {
		name string
		m    Mat4
	}{
		{"zero", Mat4{}},
		{"flatten Y", Scaling(V3(1, 0, 1))},
		{"flatten after rotate", Scaling(V3(0, 1, 1)).Mul(Translate(V3(1, 2, 3)))},
		{"duplicate columns", Mat4{1, 2, 3, 4, 1, 2, 3, 4, 5, 6, 7, 8, 0, 1, 0, 1}},
		{"col2 = col0+col1", Mat4{1, 2, 0, 1, 0, 1, 3, 2, 1, 3, 3, 3, 4, 0, 1, 7}},
		{"no w row", Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0}},
		{"rank one", Mat4{1, 2, 3, 4, 2, 4, 6, 8, 3, 6, 9, 12, 4, 8, 12, 16}},
		{"seq", seq},
	}
	for _, tc := range tests {
		inv, ok := tc.m.Inverse()
		if ok {
			t.Errorf("%s: Inverse reported ok for a singular matrix (got %v)", tc.name, inv)
		}
		if inv != Ident4() {
			t.Errorf("%s: singular Inverse = %v, want identity", tc.name, inv)
		}
		if d := tc.m.Det(); d != 0 {
			t.Errorf("%s: Det = %v, want 0", tc.name, d)
		}
	}
	// Invertible in exact arithmetic but not representable: a subnormal scale's
	// reciprocal overflows float32, and NaN/Inf elements poison every element. These
	// used to report ok with Inf/NaN in the result (and NormalMatrix then fed NaN
	// normals to the rasterizer).
	sub := math.Float32frombits(1)
	nonFinite := []struct {
		name string
		m    Mat4
	}{
		{"subnormal scale", Scaling(V3(sub, 1, 1))},
		{"tiny uniform scale", Scaling(V3(1e-39, 1e-39, 1e-39))},
		{"NaN element", Scaling(V3(nan32, 1, 1))},
		{"Inf element", Translate(V3(inf32, 0, 0))},
	}
	for _, tc := range nonFinite {
		if inv, ok := tc.m.Inverse(); ok || inv != Ident4() {
			t.Errorf("%s: Inverse = %v, %v; want identity, false", tc.name, inv, ok)
		}
		if inv, ok := tc.m.Mat3().Inverse(); tc.name != "Inf element" && (ok || inv != Ident3()) {
			t.Errorf("%s: Mat3.Inverse = %v, %v; want identity, false", tc.name, inv, ok)
		}
	}
	if nm := Scaling(V3(sub, 1, 1)).NormalMatrix(); nm != Scaling(V3(sub, 1, 1)).Mat3() {
		t.Errorf("NormalMatrix(subnormal scale) = %v, want the block itself", nm)
	}
	// Small but representable scales still invert.
	if inv, ok := Scaling(V3(1e-30, 1e-30, 1e-30)).Inverse(); !ok || !approx(inv[0], 1e30, 1e-6) {
		t.Errorf("Inverse(scale 1e-30) = %v, %v", inv, ok)
	}
}

func TestMat4Det(t *testing.T) {
	upper := Mat4{2, 0, 0, 0, 7, 3, 0, 0, -1, 5, 4, 0, 9, 8, 6, 0.5}
	tests := []struct {
		name string
		m    Mat4
		want float32
	}{
		{"identity", Ident4(), 1},
		{"scale", Scaling(V3(2, 3, 4)), 24},
		{"mirror", Scaling(V3(-1, 1, 1)), -1},
		{"translate", Translate(V3(5, 6, 7)), 1},
		{"upper triangular", upper, 12},
		{"lower triangular", upper.Transpose(), 12},
		{"swap X and Y", Mat4{0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}, -1},
		{"swap Z and W", Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 1, 0}, -1},
		{"integer", Mat4{2, 1, 0, 3, 1, 3, 2, 0, 0, 1, 4, 1, 1, 0, 2, 5}, 32}, // exact, by cofactor expansion
		{"rotation", RotateY(0.3).Mul(RotateX(1.1)), 1},
	}
	for _, tc := range tests {
		if got := tc.m.Det(); !approx(got, tc.want, 1e-6) {
			t.Errorf("%s: Det = %v, want %v", tc.name, got, tc.want)
		}
	}
	r := newRNG(89)
	for i := 0; i < 300; i++ {
		a, b := r.mat4(-2, 2), r.mat4(-2, 2)
		if got, want := a.Mul(b).Det(), a.Det()*b.Det(); !approx(got, want, 1e-4) {
			t.Fatalf("det(AB) = %v, det(A)det(B) = %v", got, want)
		}
		if got, want := a.Transpose().Det(), a.Det(); !approx(got, want, 1e-6) {
			t.Fatalf("det(Aᵀ) = %v, det(A) = %v", got, want)
		}
		tr, q, s := r.vec3(-10, 10), r.quat(), r.scale()
		if got, want := TRS(tr, q, s).Det(), s.X*s.Y*s.Z; !approx(got, want, 1e-5) {
			t.Fatalf("det(TRS) = %v, want %v", got, want)
		}
		if inv, ok := a.Inverse(); ok && a.Det() > 0.1 {
			if got := inv.Det() * a.Det(); !approx(got, 1, 1e-4) {
				t.Fatalf("det(A⁻¹)det(A) = %v", got)
			}
		}
	}
}

func TestMat3(t *testing.T) {
	a := Mat3{1, 2, 3, 4, 5, 6, 7, 8, 10} // columns (1,2,3), (4,5,6), (7,8,10)
	if got := a.Det(); got != -3 {
		t.Errorf("Det = %v, want -3", got)
	}
	if got := a.MulVec3(UnitX); got != V3(1, 2, 3) {
		t.Errorf("a·X = %v, want column 0", got)
	}
	if got := a.MulVec3(One3); got != V3(12, 15, 19) {
		t.Errorf("a·1 = %v", got)
	}
	if got := a.Transpose(); got != (Mat3{1, 4, 7, 2, 5, 8, 3, 6, 10}) {
		t.Errorf("Transpose = %v", got)
	}
	if a.Transpose().Transpose() != a {
		t.Error("Transpose is not an involution")
	}
	if Ident3().Mul(a) != a || a.Mul(Ident3()) != a {
		t.Error("Ident3 not neutral")
	}
	inv, ok := a.Inverse()
	if !ok {
		t.Fatal("a reported singular")
	}
	if !approxM3(a.Mul(inv), Ident3(), 1e-6) || !approxM3(inv.Mul(a), Ident3(), 1e-6) {
		t.Errorf("a·a⁻¹ = %v", a.Mul(inv))
	}
	// Rows of a⁻¹ are (-2/3, -2/3, 1), (-4/3, 11/3, -2), (1, -2, 1); stored by column.
	want := Mat3{-2.0 / 3, -4.0 / 3, 1, -2.0 / 3, 11.0 / 3, -2, 1, -2, 1}
	if !approxM3(inv, want, 1e-6) {
		t.Errorf("Inverse = %v, want %v", inv, want)
	}
	singular := []Mat3{{}, {1, 2, 3, 2, 4, 6, 0, 0, 1}, {1, 0, 0, 0, 0, 0, 0, 0, 1}}
	for _, s := range singular {
		if got, ok := s.Inverse(); ok || got != Ident3() {
			t.Errorf("Inverse(%v) = %v, %v; want identity, false", s, got, ok)
		}
		if s.Det() != 0 {
			t.Errorf("Det(%v) = %v", s, s.Det())
		}
	}
	r := newRNG(97)
	for i := 0; i < 300; i++ {
		// With zero translation, the 3×3 block of a 4×4 product is the product of the blocks.
		m4, n4 := r.trs(), r.trs()
		m4[12], m4[13], m4[14] = 0, 0, 0
		m3, n3 := m4.Mat3(), n4.Mat3()
		if !approxM3(m3.Mul(n3), m4.Mul(n4).Mat3(), 1e-6) {
			t.Fatalf("Mat3.Mul disagrees with Mat4.Mul")
		}
		v := r.vec3(-5, 5)
		checkV3(t, "Mat3.MulVec3", m3.MulVec3(v), m4.MulDir(v), 1e-6)
		if got, want := m3.Det(), m4.Det(); !approx(got, want, 1e-5) {
			t.Fatalf("Mat3.Det %v, Mat4.Det %v", got, want)
		}
		inv, ok := m3.Inverse()
		if !ok || !approxM3(m3.Mul(inv), Ident3(), 1e-5) {
			t.Fatalf("Mat3 inverse failed: %v %v", ok, m3.Mul(inv))
		}
		if !approxM3(m3.Transpose().Transpose(), m3, 0) || !approxM3(m3.Mul(n3).Transpose(), n3.Transpose().Mul(m3.Transpose()), 1e-6) {
			t.Fatal("Mat3 transpose identities fail")
		}
	}
}

func TestNormalMatrix(t *testing.T) {
	r := newRNG(109)
	scales := []Vec3{V3(1, 4, 0.25), V3(-2, 0.5, 3), V3(10, 1, 1), V3(0.1, 0.1, 5)}
	vacuous := true
	for i := 0; i < 400; i++ {
		s := scales[i%len(scales)]
		if i >= 200 {
			s = r.scale()
		}
		m := TRS(r.vec3(-5, 5), r.quat(), s)
		nm := m.NormalMatrix()
		n := r.dir()
		tan := n.Cross(r.dir()).Normalize()
		tn := nm.MulVec3(n).Normalize()
		tt := m.MulDir(tan).Normalize()
		if d := tn.Dot(tt); Abs(d) > 1e-5 {
			t.Fatalf("scale %v: transformed normal·tangent = %v, want 0", s, d)
		}
		if Abs(m.MulDir(n).Normalize().Dot(tt)) > 0.05 {
			vacuous = false
		}
	}
	if vacuous {
		t.Error("M·n stayed perpendicular too; the non-uniform scale cases do not test anything")
	}
	// Pure rotation: the normal matrix is the rotation itself.
	q := r.quat()
	if got, want := q.Mat4().NormalMatrix(), q.Mat4().Mat3(); !approxM3(got, want, 2e-6) {
		t.Errorf("NormalMatrix(R) = %v, want %v", got, want)
	}
	// Translation does not affect it.
	if got := Translate(V3(3, 4, 5)).NormalMatrix(); got != Ident3() {
		t.Errorf("NormalMatrix(T) = %v", got)
	}
	// Documented fallback: a singular block yields the block itself.
	flat := Scaling(V3(1, 0, 1))
	if got := flat.NormalMatrix(); got != flat.Mat3() {
		t.Errorf("NormalMatrix(singular) = %v, want %v", got, flat.Mat3())
	}
}

func TestRotationConventions(t *testing.T) {
	// Right-handed, Y up, -Z forward; positive angles are counter-clockwise when looking
	// down the axis towards the origin.
	r90 := Radians(90)
	tests := []struct {
		name    string
		m       Mat4
		in, out Vec3
	}{
		{"RotateY(+90) +X -> -Z", RotateY(r90), UnitX, Forward},
		{"RotateY(+90) +Z -> +X", RotateY(r90), UnitZ, UnitX},
		{"RotateY(+90) -Z -> -X (turn left)", RotateY(r90), Forward, UnitX.Neg()},
		{"RotateY(+90) +Y fixed", RotateY(r90), UnitY, UnitY},
		{"RotateY(-90) +X -> +Z", RotateY(-r90), UnitX, UnitZ},
		{"RotateX(+90) +Y -> +Z", RotateX(r90), UnitY, UnitZ},
		{"RotateX(+90) +Z -> -Y", RotateX(r90), UnitZ, UnitY.Neg()},
		{"RotateX(+90) -Z -> +Y (pitch up)", RotateX(r90), Forward, UnitY},
		{"RotateX(+90) +X fixed", RotateX(r90), UnitX, UnitX},
		{"RotateZ(+90) +X -> +Y", RotateZ(r90), UnitX, UnitY},
		{"RotateZ(+90) +Y -> -X", RotateZ(r90), UnitY, UnitX.Neg()},
		{"RotateZ(+90) +Z fixed", RotateZ(r90), UnitZ, UnitZ},
		{"RotateX(180) +Y -> -Y", RotateX(Pi), UnitY, UnitY.Neg()},
		{"RotateY(180) -Z -> +Z", RotateY(Pi), Forward, UnitZ},
	}
	for _, tc := range tests {
		checkV3(t, tc.name, tc.m.MulDir(tc.in), tc.out, 1e-6)
		checkV3(t, tc.name+" (point)", tc.m.MulPoint(tc.in), tc.out, 1e-6)
	}
	// Column-major storage: column 0 is the image of +X.
	if c := RotateY(r90).Col(0).XYZ(); !approxV3(c, Forward, 1e-6) {
		t.Errorf("RotateY(90) column 0 = %v, want -Z (is the matrix row-major?)", c)
	}
	rots := []func(float32) Mat4{RotateX, RotateY, RotateZ}
	names := []string{"RotateX", "RotateY", "RotateZ"}
	for k, rot := range rots {
		for _, a := range []float32{-2.5, -0.4, 0, 0.3, 1.7, 3} {
			m := rot(a)
			if d := m.Det(); !approx(d, 1, 1e-6) {
				t.Errorf("%s(%v) det = %v", names[k], a, d)
			}
			checkM4(t, names[k]+" RᵀR", m.Transpose().Mul(m), Ident4(), 1e-6)
			checkM4(t, names[k]+" inverse", m.Transpose(), rot(-a), 1e-6)
			checkM4(t, names[k]+" additive", m.Mul(rot(0.9)), rot(a+0.9), 2e-6)
		}
		if rot(0) != Ident4() {
			t.Errorf("%s(0) = %v", names[k], rot(0))
		}
		checkM4(t, names[k]+"(2π)", rot(2*Pi), Ident4(), 1e-6)
	}
}

func TestPerspective(t *testing.T) {
	cases := []struct{ fovDeg, aspect, near, far float32 }{
		{60, 16.0 / 9, 0.1, 200},
		{90, 1, 1, 100},
		{30, 0.5, 0.01, 10},
		{120, 2, 0.5, 1000},
	}
	r := newRNG(113)
	for _, c := range cases {
		p := Perspective(Radians(c.fovDeg), c.aspect, c.near, c.far)
		th := float32(math.Tan(float64(Radians(c.fovDeg)) / 2))
		if z := p.Project(V3(0, 0, -c.near)).Z; !approx(z, -1, 1e-5) {
			t.Errorf("%v: near plane -> NDC z %v, want -1", c, z)
		}
		if z := p.Project(V3(0, 0, -c.far)).Z; !approx(z, 1, 1e-5) {
			t.Errorf("%v: far plane -> NDC z %v, want +1", c, z)
		}
		// Clip w is the distance in front of the eye.
		if w := p.MulVec4(V4(1, 2, -5, 1)).W; w != 5 {
			t.Errorf("%v: clip w = %v, want 5", c, w)
		}
		if w := p.MulVec4(V4(0, 0, 3, 1)).W; w >= 0 {
			t.Errorf("%v: point behind the eye has w = %v, want < 0", c, w)
		}
		// Frustum corners on both planes map to (±1, ±1).
		for _, d := range []float32{c.near, c.far} {
			for _, sx := range []float32{-1, 1} {
				for _, sy := range []float32{-1, 1} {
					ndc := p.Project(V3(sx*d*th*c.aspect, sy*d*th, -d))
					if !approx(ndc.X, sx, 1e-5) || !approx(ndc.Y, sy, 1e-5) {
						t.Errorf("%v: corner (%v,%v) at depth %v -> %v", c, sx, sy, d, ndc)
					}
				}
			}
		}
		// Points inside the frustum stay inside the NDC cube; +X right, +Y up.
		prevZ := float32(-2)
		for i := 0; i < 500; i++ {
			d := c.near + (c.far-c.near)*float32(i)/499
			u, v := r.f(-1, 1), r.f(-1, 1)
			ndc := p.Project(V3(u*d*th*c.aspect, v*d*th, -d))
			if Abs(ndc.X) > 1+1e-5 || Abs(ndc.Y) > 1+1e-5 || ndc.Z < -1-1e-5 || ndc.Z > 1+1e-5 {
				t.Fatalf("%v: interior point -> %v outside NDC", c, ndc)
			}
			if !approx(ndc.X, u, 1e-5) || !approx(ndc.Y, v, 1e-5) {
				t.Fatalf("%v: interior (%v,%v) -> %v", c, u, v, ndc)
			}
			if ndc.Z < prevZ {
				t.Fatalf("%v: depth not monotone at %v: %v < %v", c, d, ndc.Z, prevZ)
			}
			prevZ = ndc.Z
		}
		// Points outside map outside.
		if z := p.Project(V3(0, 0, -c.far*1.5)).Z; z <= 1 {
			t.Errorf("%v: beyond far -> z %v", c, z)
		}
		if z := p.Project(V3(0, 0, -c.near*0.5)).Z; z >= -1 {
			t.Errorf("%v: before near -> z %v", c, z)
		}
		d := (c.near + c.far) / 2
		if x := p.Project(V3(1.1*d*th*c.aspect, 0, -d)).X; x <= 1 {
			t.Errorf("%v: right of frustum -> x %v", c, x)
		}
		if y := p.Project(V3(0, -1.1*d*th, -d)).Y; y >= -1 {
			t.Errorf("%v: below frustum -> y %v", c, y)
		}
	}
	// w == 0 (a point in the eye plane) returns the raw clip coordinates.
	p := Perspective(1, 1, 0.1, 10)
	if got := p.Project(V3(1, 2, 0)); !got.IsFinite() || got != p.MulVec4(V4(1, 2, 0, 1)).XYZ() {
		t.Errorf("Project at w=0 = %v", got)
	}
}

func TestOrthographic(t *testing.T) {
	cases := []struct{ l, r, b, t, n, f float32 }{
		{-2, 2, -1, 1, 0.1, 100},
		{0, 640, 0, 360, -1, 1},
		{-10, 5, 3, 7, 1, 2},
		{-1, 1, -1, 1, -5, 5},
	}
	for _, c := range cases {
		o := Orthographic(c.l, c.r, c.b, c.t, c.n, c.f)
		for i := 0; i < 8; i++ {
			x, y, z := c.l, c.b, -c.n
			want := V3(-1, -1, -1)
			if i&1 != 0 {
				x, want.X = c.r, 1
			}
			if i&2 != 0 {
				y, want.Y = c.t, 1
			}
			if i&4 != 0 {
				z, want.Z = -c.f, 1
			}
			v := o.MulVec4(V4(x, y, z, 1))
			if v.W != 1 {
				t.Errorf("%v: orthographic w = %v", c, v.W)
			}
			if !approxV3(v.XYZ(), want, 1e-5) {
				t.Errorf("%v: corner (%v,%v,%v) -> %v, want %v", c, x, y, z, v.XYZ(), want)
			}
		}
		centre := V3((c.l+c.r)/2, (c.b+c.t)/2, -(c.n+c.f)/2)
		checkV3(t, "ortho centre", o.Project(centre), Zero3, 1e-5)
	}
}

func TestLookAt(t *testing.T) {
	// vertical: straight up or down; zeroUp: no up given; parallelUp: up along the view
	// direction, which falls back to +Y.
	cases := []struct {
		name                         string
		eye, target, up              Vec3
		vertical, zeroUp, parallelUp bool
	}{
		{name: "default", eye: V3(0, 0, 5), target: Zero3, up: Up},
		{name: "spec camera", eye: V3(0, 5, 10), target: Zero3, up: Up},
		{name: "offset", eye: V3(3, -2, 7), target: V3(-1, 4, 0.5), up: Up},
		{name: "along -X", eye: V3(10, 1, 0), target: V3(0, 1, 0), up: Up},
		{name: "along +Z", eye: V3(0, 0, -5), target: Zero3, up: Up},
		{name: "unnormalized up", eye: V3(1, 2, 3), target: V3(4, 0, -2), up: V3(0, 7, 0)},
		{name: "tilted up", eye: V3(1, 2, 3), target: V3(4, 0, -2), up: V3(0.3, 1, 0.2)},
		{name: "top-down", eye: V3(0, 10, 0), target: Zero3, up: Up, vertical: true},
		{name: "top-down offset", eye: V3(2, 8, -3), target: V3(2, 0, -3), up: Up, vertical: true},
		{name: "top-down up=-Y", eye: V3(0, 10, 0), target: Zero3, up: V3(0, -1, 0), vertical: true},
		{name: "bottom-up", eye: V3(0, -10, 0), target: Zero3, up: Up, vertical: true},
		{name: "zero up", eye: V3(1, 1, 1), target: Zero3, up: Zero3, zeroUp: true},
		{name: "along -Z, up=+Z", eye: V3(0, 0, 5), target: Zero3, up: UnitZ, parallelUp: true},
		{name: "along +Z, up=-Z", eye: V3(0, 0, -5), target: Zero3, up: UnitZ.Neg(), parallelUp: true},
	}
	for _, c := range cases {
		v := LookAt(c.eye, c.target, c.up)
		for i, e := range v {
			if !IsFinite(e) {
				t.Fatalf("%s: element %d is %v", c.name, i, e)
			}
		}
		if v.Row(3) != V4(0, 0, 0, 1) {
			t.Errorf("%s: bottom row %v", c.name, v.Row(3))
		}
		rot := v
		rot[12], rot[13], rot[14] = 0, 0, 0
		checkM4(t, c.name+" RᵀR", rot.Transpose().Mul(rot), Ident4(), 2e-6)
		if d := v.Det(); !approx(d, 1, 2e-6) {
			t.Errorf("%s: det = %v, want +1 (proper rotation, no mirror)", c.name, d)
		}
		checkV3(t, c.name+" eye -> origin", v.MulPoint(c.eye), Zero3, 2e-6)
		dist := c.target.Dist(c.eye)
		checkV3(t, c.name+" target -> -Z axis", v.MulPoint(c.target), V3(0, 0, -dist), 2e-6)
		checkV3(t, c.name+" view dir -> -Z", v.MulDir(c.target.Sub(c.eye).Normalize()), Forward, 2e-6)
		inv, ok := v.Inverse()
		if !ok {
			t.Fatalf("%s: view matrix singular", c.name)
		}
		checkV3(t, c.name+" camera position", inv.MulPoint(Zero3), c.eye, 2e-6)
		switch {
		case c.vertical:
			// Straight down or up: world -Z points up on screen, and world up is along the
			// eye-space Z axis.
			checkV3(t, c.name+" world -Z -> eye +Y", v.MulDir(Forward), UnitY, 1e-6)
			if z := v.MulDir(Up).Z; Abs(z) < 0.9999 {
				t.Errorf("%s: world up -> eye %v, want ±Z", c.name, v.MulDir(Up))
			}
		case !c.zeroUp:
			// Screen up: the up vector lands in the eye-space YZ plane with positive Y.
			up := c.up
			if c.parallelUp {
				up = Up
			}
			e := v.MulDir(up.Normalize())
			if !approx(e.X, 0, 2e-6) || e.Y <= 0 {
				t.Errorf("%s: up -> eye %v, want x = 0, y > 0", c.name, e)
			}
		}
	}
	// Up = +Y, level view: screen up is eye +Y, screen right is eye +X.
	v := LookAt(V3(0, 0, 5), Zero3, Up)
	checkV3(t, "level up", v.MulDir(Up), UnitY, 0)
	checkV3(t, "level right", v.MulDir(RightDir), UnitX, 0)
	checkV3(t, "above target", v.MulPoint(V3(0, 1, 0)), V3(0, 1, -5), 1e-6)
	// The canonical camera is the identity.
	checkM4(t, "LookAt(0, -Z, Y)", LookAt(Zero3, Forward, Up), Ident4(), 0)
	// Top-down: screen right is world +X, screen up is world -Z.
	td := LookAt(V3(0, 10, 0), Zero3, Up)
	checkV3(t, "top-down right", td.MulDir(RightDir), UnitX, 1e-6)
	checkV3(t, "top-down world up -> towards viewer", td.MulDir(Up), UnitZ, 1e-6)
	checkV3(t, "top-down point", td.MulPoint(V3(1, 0, -2)), V3(1, 2, -10), 1e-6)
	// Along ±Z with a parallel up falls back to +Y up.
	checkM4(t, "along -Z with up=Z", LookAt(V3(0, 0, 5), Zero3, UnitZ), LookAt(V3(0, 0, 5), Zero3, Up), 1e-6)
	// Eye at the target: well defined, looks down -Z.
	eye := V3(1, 2, 3)
	checkM4(t, "eye == target", LookAt(eye, eye, Up), Translate(eye.Neg()), 1e-6)
}

func TestLookAtWithPerspective(t *testing.T) {
	// End to end: the target lands at the centre of the screen, a point above it lands
	// above the centre, a point to the camera's right lands right of it.
	eye, target := V3(4, 3, 6), V3(0, 1, 0)
	vp := Perspective(Radians(60), 16.0/9, 0.1, 100).Mul(LookAt(eye, target, Up))
	c := vp.Project(target)
	if !approx(c.X, 0, 1e-5) || !approx(c.Y, 0, 1e-5) || c.Z <= -1 || c.Z >= 1 {
		t.Errorf("target -> %v, want screen centre", c)
	}
	if above := vp.Project(target.Add(V3(0, 0.5, 0))); above.Y <= 0 || !approx(above.X, 0, 1e-5) {
		t.Errorf("point above target -> %v", above)
	}
	right := target.Sub(eye).Cross(Up).Normalize()
	if pr := vp.Project(target.Add(right)); pr.X <= 0 || !approx(pr.Y, 0, 1e-5) {
		t.Errorf("point right of target -> %v", pr)
	}
}
