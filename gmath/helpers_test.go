package gmath

import (
	"math"
	"testing"
)

// testRNG is a splitmix64 generator. The tests use their own generator instead of
// math/rand so the random cases are fixed by the seed forever, independent of any
// change to the standard library's algorithms.
type testRNG struct{ s uint64 }

func newRNG(seed uint64) *testRNG { return &testRNG{seed} }

func (r *testRNG) u64() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// f returns a uniform float32 in [lo, hi].
func (r *testRNG) f(lo, hi float32) float32 {
	u := float64(float64(r.u64()>>11) / (1 << 53))
	return float32(float64(lo) + float64(u*float64(hi-lo)))
}

func (r *testRNG) vec3(lo, hi float32) Vec3 { return Vec3{r.f(lo, hi), r.f(lo, hi), r.f(lo, hi)} }

// dir returns a uniformly distributed unit vector.
func (r *testRNG) dir() Vec3 {
	for {
		v := r.vec3(-1, 1)
		if l := v.Len(); l > 0.1 && l <= 1 {
			return v.Normalize()
		}
	}
}

// quat returns a random unit rotation.
func (r *testRNG) quat() Quat { return QuatAxisAngle(r.dir(), r.f(-Pi, Pi)) }

// scale returns a scale vector with magnitudes in [0.5, 2] and random signs, so the
// matrices built from it are well conditioned but may mirror.
func (r *testRNG) scale() Vec3 {
	var s Vec3
	for i := 0; i < 3; i++ {
		v := r.f(0.5, 2)
		if r.u64()&1 != 0 {
			v = -v
		}
		s = s.With(i, v)
	}
	return s
}

// trs returns a well-conditioned affine matrix.
func (r *testRNG) trs() Mat4 { return TRS(r.vec3(-10, 10), r.quat(), r.scale()) }

// mat4 returns a matrix with every element uniform in [lo, hi].
func (r *testRNG) mat4(lo, hi float32) Mat4 {
	var m Mat4
	for i := range m {
		m[i] = r.f(lo, hi)
	}
	return m
}

// approx reports whether a and b agree within tol, relative to their magnitude when
// that exceeds 1 and absolute otherwise.
func approx(a, b, tol float32) bool {
	if a == b {
		return true
	}
	// a or b is often a product inlined from the caller: round it before subtracting.
	return Abs(float32(a)-float32(b)) <= tol*max(1, Abs(a), Abs(b))
}

// within reports whether |a-b| <= eps (exact equality also covers infinities).
func within(a, b, eps float32) bool { return a == b || Abs(a-b) <= eps }

// The vector comparisons scale tol by the largest component magnitude of either vector
// (or 1): that is the error scale of vector arithmetic, where a small component of a
// long vector carries the rounding error of the long one.

func approxV2(a, b Vec2, tol float32) bool {
	e := tol * max(1, Abs(a.X), Abs(a.Y), Abs(b.X), Abs(b.Y))
	return within(a.X, b.X, e) && within(a.Y, b.Y, e)
}

func approxV3(a, b Vec3, tol float32) bool {
	e := tol * max(1, Abs(a.X), Abs(a.Y), Abs(a.Z), Abs(b.X), Abs(b.Y), Abs(b.Z))
	return within(a.X, b.X, e) && within(a.Y, b.Y, e) && within(a.Z, b.Z, e)
}

func approxV4(a, b Vec4, tol float32) bool {
	e := tol * max(1, Abs(a.X), Abs(a.Y), Abs(a.Z), Abs(a.W), Abs(b.X), Abs(b.Y), Abs(b.Z), Abs(b.W))
	return within(a.X, b.X, e) && within(a.Y, b.Y, e) && within(a.Z, b.Z, e) && within(a.W, b.W, e)
}

func approxM4(a, b Mat4, tol float32) bool {
	for i := range a {
		if !approx(a[i], b[i], tol) {
			return false
		}
	}
	return true
}

func approxM3(a, b Mat3, tol float32) bool {
	for i := range a {
		if !approx(a[i], b[i], tol) {
			return false
		}
	}
	return true
}

// sameRotation reports whether two unit quaternions describe the same rotation
// (q and -q are equivalent).
func sameRotation(a, b Quat, tol float32) bool {
	pos := approx(a.X, b.X, tol) && approx(a.Y, b.Y, tol) && approx(a.Z, b.Z, tol) && approx(a.W, b.W, tol)
	neg := approx(a.X, -b.X, tol) && approx(a.Y, -b.Y, tol) && approx(a.Z, -b.Z, tol) && approx(a.W, -b.W, tol)
	return pos || neg
}

func checkV3(t *testing.T, name string, got, want Vec3, tol float32) {
	t.Helper()
	if !approxV3(got, want, tol) {
		t.Errorf("%s = %v, want %v (tol %g)", name, got, want, tol)
	}
}

func checkM4(t *testing.T, name string, got, want Mat4, tol float32) {
	t.Helper()
	if !approxM4(got, want, tol) {
		t.Errorf("%s =\n%v\nwant\n%v (tol %g)", name, got, want, tol)
	}
}

var (
	nan32  = float32(math.NaN())
	inf32  = float32(math.Inf(1))
	ninf32 = float32(math.Inf(-1))
)

func TestTestRNGDeterministic(t *testing.T) {
	// Pin the generator itself: if it changed, every "random" case in this package would
	// silently become a different case.
	r := newRNG(1)
	got := [3]uint64{r.u64(), r.u64(), r.u64()}
	want := [3]uint64{0x910a2dec89025cc1, 0xbeeb8da1658eec67, 0xf893a2eefb32555e}
	if got != want {
		t.Fatalf("splitmix64(1) = %#x, want %#x", got, want)
	}
	for i := 0; i < 1000; i++ {
		if v := r.f(-3, 5); v < -3 || v > 5 {
			t.Fatalf("f out of range: %v", v)
		}
		if l := r.dir().Len(); !approx(l, 1, 1e-6) {
			t.Fatalf("dir not unit: %v", l)
		}
	}
}
