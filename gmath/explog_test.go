package gmath

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

func TestExpLogMatchMath(t *testing.T) {
	for i := -3000; i <= 3000; i++ {
		x := float64(i) * 0.2371
		if got, want := Exp64(x), math.Exp(x); x < 709 && !close64(got, want) {
			t.Errorf("Exp64(%g) = %g, want %g", x, got, want)
		}
		if x > 0 {
			if got, want := Log64(x), math.Log(x); !close64(got, want) {
				t.Errorf("Log64(%g) = %g, want %g", x, got, want)
			}
		}
	}
	// Near the top of the range math.Exp on amd64 overflows early; Exp64 does not.
	if got := Exp64(709.6403); math.IsInf(got, 0) || !close64(got, 1.5590729126401796e+308) {
		t.Errorf("Exp64(709.6403) = %g", got)
	}
	// And math.Log on amd64 is wrong for subnormals; Log64 is not.
	if got := Log64(5e-324); !close64(got, -744.4400719213812) {
		t.Errorf("Log64(5e-324) = %g", got)
	}
	for _, x := range []float64{1e-300, 1e300, math.MaxFloat64, 1, 2, 0.5, math.E} {
		if got, want := Log64(x), math.Log(x); !close64(got, want) {
			t.Errorf("Log64(%g) = %g, want %g", x, got, want)
		}
	}
}

func close64(a, b float64) bool {
	if math.IsInf(b, 0) || b == 0 {
		return a == b
	}
	return math.Abs(a-b) <= 1e-14*math.Abs(b)
}

func TestPowSpecialCases(t *testing.T) {
	inf, nan := math.Inf(1), math.NaN()
	negZero := math.Copysign(0, -1)
	cases := []struct{ x, y, want float64 }{
		{3, 0, 1}, {nan, 0, 1}, {1, nan, 1}, {5, 1, 5},
		{nan, 2, nan}, {2, nan, nan},
		{0, -3, inf}, {negZero, -3, -inf}, {0, -2, inf}, {negZero, 3, negZero}, {0, 2, 0}, {negZero, 2, 0},
		{-1, inf, 1}, {2, inf, inf}, {2, -inf, 0}, {0.5, inf, 0}, {0.5, -inf, inf},
		{inf, 2, inf}, {inf, -2, 0}, {-inf, 3, -inf}, {-inf, 2, inf}, {-inf, -3, negZero},
		{-8, 0.5, nan}, {-2, 3, -8}, {-2, 2, 4}, {2, -2, 0.25}, {2, 10, 1024}, {10, 15, 1e15},
		{4, 0.5, 2}, {9, 0.5, 3},
	}
	for _, c := range cases {
		got := Pow64(c.x, c.y)
		if math.IsNaN(c.want) != math.IsNaN(got) || !math.IsNaN(got) && (got != c.want || math.Signbit(got) != math.Signbit(c.want)) {
			t.Errorf("Pow64(%g, %g) = %g, want %g", c.x, c.y, got, c.want)
		}
	}
	for _, c := range [][2]float64{{2.5, 1.3}, {0.1, 3.7}, {123.4, -0.77}, {7, 100.5}, {1.0001, 5000}} {
		if got, want := Pow64(c[0], c[1]), math.Pow(c[0], c[1]); math.Abs(got-want) > 1e-12*math.Abs(want) {
			t.Errorf("Pow64(%g, %g) = %g, want %g", c[0], c[1], got, want)
		}
	}
}

// TestExpLogPowGolden pins the bits: CI runs it on amd64 and on arm64 under qemu.
func TestExpLogPowGolden(t *testing.T) {
	const want = "584a76636928b5a552a5fbcfc67307bb899824e4a50f73ddc779c9370e9f0eae"
	h := sha256.New()
	var b [8]byte
	for i := 0; i < 20000; i++ {
		x := float64(i-10000) * 0.0373
		y := float64(i%200-100) * 0.113
		for _, v := range [...]float64{Exp64(x), Log64(math.Abs(x)), Pow64(math.Abs(x), y), Pow64(x, float64(i%9-4))} {
			binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
			h.Write(b[:])
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		t.Errorf("Exp64/Log64/Pow64 golden hash = %s, want %s", got, want)
	}
}
