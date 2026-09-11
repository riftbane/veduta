package gmath

import (
	"encoding/json"
	"fmt"
	"math"
)

// minNormal32 is the smallest normal float32. A squared length below it has lost bits
// to underflow (or is 0 for a non-zero vector); one above MaxFloat32 has overflowed.
// Len and Normalize redo only those cases in float64, so every other result keeps the
// exact bits of the float32 formula.
const minNormal32 = 0x1p-126

// len64 returns the Euclidean length of (x, y, z) computed in float64.
func len64(x, y, z float32) float64 {
	fx, fy, fz := float64(x), float64(y), float64(z)
	return math.Sqrt(float64(fx*fx) + float64(fy*fy) + float64(fz*fz))
}

// Vec2 is a 2D vector.
type Vec2 struct{ X, Y float32 }

// Vec3 is a 3D vector.
type Vec3 struct{ X, Y, Z float32 }

// Vec4 is a 4D vector, typically homogeneous coordinates or RGBA.
type Vec4 struct{ X, Y, Z, W float32 }

// V2 constructs a Vec2.
func V2(x, y float32) Vec2 { return Vec2{x, y} }

// V3 constructs a Vec3.
func V3(x, y, z float32) Vec3 { return Vec3{x, y, z} }

// V4 constructs a Vec4.
func V4(x, y, z, w float32) Vec4 { return Vec4{x, y, z, w} }

// Common directions.
var (
	Zero3    = Vec3{}
	One3     = Vec3{1, 1, 1}
	UnitX    = Vec3{1, 0, 0}
	UnitY    = Vec3{0, 1, 0}
	UnitZ    = Vec3{0, 0, 1}
	Up       = Vec3{0, 1, 0}
	Forward  = Vec3{0, 0, -1}
	RightDir = Vec3{1, 0, 0}
)

// Add returns a+b.
func (a Vec2) Add(b Vec2) Vec2 { return Vec2{a.X + b.X, a.Y + b.Y} }

// Sub returns a-b.
func (a Vec2) Sub(b Vec2) Vec2 { return Vec2{a.X - b.X, a.Y - b.Y} }

// Scale returns a*s.
func (a Vec2) Scale(s float32) Vec2 { return Vec2{a.X * s, a.Y * s} }

// Mul returns the component-wise product.
func (a Vec2) Mul(b Vec2) Vec2 { return Vec2{a.X * b.X, a.Y * b.Y} }

// Dot returns the dot product.
func (a Vec2) Dot(b Vec2) float32 { return a.X*b.X + a.Y*b.Y }

// Cross returns the z component of the 3D cross product of (a,0) and (b,0).
func (a Vec2) Cross(b Vec2) float32 { return a.X*b.Y - a.Y*b.X }

// Len returns the Euclidean length (+Inf only if it exceeds MaxFloat32).
func (a Vec2) Len() float32 {
	d := a.Dot(a)
	if d < minNormal32 || d > math.MaxFloat32 {
		return float32(len64(a.X, a.Y, 0))
	}
	return Sqrt(d)
}

// Normalize returns a unit vector in the direction of a, or zero if a is zero. Very
// small and very large vectors are normalized too.
func (a Vec2) Normalize() Vec2 {
	d := a.Dot(a)
	if d < minNormal32 || d > math.MaxFloat32 {
		l := len64(a.X, a.Y, 0)
		if l == 0 {
			return Vec2{}
		}
		return Vec2{float32(float64(a.X) / l), float32(float64(a.Y) / l)}
	}
	return a.Scale(1 / Sqrt(d))
}

// Lerp interpolates between a and b.
func (a Vec2) Lerp(b Vec2, t float32) Vec2 { return Vec2{Lerp(a.X, b.X, t), Lerp(a.Y, b.Y, t)} }

// Min returns the component-wise minimum.
func (a Vec2) Min(b Vec2) Vec2 { return Vec2{min(a.X, b.X), min(a.Y, b.Y)} }

// Max returns the component-wise maximum.
func (a Vec2) Max(b Vec2) Vec2 { return Vec2{max(a.X, b.X), max(a.Y, b.Y)} }

// IsFinite reports whether all components are finite.
func (a Vec2) IsFinite() bool { return IsFinite(a.X) && IsFinite(a.Y) }

// Add returns a+b.
func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Sub returns a-b.
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// Scale returns a*s.
func (a Vec3) Scale(s float32) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

// Mul returns the component-wise product.
func (a Vec3) Mul(b Vec3) Vec3 { return Vec3{a.X * b.X, a.Y * b.Y, a.Z * b.Z} }

// Neg returns -a.
func (a Vec3) Neg() Vec3 { return Vec3{-a.X, -a.Y, -a.Z} }

// Dot returns the dot product.
func (a Vec3) Dot(b Vec3) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// Cross returns the cross product a×b.
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

// LenSq returns the squared length.
func (a Vec3) LenSq() float32 { return a.Dot(a) }

// Len returns the Euclidean length (+Inf only if it exceeds MaxFloat32).
func (a Vec3) Len() float32 {
	d := a.Dot(a)
	if d < minNormal32 || d > math.MaxFloat32 {
		return float32(len64(a.X, a.Y, a.Z))
	}
	return Sqrt(d)
}

// Dist returns the distance between a and b.
func (a Vec3) Dist(b Vec3) float32 { return a.Sub(b).Len() }

// Normalize returns a unit vector in the direction of a, or zero if a is zero. Very
// small and very large vectors are normalized too (a float32 squared length would
// underflow to 0 below about 1e-22 or overflow above about 1.8e19).
func (a Vec3) Normalize() Vec3 {
	d := a.Dot(a)
	if d < minNormal32 || d > math.MaxFloat32 {
		l := len64(a.X, a.Y, a.Z)
		if l == 0 {
			return Vec3{}
		}
		return Vec3{float32(float64(a.X) / l), float32(float64(a.Y) / l), float32(float64(a.Z) / l)}
	}
	return a.Scale(1 / Sqrt(d))
}

