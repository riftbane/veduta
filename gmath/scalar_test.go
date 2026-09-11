package gmath

import (
	"math"
	"testing"
)

func TestRadiansDegrees(t *testing.T) {
	tests := []struct {
		deg, rad float32
	}{
		{0, 0},
		{180, math.Pi},
		{-180, -math.Pi},
		{90, math.Pi / 2},
		{45, math.Pi / 4},
		{360, 2 * math.Pi},
		{-30, -math.Pi / 6},
		{1, math.Pi / 180},
	}
	for _, tc := range tests {
		if got := Radians(tc.deg); got != tc.rad {
			t.Errorf("Radians(%v) = %v, want %v", tc.deg, got, tc.rad)
		}
		if got := Degrees(tc.rad); !approx(got, tc.deg, 6e-8) {
			t.Errorf("Degrees(%v) = %v, want %v", tc.rad, got, tc.deg)
		}
	}
	if Deg2Rad*Rad2Deg != 1 {
		t.Errorf("Deg2Rad*Rad2Deg = %v", Deg2Rad*Rad2Deg)
	}
}

func TestRadiansDegreesRoundTrip(t *testing.T) {
	// Each conversion rounds once to float32, so a round trip is within one ulp; it is
	// exact for most whole degrees but not all (the rounding is not invertible).
	oneULP := func(a, b float32) bool {
		d := int64(math.Float32bits(a)) - int64(math.Float32bits(b))
		return d >= -1 && d <= 1 && (a < 0) == (b < 0)
	}
	for d := -720; d <= 720; d++ {
		deg := float32(d)
		if got := Degrees(Radians(deg)); !oneULP(got, deg) {
			t.Errorf("Degrees(Radians(%v)) = %v", deg, got)
		}
	}
	r := newRNG(17)
	for i := 0; i < 10000; i++ {
		rad := r.f(-100, 100)
		if got := Radians(Degrees(rad)); !oneULP(got, rad) && !approx(got, rad, 1.2e-7) {
			t.Fatalf("Radians(Degrees(%v)) = %v", rad, got)
		}
		deg := r.f(-1e4, 1e4)
		if got := Degrees(Radians(deg)); !oneULP(got, deg) && !approx(got, deg, 1.2e-7) {
			t.Fatalf("Degrees(Radians(%v)) = %v", deg, got)
		}
	}
}

func TestAbs(t *testing.T) {
	tests := []struct{ in, want float32 }{
		{0, 0},
		{float32(math.Copysign(0, -1)), 0},
		{1.5, 1.5},
		{-1.5, 1.5},
		{ninf32, inf32},
		{-math.MaxFloat32, math.MaxFloat32},
		{-math.SmallestNonzeroFloat32, math.SmallestNonzeroFloat32},
	}
	for _, tc := range tests {
		got := Abs(tc.in)
		if !sameBits(got, tc.want) {
			t.Errorf("Abs(%v) = %v (bits %#x), want %v", tc.in, got, math.Float32bits(got), tc.want)
		}
	}
	if got := Abs(-nan32); !math.IsNaN(float64(got)) || math.Signbit(float64(got)) {
		t.Errorf("Abs(-NaN) = %v, want positive NaN", got)
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		x, lo, hi, want float32
	}{
		{0.5, 0, 1, 0.5},
		{-1, 0, 1, 0},
		{2, 0, 1, 1},
		{0, 0, 1, 0},
		{1, 0, 1, 1},
		{-5, -10, -2, -5},
		{-20, -10, -2, -10},
		{3, 3, 3, 3},
		{ninf32, -1, 1, -1},
		{inf32, -1, 1, 1},
	}
	for _, tc := range tests {
		if got := Clamp(tc.x, tc.lo, tc.hi); got != tc.want {
			t.Errorf("Clamp(%v, %v, %v) = %v, want %v", tc.x, tc.lo, tc.hi, got, tc.want)
		}
	}
	c01 := []struct{ x, want float32 }{{-0.1, 0}, {0, 0}, {0.3, 0.3}, {1, 1}, {1.0001, 1}, {inf32, 1}}
	for _, tc := range c01 {
		if got := Clamp01(tc.x); got != tc.want {
			t.Errorf("Clamp01(%v) = %v, want %v", tc.x, got, tc.want)
		}
	}
}

