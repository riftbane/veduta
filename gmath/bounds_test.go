package gmath

import (
	"encoding/json"
	"strings"
	"testing"
)

func box(x0, y0, z0, x1, y1, z1 float32) AABB { return AABB{V3(x0, y0, z0), V3(x1, y1, z1)} }

func TestAABBEmpty(t *testing.T) {
	e := EmptyAABB()
	if !e.IsEmpty() {
		t.Fatal("EmptyAABB is not empty")
	}
	for _, p := range []Vec3{Zero3, V3(1e30, -1e30, 0), V3(-1e38, 1e38, 3)} {
		if e.Contains(p) {
			t.Errorf("EmptyAABB contains %v", p)
		}
	}
	tests := []struct {
		name  string
		b     AABB
		empty bool
	}{
		{"unit", box(0, 0, 0, 1, 1, 1), false},
		{"point", box(1, 2, 3, 1, 2, 3), false},
		{"flatY", box(-5, 0, -5, 5, 0, 5), false},
		{"invertedX", box(1, 0, 0, 0, 1, 1), true},
		{"invertedY", box(0, 1, 0, 1, 0, 1), true},
		{"invertedZ", box(0, 0, 1, 1, 1, 0), true},
		{"zero", AABB{}, false},
	}
	for _, tc := range tests {
		if got := tc.b.IsEmpty(); got != tc.empty {
			t.Errorf("%s.IsEmpty() = %v, want %v", tc.name, got, tc.empty)
		}
	}
	if got := e.Transform(RotateY(0.7)); !got.IsEmpty() {
		t.Errorf("Transform(empty) = %v, want empty", got)
	}
	if got := e.Transform(Translate(V3(1, 2, 3))); got != e {
		t.Errorf("Transform(empty) = %v, want unchanged", got)
	}
}

func TestAABBExtend(t *testing.T) {
	p := V3(1, -2, 3)
	b := EmptyAABB().Extend(p)
	if b != (AABB{p, p}) || b.IsEmpty() || !b.Contains(p) || b.Size() != Zero3 {
		t.Fatalf("Empty.Extend(p) = %v", b)
	}
	b = b.Extend(V3(-1, 5, 3)).Extend(V3(0, 0, -4))
	if want := box(-1, -2, -4, 1, 5, 3); b != want {
		t.Errorf("Extend = %v, want %v", b, want)
	}
	// Extending by an interior point changes nothing.
	if got := b.Extend(V3(0, 0, 0)); got != b {
		t.Errorf("Extend(interior) = %v", got)
	}
	r := newRNG(59)
	pts := make([]Vec3, 100)
	acc := EmptyAABB()
	for i := range pts {
		pts[i] = r.vec3(-50, 50)
		acc = acc.Extend(pts[i])
	}
	for _, q := range pts {
		if !acc.Contains(q) {
			t.Fatalf("extended box %v misses %v", acc, q)
		}
	}
}

