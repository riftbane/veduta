package gmath

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

func TestTrigMatchesMath(t *testing.T) {
	const tol = 4e-16
	for i := -20000; i <= 20000; i++ {
		x := float64(i) * 0.00157 // covers about ±31 rad
		s, c := SinCos64(x)
		if d := math.Abs(s - math.Sin(x)); d > tol {
			t.Fatalf("Sin(%v) = %v, math %v (diff %g)", x, s, math.Sin(x), d)
		}
		if d := math.Abs(c - math.Cos(x)); d > tol {
			t.Fatalf("Cos(%v) = %v, math %v (diff %g)", x, c, math.Cos(x), d)
		}
		y := float64(i) * 0.0031
		if d := math.Abs(Atan64(y) - math.Atan(y)); d > tol {
			t.Fatalf("Atan(%v) = %v, math %v (diff %g)", y, Atan64(y), math.Atan(y), d)
		}
	}
	for _, y := range []float64{1e-30, 1e-9, 0.43, 0.44, 0.68, 0.69, 1.18, 1.19, 2.43, 2.44, 1e10, 1e30, math.Inf(1)} {
		if d := math.Abs(Atan64(y) - math.Atan(y)); d > tol {
			t.Fatalf("Atan(%v) = %v, math %v", y, Atan64(y), math.Atan(y))
		}
	}
}

func TestAtan2Quadrants(t *testing.T) {
	vals := []float64{-3, -1, -0.5, -1e-3, 0, 1e-3, 0.5, 1, 3, math.Inf(1), math.Inf(-1)}
	for _, y := range vals {
		for _, x := range vals {
			got, want := Atan264(y, x), math.Atan2(y, x)
			if math.Abs(got-want) > 1e-15 || math.Signbit(got) != math.Signbit(want) {
				t.Errorf("Atan2(%v, %v) = %v, want %v", y, x, got, want)
			}
		}
	}
	if !math.IsNaN(Atan264(math.NaN(), 1)) {
		t.Error("Atan2(NaN, 1) should be NaN")
	}
}

func TestAsinAcos(t *testing.T) {
	for i := -100; i <= 100; i++ {
		x := float64(i) / 100
		if d := math.Abs(Asin64(x) - math.Asin(x)); d > 1e-15 {
			t.Fatalf("Asin(%v) diff %g", x, d)
		}
		if d := math.Abs(Acos64(x) - math.Acos(x)); d > 1e-15 {
			t.Fatalf("Acos(%v) diff %g", x, d)
		}
	}
	if !math.IsNaN(Asin64(1.5)) || !math.IsNaN(Acos64(-1.5)) {
		t.Error("out of domain should be NaN")
	}
}

func TestTrigSpecial(t *testing.T) {
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if s, c := SinCos64(x); !math.IsNaN(s) || !math.IsNaN(c) {
			t.Errorf("SinCos(%v) = %v, %v; want NaN", x, s, c)
		}
	}
	if Sin64(0) != 0 || Cos64(0) != 1 {
		t.Error("Sin(0)/Cos(0) wrong")
	}
}

// TestTrigGolden pins the exact output bits of Sin, Cos and Atan2 over a fixed grid of
// 10,000 inputs (float32 API and float64 kernels, hashed separately).
//
// These hashes must be identical on every GOOS/GOARCH, with any GOAMD64/GOARM64 level.
// The kernels wrap every product in an explicit float64 conversion (see m in trig.go),
// which the Go spec defines as a rounding point, so the compiler may not fuse a multiply
// and an add into an FMA; every remaining operation (+, -, *, /, sqrt, floor, float64 to
// float32 conversion) is a correctly rounded IEEE-754 operation. The inputs are built
// with a single rounding each (integer times constant), so they are bit-identical too.
// If this test fails on one platform only, a fusion (or an assembly path) has leaked
// into the kernels: fix the kernels, never update the constant to match that platform.
// Update the constants only for a deliberate change of the algorithm, and say so in the
// commit message, because every trace hash and golden frame depends on these bits.
func TestTrigGolden(t *testing.T) {
	const (
		want32 = "d487b5e47d87a4508adad2009e083fb48d9fd985f79b1d339f99209dc6311824"
		want64 = "b0617b8c818f6407d10bd56e7c40e4e3873e4dbc6c120ef48510ea77e463a4c1"
	)
	got32, got64 := trigGoldenHashes()
	if got32 != want32 {
		t.Errorf("float32 Sin/Cos/Atan2 golden hash = %s, want %s", got32, want32)
	}
	if got64 != want64 {
		t.Errorf("float64 Sin64/Cos64/Atan264 golden hash = %s, want %s", got64, want64)
	}
}