func TestLerpScalar(t *testing.T) {
	tests := []struct {
		a, b, t, want float32
	}{
		{0, 10, 0, 0},
		{0, 10, 1, 10},
		{0, 10, 0.5, 5},
		{-4, 4, 0.25, -2},
		{2, 2, 0.7, 2},
		{1, 3, 2, 5},
		{1, 3, -1, -1},
		{10, 0, 0.1, 9},
		// a + (b-a) would give 0 here (b-a rounds to -1e8); t = 1 must be exactly b.
		{1e8, 0.1, 1, 0.1},
		{0.1, 1e8, 0, 0.1},
		{-3e7, 1.7, 1, 1.7},
	}
	for _, tc := range tests {
		if got := Lerp(tc.a, tc.b, tc.t); got != tc.want {
			t.Errorf("Lerp(%v, %v, %v) = %v, want %v", tc.a, tc.b, tc.t, got, tc.want)
		}
	}
	// Monotone in t for a fixed pair.
	prev := Lerp(-3, 7, 0)
	for i := 1; i <= 1000; i++ {
		v := Lerp(-3, 7, float32(i)/1000)
		if v < prev {
			t.Fatalf("Lerp not monotone at t=%v: %v < %v", float32(i)/1000, v, prev)
		}
		prev = v
	}
}