// Lerp interpolates between a and b.
func (a Vec3) Lerp(b Vec3, t float32) Vec3 {
	return Vec3{Lerp(a.X, b.X, t), Lerp(a.Y, b.Y, t), Lerp(a.Z, b.Z, t)}
}

// Min returns the component-wise minimum.
func (a Vec3) Min(b Vec3) Vec3 { return Vec3{min(a.X, b.X), min(a.Y, b.Y), min(a.Z, b.Z)} }

// Max returns the component-wise maximum.
func (a Vec3) Max(b Vec3) Vec3 { return Vec3{max(a.X, b.X), max(a.Y, b.Y), max(a.Z, b.Z)} }

// Abs returns the component-wise absolute value.
func (a Vec3) Abs() Vec3 { return Vec3{Abs(a.X), Abs(a.Y), Abs(a.Z)} }

// IsFinite reports whether all components are finite.
func (a Vec3) IsFinite() bool { return IsFinite(a.X) && IsFinite(a.Y) && IsFinite(a.Z) }

// Get returns component i (0=X, 1=Y, 2=Z).
func (a Vec3) Get(i int) float32 {
	switch i {
	case 0:
		return a.X
	case 1:
		return a.Y
	}
	return a.Z
}

// With returns a copy of a with component i set to v.
func (a Vec3) With(i int, v float32) Vec3 {
	switch i {
	case 0:
		a.X = v
	case 1:
		a.Y = v
	default:
		a.Z = v
	}
	return a
}

// XY returns the X and Y components.
func (a Vec3) XY() Vec2 { return Vec2{a.X, a.Y} }

// Vec4 extends a with w.
func (a Vec3) Vec4(w float32) Vec4 { return Vec4{a.X, a.Y, a.Z, w} }

// Add returns a+b.
func (a Vec4) Add(b Vec4) Vec4 { return Vec4{a.X + b.X, a.Y + b.Y, a.Z + b.Z, a.W + b.W} }

// Sub returns a-b.
func (a Vec4) Sub(b Vec4) Vec4 { return Vec4{a.X - b.X, a.Y - b.Y, a.Z - b.Z, a.W - b.W} }

// Scale returns a*s.
func (a Vec4) Scale(s float32) Vec4 { return Vec4{a.X * s, a.Y * s, a.Z * s, a.W * s} }

// Mul returns the component-wise product.
func (a Vec4) Mul(b Vec4) Vec4 { return Vec4{a.X * b.X, a.Y * b.Y, a.Z * b.Z, a.W * b.W} }

// Dot returns the dot product.
func (a Vec4) Dot(b Vec4) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }

// Lerp interpolates between a and b.
func (a Vec4) Lerp(b Vec4, t float32) Vec4 {
	return Vec4{Lerp(a.X, b.X, t), Lerp(a.Y, b.Y, t), Lerp(a.Z, b.Z, t), Lerp(a.W, b.W, t)}
}

// XYZ returns the first three components.
func (a Vec4) XYZ() Vec3 { return Vec3{a.X, a.Y, a.Z} }

// IsFinite reports whether all components are finite.
func (a Vec4) IsFinite() bool {
	return IsFinite(a.X) && IsFinite(a.Y) && IsFinite(a.Z) && IsFinite(a.W)
}

// JSON: vectors are encoded as arrays, the form used by every Veduta source file.

// unmarshalFloats decodes a JSON array of exactly len(dst) numbers into dst. A null
// element is an error (encoding/json would otherwise silently read it as 0), so
// [1, null, 3] cannot slip through a strict source file.
func unmarshalFloats(b []byte, dst []float32, name string) error {
	var v []*float32
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if len(v) != len(dst) {
		return fmt.Errorf("%s: want %d numbers, got %d", name, len(dst), len(v))
	}
	for i, p := range v {
		if p == nil {
			return fmt.Errorf("%s: element %d is null, want a number", name, i)
		}
		dst[i] = *p
	}
	return nil
}

// MarshalJSON encodes the vector as [x, y].
func (a Vec2) MarshalJSON() ([]byte, error) { return json.Marshal([2]float32{a.X, a.Y}) }

// UnmarshalJSON decodes [x, y].
func (a *Vec2) UnmarshalJSON(b []byte) error {
	var v [2]float32
	if err := unmarshalFloats(b, v[:], "vec2"); err != nil {
		return err
	}
	*a = Vec2{v[0], v[1]}
	return nil
}

// MarshalJSON encodes the vector as [x, y, z].
func (a Vec3) MarshalJSON() ([]byte, error) { return json.Marshal([3]float32{a.X, a.Y, a.Z}) }

// UnmarshalJSON decodes [x, y, z].
func (a *Vec3) UnmarshalJSON(b []byte) error {
	var v [3]float32
	if err := unmarshalFloats(b, v[:], "vec3"); err != nil {
		return err
	}
	*a = Vec3{v[0], v[1], v[2]}
	return nil
}

// MarshalJSON encodes the vector as [x, y, z, w].
func (a Vec4) MarshalJSON() ([]byte, error) {
	return json.Marshal([4]float32{a.X, a.Y, a.Z, a.W})
}

// UnmarshalJSON decodes [x, y, z, w].
func (a *Vec4) UnmarshalJSON(b []byte) error {
	var v [4]float32
	if err := unmarshalFloats(b, v[:], "vec4"); err != nil {
		return err
	}
	*a = Vec4{v[0], v[1], v[2], v[3]}
	return nil
}
