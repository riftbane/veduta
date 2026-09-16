package inspect

import (
	"testing"

	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

func testFrame() *Frame {
	const w, h = 8, 6
	f := &Frame{FrameHeader: FrameHeader{Width: w, Height: h, Scene: "main", Mode: "color",
		View: gmath.LookAt(gmath.V3(0, 0, 5), gmath.V3(0, 0, 0), gmath.Up),
		Proj: gmath.Perspective(gmath.Radians(60), float32(w)/h, 0.1, 100),
		Entities: []FrameEntity{
			{ID: 1, Name: "ground", Kind: "static", Projected: 20},
			{ID: 2, Name: "player", Kind: "player", Projected: 8},
			{ID: 3, Name: "hidden", Kind: "gem", Projected: 4},
		}},
		Color: make([]uint32, w*h), Depth: make([]float32, w*h), ID: make([]uint32, w*h), Normal: make([]uint32, w*h)}
	for i := range f.Depth {
		f.Depth[i] = 1
		f.Color[i] = 0xff202830
	}
	set := func(x, y int, id uint32) {
		i := y*w + x
		f.ID[i] = id
		f.Depth[i] = 0.9
		f.Normal[i] = 0xff80ff80 // +Y
		f.Color[i] = gfx.IDColor(id)
	}
	for x := 0; x < w; x++ {
		for y := 4; y < 6; y++ {
			set(x, y, 1)
		}
	}
	for x := 3; x < 5; x++ {
		for y := 1; y < 4; y++ {
			set(x, y, 2)
		}
	}
	return f
}

func TestFrameRoundTrip(t *testing.T) {
	f := testFrame()
	data, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	g, err := DecodeFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	if g.Width != f.Width || len(g.Normal) != len(f.Normal) || g.Entities[1].Name != "player" || g.View != f.View {
		t.Fatalf("round trip lost data: %+v", g.FrameHeader)
	}
	for i := range f.Color {
		if g.Color[i] != f.Color[i] || g.Depth[i] != f.Depth[i] || g.ID[i] != f.ID[i] {
			t.Fatalf("pixel %d differs", i)
		}
	}
	data[len(data)-10] ^= 0xff
	if _, err := DecodeFrame(data); err == nil {
		t.Fatal("corruption not detected")
	}
	if _, err := DecodeFrame([]byte("VDA1")); err == nil {
		t.Fatal("bad magic accepted")
	}
}

func TestQueryAtAndCoverage(t *testing.T) {
	f := testFrame()
	p, err := f.At(3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.Entity == nil || p.Entity.Name != "player" || p.Depth == nil || p.Normal == nil || p.Normal.Y < 0.99 || p.Distance == nil || *p.Distance <= 0 {
		t.Fatalf("At = %+v", p)
	}
	if p, _ := f.At(0, 0); p.Entity != nil || p.Depth != nil {
		t.Fatalf("background At = %+v", p)
	}
	if _, err := f.At(8, 0); err == nil {
		t.Fatal("out of range accepted")
	}
	c := f.Coverage()
	if c.Background != 48-16-6 || len(c.Entities) != 3 {
		t.Fatalf("coverage %+v", c)
	}
	pl := c.Entities[1]
	if pl.Pixels != 6 || *pl.BBox != [4]int{3, 1, 5, 4} || pl.OcclusionRatio != 0.75 {
		t.Fatalf("player coverage %+v", pl)
	}
	if h := c.Entities[2]; h.Pixels != 0 || h.BBox != nil || h.OcclusionRatio != 0 {
		t.Fatalf("hidden coverage %+v", h)
	}
}

func TestDiff(t *testing.T) {
	a := gfx.NewImage(20, 10)
	a.Fill(0xff102030)
	b := a.Clone()
	r, s := Diff(a, b, 0)
	if r.ChangedPixels != 0 || r.BBox != nil || s.W > 640 {
		t.Fatalf("identical: %+v", r)
	}
	b.Set(3, 4, 0xff10ff30)
	b.Set(7, 8, 0xff102031)
	r, _ = Diff(a, b, 0)
	if r.ChangedPixels != 2 || *r.BBox != [4]int{3, 4, 8, 9} || r.MaxDelta != 0xdf {
		t.Fatalf("diff %+v", r)
	}
	r, _ = Diff(a, b, 2)
	if r.ChangedPixels != 1 {
		t.Fatalf("threshold ignored: %+v", r)
	}
	r, _ = Diff(a, gfx.NewImage(10, 10), 0)
	if !r.SizeMismatch || r.Width != 10 {
		t.Fatalf("size mismatch not reported: %+v", r)
	}
}