func TestSqrtFloorCeil(t *testing.T) {
	sq := []struct{ in, want float32 }{
		{0, 0}, {1, 1}, {4, 2}, {0.25, 0.5}, {2, float32(math.Sqrt(2))}, {1e30, 1e15}, {inf32, inf32},
	}
	for _, tc := range sq {
		if got := Sqrt(tc.in); got != tc.want {
			t.Errorf("Sqrt(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if got := Sqrt(-1); !math.IsNaN(float64(got)) {
		t.Errorf("Sqrt(-1) = %v, want NaN", got)
	}
	fc := []struct{ in, floor, ceil float32 }{
		{0, 0, 0}, {1.5, 1, 2}, {-1.5, -2, -1}, {3, 3, 3}, {-3, -3, -3}, {0.0001, 0, 1}, {-0.0001, -1, 0},
		{16777216, 16777216, 16777216},
	}
	for _, tc := range fc {
		if got := Floor(tc.in); got != tc.floor {
			t.Errorf("Floor(%v) = %v, want %v", tc.in, got, tc.floor)
		}
		if got := Ceil(tc.in); got != tc.ceil {
			t.Errorf("Ceil(%v) = %v, want %v", tc.in, got, tc.ceil)
		}
	}
}

func TestIsFiniteScalar(t *testing.T) {
	tests := []struct {
		in   float32
		want bool
	}{
		{0, true},
		{float32(math.Copysign(0, -1)), true},
		{1, true},
		{math.MaxFloat32, true},
		{-math.MaxFloat32, true},
		{math.SmallestNonzeroFloat32, true},
		{nan32, false},
		{-nan32, false},
		{inf32, false},
		{ninf32, false},
		{math.Float32frombits(0x7f800001), false}, // signalling NaN
	}
	for _, tc := range tests {
		if got := IsFinite(tc.in); got != tc.want {
			t.Errorf("IsFinite(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSign(t *testing.T) {
	tests := []struct{ in, want float32 }{
		{3, 1},
		{-3, -1},
		{0, 0},
		{float32(math.Copysign(0, -1)), 0},
		{math.SmallestNonzeroFloat32, 1},
		{-math.SmallestNonzeroFloat32, -1},
		{inf32, 1},
		{ninf32, -1},
		{nan32, 0},
	}
	for _, tc := range tests {
		if got := Sign(tc.in); got != tc.want {
			t.Errorf("Sign(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSmoothstep(t *testing.T) {
	tests := []struct {
		e0, e1, x, want float32
	}{
		{0, 1, 0, 0},
		{0, 1, 1, 1},
		{0, 1, 0.5, 0.5},
		{0, 1, -1, 0},
		{0, 1, 2, 1},
		{0, 1, 0.25, 0.15625},
		{0, 1, 0.75, 0.84375},
		{2, 4, 3, 0.5},
		{2, 4, 1, 0},
		{2, 4, 5, 1},
		{-1, 1, 0, 0.5},
		{1, 0, 0, 1}, // reversed edges mirror the curve
		{1, 0, 1, 0},
		{1, 0, 0.25, 0.84375},
		// Equal edges: a step, never NaN (0/0 at x == e).
		{2, 2, 2, 1},
		{2, 2, 1.9, 0},
		{2, 2, 2.1, 1},
	}
	for _, tc := range tests {
		if got := Smoothstep(tc.e0, tc.e1, tc.x); got != tc.want {
			t.Errorf("Smoothstep(%v, %v, %v) = %v, want %v", tc.e0, tc.e1, tc.x, got, tc.want)
		}
	}
	prev := float32(0)
	for i := 0; i <= 1000; i++ {
		x := float32(i) / 1000
		v := Smoothstep(0, 1, x)
		if v < prev || v < 0 || v > 1 {
			t.Fatalf("Smoothstep not monotone in [0,1] at %v: %v (prev %v)", x, v, prev)
		}
		if s := v + Smoothstep(0, 1, 1-x); !approx(s, 1, 3e-7) {
			t.Fatalf("Smoothstep(x) + Smoothstep(1-x) = %v at x=%v", s, x)
		}
		prev = v
	}
}

func TestWrap(t *testing.T) {
	negZero := float32(math.Copysign(0, -1))
	tests := []struct {
		x, m, want float32
	}{
		{0, 1, 0},
		{0.25, 1, 0.25},
		{1, 1, 0},
		{1.25, 1, 0.25},
		{-0.25, 1, 0.75},
		{-1.25, 1, 0.75},
		{-1, 1, 0},
		{-3, 2, 1},
		{7, 3, 1},
		{-7, 3, 2},
		{370, 360, 10},
		{-10, 360, 350},
		{-725, 360, 355},
		{5.5, 0.5, 0},
		{-5.25, 0.5, 0.25},
		{negZero, 1, 0},
		// A tiny negative x: x+m rounds to m in float32, which would leave [0, m).
		{-1e-8, 1, math.Nextafter32(1, 0)},
		{-1e-30, 360, math.Nextafter32(360, 0)},
	}
	for _, tc := range tests {
		got := Wrap(tc.x, tc.m)
		if got != tc.want {
			t.Errorf("Wrap(%v, %v) = %v, want %v", tc.x, tc.m, got, tc.want)
		}
	}
	r := newRNG(23)
	for i := 0; i < 20000; i++ {
		m := r.f(0.01, 100)
		var x float32
		switch i % 3 {
		case 0:
			x = r.f(-1000, 1000)
		case 1:
			x = r.f(-1e-6, 1e-6)
		default:
			x = -m * float32(r.u64()%50) // exact negative multiples
		}
		got := Wrap(x, m)
		if !(got >= 0 && got < m) {
			t.Fatalf("Wrap(%v, %v) = %v, not in [0, m)", x, m, got)
		}
		// x - got must be a whole number of periods.
		k := (float64(x) - float64(got)) / float64(m)
		if math.Abs(k-math.Round(k)) > 1e-3 && math.Abs(float64(got)-float64(m)) > 1e-5*float64(m) {
			t.Fatalf("Wrap(%v, %v) = %v: x-r is %v periods", x, m, got, k)
		}
	}
	if got := Wrap(2*Pi+0.5, 2*Pi); !approx(got, 0.5, 1e-6) {
		t.Errorf("Wrap(2pi+0.5, 2pi) = %v", got)
	}
}
