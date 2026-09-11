package gmath

import "testing"

// Sinks keep the compiler from discarding benchmark results.
var (
	sinkM4 Mat4
	sinkV4 Vec4
	sinkF  float32
)

func BenchmarkMat4Mul(b *testing.B) {
	r := newRNG(101)
	x, y := r.trs(), Perspective(1, 16.0/9, 0.1, 200)
	b.ReportAllocs()
	for b.Loop() {
		x = y.Mul(x)
		x[15] = 1 // keep the chain bounded
	}
	sinkM4 = x
}

func BenchmarkMat4MulVec4(b *testing.B) {
	r := newRNG(103)
	m := r.trs()
	v := V4(1, 2, 3, 1)
	b.ReportAllocs()
	for b.Loop() {
		v = m.MulVec4(v)
		v = v.Scale(0.1)
	}
	sinkV4 = v
}

func BenchmarkSinCos(b *testing.B) {
	x, acc := float32(0), float32(0)
	b.ReportAllocs()
	for b.Loop() {
		s, c := SinCos(x)
		acc += s + c
		x += 0.001
		if x > 100 {
			x = -100
		}
	}
	sinkF = acc
}

// TestZeroAllocs gates the hot-path functions at 0 allocations, so a regression shows
// up in plain `go test`, not only when someone reads benchmark output.
func TestZeroAllocs(t *testing.T) {
	r := newRNG(107)
	m, n := r.trs(), r.trs()
	q := r.quat()
	v := V4(1, 2, 3, 1)
	bx := box(0, 0, 0, 1, 1, 1)
	tests := []struct {
		name string
		f    func()
	}{
		{"Mat4.Mul", func() { sinkM4 = m.Mul(n) }},
		{"Mat4.MulVec4", func() { sinkV4 = m.MulVec4(v) }},
		{"Mat4.Inverse", func() { sinkM4, _ = m.Inverse() }},
		{"Mat4.NormalMatrix", func() { sinkF = m.NormalMatrix()[0] }},
		{"SinCos", func() { s, c := SinCos(v.X); sinkF = s + c }},
		{"Atan2", func() { sinkF = Atan2(v.Y, v.X) }},
		{"Quat.Mat4", func() { sinkM4 = q.Mat4() }},
		{"Quat.Slerp", func() { sinkF = q.Slerp(QuatIdent(), 0.3).W }},
		{"AABB.Transform", func() { sinkF = bx.Transform(m).Max.X }},
		{"LookAt", func() { sinkM4 = LookAt(V3(0, 5, 10), Zero3, Up) }},
	}
	for _, tc := range tests {
		if a := testing.AllocsPerRun(100, tc.f); a != 0 {
			t.Errorf("%s allocates %v times per call", tc.name, a)
		}
	}
}
