package gmath

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestVec2Arithmetic(t *testing.T) {
	a, b := V2(1, 2), V2(3, -5)
	tests := []struct {
		name      string
		got, want Vec2
		tol       float32 // 0: exact
	}{
		{"V2", V2(1, 2), Vec2{1, 2}, 0},
		{"Add", a.Add(b), V2(4, -3), 0},
		{"Sub", a.Sub(b), V2(-2, 7), 0},
		{"Scale", V2(1, -2).Scale(2.5), V2(2.5, -5), 0},
		{"ScaleZero", a.Scale(0), V2(0, 0), 0},
		{"Mul", V2(2, 3).Mul(V2(-1, 4)), V2(-2, 12), 0},
		{"Min", V2(1, 5).Min(V2(3, -5)), V2(1, -5), 0},
		{"Max", V2(1, 5).Max(V2(3, -5)), V2(3, 5), 0},
		{"MinSelf", a.Min(a), a, 0},
		{"Lerp0", a.Lerp(b, 0), a, 0},
		{"Lerp1", a.Lerp(b, 1), b, 0},
		{"LerpHalf", a.Lerp(b, 0.5), V2(2, -1.5), 0},
		{"LerpExtrapolate", a.Lerp(b, 2), V2(5, -12), 0},
		{"NormalizeZero", Vec2{}.Normalize(), Vec2{}, 0},
		{"Normalize34", V2(3, 4).Normalize(), V2(0.6, 0.8), 1e-7},
		{"NormalizeAxis", V2(0, -7).Normalize(), V2(0, -1), 0},
	}
	for _, tc := range tests {
		if tc.tol == 0 && tc.got != tc.want || !approxV2(tc.got, tc.want, tc.tol) {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}

	scalars := []struct {
		name      string
		got, want float32
	}{
		{"Dot", a.Dot(b), -7},
		{"DotSelf", a.Dot(a), 5},
		{"CrossXY", V2(1, 0).Cross(V2(0, 1)), 1},
		{"CrossYX", V2(0, 1).Cross(V2(1, 0)), -1},
		{"CrossParallel", V2(2, 4).Cross(V2(1, 2)), 0},
		{"Cross", a.Cross(b), -11},
		{"Len", V2(3, -4).Len(), 5},
		{"LenZero", Vec2{}.Len(), 0},
	}
	for _, tc := range scalars {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestVec3Arithmetic(t *testing.T) {
	a, b := V3(1, 2, 3), V3(-4, 5, 0.5)
	tests := []struct {
		name      string
		got, want Vec3
		tol       float32
	}{
		{"V3", V3(1, 2, 3), Vec3{1, 2, 3}, 0},
		{"Add", a.Add(b), V3(-3, 7, 3.5), 0},
		{"Sub", a.Sub(b), V3(5, -3, 2.5), 0},
		{"Scale", a.Scale(-2), V3(-2, -4, -6), 0},
		{"Mul", a.Mul(b), V3(-4, 10, 1.5), 0},
		{"Neg", a.Neg(), V3(-1, -2, -3), 0},
		{"NegNeg", a.Neg().Neg(), a, 0},
		{"Cross", a.Cross(b), V3(2*0.5-3*5, 3*-4-1*0.5, 1*5-2*-4), 0},
		{"CrossAnti", b.Cross(a), a.Cross(b).Neg(), 0},
		{"CrossSelf", a.Cross(a), Vec3{}, 0},
		{"CrossXY", UnitX.Cross(UnitY), UnitZ, 0},
		{"CrossYZ", UnitY.Cross(UnitZ), UnitX, 0},
		{"CrossZX", UnitZ.Cross(UnitX), UnitY, 0},
		{"ForwardCrossUpIsRight", Forward.Cross(Up), RightDir, 0},
		{"RightCrossUpIsBack", RightDir.Cross(Up), Forward.Neg(), 0},
		{"Abs", V3(-1, 0, -2.5).Abs(), V3(1, 0, 2.5), 0},
		{"Min", a.Min(b), V3(-4, 2, 0.5), 0},
		{"Max", a.Max(b), V3(1, 5, 3), 0},
		{"Lerp0", a.Lerp(b, 0), a, 0},
		{"Lerp1", a.Lerp(b, 1), b, 0},
		{"LerpQuarter", a.Lerp(b, 0.25), V3(-0.25, 2.75, 2.375), 0},
		{"NormalizeZero", Zero3.Normalize(), Vec3{}, 0},
		{"NormalizeAxis", V3(0, 0, -9).Normalize(), Forward, 0},
		{"Normalize", V3(2, 3, 6).Normalize(), V3(2.0/7, 3.0/7, 6.0/7), 1e-7},
	}
	for _, tc := range tests {
		if tc.tol == 0 && tc.got != tc.want || !approxV3(tc.got, tc.want, tc.tol) {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}

	scalars := []struct {
		name      string
		got, want float32
	}{
		{"Dot", a.Dot(b), -4 + 10 + 1.5},
		{"DotOrthogonal", UnitX.Dot(UnitY), 0},
		{"LenSq", a.LenSq(), 14},
		{"Len", V3(2, 3, 6).Len(), 7},
		{"LenZero", Zero3.Len(), 0},
		{"Dist", V3(1, 1, 1).Dist(V3(3, 4, 7)), 7},
		{"DistSelf", a.Dist(a), 0},
	}
	for _, tc := range scalars {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}

	if got := a.XY(); got != V2(1, 2) {
		t.Errorf("XY = %v", got)
	}
	if got := a.Vec4(7); got != V4(1, 2, 3, 7) {
		t.Errorf("Vec4 = %v", got)
	}
}

func TestVec3Constants(t *testing.T) {
	tests := []struct {
		name      string
		got, want Vec3
	}{
		{"Zero3", Zero3, Vec3{0, 0, 0}},
		{"One3", One3, Vec3{1, 1, 1}},
		{"UnitX", UnitX, Vec3{1, 0, 0}},
		{"UnitY", UnitY, Vec3{0, 1, 0}},
		{"UnitZ", UnitZ, Vec3{0, 0, 1}},
		{"Up", Up, Vec3{0, 1, 0}},
		{"Forward", Forward, Vec3{0, 0, -1}},
		{"RightDir", RightDir, Vec3{1, 0, 0}},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestVec3NormalizeProperties(t *testing.T) {
	r := newRNG(3)
	for i := 0; i < 1000; i++ {
		v := r.vec3(-100, 100)
		n := v.Normalize()
		if !approx(n.Len(), 1, 2e-7) {
			t.Fatalf("|Normalize(%v)| = %v", v, n.Len())
		}
		if n.Dot(v) <= 0 || !approxV3(n.Cross(v), Vec3{}, 2e-5) {
			t.Fatalf("Normalize(%v) = %v changed direction", v, n)
		}
	}
	for _, v := range []Vec3{V3(1e-15, 0, 0), V3(0, -1e15, 0), V3(1e-10, 2e-10, -3e-10)} {
		if n := v.Normalize(); !approx(n.Len(), 1, 2e-7) {
			t.Errorf("|Normalize(%v)| = %v", v, n.Len())
		}
	}
	// Outside the range where the float32 squared length is a normal number: tiny vectors
	// (squared length underflows) and huge ones (overflows to +Inf) used to normalize to
	// the zero vector, which silently turned QuatAxisAngle into the identity and LookAt
	// into a singular matrix.
	sub := math.Float32frombits(1) // smallest subnormal
	extreme := []struct {
		v    Vec3
		want Vec3
		len  float32
	}{
		{V3(1e-23, 0, 0), UnitX, 1e-23},
		{V3(0, -3e-30, 4e-30), V3(0, -0.6, 0.8), 5e-30},
		{V3(sub, 0, 0), UnitX, sub},
		{V3(0, 0, -1e20), Forward, 1e20},
		{V3(3e25, 4e25, 0), V3(0.6, 0.8, 0), 5e25},
		{V3(math.MaxFloat32, math.MaxFloat32, 0), V3(1/math.Sqrt2, 1/math.Sqrt2, 0), inf32},
		{V3(1e-20, 1e-20, 1e-20), V3(0.57735026, 0.57735026, 0.57735026), 1.7320508e-20},
	}
	for _, tc := range extreme {
		if n := tc.v.Normalize(); !approxV3(n, tc.want, 2e-7) {
			t.Errorf("Normalize(%v) = %v, want %v", tc.v, n, tc.want)
		}
		if l := tc.v.Len(); !(l == tc.len || Abs(l-tc.len) <= 2e-7*tc.len) {
			t.Errorf("Len(%v) = %v, want %v", tc.v, l, tc.len)
		}
		if n := tc.v.XY().Normalize(); tc.v.XY() != (Vec2{}) && !approx(n.Len(), 1, 2e-7) {
			t.Errorf("Vec2.Normalize(%v) = %v", tc.v.XY(), n)
		}
	}
	if n := V3(nan32, 1, 0).Normalize(); n.IsFinite() {
		t.Errorf("Normalize(NaN vector) = %v, want NaN", n)
	}
	if n := V3(float32(math.Copysign(0, -1)), 0, 0).Normalize(); n != (Vec3{}) {
		t.Errorf("Normalize(-0, 0, 0) = %v, want zero", n)
	}
	// Downstream: a tiny or huge axis is still a valid axis, and a huge up vector is
	// still a valid up vector.
	if q := QuatAxisAngle(V3(0, 1e-25, 0), 1); !sameRotation(q, QuatAxisAngle(UnitY, 1), 1e-7) {
		t.Errorf("QuatAxisAngle(tiny Y axis) = %v", q)
	}
	if q := QuatAxisAngle(V3(0, 1e25, 0), 1); !sameRotation(q, QuatAxisAngle(UnitY, 1), 1e-7) {
		t.Errorf("QuatAxisAngle(huge Y axis) = %v", q)
	}
	if got, want := LookAt(V3(0, 0, 5), Zero3, V3(0, 1e20, 0)), LookAt(V3(0, 0, 5), Zero3, Up); !approxM4(got, want, 1e-6) {
		t.Errorf("LookAt with huge up =\n%v\nwant\n%v", got, want)
	}
}

func TestVec3GetWith(t *testing.T) {
	a := V3(10, 20, 30)
	for i, want := range []float32{10, 20, 30} {
		if got := a.Get(i); got != want {
			t.Errorf("Get(%d) = %v, want %v", i, got, want)
		}
	}
	tests := []struct {
		i    int
		want Vec3
	}{
		{0, V3(-1, 20, 30)},
		{1, V3(10, -1, 30)},
		{2, V3(10, 20, -1)},
	}
	for _, tc := range tests {
		got := a.With(tc.i, -1)
		if got != tc.want {
			t.Errorf("With(%d, -1) = %v, want %v", tc.i, got, tc.want)
		}
		if got.Get(tc.i) != -1 {
			t.Errorf("With(%d).Get(%d) = %v", tc.i, tc.i, got.Get(tc.i))
		}
	}
	if a != V3(10, 20, 30) {
		t.Errorf("With modified the receiver: %v", a)
	}
}

func TestVec4Arithmetic(t *testing.T) {
	a, b := V4(1, 2, 3, 4), V4(-1, 0.5, 2, -2)
	tests := []struct {
		name      string
		got, want Vec4
	}{
		{"V4", V4(1, 2, 3, 4), Vec4{1, 2, 3, 4}},
		{"Add", a.Add(b), V4(0, 2.5, 5, 2)},
		{"Sub", a.Sub(b), V4(2, 1.5, 1, 6)},
		{"Scale", a.Scale(0.5), V4(0.5, 1, 1.5, 2)},
		{"Mul", a.Mul(b), V4(-1, 1, 6, -8)},
		{"Lerp0", a.Lerp(b, 0), a},
		{"Lerp1", a.Lerp(b, 1), b},
		{"LerpHalf", a.Lerp(b, 0.5), V4(0, 1.25, 2.5, 1)},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if got := a.Dot(b); got != -1+1+6-8 {
		t.Errorf("Dot = %v", got)
	}
	if got := a.XYZ(); got != V3(1, 2, 3) {
		t.Errorf("XYZ = %v", got)
	}
}

func TestLerpEndpointsRandom(t *testing.T) {
	// Both endpoints are exact for any inputs, including magnitudes far apart where
	// a + (b-a) alone would miss b.
	r := newRNG(11)
	for i := 0; i < 2000; i++ {
		a, b := r.vec3(-1000, 1000), r.vec3(-1000, 1000)
		if i%2 == 1 {
			a = a.Scale(1e5)
			b = b.Scale(1e-3)
		}
		if got := a.Lerp(b, 0); got != a {
			t.Fatalf("Lerp(%v, %v, 0) = %v", a, b, got)
		}
		if got := a.Lerp(b, 1); got != b {
			t.Fatalf("Lerp(%v, %v, 1) = %v, want exactly b", a, b, got)
		}
		if got := V2(a.X, a.Y).Lerp(V2(b.X, b.Y), 1); got != V2(b.X, b.Y) {
			t.Fatalf("Vec2.Lerp(1) = %v", got)
		}
		if got := a.Vec4(a.Z).Lerp(b.Vec4(b.X), 1); got != b.Vec4(b.X) {
			t.Fatalf("Vec4.Lerp(1) = %v", got)
		}
		ai, bi := V3(float32(i), float32(-i), 7), V3(float32(3*i), 5, float32(-2*i))
		if got := ai.Lerp(bi, 1); got != bi {
			t.Fatalf("Lerp(%v, %v, 1) = %v, want exact", ai, bi, got)
		}
	}
}

func TestVecIsFinite(t *testing.T) {
	bad := []float32{nan32, inf32, ninf32}
	good := []float32{0, float32(math.Copysign(0, -1)), 1, -1, math.MaxFloat32, -math.MaxFloat32, math.SmallestNonzeroFloat32}
	for _, g := range good {
		if !V2(g, g).IsFinite() || !V3(g, g, g).IsFinite() || !V4(g, g, g, g).IsFinite() {
			t.Errorf("IsFinite false for all-%v vector", g)
		}
	}
	for _, v := range bad {
		for i := 0; i < 4; i++ {
			c := [4]float32{1, 2, 3, 4}
			c[i] = v
			if i < 2 && V2(c[0], c[1]).IsFinite() {
				t.Errorf("Vec2 with %v at %d reported finite", v, i)
			}
			if i < 3 && V3(c[0], c[1], c[2]).IsFinite() {
				t.Errorf("Vec3 with %v at %d reported finite", v, i)
			}
			if V4(c[0], c[1], c[2], c[3]).IsFinite() {
				t.Errorf("Vec4 with %v at %d reported finite", v, i)
			}
		}
	}
}

func TestVecMarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"Vec2", V2(1, -2.5), `[1,-2.5]`},
		{"Vec2Zero", Vec2{}, `[0,0]`},
		{"Vec3", V3(0.1, 0, -3), `[0.1,0,-3]`},
		{"Vec3Ptr", &Vec3{1, 2, 3}, `[1,2,3]`},
		{"Vec4", V4(1, 2, 3, 4), `[1,2,3,4]`},
		{"Vec3Large", V3(1e20, -1e-20, 3.4028235e38), `[100000000000000000000,-1e-20,3.4028235e+38]`},
		{"InStruct", struct {
			P Vec3 `json:"p"`
			S Vec2 `json:"s"`
		}{V3(1, 2, 3), V2(4, 5)}, `{"p":[1,2,3],"s":[4,5]}`},
		{"Slice", []Vec2{V2(1, 2), V2(3, 4)}, `[[1,2],[3,4]]`},
	}
	for _, tc := range tests {
		b, err := json.Marshal(tc.v)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if string(b) != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, b, tc.want)
		}
	}
	for _, v := range []any{V2(nan32, 0), V3(0, inf32, 0), V4(0, 0, 0, ninf32)} {
		if b, err := json.Marshal(v); err == nil {
			t.Errorf("Marshal(%v) = %s, want error for non-finite component", v, b)
		}
	}
}

func TestVecJSONRoundTrip(t *testing.T) {
	r := newRNG(5)
	special := []float32{0, float32(math.Copysign(0, -1)), math.MaxFloat32, -math.MaxFloat32,
		math.SmallestNonzeroFloat32, 1.17549435e-38, 0.1, 1.0 / 3, 16777217}
	for i := 0; i < 500+len(special); i++ {
		var x, y, z, w float32
		if i < len(special) {
			x, y, z, w = special[i], -special[i], special[(i+1)%len(special)], 1
		} else {
			x = math.Float32frombits(uint32(r.u64()))
			y, z, w = r.f(-1e6, 1e6), r.f(-1, 1), r.f(-1e-30, 1e-30)
			if !IsFinite(x) {
				x = 42
			}
		}
		v2, v3, v4 := V2(x, y), V3(x, y, z), V4(x, y, z, w)
		var g2 Vec2
		var g3 Vec3
		var g4 Vec4
		roundTrip(t, v2, &g2)
		roundTrip(t, v3, &g3)
		roundTrip(t, v4, &g4)
		if !sameBits(g2.X, x) || !sameBits(g2.Y, y) {
			t.Fatalf("Vec2 round trip %v -> %v", v2, g2)
		}
		if !sameBits(g3.X, x) || !sameBits(g3.Y, y) || !sameBits(g3.Z, z) {
			t.Fatalf("Vec3 round trip %v -> %v", v3, g3)
		}
		if !sameBits(g4.X, x) || !sameBits(g4.Y, y) || !sameBits(g4.Z, z) || !sameBits(g4.W, w) {
			t.Fatalf("Vec4 round trip %v -> %v", v4, g4)
		}
	}
}

func sameBits(a, b float32) bool { return math.Float32bits(a) == math.Float32bits(b) }

func roundTrip(t *testing.T, in any, out any) {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal(%v): %v", in, err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("Unmarshal(%s): %v", b, err)
	}
}

func TestVecUnmarshalJSONRejects(t *testing.T) {
	inputs := map[string][]string{
		"vec2": {`[]`, `[1]`, `[1,2,3]`, `{}`, `{"X":1,"Y":2}`, `"1,2"`, `1`, `true`, `null`,
			`[1,null]`, `[null,null]`, `[1,"2"]`, `[[1],2]`, `[1e39,0]`, `[1,2`, ``},
		"vec3": {`[]`, `[1,2]`, `[1,2,3,4]`, `{}`, `"x"`, `3`, `null`, `[1,2,null]`, `[1,2,false]`,
			`[1,2,[3]]`, `[1,2,-1e40]`},
		"vec4": {`[]`, `[1,2,3]`, `[1,2,3,4,5]`, `{}`, `"x"`, `null`, `[1,2,3,null]`, `[1,2,3,{}]`},
	}
	// Iterate in a fixed order (hard rule 4, even in tests).
	for _, kind := range []string{"vec2", "vec3", "vec4"} {
		for _, in := range inputs[kind] {
			var err error
			switch kind {
			case "vec2":
				v := V2(7, 7)
				err = v.UnmarshalJSON([]byte(in))
				if v != V2(7, 7) {
					t.Errorf("%s %q: value changed to %v on error", kind, in, v)
				}
			case "vec3":
				v := V3(7, 7, 7)
				err = v.UnmarshalJSON([]byte(in))
				if v != V3(7, 7, 7) {
					t.Errorf("%s %q: value changed to %v on error", kind, in, v)
				}
			case "vec4":
				v := V4(7, 7, 7, 7)
				err = v.UnmarshalJSON([]byte(in))
				if v != V4(7, 7, 7, 7) {
					t.Errorf("%s %q: value changed to %v on error", kind, in, v)
				}
			}
			if err == nil {
				t.Errorf("%s: UnmarshalJSON(%q) accepted invalid input", kind, in)
			} else if !strings.HasPrefix(err.Error(), kind+": ") {
				t.Errorf("%s: error %q lacks the %q context prefix", kind, err, kind)
			}
		}
	}
}

func TestVecUnmarshalJSONInStrictStruct(t *testing.T) {
	type part struct {
		Position Vec3 `json:"position"`
		Size     Vec2 `json:"size"`
		Color    Vec4 `json:"color"`
	}
	dec := json.NewDecoder(strings.NewReader(`{"position":[0,0.5,-2],"size":[10,10],"color":[1,0.5,0.25,1]}`))
	dec.DisallowUnknownFields()
	var p part
	if err := dec.Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Position != V3(0, 0.5, -2) || p.Size != V2(10, 10) || p.Color != V4(1, 0.5, 0.25, 1) {
		t.Errorf("decoded %+v", p)
	}
	bad := []string{
		`{"position":[0,0.5],"size":[10,10],"color":[1,1,1,1]}`,
		`{"position":null}`,
		`{"position":[0,null,0]}`,
		`{"size":{"x":1,"y":2}}`,
	}
	for _, in := range bad {
		var q part
		if err := json.Unmarshal([]byte(in), &q); err == nil {
			t.Errorf("Unmarshal(%s) accepted invalid vector: %+v", in, q)
		}
	}
}
