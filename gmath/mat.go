package gmath

// Mat3 is a 3×3 column-major matrix: element (row r, column c) is m[c*3+r].
type Mat3 [9]float32

// Mat4 is a 4×4 column-major matrix: element (row r, column c) is m[c*4+r].
// Vectors are columns and are multiplied on the right: v' = M·v.
type Mat4 [16]float32

// Ident3 returns the 3×3 identity.
func Ident3() Mat3 { return Mat3{1, 0, 0, 0, 1, 0, 0, 0, 1} }

// Ident4 returns the 4×4 identity.
func Ident4() Mat4 { return Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1} }

// At returns element (row r, column c).
func (a Mat4) At(r, c int) float32 { return a[c*4+r] }

// Col returns column c.
func (a Mat4) Col(c int) Vec4 { return Vec4{a[c*4], a[c*4+1], a[c*4+2], a[c*4+3]} }

// Row returns row r.
func (a Mat4) Row(r int) Vec4 { return Vec4{a[r], a[4+r], a[8+r], a[12+r]} }

// Mul returns a·b.
func (a Mat4) Mul(b Mat4) Mat4 {
	var o Mat4
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			o[c*4+r] = a[r]*b[c*4] + a[4+r]*b[c*4+1] + a[8+r]*b[c*4+2] + a[12+r]*b[c*4+3]
		}
	}
	return o
}

// MulVec4 returns a·v.
func (a Mat4) MulVec4(v Vec4) Vec4 {
	return Vec4{
		a[0]*v.X + a[4]*v.Y + a[8]*v.Z + a[12]*v.W,
		a[1]*v.X + a[5]*v.Y + a[9]*v.Z + a[13]*v.W,
		a[2]*v.X + a[6]*v.Y + a[10]*v.Z + a[14]*v.W,
		a[3]*v.X + a[7]*v.Y + a[11]*v.Z + a[15]*v.W,
	}
}

// MulPoint transforms the point p (w = 1) without the perspective divide.
//
// Each product is rounded explicitly (a float32 conversion is a rounding point the
// compiler may not fuse into an FMA), so the result is the same on every architecture
// and AABB.Transform, which sums the same terms in the same order, always contains it.
func (a Mat4) MulPoint(p Vec3) Vec3 {
	return Vec3{
		float32(a[0]*p.X) + float32(a[4]*p.Y) + float32(a[8]*p.Z) + a[12],
		float32(a[1]*p.X) + float32(a[5]*p.Y) + float32(a[9]*p.Z) + a[13],
		float32(a[2]*p.X) + float32(a[6]*p.Y) + float32(a[10]*p.Z) + a[14],
	}
}

// MulDir transforms the direction d (w = 0).
func (a Mat4) MulDir(d Vec3) Vec3 {
	return Vec3{
		a[0]*d.X + a[4]*d.Y + a[8]*d.Z,
		a[1]*d.X + a[5]*d.Y + a[9]*d.Z,
		a[2]*d.X + a[6]*d.Y + a[10]*d.Z,
	}
}

// Project transforms p (w = 1) and divides by the resulting w.
func (a Mat4) Project(p Vec3) Vec3 {
	v := a.MulVec4(p.Vec4(1))
	if v.W == 0 {
		return v.XYZ()
	}
	return Vec3{v.X / v.W, v.Y / v.W, v.Z / v.W}
}

// Transpose returns aᵀ.
func (a Mat4) Transpose() Mat4 {
	var o Mat4
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			o[r*4+c] = a[c*4+r]
		}
	}
	return o
}

// Translation returns the translation part (column 3).
func (a Mat4) Translation() Vec3 { return Vec3{a[12], a[13], a[14]} }

// Mat3 returns the upper-left 3×3 block.
func (a Mat4) Mat3() Mat3 {
	return Mat3{a[0], a[1], a[2], a[4], a[5], a[6], a[8], a[9], a[10]}
}

