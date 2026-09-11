package gmath

// Quat is a rotation quaternion x·i + y·j + z·k + w.
type Quat struct{ X, Y, Z, W float32 }

// QuatIdent returns the identity rotation.
func QuatIdent() Quat { return Quat{0, 0, 0, 1} }

// QuatAxisAngle returns a rotation of angle radians about axis (normalized internally).
// A zero axis defines no rotation and yields the identity.
func QuatAxisAngle(axis Vec3, angle float32) Quat {
	a := axis.Normalize()
	if a == (Vec3{}) {
		return QuatIdent()
	}
	s, c := SinCos(angle / 2)
	return Quat{a.X * s, a.Y * s, a.Z * s, c}
}

// QuatEuler returns the rotation for Euler angles in radians, applied to a vector in the
// order Z (roll), then X (pitch), then Y (yaw): R = Ry·Rx·Rz. This is the meaning of
// "rotation_deg": [x, y, z] in every Veduta source file.
func QuatEuler(x, y, z float32) Quat {
	qx := QuatAxisAngle(UnitX, x)
	qy := QuatAxisAngle(UnitY, y)
	qz := QuatAxisAngle(UnitZ, z)
	return qy.Mul(qx).Mul(qz)
}

// QuatEulerDeg is QuatEuler with angles in degrees.
func QuatEulerDeg(d Vec3) Quat { return QuatEuler(Radians(d.X), Radians(d.Y), Radians(d.Z)) }

// Mul returns the composition a·b (apply b first, then a).
func (a Quat) Mul(b Quat) Quat {
	return Quat{
		a.W*b.X + a.X*b.W + a.Y*b.Z - a.Z*b.Y,
		a.W*b.Y - a.X*b.Z + a.Y*b.W + a.Z*b.X,
		a.W*b.Z + a.X*b.Y - a.Y*b.X + a.Z*b.W,
		a.W*b.W - a.X*b.X - a.Y*b.Y - a.Z*b.Z,
	}
}

// Conj returns the conjugate (the inverse for unit quaternions).
func (a Quat) Conj() Quat { return Quat{-a.X, -a.Y, -a.Z, a.W} }

// Dot returns the 4D dot product.
func (a Quat) Dot(b Quat) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }

// Len returns the quaternion norm.
func (a Quat) Len() float32 { return Sqrt(a.Dot(a)) }

// Normalize returns a unit quaternion; the zero quaternion yields the identity.
func (a Quat) Normalize() Quat {
	l := a.Len()
	if l == 0 {
		return QuatIdent()
	}
	i := 1 / l
	return Quat{a.X * i, a.Y * i, a.Z * i, a.W * i}
}

// Rotate applies the rotation to v.
func (a Quat) Rotate(v Vec3) Vec3 {
	u := Vec3{a.X, a.Y, a.Z}
	t := u.Cross(v).Scale(2)
	return v.Add(t.Scale(a.W)).Add(u.Cross(t))
}

// Mat4 returns the rotation matrix.
func (a Quat) Mat4() Mat4 {
	x, y, z, w := a.X, a.Y, a.Z, a.W
	xx, yy, zz := x*x, y*y, z*z
	xy, xz, yz := x*y, x*z, y*z
	wx, wy, wz := w*x, w*y, w*z
	return Mat4{
		1 - 2*(yy+zz), 2 * (xy + wz), 2 * (xz - wy), 0,
		2 * (xy - wz), 1 - 2*(xx+zz), 2 * (yz + wx), 0,
		2 * (xz + wy), 2 * (yz - wx), 1 - 2*(xx+yy), 0,
		0, 0, 0, 1,
	}
}

// Slerp interpolates spherically between a and b along the shortest arc.
func (a Quat) Slerp(b Quat, t float32) Quat {
	d := a.Dot(b)
	if d < 0 {
		b = Quat{-b.X, -b.Y, -b.Z, -b.W}
		d = -d
	}
	if d > 0.9995 {
		return Quat{Lerp(a.X, b.X, t), Lerp(a.Y, b.Y, t), Lerp(a.Z, b.Z, t), Lerp(a.W, b.W, t)}.Normalize()
	}
	th := Acos64(float64(d))
	s := Sin64(th)
	wa := float32(Sin64((1-float64(t))*th) / s)
	wb := float32(Sin64(float64(t)*th) / s)
	return Quat{a.X*wa + b.X*wb, a.Y*wa + b.Y*wb, a.Z*wa + b.Z*wb, a.W*wa + b.W*wb}
}