func trigGoldenHashes() (h32, h64 string) {
	s32, s64 := sha256.New(), sha256.New()
	var b4 [4]byte
	var b8 [8]byte
	for i := 0; i < 10000; i++ {
		x := float32(i-5000) * 0.0071  // ±35.5 rad: every quadrant, several turns
		ay := float32(i%100-50) * 0.37 // a 100×100 grid for Atan2 covering all four
		ax := float32(i/100-50) * 0.29 // quadrants and both axes (including (0, 0))
		for _, v := range [...]float32{Sin(x), Cos(x), Atan2(ay, ax)} {
			binary.LittleEndian.PutUint32(b4[:], math.Float32bits(v))
			s32.Write(b4[:])
		}
		x64 := float64(i-5000) * 0.0071
		ay64 := float64(i%100-50) * 0.37
		ax64 := float64(i/100-50) * 0.29
		for _, v := range [...]float64{Sin64(x64), Cos64(x64), Atan264(ay64, ax64)} {
			binary.LittleEndian.PutUint64(b8[:], math.Float64bits(v))
			s64.Write(b8[:])
		}
	}
	return hex.EncodeToString(s32.Sum(nil)), hex.EncodeToString(s64.Sum(nil))
}

func TestTrigFloat32Wrappers(t *testing.T) {
	// The float32 functions are the float64 kernels rounded once, so they agree with
	// the math package to within one float32 ulp (usually exactly).
	ulps := func(a, b float32) int64 {
		d := int64(math.Float32bits(a)) - int64(math.Float32bits(b))
		if d < 0 {
			d = -d
		}
		return d
	}
	for i := -5000; i <= 5000; i++ {
		x := float32(i) * 0.0063
		s, c := SinCos(x)
		if !sameBits(s, Sin(x)) || !sameBits(c, Cos(x)) {
			t.Fatalf("SinCos(%v) = %v,%v; Sin/Cos = %v,%v", x, s, c, Sin(x), Cos(x))
		}
		checks := []struct {
			name      string
			got, want float32
		}{
			{"Sin", s, float32(math.Sin(float64(x)))},
			{"Cos", c, float32(math.Cos(float64(x)))},
			{"Atan", Atan(x), float32(math.Atan(float64(x)))},
			{"Atan2", Atan2(x, 1.5-x), float32(math.Atan2(float64(x), float64(1.5-x)))},
		}
		if Abs(Cos(x)) > 0.01 {
			checks = append(checks, struct {
				name      string
				got, want float32
			}{"Tan", Tan(x), float32(math.Tan(float64(x)))})
		}
		if u := x / 31.5; Abs(u) <= 1 {
			checks = append(checks,
				struct {
					name      string
					got, want float32
				}{"Asin", Asin(u), float32(math.Asin(float64(u)))},
				struct {
					name      string
					got, want float32
				}{"Acos", Acos(u), float32(math.Acos(float64(u)))})
		}
		for _, ck := range checks {
			if ulps(ck.got, ck.want) > 1 {
				t.Fatalf("%s at %v = %v, math %v", ck.name, x, ck.got, ck.want)
			}
		}
	}
	known := []struct {
		name      string
		got, want float32
	}{
		{"Sin(pi/2)", Sin(Pi / 2), 1},
		{"Cos(pi)", Cos(Pi), -1},
		{"Sin(0)", Sin(0), 0},
		{"Cos(0)", Cos(0), 1},
		{"Tan(pi/4)", Tan(Pi / 4), 1},
		{"Atan(1)", Atan(1), Pi / 4},
		{"Atan2(1,1)", Atan2(1, 1), Pi / 4},
		{"Atan2(1,0)", Atan2(1, 0), Pi / 2},
		{"Atan2(0,-1)", Atan2(0, -1), Pi},
		{"Atan2(-1,0)", Atan2(-1, 0), -Pi / 2},
		{"Asin(1)", Asin(1), Pi / 2},
		{"Acos(-1)", Acos(-1), Pi},
		{"Acos(1)", Acos(1), 0},
	}
	for _, k := range known {
		if !approx(k.got, k.want, 1e-7) {
			t.Errorf("%s = %v, want %v", k.name, k.got, k.want)
		}
	}
}