// Det returns the determinant (computed in float64).
func (a Mat4) Det() float32 {
	inv, det := a.inverse64()
	_ = inv
	return float32(det)
}

// Inverse returns a⁻¹ and whether a was invertible. A singular matrix, or one whose
// inverse is not finite in float32 (a NaN or Inf element, or a scale so small that its
// reciprocal overflows), yields the identity and false.
func (a Mat4) Inverse() (Mat4, bool) {
	inv, det := a.inverse64()
	if det == 0 {
		return Ident4(), false
	}
	var o Mat4
	for i := range o {
		o[i] = float32(inv[i] / det)
		if !IsFinite(o[i]) {
			return Ident4(), false
		}
	}
	return o, true
}

// inverse64 returns the adjugate and determinant in float64.
func (a Mat4) inverse64() (adj [16]float64, det float64) {
	var m [16]float64
	for i, v := range a {
		m[i] = float64(v)
	}
	adj[0] = m[5]*m[10]*m[15] - m[5]*m[11]*m[14] - m[9]*m[6]*m[15] + m[9]*m[7]*m[14] + m[13]*m[6]*m[11] - m[13]*m[7]*m[10]
	adj[4] = -m[4]*m[10]*m[15] + m[4]*m[11]*m[14] + m[8]*m[6]*m[15] - m[8]*m[7]*m[14] - m[12]*m[6]*m[11] + m[12]*m[7]*m[10]
	adj[8] = m[4]*m[9]*m[15] - m[4]*m[11]*m[13] - m[8]*m[5]*m[15] + m[8]*m[7]*m[13] + m[12]*m[5]*m[11] - m[12]*m[7]*m[9]
	adj[12] = -m[4]*m[9]*m[14] + m[4]*m[10]*m[13] + m[8]*m[5]*m[14] - m[8]*m[6]*m[13] - m[12]*m[5]*m[10] + m[12]*m[6]*m[9]
	adj[1] = -m[1]*m[10]*m[15] + m[1]*m[11]*m[14] + m[9]*m[2]*m[15] - m[9]*m[3]*m[14] - m[13]*m[2]*m[11] + m[13]*m[3]*m[10]
	adj[5] = m[0]*m[10]*m[15] - m[0]*m[11]*m[14] - m[8]*m[2]*m[15] + m[8]*m[3]*m[14] + m[12]*m[2]*m[11] - m[12]*m[3]*m[10]
	adj[9] = -m[0]*m[9]*m[15] + m[0]*m[11]*m[13] + m[8]*m[1]*m[15] - m[8]*m[3]*m[13] - m[12]*m[1]*m[11] + m[12]*m[3]*m[9]
	adj[13] = m[0]*m[9]*m[14] - m[0]*m[10]*m[13] - m[8]*m[1]*m[14] + m[8]*m[2]*m[13] + m[12]*m[1]*m[10] - m[12]*m[2]*m[9]
	adj[2] = m[1]*m[6]*m[15] - m[1]*m[7]*m[14] - m[5]*m[2]*m[15] + m[5]*m[3]*m[14] + m[13]*m[2]*m[7] - m[13]*m[3]*m[6]
	adj[6] = -m[0]*m[6]*m[15] + m[0]*m[7]*m[14] + m[4]*m[2]*m[15] - m[4]*m[3]*m[14] - m[12]*m[2]*m[7] + m[12]*m[3]*m[6]
	adj[10] = m[0]*m[5]*m[15] - m[0]*m[7]*m[13] - m[4]*m[1]*m[15] + m[4]*m[3]*m[13] + m[12]*m[1]*m[7] - m[12]*m[3]*m[5]
	adj[14] = -m[0]*m[5]*m[14] + m[0]*m[6]*m[13] + m[4]*m[1]*m[14] - m[4]*m[2]*m[13] - m[12]*m[1]*m[6] + m[12]*m[2]*m[5]
	adj[3] = -m[1]*m[6]*m[11] + m[1]*m[7]*m[10] + m[5]*m[2]*m[11] - m[5]*m[3]*m[10] - m[9]*m[2]*m[7] + m[9]*m[3]*m[6]
	adj[7] = m[0]*m[6]*m[11] - m[0]*m[7]*m[10] - m[4]*m[2]*m[11] + m[4]*m[3]*m[10] + m[8]*m[2]*m[7] - m[8]*m[3]*m[6]
	adj[11] = -m[0]*m[5]*m[11] + m[0]*m[7]*m[9] + m[4]*m[1]*m[11] - m[4]*m[3]*m[9] - m[8]*m[1]*m[7] + m[8]*m[3]*m[5]
	adj[15] = m[0]*m[5]*m[10] - m[0]*m[6]*m[9] - m[4]*m[1]*m[10] + m[4]*m[2]*m[9] + m[8]*m[1]*m[6] - m[8]*m[2]*m[5]
	det = m[0]*adj[0] + m[1]*adj[4] + m[2]*adj[8] + m[3]*adj[12]
	return adj, det
}

