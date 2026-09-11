package asset

import (
	"fmt"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// Checker accumulates located validation errors while a source is compiled. Its helpers
// validate one value each and return a usable fallback on error, so compilation can keep
// going and report every problem at once.
type Checker struct {
	Loc  *Locator
	Errs Errors
}

// NewChecker returns a checker reporting positions through loc.
func NewChecker(loc *Locator) *Checker { return &Checker{Loc: loc} }

// Errorf records an error at path.
func (c *Checker) Errorf(path, format string, args ...any) {
	c.Errs = append(c.Errs, c.Loc.Errorf(path, format, args...))
}

// Err returns the accumulated errors, or nil.
func (c *Checker) Err() error { return c.Errs.Err() }

// OK reports whether no error has been recorded.
func (c *Checker) OK() bool { return len(c.Errs) == 0 }

// Vec3 validates an optional 3-vector (nil yields def).
func (c *Checker) Vec3(path string, v []float32, def gmath.Vec3) gmath.Vec3 {
	if v == nil {
		return def
	}
	if len(v) != 3 {
		c.Errorf(path, "want 3 numbers, got %d", len(v))
		return def
	}
	for i, x := range v {
		if !gmath.IsFinite(x) {
			c.Errorf(Path(path, i), "not a finite number")
			return def
		}
	}
	return gmath.V3(v[0], v[1], v[2])
}

// Vec2 validates an optional 2-vector (nil yields def).
func (c *Checker) Vec2(path string, v []float32, def gmath.Vec2) gmath.Vec2 {
	if v == nil {
		return def
	}
	if len(v) != 2 {
		c.Errorf(path, "want 2 numbers, got %d", len(v))
		return def
	}
	for i, x := range v {
		if !gmath.IsFinite(x) {
			c.Errorf(Path(path, i), "not a finite number")
			return def
		}
	}
	return gmath.V2(v[0], v[1])
}

// RequireVec3 is Vec3 for a mandatory field.
func (c *Checker) RequireVec3(path string, v []float32) gmath.Vec3 {
	if v == nil {
		c.Errorf(path, "is required")
		return gmath.Vec3{}
	}
	return c.Vec3(path, v, gmath.Vec3{})
}

// Color parses an optional "#RRGGBB" or "#RRGGBBAA" color ("" yields def).
func (c *Checker) Color(path, s string, def uint32) uint32 {
	if s == "" {
		return def
	}
	v, err := gfx.ParseColor(s)
	if err != nil {
		c.Errorf(path, "%v", err)
		return def
	}
	return v
}

// Enum validates an optional enumerated string ("" yields def).
func (c *Checker) Enum(path, s string, allowed []string, def string) string {
	if s == "" {
		return def
	}
	for _, a := range allowed {
		if s == a {
			return s
		}
	}
	c.Errorf(path, "unknown value %q (want one of %v)", s, allowed)
	return def
}

// Float validates an optional number in [lo, hi] (nil yields def).
func (c *Checker) Float(path string, v *float32, lo, hi, def float32) float32 {
	if v == nil {
		return def
	}
	if !gmath.IsFinite(*v) || *v < lo || *v > hi {
		c.Errorf(path, "%v out of range [%v, %v]", *v, lo, hi)
		return def
	}
	return *v
}

// Positive validates an optional number > 0 (nil yields def).
func (c *Checker) Positive(path string, v *float32, def float32) float32 {
	if v == nil {
		return def
	}
	if !gmath.IsFinite(*v) || *v <= 0 {
		c.Errorf(path, "must be a positive number, got %v", *v)
		return def
	}
	return *v
}

// RequirePositive is Positive for a mandatory field.
func (c *Checker) RequirePositive(path string, v *float32) float32 {
	if v == nil {
		c.Errorf(path, "is required")
		return 1
	}
	return c.Positive(path, v, 1)
}

// Int validates an integer in [lo, hi]; zero means "absent" and yields def.
func (c *Checker) Int(path string, v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	if v < lo || v > hi {
		c.Errorf(path, "%d out of range [%d, %d]", v, lo, hi)
		return def
	}
	return v
}

// Forbid records an error for each field that is set but not allowed in this context
// (for example "radius" on a box). fields maps JSON names to "is set".
func (c *Checker) Forbid(path, context string, fields map[string]bool) {
	names := make([]string, 0, len(fields))
	for n, set := range fields {
		if set {
			names = append(names, n)
		}
	}
	sortStrings(names)
	for _, n := range names {
		c.Errorf(Path(path, n), "not used by %s", context)
	}
}

// Name validates an asset or entity name: 1–64 characters of [a-z0-9_-], starting with a
// letter or digit.
func (c *Checker) Name(path, s string) bool {
	if err := ValidName(s); err != nil {
		c.Errorf(path, "%v", err)
		return false
	}
	return true
}

// ValidName reports why s is not a valid asset name (see Checker.Name).
func ValidName(s string) error {
	if len(s) == 0 || len(s) > 64 {
		return fmt.Errorf("name %q must be 1-64 characters", s)
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '_' || r == '-') && i > 0:
		default:
			return fmt.Errorf("name %q may only contain a-z, 0-9, '_' and '-' (not first)", s)
		}
	}
	return nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
