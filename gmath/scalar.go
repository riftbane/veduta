// Package gmath provides the vector, matrix and quaternion types used by Veduta, plus
// deterministic scalar helpers.
//
// Conventions: right-handed coordinates, Y up, -Z forward; matrices are column-major
// (element at row r, column c is m[c*4+r]); angles are radians. Storage is float32; setup
// math that needs more precision is done in float64 internally.
//
// The trigonometric functions are implemented in Go with explicit rounding between
// operations so the compiler cannot fuse multiply-adds. Their results are therefore
// identical on every GOOS/GOARCH, unlike the math package on architectures with FMA.
package gmath

import "math"

// Angle conversion constants.
const (
	Pi      = math.Pi
	Deg2Rad = Pi / 180
	Rad2Deg = 180 / Pi
)

// Radians converts degrees to radians.
func Radians(deg float32) float32 { return float32(float64(deg) * Deg2Rad) }

// Degrees converts radians to degrees.
func Degrees(rad float32) float32 { return float32(float64(rad) * Rad2Deg) }

// m32 rounds a float32 product explicitly. A conversion is a rounding point the compiler
// may not fuse into a multiply-add, and the Go specification allows fusion across
// statements, so splitting an expression into temporaries protects nothing: only this
// does. Without it a*b + c*d rounds twice on amd64 and once on arm64 (a fused FMADDS),
// and the two differ in the last bit for about a quarter of all inputs.
func m32(a, b float32) float32 { return float32(a * b) }

// Abs returns |x|.
func Abs(x float32) float32 { return math.Float32frombits(math.Float32bits(x) &^ (1 << 31)) }

// Clamp limits x to [lo, hi].
func Clamp(x, lo, hi float32) float32 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// Clamp01 limits x to [0, 1].
func Clamp01(x float32) float32 { return Clamp(x, 0, 1) }

// Lerp interpolates linearly between a and b. The endpoints are exact: t = 0 yields a
// and t = 1 yields b (a + (b-a) alone can miss b by an ulp, e.g. Lerp(1e8, 0.1, 1)).
func Lerp(a, b, t float32) float32 {
	if t == 1 {
		return b
	}
	return a + float32((b-a)*t)
}

// Sqrt returns the correctly rounded square root of x.
func Sqrt(x float32) float32 { return float32(math.Sqrt(float64(x))) }

// Floor returns the greatest integer value less than or equal to x.
func Floor(x float32) float32 { return float32(math.Floor(float64(x))) }

// Ceil returns the least integer value greater than or equal to x.
func Ceil(x float32) float32 { return float32(math.Ceil(float64(x))) }

// IsFinite reports whether x is neither infinite nor NaN.
func IsFinite(x float32) bool {
	return math.Float32bits(x)&0x7f800000 != 0x7f800000
}

// Sign returns -1, 0 or 1 according to the sign of x.
func Sign(x float32) float32 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return 0
}

// Smoothstep is the cubic Hermite interpolation of x between edges e0 and e1. Equal
// edges degenerate to a step: 0 for x < e0, 1 otherwise (never NaN).
func Smoothstep(e0, e1, x float32) float32 {
	if e0 == e1 {
		if x < e0 {
			return 0
		}
		return 1
	}
	t := Clamp01((x - e0) / (e1 - e0))
	// The result is rounded too: inlined, it is often an operand of the caller's sum.
	return m32(m32(t, t), 3-m32(2, t))
}

// Wrap returns x modulo m in [0, m) for m > 0. The result is always strictly below m:
// when a tiny negative x would round up to m, the largest float32 below m is returned.
func Wrap(x, m float32) float32 {
	r := float32(math.Mod(float64(x), float64(m)))
	if r < 0 {
		r += m
		if r >= m {
			r = math.Nextafter32(m, 0)
		}
	}
	return r
}