// NormalMatrix returns the inverse transpose of the upper-left 3×3 block, used to
// transform normals. A singular block yields the block itself.
func (a Mat4) NormalMatrix() Mat3 {
	m3 := a.Mat3()
	inv, ok := m3.Inverse()
	if !ok {
		return m3
	}
	return inv.Transpose()
}

// Translate returns a translation matrix.
func Translate(t Vec3) Mat4 {
	return Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, t.X, t.Y, t.Z, 1}
}

// Scaling returns a scale matrix.
func Scaling(s Vec3) Mat4 {
	return Mat4{s.X, 0, 0, 0, 0, s.Y, 0, 0, 0, 0, s.Z, 0, 0, 0, 0, 1}
}

// RotateX returns a rotation of angle radians about +X (counter-clockwise looking down
// the axis towards the origin).
func RotateX(angle float32) Mat4 {
	s, c := SinCos(angle)
	return Mat4{1, 0, 0, 0, 0, c, s, 0, 0, -s, c, 0, 0, 0, 0, 1}
}

// RotateY returns a rotation of angle radians about +Y.
func RotateY(angle float32) Mat4 {
	s, c := SinCos(angle)
	return Mat4{c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, 0, 0, 0, 1}
}

// RotateZ returns a rotation of angle radians about +Z.
func RotateZ(angle float32) Mat4 {
	s, c := SinCos(angle)
	return Mat4{c, s, 0, 0, -s, c, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
}

// TRS composes translation, rotation and scale: T·R·S.
func TRS(t Vec3, r Quat, s Vec3) Mat4 {
	m := r.Mat4()
	m[0], m[1], m[2] = m[0]*s.X, m[1]*s.X, m[2]*s.X
	m[4], m[5], m[6] = m[4]*s.Y, m[5]*s.Y, m[6]*s.Y
	m[8], m[9], m[10] = m[8]*s.Z, m[9]*s.Z, m[10]*s.Z
	m[12], m[13], m[14] = t.X, t.Y, t.Z
	return m
}

// Perspective returns an OpenGL-style projection: right-handed eye space looking down
// -Z, clip-space z in [-w, w]. fovY is the vertical field of view in radians.
func Perspective(fovY, aspect, near, far float32) Mat4 {
	f := float32(1 / Tan64(float64(fovY)/2))
	nf := 1 / (near - far)
	return Mat4{
		f / aspect, 0, 0, 0,
		0, f, 0, 0,
		0, 0, (far + near) * nf, -1,
		0, 0, 2 * far * near * nf, 0,
	}
}

// Orthographic returns an orthographic projection mapping the box [l,r]×[b,t]×[-n,-f]
// to clip space with z in [-1, 1].
func Orthographic(l, r, b, t, n, f float32) Mat4 {
	return Mat4{
		2 / (r - l), 0, 0, 0,
		0, 2 / (t - b), 0, 0,
		0, 0, -2 / (f - n), 0,
		-(r + l) / (r - l), -(t + b) / (t - b), -(f + n) / (f - n), 1,
	}
}

// LookAt returns a view matrix for an eye at eye looking at target. If up is parallel to
// the view direction, -Z (or +Y when looking along Z) is used instead so the matrix is
// always well defined; a top-down camera therefore has -Z (forward) pointing up on screen.
func LookAt(eye, target, up Vec3) Mat4 {
	f := target.Sub(eye).Normalize()
	if f == (Vec3{}) {
		f = Forward
	}
	if Abs(f.Normalize().Dot(up.Normalize())) > 0.9999 || up == (Vec3{}) {
		up = Vec3{0, 0, -1}
		if Abs(f.Z) > 0.9999 {
			up = Vec3{0, 1, 0}
		}
	}
	s := f.Cross(up).Normalize()
	u := s.Cross(f)
	return Mat4{
		s.X, u.X, -f.X, 0,
		s.Y, u.Y, -f.Y, 0,
		s.Z, u.Z, -f.Z, 0,
		-s.Dot(eye), -u.Dot(eye), f.Dot(eye), 1,
	}
}

// Mul returns a·b.
func (a Mat3) Mul(b Mat3) Mat3 {
	var o Mat3
	for c := 0; c < 3; c++ {
		for r := 0; r < 3; r++ {
			o[c*3+r] = a[r]*b[c*3] + a[3+r]*b[c*3+1] + a[6+r]*b[c*3+2]
		}
	}
	return o
}

// MulVec3 returns a·v.
func (a Mat3) MulVec3(v Vec3) Vec3 {
	return Vec3{
		a[0]*v.X + a[3]*v.Y + a[6]*v.Z,
		a[1]*v.X + a[4]*v.Y + a[7]*v.Z,
		a[2]*v.X + a[5]*v.Y + a[8]*v.Z,
	}
}

// Transpose returns aᵀ.
func (a Mat3) Transpose() Mat3 {
	return Mat3{a[0], a[3], a[6], a[1], a[4], a[7], a[2], a[5], a[8]}
}

// Det returns the determinant.
func (a Mat3) Det() float32 {
	m := [9]float64{}
	for i, v := range a {
		m[i] = float64(v)
	}
	return float32(m[0]*(m[4]*m[8]-m[7]*m[5]) - m[3]*(m[1]*m[8]-m[7]*m[2]) + m[6]*(m[1]*m[5]-m[4]*m[2]))
}

// Inverse returns a⁻¹ and whether a was invertible. A singular matrix, or one whose
// inverse is not finite in float32, yields the identity and false.
func (a Mat3) Inverse() (Mat3, bool) {
	m := [9]float64{}
	for i, v := range a {
		m[i] = float64(v)
	}
	c0 := m[4]*m[8] - m[7]*m[5]
	c1 := m[7]*m[2] - m[1]*m[8]
	c2 := m[1]*m[5] - m[4]*m[2]
	det := m[0]*c0 + m[3]*c1 + m[6]*c2
	if det == 0 {
		return Ident3(), false
	}
	id := 1 / det
	o := Mat3{
		float32(c0 * id), float32(c1 * id), float32(c2 * id),
		float32((m[6]*m[5] - m[3]*m[8]) * id), float32((m[0]*m[8] - m[6]*m[2]) * id), float32((m[3]*m[2] - m[0]*m[5]) * id),
		float32((m[3]*m[7] - m[6]*m[4]) * id), float32((m[6]*m[1] - m[0]*m[7]) * id), float32((m[0]*m[4] - m[3]*m[1]) * id),
	}
	for _, v := range o {
		if !IsFinite(v) {
			return Ident3(), false
		}
	}
	return o, true
}
