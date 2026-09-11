package gmath

import (
	"encoding/json"
	"fmt"
	"math"
)

// AABB is an axis-aligned bounding box. An empty box has Min > Max.
type AABB struct{ Min, Max Vec3 }

// EmptyAABB returns a box that contains nothing; Extend grows it.
func EmptyAABB() AABB {
	inf := float32(math.Inf(1))
	return AABB{Vec3{inf, inf, inf}, Vec3{-inf, -inf, -inf}}
}

// IsEmpty reports whether the box contains no point.
func (b AABB) IsEmpty() bool { return b.Min.X > b.Max.X || b.Min.Y > b.Max.Y || b.Min.Z > b.Max.Z }

// Extend returns the smallest box containing b and p.
func (b AABB) Extend(p Vec3) AABB { return AABB{b.Min.Min(p), b.Max.Max(p)} }

// Union returns the smallest box containing b and o.
func (b AABB) Union(o AABB) AABB {
	if o.IsEmpty() {
		return b
	}
	if b.IsEmpty() {
		return o
	}
	return AABB{b.Min.Min(o.Min), b.Max.Max(o.Max)}
}

// Overlaps reports whether the interiors of b and o intersect. Boxes that only touch
// along a face, edge or corner do not overlap. An empty (inverted) box overlaps nothing.
func (b AABB) Overlaps(o AABB) bool {
	return !b.IsEmpty() && !o.IsEmpty() &&
		b.Min.X < o.Max.X && b.Max.X > o.Min.X &&
		b.Min.Y < o.Max.Y && b.Max.Y > o.Min.Y &&
		b.Min.Z < o.Max.Z && b.Max.Z > o.Min.Z
}

// Contains reports whether p lies inside b (boundary included).
func (b AABB) Contains(p Vec3) bool {
	return p.X >= b.Min.X && p.X <= b.Max.X &&
		p.Y >= b.Min.Y && p.Y <= b.Max.Y &&
		p.Z >= b.Min.Z && p.Z <= b.Max.Z
}

// ContainsBox reports whether o lies entirely inside b.
func (b AABB) ContainsBox(o AABB) bool { return b.Contains(o.Min) && b.Contains(o.Max) }

// Center returns the midpoint.
func (b AABB) Center() Vec3 { return b.Min.Add(b.Max).Scale(0.5) }

// Size returns Max-Min.
func (b AABB) Size() Vec3 { return b.Max.Sub(b.Min) }

// Translate returns b moved by t.
func (b AABB) Translate(t Vec3) AABB { return AABB{b.Min.Add(t), b.Max.Add(t)} }

// Transform returns the axis-aligned box enclosing b transformed by m (affine).
//
// The terms are summed in the same order as MulPoint, so by the monotonicity of IEEE
// rounding the result is exactly the box of the eight transformed corners: it always
// contains m.MulPoint(p) for every p in b, never missing one by an ulp.
func (b AABB) Transform(m Mat4) AABB {
	if b.IsEmpty() {
		return b
	}
	var lo, hi Vec3
	for r := 0; r < 3; r++ {
		var l, h float32
		for c := 0; c < 3; c++ {
			e := m[c*4+r]
			x, y := float32(e*b.Min.Get(c)), float32(e*b.Max.Get(c))
			if x > y {
				x, y = y, x
			}
			if c == 0 {
				l, h = x, y
			} else {
				l, h = l+x, h+y
			}
		}
		lo = lo.With(r, l+m[12+r])
		hi = hi.With(r, h+m[12+r])
	}
	return AABB{lo, hi}
}

// Corners returns the eight corners in a fixed order (bit 0: X, bit 1: Y, bit 2: Z).
func (b AABB) Corners() [8]Vec3 {
	var c [8]Vec3
	for i := range c {
		p := b.Min
		if i&1 != 0 {
			p.X = b.Max.X
		}
		if i&2 != 0 {
			p.Y = b.Max.Y
		}
		if i&4 != 0 {
			p.Z = b.Max.Z
		}
		c[i] = p
	}
	return c
}

// MarshalJSON encodes the box as [[minx,miny,minz],[maxx,maxy,maxz]].
func (b AABB) MarshalJSON() ([]byte, error) { return json.Marshal([2]Vec3{b.Min, b.Max}) }

// UnmarshalJSON decodes [[minx,miny,minz],[maxx,maxy,maxz]].
func (b *AABB) UnmarshalJSON(data []byte) error {
	var v []Vec3
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("aabb: %w", err)
	}
	if len(v) != 2 {
		return fmt.Errorf("aabb: want [min, max], got %d vectors", len(v))
	}
	*b = AABB{v[0], v[1]}
	return nil
}

// Rect is an axis-aligned 2D rectangle [Min, Max).
type Rect struct{ Min, Max Vec2 }

// R returns the rectangle with corner (x, y) and size (w, h).
func R(x, y, w, h float32) Rect { return Rect{Vec2{x, y}, Vec2{x + w, y + h}} }

// W returns the width.
func (r Rect) W() float32 { return r.Max.X - r.Min.X }

// H returns the height.
func (r Rect) H() float32 { return r.Max.Y - r.Min.Y }

// Size returns the width and height.
func (r Rect) Size() Vec2 { return r.Max.Sub(r.Min) }

// IsEmpty reports whether the rectangle has no area.
func (r Rect) IsEmpty() bool { return r.Min.X >= r.Max.X || r.Min.Y >= r.Max.Y }

// Contains reports whether p lies in [Min, Max).
func (r Rect) Contains(p Vec2) bool {
	return p.X >= r.Min.X && p.X < r.Max.X && p.Y >= r.Min.Y && p.Y < r.Max.Y
}

// Overlaps reports whether the interiors intersect, i.e. whether Intersect is non-empty.
// An empty rectangle (zero or negative width or height) overlaps nothing.
func (r Rect) Overlaps(o Rect) bool {
	return !r.IsEmpty() && !o.IsEmpty() &&
		r.Min.X < o.Max.X && r.Max.X > o.Min.X && r.Min.Y < o.Max.Y && r.Max.Y > o.Min.Y
}

// Intersect returns the common area (possibly empty).
func (r Rect) Intersect(o Rect) Rect { return Rect{r.Min.Max(o.Min), r.Max.Min(o.Max)} }

// Union returns the smallest rectangle containing both.
func (r Rect) Union(o Rect) Rect {
	if o.IsEmpty() {
		return r
	}
	if r.IsEmpty() {
		return o
	}
	return Rect{r.Min.Min(o.Min), r.Max.Max(o.Max)}
}
