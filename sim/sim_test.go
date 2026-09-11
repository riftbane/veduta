package sim

import (
	"math"
	"testing"

	"github.com/riftbane/veduta/gmath"
)

func TestRNGReferenceVector(t *testing.T) {
	// xoshiro256** reference: state {1, 2, 3, 4} yields 11520, 0, 1509978240, ...
	r := &RNG{}
	r.SetState([4]uint64{1, 2, 3, 4})
	want := []uint64{11520, 0, 1509978240, 1215971899390074240}
	for i, w := range want {
		if got := r.Uint64(); got != w {
			t.Fatalf("output %d = %d, want %d", i, got, w)
		}
	}
}

func TestRNGDeterministicAndDistinct(t *testing.T) {
	a, b, c := NewRNG(42), NewRNG(42), NewRNG(43)
	same := true
	for i := 0; i < 100; i++ {
		x, y, z := a.Uint64(), b.Uint64(), c.Uint64()
		if x != y {
			t.Fatal("same seed diverged")
		}
		if x != z {
			same = false
		}
	}
	if same {
		t.Fatal("different seeds produced identical streams")
	}
	st := a.State()
	next := a.Uint64()
	a.SetState(st)
	if a.Uint64() != next {
		t.Fatal("SetState did not restore the stream")
	}
}

func TestRNGRanges(t *testing.T) {
	r := NewRNG(7)
	counts := make([]int, 6)
	for i := 0; i < 60000; i++ {
		f := r.Float32()
		if f < 0 || f >= 1 {
			t.Fatalf("Float32 = %v", f)
		}
		d := r.Float64()
		if d < 0 || d >= 1 {
			t.Fatalf("Float64 = %v", d)
		}
		counts[r.Intn(6)]++
		if v := r.IntRange(-2, 2); v < -2 || v > 2 {
			t.Fatalf("IntRange = %d", v)
		}
		if v := r.Range(3, 5); v < 3 || v >= 5 {
			t.Fatalf("Range = %v", v)
		}
	}
	for i, n := range counts {
		if n < 9000 || n > 11000 {
			t.Fatalf("Intn bucket %d has %d of 60000", i, n)
		}
	}
}

func TestCanonical(t *testing.T) {
	type state struct {
		Score int     `json:"score"`
		Speed float32 `json:"speed"`
		Name  string  `json:"name"`
	}
	v := map[string]any{
		"tick":   uint64(12),
		"b":      []any{true, nil, "x\"y"},
		"a":      gmath.V3(0.1, -2, 1e-7),
		"f64":    0.1,
		"nan":    float32(math.NaN()),
		"inf":    math.Inf(-1),
		"state":  state{Score: 3, Speed: 1.5, Name: "hero"},
		"ptr":    &state{},
		"nested": map[string]any{"z": 1, "y": []float32{0.25, 3}},
	}
	got, err := AppendCanonical(nil, v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":[0.1,-2,1e-7],"b":[true,null,"x\"y"],"f64":0.1,"inf":"-Inf","nan":"NaN","nested":{"y":[0.25,3],"z":1},"ptr":{"name":"","score":0,"speed":0},"state":{"name":"hero","score":3,"speed":1.5},"tick":12}`
	if string(got) != want {
		t.Fatalf("canonical =\n%s\nwant\n%s", got, want)
	}
	if _, err := AppendCanonical(nil, map[int]int{1: 2}); err == nil {
		t.Fatal("non-string map keys should fail")
	}
}
