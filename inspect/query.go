package inspect

import (
	"fmt"
	"math"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// PixelInfo answers "what is at this pixel" for a frame.
type PixelInfo struct {
	X        int         `json:"x"`
	Y        int         `json:"y"`
	Entity   *EntityRef  `json:"entity"`             // nil for background or geometry without an entity
	Color    string      `json:"color"`              // #rrggbb(aa)
	Depth    *float32    `json:"depth"`              // window depth in [0, 1), nil for background
	Distance *float32    `json:"distance,omitempty"` // eye-space distance along the view axis, in meters
	World    *gmath.Vec3 `json:"world,omitempty"`    // world-space position of the surface
	Normal   *gmath.Vec3 `json:"normal,omitempty"`   // world-space surface normal (bundles with normals)
}

// EntityRef names an entity.
type EntityRef struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"`
}

func (f *Frame) entity(id uint32) *EntityRef {
	if id == 0 {
		return nil
	}
	for _, e := range f.Entities {
		if e.ID == id {
			return &EntityRef{ID: id, Name: e.Name, Kind: e.Kind}
		}
	}
	return &EntityRef{ID: id}
}

// At returns what is visible at pixel (x, y).
func (f *Frame) At(x, y int) (PixelInfo, error) {
	if x < 0 || y < 0 || x >= f.Width || y >= f.Height {
		return PixelInfo{}, fmt.Errorf("pixel %d,%d outside the %dx%d frame", x, y, f.Width, f.Height)
	}
	i := y*f.Width + x
	p := PixelInfo{X: x, Y: y, Entity: f.entity(f.ID[i]), Color: gfx.FormatColor(f.Color[i])}
	d := f.Depth[i]
	if d < 1 {
		p.Depth = &d
		// Unproject the pixel center at depth d.
		inv, ok := f.Proj.Mul(f.View).Inverse()
		if ok {
			ndc := gmath.V4((float32(x)+0.5)/float32(f.Width)*2-1, 1-(float32(y)+0.5)/float32(f.Height)*2, d*2-1, 1)
			w := inv.MulVec4(ndc)
			if w.W != 0 {
				world := gmath.V3(w.X/w.W, w.Y/w.W, w.Z/w.W)
				p.World = &world
				eye := f.View.MulPoint(world)
				dist := -eye.Z
				p.Distance = &dist
			}
		}
	}
	if f.Normal != nil && f.Normal[i] != 0 {
		c := f.Normal[i]
		n := gmath.V3(float32(c>>16&0xff)/127.5-1, float32(c>>8&0xff)/127.5-1, float32(c&0xff)/127.5-1).Normalize()
		p.Normal = &n
	}
	return p, nil
}

// Coverage is the per-entity visibility of a frame.
type Coverage struct {
	Width      int              `json:"width"`
	Height     int              `json:"height"`
	Background int              `json:"background_pixels"` // pixels with no entity
	Entities   []EntityCoverage `json:"entities"`
}

// EntityCoverage is one entity's visibility. OcclusionRatio is visible pixels divided by
// projected pixels (1 = fully visible, 0 = fully hidden); it is 0 when the entity does
// not project onto the frame.
type EntityCoverage struct {
	ID             uint32  `json:"id"`
	Name           string  `json:"name"`
	Kind           string  `json:"kind,omitempty"`
	Pixels         int     `json:"pixels"`    // visible pixels in the ID buffer
	BBox           *[4]int `json:"bbox"`      // visible screen bounds x0, y0, x1, y1 (exclusive), nil when not visible
	Projected      int     `json:"projected"` // pixels covered when drawn alone
	OcclusionRatio float64 `json:"occlusion_ratio"`
	Fraction       float64 `json:"screen_fraction"` // visible pixels / frame pixels
}

// Coverage computes per-entity pixel counts, visible bounds and occlusion ratios, for
// every entity listed in the bundle (in id order), including invisible ones.
func (f *Frame) Coverage() Coverage {
	type acc struct {
		n              int
		x0, y0, x1, y1 int
	}
	counts := map[uint32]*acc{}
	bg := 0
	for y := 0; y < f.Height; y++ {
		for x := 0; x < f.Width; x++ {
			id := f.ID[y*f.Width+x]
			if id == 0 {
				bg++
				continue
			}
			a := counts[id]
			if a == nil {
				a = &acc{x0: x, y0: y, x1: x + 1, y1: y + 1}
				counts[id] = a
			}
			a.n++
			a.x0, a.y0 = min(a.x0, x), min(a.y0, y)
			a.x1, a.y1 = max(a.x1, x+1), max(a.y1, y+1)
		}
	}
	out := Coverage{Width: f.Width, Height: f.Height, Background: bg, Entities: []EntityCoverage{}}
	total := float64(f.Width * f.Height)
	for _, e := range f.Entities {
		c := EntityCoverage{ID: e.ID, Name: e.Name, Kind: e.Kind, Projected: e.Projected}
		if a := counts[e.ID]; a != nil {
			c.Pixels = a.n
			c.BBox = &[4]int{a.x0, a.y0, a.x1, a.y1}
		}
		if e.Projected > 0 {
			c.OcclusionRatio = round4(float64(c.Pixels) / float64(e.Projected))
		}
		c.Fraction = round4(float64(c.Pixels) / total)
		out.Entities = append(out.Entities, c)
	}
	return out
}

// round4 rounds to 4 decimals, half away from zero; non-finite values pass through.
func round4(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	return math.Round(x*10000) / 10000
}