func TestAtan2SignedZero(t *testing.T) {
	negZero := math.Copysign(0, -1)
	vals := []float64{negZero, 0, -1, 1, math.Inf(-1), math.Inf(1)}
	for _, y := range vals {
		for _, x := range vals {
			got, want := Atan264(y, x), math.Atan2(y, x)
			if got != want || math.Signbit(got) != math.Signbit(want) {
				t.Errorf("Atan2(%v, %v) = %v, want %v", y, x, got, want)
			}
		}
	}
	if !math.IsNaN(Atan264(1, math.NaN())) || !math.IsNaN(float64(Atan2(nan32, 0))) {
		t.Error("Atan2 with NaN should be NaN")
	}
	if s := Sin64(negZero); s != 0 || !math.Signbit(s) {
		t.Errorf("Sin(-0) = %v, want -0", s)
	}
	// y/x underflows to ±0: the quadrant must come from the signs of y and x (math.Atan2
	// itself returns +pi for the third-quadrant case).
	underflow := []struct{ y, x, want float64 }{
		{-1e-300, -1e300, -Pi},
		{1e-300, -1e300, Pi},
		{-1e-300, 1e300, negZero},
		{1e-300, 1e300, 0},
	}
	for _, tc := range underflow {
		if got := Atan264(tc.y, tc.x); got != tc.want || math.Signbit(got) != math.Signbit(tc.want) {
			t.Errorf("Atan2(%v, %v) = %v, want %v", tc.y, tc.x, got, tc.want)
		}
	}
}

func TestTrigLargeArguments(t *testing.T) {
	// Accurate reduction holds up to 2^20·pi/2 ≈ 1.6e6 rad.
	r := newRNG(127)
	for i := 0; i < 2000; i++ {
		x := float64(r.f(-1.6e6, 1.6e6))
		s, c := SinCos64(x)
		if math.Abs(s-math.Sin(x)) > 1e-15 || math.Abs(c-math.Cos(x)) > 1e-15 {
			t.Fatalf("SinCos(%v) = %v,%v; math %v,%v", x, s, c, math.Sin(x), math.Cos(x))
		}
	}
	// Beyond that the result is still a finite value in [-1, 1].
	for _, x := range []float64{1e9, -3e12, 1e20, math.MaxFloat32, -math.MaxFloat64, reduceMax, -reduceMax} {
		s, c := SinCos64(x)
		if math.IsNaN(s) || math.Abs(s) > 1 || math.IsNaN(c) || math.Abs(c) > 1 {
			t.Errorf("SinCos(%v) = %v, %v", x, s, c)
		}
		// Still a point on the unit circle, and odd/even like sin and cos.
		if d := math.Abs(s*s + c*c - 1); d > 1e-15 {
			t.Errorf("SinCos(%v): sin²+cos² off by %g", x, d)
		}
		if s2, c2 := SinCos64(-x); s2 != -s || c2 != c {
			t.Errorf("SinCos(-%v) = %v,%v; want %v,%v", x, s2, c2, -s, c)
		}
	}
}

func TestTanAndPythagoras(t *testing.T) {
	for i := -1000; i <= 1000; i++ {
		x := float64(i) * 0.0123
		s, c := SinCos64(x)
		if d := math.Abs(s*s + c*c - 1); d > 4e-16 {
			t.Fatalf("sin²+cos² at %v off by %g", x, d)
		}
		if math.Abs(c) > 1e-3 {
			if got, want := Tan64(x), math.Tan(x); math.Abs(got-want) > 1e-14*math.Max(1, math.Abs(want)) {
				t.Fatalf("Tan(%v) = %v, want %v", x, got, want)
			}
		}
	}
}