func TestAABBUnion(t *testing.T) {
	a := box(0, 0, 0, 1, 1, 1)
	b := box(2, -1, 0.5, 3, 0.5, 4)
	e := EmptyAABB()
	tests := []struct {
		name      string
		got, want AABB
	}{
		{"disjoint", a.Union(b), box(0, -1, 0, 3, 1, 4)},
		{"commutes", b.Union(a), box(0, -1, 0, 3, 1, 4)},
		{"self", a.Union(a), a},
		{"withEmpty", a.Union(e), a},
		{"emptyWith", e.Union(a), a},
		{"inverted operand ignored", a.Union(box(5, 5, 5, 4, 4, 4)), a},
		{"contained", a.Union(box(0.2, 0.2, 0.2, 0.3, 0.3, 0.3)), a},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if got := e.Union(e); !got.IsEmpty() {
		t.Errorf("empty ∪ empty = %v", got)
	}
}

func TestAABBOverlaps(t *testing.T) {
	a := box(0, 0, 0, 1, 1, 1)
	tests := []struct {
		name string
		b    AABB
		want bool
	}{
		{"same", a, true},
		{"overlapping", box(0.5, 0.5, 0.5, 1.5, 1.5, 1.5), true},
		{"contained", box(0.25, 0.25, 0.25, 0.75, 0.75, 0.75), true},
		{"containing", box(-1, -1, -1, 2, 2, 2), true},
		{"thin slab through", box(-1, 0.5, -1, 2, 0.5, 2), true},
		{"touching +X face", box(1, 0, 0, 2, 1, 1), false},
		{"touching -X face", box(-1, 0, 0, 0, 1, 1), false},
		{"touching +Y face", box(0, 1, 0, 1, 2, 1), false},
		{"touching -Y face", box(0, -1, 0, 1, 0, 1), false},
		{"touching +Z face", box(0, 0, 1, 1, 1, 2), false},
		{"touching -Z face", box(0, 0, -1, 1, 1, 0), false},
		{"touching edge", box(1, 1, 0, 2, 2, 1), false},
		{"touching corner", box(1, 1, 1, 2, 2, 2), false},
		{"separated X", box(1.01, 0, 0, 2, 1, 1), false},
		{"separated Y", box(0, -3, 0, 1, -2, 1), false},
		{"separated Z", box(0, 0, 5, 1, 1, 6), false},
		{"overlap XY not Z", box(0.5, 0.5, 2, 1.5, 1.5, 3), false},
		{"empty", EmptyAABB(), false},
		// An inverted box is empty (IsEmpty, Union ignore it), so it overlaps nothing,
		// even when its inverted span straddles a's interior.
		{"inverted inside", box(0.6, 0.6, 0.6, 0.4, 0.4, 0.4), false},
		{"inverted X only", box(0.6, 0, 0, 0.4, 1, 1), false},
		{"flat at face", box(1, 0, 0, 1, 1, 1), false},
	}
	for _, tc := range tests {
		if got := a.Overlaps(tc.b); got != tc.want {
			t.Errorf("a.Overlaps(%s) = %v, want %v", tc.name, got, tc.want)
		}
		if got := tc.b.Overlaps(a); got != tc.want {
			t.Errorf("%s.Overlaps(a) = %v, want %v (not symmetric)", tc.name, got, tc.want)
		}
	}
}

func TestAABBContains(t *testing.T) {
	b := box(-1, 0, 2, 1, 3, 4)
	tests := []struct {
		p    Vec3
		want bool
	}{
		{V3(0, 1, 3), true},
		{V3(-1, 0, 2), true}, // min corner, boundary included
		{V3(1, 3, 4), true},  // max corner
		{V3(1, 1.5, 3), true},
		{V3(1.0001, 1, 3), false},
		{V3(0, -0.0001, 3), false},
		{V3(0, 1, 4.5), false},
		{V3(0, 1, 1.9), false},
		{V3(nan32, 1, 3), false},
	}
	for _, tc := range tests {
		if got := b.Contains(tc.p); got != tc.want {
			t.Errorf("Contains(%v) = %v, want %v", tc.p, got, tc.want)
		}
	}
	boxes := []struct {
		o    AABB
		want bool
	}{
		{b, true},
		{box(-0.5, 1, 2.5, 0.5, 2, 3.5), true},
		{box(-1, 0, 2, 0, 0, 2), true},
		{box(-2, 1, 3, 0, 2, 3.5), false},
		{box(0, 1, 3, 0, 2, 5), false},
	}
	for _, tc := range boxes {
		if got := b.ContainsBox(tc.o); got != tc.want {
			t.Errorf("ContainsBox(%v) = %v, want %v", tc.o, got, tc.want)
		}
	}
}

func TestAABBCenterSizeTranslate(t *testing.T) {
	b := box(-1, 0, 2, 3, 6, 3)
	if got := b.Center(); got != V3(1, 3, 2.5) {
		t.Errorf("Center = %v", got)
	}
	if got := b.Size(); got != V3(4, 6, 1) {
		t.Errorf("Size = %v", got)
	}
	if got := b.Translate(V3(1, -1, 0.5)); got != box(0, -1, 2.5, 4, 5, 3.5) {
		t.Errorf("Translate = %v", got)
	}
}

func TestAABBCorners(t *testing.T) {
	b := box(0, 0, 0, 1, 2, 3)
	want := [8]Vec3{
		V3(0, 0, 0), V3(1, 0, 0), V3(0, 2, 0), V3(1, 2, 0),
		V3(0, 0, 3), V3(1, 0, 3), V3(0, 2, 3), V3(1, 2, 3),
	}
	got := b.Corners()
	if got != want {
		t.Fatalf("Corners =\n%v\nwant\n%v", got, want)
	}
	for i, c := range got {
		if (c.X == b.Max.X) != (i&1 != 0) || (c.Y == b.Max.Y) != (i&2 != 0) || (c.Z == b.Max.Z) != (i&4 != 0) {
			t.Errorf("corner %d = %v does not follow bit order", i, c)
		}
	}
	acc := EmptyAABB()
	for _, c := range got {
		acc = acc.Extend(c)
	}
	if acc != b {
		t.Errorf("box of corners = %v, want %v", acc, b)
	}
}

func TestAABBTransform(t *testing.T) {
	b := box(-1, 0, -2, 3, 1, 5)
	if got := b.Transform(Ident4()); got != b {
		t.Errorf("Transform(I) = %v", got)
	}
	tr := V3(4, -5, 0.5)
	if got := b.Transform(Translate(tr)); got != b.Translate(tr) {
		t.Errorf("Transform(T) = %v, want %v", got, b.Translate(tr))
	}
	if got := b.Transform(Scaling(V3(-1, 2, 1))); got != box(-3, 0, -2, 1, 2, 5) {
		t.Errorf("Transform(mirror X, scale Y) = %v", got)
	}
	// RotateY(+90°) maps +X to -Z and +Z to +X.
	if got := box(0, 0, 0, 2, 1, 1).Transform(RotateY(Pi / 2)); !approxV3(got.Min, V3(0, 0, -2), 1e-6) ||
		!approxV3(got.Max, V3(1, 1, 0), 1e-6) {
		t.Errorf("Transform(RotateY 90) = %v", got)
	}

	// Brute force: the enclosing box of the 8 transformed corners. The match must be
	// exact, not approximate: a box that misses a transformed corner by one ulp breaks
	// conservative culling and the "touching faces do not overlap" rule. (Summing in a
	// different order than MulPoint missed ~14% of corners by an ulp.)
	r := newRNG(61)
	for i := 0; i < 20000; i++ {
		lo := r.vec3(-100, 100)
		size := r.vec3(0, 50)
		if i%10 == 0 {
			size = size.With(i%3, 0) // degenerate (flat) boxes too
		}
		b := AABB{lo, lo.Add(size)}
		m := r.trs()
		m = m.Mul(Translate(r.vec3(-100, 100)))
		want := EmptyAABB()
		for _, c := range b.Corners() {
			want = want.Extend(m.MulPoint(c))
		}
		got := b.Transform(m)
		if got != want {
			t.Fatalf("case %d: Transform = %v, brute force %v\nm=%v", i, got, want, m)
		}
		for _, c := range b.Corners() {
			if p := m.MulPoint(c); !got.Contains(p) {
				t.Fatalf("case %d: Transform = %v misses transformed corner %v", i, got, p)
			}
		}
	}
}

func TestAABBJSON(t *testing.T) {
	b := box(-1, 0, 0.5, 1, 2, 3)
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `[[-1,0,0.5],[1,2,3]]` {
		t.Errorf("Marshal = %s", data)
	}
	wrapped, err := json.Marshal(struct {
		B AABB `json:"bounds"`
	}{b})
	if err != nil || string(wrapped) != `{"bounds":[[-1,0,0.5],[1,2,3]]}` {
		t.Errorf("Marshal in struct = %s, %v", wrapped, err)
	}
	var got AABB
	if err := json.Unmarshal([]byte(`[[-100, -50, -100], [100, 100, 100]]`), &got); err != nil {
		t.Fatal(err)
	}
	if got != box(-100, -50, -100, 100, 100, 100) {
		t.Errorf("Unmarshal = %v", got)
	}
	r := newRNG(67)
	for i := 0; i < 200; i++ {
		in := AABB{r.vec3(-1e5, 1e5), r.vec3(-1e-3, 1e-3)}
		var out AABB
		roundTrip(t, in, &out)
		if out != in {
			t.Fatalf("round trip %v -> %v", in, out)
		}
	}
	if _, err := json.Marshal(EmptyAABB()); err == nil {
		t.Error("Marshal(EmptyAABB) should fail: JSON has no infinities")
	}

	bad := []string{
		`[]`, `[[0,0,0]]`, `[[0,0,0],[1,1,1],[2,2,2]]`, `[[0,0],[1,1,1]]`, `[[0,0,0],[1,1,1,1]]`,
		`[[0,0,0],[1,null,1]]`, `[null,[1,1,1]]`, `[0,0,0,1,1,1]`, `{}`, `{"Min":[0,0,0],"Max":[1,1,1]}`,
		`"box"`, `1`, `null`, `[[0,0,0],"x"]`,
	}
	for _, in := range bad {
		out := box(7, 7, 7, 8, 8, 8)
		err := out.UnmarshalJSON([]byte(in))
		if err == nil {
			t.Errorf("UnmarshalJSON(%s) accepted invalid input: %v", in, out)
			continue
		}
		if !strings.HasPrefix(err.Error(), "aabb: ") {
			t.Errorf("error %q lacks the aabb context prefix", err)
		}
		if out != box(7, 7, 7, 8, 8, 8) {
			t.Errorf("UnmarshalJSON(%s) modified the value on error: %v", in, out)
		}
	}
}

func TestRectBasics(t *testing.T) {
	r := R(10, 20, 30, 40)
	if r != (Rect{V2(10, 20), V2(40, 60)}) {
		t.Fatalf("R = %v", r)
	}
	if r.W() != 30 || r.H() != 40 || r.Size() != V2(30, 40) {
		t.Errorf("W/H/Size = %v %v %v", r.W(), r.H(), r.Size())
	}
	empties := []struct {
		name  string
		r     Rect
		empty bool
	}{
		{"normal", r, false},
		{"zeroWidth", R(0, 0, 0, 5), true},
		{"zeroHeight", R(0, 0, 5, 0), true},
		{"negative", R(0, 0, -1, 5), true},
		{"zero", Rect{}, true},
		{"tiny", R(0, 0, 1e-6, 1e-6), false},
	}
	for _, tc := range empties {
		if got := tc.r.IsEmpty(); got != tc.empty {
			t.Errorf("%s.IsEmpty() = %v, want %v", tc.name, got, tc.empty)
		}
	}
}

func TestRectContains(t *testing.T) {
	r := R(0, 0, 10, 5)
	tests := []struct {
		p    Vec2
		want bool
	}{
		{V2(0, 0), true}, // Min is inside
		{V2(5, 2.5), true},
		{V2(9.999, 4.999), true},
		{V2(10, 0), false}, // Max is outside (half-open)
		{V2(0, 5), false},
		{V2(10, 5), false},
		{V2(-0.001, 1), false},
		{V2(1, -0.001), false},
		{V2(nan32, 1), false},
	}
	for _, tc := range tests {
		if got := r.Contains(tc.p); got != tc.want {
			t.Errorf("Contains(%v) = %v, want %v", tc.p, got, tc.want)
		}
	}
	// Half-open rectangles tile the plane: each pixel centre of a split lies in exactly one.
	left, right := R(0, 0, 4, 4), R(4, 0, 4, 4)
	for x := float32(0); x < 8; x += 0.5 {
		p := V2(x, 1)
		if left.Contains(p) == right.Contains(p) {
			t.Errorf("point %v is in %v of the two adjacent rects", p, left.Contains(p))
		}
	}
	if (Rect{}).Contains(V2(0, 0)) {
		t.Error("empty rect contains its corner")
	}
}

func TestRectOverlaps(t *testing.T) {
	a := R(0, 0, 10, 10)
	tests := []struct {
		name string
		b    Rect
		want bool
	}{
		{"same", a, true},
		{"overlap", R(5, 5, 10, 10), true},
		{"inside", R(2, 2, 1, 1), true},
		{"around", R(-5, -5, 20, 20), true},
		{"touch right", R(10, 0, 5, 10), false},
		{"touch left", R(-5, 0, 5, 10), false},
		{"touch top", R(0, 10, 10, 5), false},
		{"touch bottom", R(0, -5, 10, 5), false},
		{"touch corner", R(10, 10, 5, 5), false},
		{"apart", R(20, 20, 1, 1), false},
		{"x only", R(5, 20, 1, 1), false},
		// Empty rectangles have no interior (and Intersect with them is empty).
		{"zero width inside", R(5, 0, 0, 10), false},
		{"zero height inside", R(0, 5, 10, 0), false},
		{"inverted inside", Rect{V2(8, 8), V2(2, 2)}, false},
	}
	for _, tc := range tests {
		if got := a.Overlaps(tc.b); got != tc.want {
			t.Errorf("Overlaps(%s) = %v, want %v", tc.name, got, tc.want)
		}
		if got := tc.b.Overlaps(a); got != tc.want {
			t.Errorf("%s.Overlaps(a) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRectIntersectUnion(t *testing.T) {
	a := R(0, 0, 10, 10)
	inter := []struct {
		name  string
		b     Rect
		want  Rect
		empty bool
	}{
		{"overlap", R(5, -5, 10, 10), Rect{V2(5, 0), V2(10, 5)}, false},
		{"inside", R(2, 3, 4, 5), R(2, 3, 4, 5), false},
		{"around", R(-1, -1, 20, 20), a, false},
		{"self", a, a, false},
		{"touching", R(10, 0, 5, 5), Rect{V2(10, 0), V2(10, 5)}, true},
		{"disjoint", R(20, 20, 5, 5), Rect{V2(20, 20), V2(10, 10)}, true},
		{"zero width inside", R(5, 2, 0, 3), Rect{V2(5, 2), V2(5, 5)}, true},
	}
	for _, tc := range inter {
		got := a.Intersect(tc.b)
		if got != tc.want || got.IsEmpty() != tc.empty {
			t.Errorf("Intersect(%s) = %v (empty %v), want %v (empty %v)", tc.name, got, got.IsEmpty(), tc.want, tc.empty)
		}
		if back := tc.b.Intersect(a); back != got {
			t.Errorf("Intersect(%s) not commutative: %v vs %v", tc.name, got, back)
		}
		if got.IsEmpty() == a.Overlaps(tc.b) {
			t.Errorf("Intersect(%s) emptiness disagrees with Overlaps", tc.name)
		}
	}
	union := []struct {
		name string
		a, b Rect
		want Rect
	}{
		{"disjoint", a, R(20, -5, 5, 5), Rect{V2(0, -5), V2(25, 10)}},
		{"overlap", a, R(5, 5, 10, 10), R(0, 0, 15, 15)},
		{"inside", a, R(1, 1, 1, 1), a},
		{"withEmpty", a, R(100, 100, 0, 0), a},
		{"emptyWith", R(-100, -100, 0, 5), a, a},
		{"bothEmpty", Rect{}, R(5, 5, 0, 0), Rect{}},
	}
	for _, tc := range union {
		if got := tc.a.Union(tc.b); got != tc.want {
			t.Errorf("Union(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
