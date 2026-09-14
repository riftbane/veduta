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
	return Quat{m32(a.X, s), m32(a.Y, s), m32(a.Z, s), c}
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

// EulerDeg returns Euler angles in degrees [x, y, z] such that QuatEulerDeg of the
// result is the same rotation (R = Ry·Rx·Rz). Pitch x is in [-90, 90]; at ±90 (gimbal
// lock) roll z is 0 and the whole yaw goes to y. Deterministic.
func (a Quat) EulerDeg() Vec3 {
	m := a.Normalize().Mat4()
	// With R = Ry·Rx·Rz: R12 = -sin x, R02 = sin y cos x, R22 = cos y cos x,
	// R10 = cos x sin z, R11 = cos x cos z (element (r,c) is m[c*4+r]).
	sx := Clamp(-m[9], -1, 1)
	x := Asin64(float64(sx))
	var y, z float64
	if Abs(sx) < 0.9999999 {
		y = Atan264(float64(m[8]), float64(m[10]))
		z = Atan264(float64(m[1]), float64(m[5]))
	} else {
		// Gimbal lock: R00 = cos(y∓z), R20 = -sin(y∓z); put it all in y.
		y = Atan264(-float64(m[2]), float64(m[0]))
	}
	return Vec3{float32(x * Rad2Deg), float32(y * Rad2Deg), float32(z * Rad2Deg)}
}

// Mul returns the composition a·b (apply b first, then a).
func (a Quat) Mul(b Quat) Quat {
	return Quat{
		m32(a.W, b.X) + m32(a.X, b.W) + m32(a.Y, b.Z) - m32(a.Z, b.Y),
		m32(a.W, b.Y) - m32(a.X, b.Z) + m32(a.Y, b.W) + m32(a.Z, b.X),
		m32(a.W, b.Z) + m32(a.X, b.Y) - m32(a.Y, b.X) + m32(a.Z, b.W),
		m32(a.W, b.W) - m32(a.X, b.X) - m32(a.Y, b.Y) - m32(a.Z, b.Z),
	}
}

// Conj returns the conjugate (the inverse for unit quaternions).
func (a Quat) Conj() Quat { return Quat{-a.X, -a.Y, -a.Z, a.W} }

// Dot returns the 4D dot product.
func (a Quat) Dot(b Quat) float32 {
	return m32(a.X, b.X) + m32(a.Y, b.Y) + m32(a.Z, b.Z) + m32(a.W, b.W)
}

// Len returns the quaternion norm.
func (a Quat) Len() float32 { return Sqrt(a.Dot(a)) }

// Normalize returns a unit quaternion; the zero quaternion yields the identity.
func (a Quat) Normalize() Quat {
	l := a.Len()
	if l == 0 {
		return QuatIdent()
	}
	i := 1 / l
	return Quat{m32(a.X, i), m32(a.Y, i), m32(a.Z, i), m32(a.W, i)}
}

// Rotate applies the rotation to v.
func (a Quat) Rotate(v Vec3) Vec3 {
	u := Vec3{a.X, a.Y, a.Z}
	t := u.Cross(v).Scale(2)
	w := Vec3{m32(t.X, a.W), m32(t.Y, a.W), m32(t.Z, a.W)}
	c := u.Cross(t)
	return Vec3{v.X + w.X + c.X, v.Y + w.Y + c.Y, v.Z + w.Z + c.Z}
}

// Mat4 returns the rotation matrix.
func (a Quat) Mat4() Mat4 {
	x, y, z, w := a.X, a.Y, a.Z, a.W
	xx, yy, zz := m32(x, x), m32(y, y), m32(z, z)
	xy, xz, yz := m32(x, y), m32(x, z), m32(y, z)
	wx, wy, wz := m32(w, x), m32(w, y), m32(w, z)
	return Mat4{
		1 - m32(2, yy+zz), m32(2, xy+wz), m32(2, xz-wy), 0,
		m32(2, xy-wz), 1 - m32(2, xx+zz), m32(2, yz+wx), 0,
		m32(2, xz+wy), m32(2, yz-wx), 1 - m32(2, xx+yy), 0,
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
	wa := float32(Sin64(float64((1-float64(t))*th)) / s)
	wb := float32(Sin64(float64(float64(t)*th)) / s)
	return Quat{
		m32(a.X, wa) + m32(b.X, wb), m32(a.Y, wa) + m32(b.Y, wb),
		m32(a.Z, wa) + m32(b.Z, wb), m32(a.W, wa) + m32(b.W, wb),
	}
}
