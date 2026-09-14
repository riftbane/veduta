// Package model compiles model sources (assets/models/<name>.model.json, header
// "model/1") into asset.Model: one indexed triangle mesh built as the union of primitive
// parts (box, cylinder, sphere, plane, extrude, lathe, and mirror copies of earlier
// parts), with smoothed normals, UVs in meters, one mesh part per source part and the
// model's pivot applied. The format is documented in docs/model.md.
//
// Compilation is deterministic: the same source always yields the same vertices,
// indices and floats, bit for bit, on a given GOOS/GOARCH. Setup math is done in float64
// (with gmath's deterministic trigonometry) and rounded once to float32 for storage.
package model

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gmath"
)

// Defaults and limits of the model format (docs/model.md).
const (
	DefaultSmoothAngleDeg = 30        // smooth_angle_deg
	DefaultTriangleBudget = 20000     // triangle_budget
	MaxTriangleBudget     = 1_000_000 // triangle_budget upper bound
	DefaultSegments       = 16        // cylinder, sphere and lathe segments
	MinSegments           = 3
	MaxSegments           = 256
	DefaultRings          = 8 // sphere rings
	MinRings              = 2
	MaxRings              = 128
	MaxProfilePoints      = 1024 // extrude and lathe profile length
)

var (
	shapes  = []string{"box", "cylinder", "sphere", "plane", "extrude", "lathe", "mirror"}
	pivots  = []string{"origin", "center", "bottom-center"}
	uvModes = []string{"box", "planar", "cylindrical", "spherical"}
	axes    = []string{"x", "y", "z"}
)

// defaultUV is the uv mode of each shape when "uv" is absent.
var defaultUV = map[string]string{
	"box": "box", "plane": "planar", "cylinder": "cylindrical", "sphere": "spherical",
	"extrude": "box", "lathe": "cylindrical",
}

// shapeFields lists the shape-specific JSON fields each shape accepts. Every shape
// except mirror also accepts the common fields in commonFields; mirror accepts
// "material" besides its own fields.
var shapeFields = map[string][]string{
	"box":      {"size"},
	"cylinder": {"radius", "height", "segments"},
	"sphere":   {"radius", "segments", "rings"},
	"plane":    {"size"},
	"extrude":  {"profile", "depth"},
	"lathe":    {"profile", "segments"},
	"mirror":   {"axis", "of", "material"},
}

var commonFields = []string{"position", "rotation_deg", "scale", "material", "uv", "flip_normals"}

// Parse decodes and compiles the model source file (for example
// "assets/models/crate.model.json"). The model name is the file name without its
// ".model.json" suffix and must be a valid asset name; a "name" field in the source, when
// present, must equal it. Errors are located *asset.SourceError values (one from
// decoding) or an asset.Errors list with every validation problem.
func Parse(file string, data []byte) (*asset.Model, error) {
	var src asset.ModelSource
	loc, err := asset.Decode(file, data, asset.TypeModel, &src)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(file)
	name, ok := asset.KindModel.NameFromFile(base)
	if !ok {
		return nil, asset.Errors{&asset.SourceError{File: loc.File(), Msg: fmt.Sprintf("file name %q must end in %q with a non-empty name", base, asset.KindModel.Ext())}}
	}
	return Compile(name, &src, loc)
}

// Compile validates src and builds the model called name. Every problem is reported,
// located through loc (nil reports positions as unknown, for sources built in code), in
// one asset.Errors value; no model is returned when there is any. The "veduta" header is
// checked by asset.Decode, not here.
func Compile(name string, src *asset.ModelSource, loc *asset.Locator) (*asset.Model, error) {
	if src == nil {
		return nil, errors.New("model: nil source")
	}
	if loc == nil {
		loc, _ = asset.NewLocator("", nil)
	}
	c := asset.NewChecker(loc)
	s := validate(c, name, src)
	if err := c.Err(); err != nil {
		return nil, err
	}
	m := build(name, s)
	// Sources are only checked to be finite, so sizes, positions and scales can still
	// multiply past the float32 range. Such geometry is refused: a NaN made from
	// infinities carries a sign that differs between amd64 and arm64, so its cooked bytes
	// would too.
	for _, i := range nonFiniteParts(m) {
		c.Errorf(asset.Path("parts", i), "geometry exceeds the float32 range (a vertex is not finite after size, position, rotation and scale are applied)")
	}
	if c.Err() == nil && !(m.PivotOffset.IsFinite() && m.Mesh.Bounds.Min.IsFinite() && m.Mesh.Bounds.Max.IsFinite()) {
		c.Errorf("pivot", "the model is too large to move to its pivot within the float32 range")
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// spec is a validated model source with every default resolved.
type spec struct {
	pivot     string
	smoothDeg float32
	symmetry  string
	budget    int
	parts     []partSpec
}

// partSpec is a validated part. Lengths are meters, angles degrees.
type partSpec struct {
	shape    string
	size     dvec // box [x,y,z]; plane [x,0,z]
	radius   float64
	height   float64
	depth    float64
	segments int
	rings    int
	profile  []dvec2
	axis     int // mirror: 0, 1, 2 for x, y, z
	of       int // mirror: source part, else -1
	pos      dvec
	rot      dvec
	scale    dvec
	material string
	uv       string
	flip     bool
}

func validate(c *asset.Checker, name string, src *asset.ModelSource) *spec {
	if err := asset.ValidName(name); err != nil {
		c.Errorf("", "model %v", err)
	}
	if src.Name != "" && src.Name != name {
		c.Errorf("name", "%q does not match the file name (want %q)", src.Name, name)
	}
	c.Enum("units", src.Units, []string{"m"}, "m")
	s := &spec{
		pivot:     c.Enum("pivot", src.Pivot, pivots, "origin"),
		smoothDeg: c.Float("smooth_angle_deg", src.SmoothAngleDeg, 0, 180, DefaultSmoothAngleDeg),
		symmetry:  c.Enum("symmetry", src.Symmetry, axes, ""),
		budget:    intField(c, "triangle_budget", src.TriangleBudget, 1, MaxTriangleBudget, DefaultTriangleBudget),
	}
	if len(src.Parts) == 0 {
		c.Errorf("parts", "at least one part is required")
	}
	s.parts = make([]partSpec, len(src.Parts))
	for i := range src.Parts {
		s.parts[i] = validatePart(c, i, &src.Parts[i], s.parts[:i])
	}
	return s
}

// validatePart checks part i; prev holds the already validated earlier parts.
func validatePart(c *asset.Checker, i int, p *asset.PartSource, prev []partSpec) partSpec {
	pp := asset.Path("parts", i)
	ps := partSpec{shape: p.Shape, of: -1, scale: dvec{1, 1, 1}}
	switch {
	case p.Shape == "":
		c.Errorf(asset.Path(pp, "shape"), "is required (one of %v)", shapes)
		return ps
	case shapeFields[p.Shape] == nil:
		c.Errorf(asset.Path(pp, "shape"), "unknown value %q (want one of %v)", p.Shape, shapes)
		return ps
	}

	// Fields present in the source (or set in code) that this shape does not use.
	has := func(field string, set bool) bool { return set || c.Loc.Has(asset.Path(pp, field)) }
	set := map[string]bool{
		"size":         has("size", p.Size != nil),
		"radius":       has("radius", p.Radius != nil),
		"height":       has("height", p.Height != nil),
		"segments":     has("segments", p.Segments != 0),
		"rings":        has("rings", p.Rings != 0),
		"profile":      has("profile", p.Profile != nil),
		"depth":        has("depth", p.Depth != nil),
		"axis":         has("axis", p.Axis != ""),
		"of":           has("of", p.Of != nil),
		"position":     has("position", p.Position != nil),
		"rotation_deg": has("rotation_deg", p.RotationDeg != nil),
		"scale":        has("scale", p.Scale != nil),
		"material":     has("material", p.Material != ""),
		"uv":           has("uv", p.UV != ""),
		"flip_normals": has("flip_normals", p.FlipNormals),
	}
	for _, f := range shapeFields[p.Shape] {
		delete(set, f)
	}
	if p.Shape != "mirror" {
		for _, f := range commonFields {
			delete(set, f)
		}
	}
	c.Forbid(pp, "shape "+p.Shape, set)

	field := func(name string) string { return asset.Path(pp, name) }
	switch p.Shape {
	case "box":
		ps.size = positiveVec(c, field("size"), p.Size, 3)
	case "plane":
		v := positiveVec(c, field("size"), p.Size, 2)
		ps.size = dvec{v[0], 0, v[1]}
	case "cylinder":
		ps.radius = float64(c.RequirePositive(field("radius"), p.Radius))
		ps.height = float64(c.RequirePositive(field("height"), p.Height))
		ps.segments = intField(c, field("segments"), p.Segments, MinSegments, MaxSegments, DefaultSegments)
	case "sphere":
		ps.radius = float64(c.RequirePositive(field("radius"), p.Radius))
		ps.segments = intField(c, field("segments"), p.Segments, MinSegments, MaxSegments, DefaultSegments)
		ps.rings = intField(c, field("rings"), p.Rings, MinRings, MaxRings, DefaultRings)
	case "extrude":
		ps.profile = extrudeProfile(c, field("profile"), p.Profile)
		ps.depth = float64(c.RequirePositive(field("depth"), p.Depth))
	case "lathe":
		ps.profile = latheProfile(c, field("profile"), p.Profile)
		ps.segments = intField(c, field("segments"), p.Segments, MinSegments, MaxSegments, DefaultSegments)
	case "mirror":
		validateMirror(c, pp, i, p, prev, &ps)
		return ps
	}

	ps.pos = dvec3(c.Vec3(field("position"), p.Position, gmath.Zero3))
	ps.rot = dvec3(c.Vec3(field("rotation_deg"), p.RotationDeg, gmath.Zero3))
	ps.scale = dvec3(c.Vec3(field("scale"), p.Scale, gmath.One3))
	if len(p.Scale) == 3 {
		for k, v := range p.Scale {
			if v == 0 {
				c.Errorf(asset.Path(field("scale"), k), "must be non-zero")
				ps.scale[k] = 1
			}
		}
	}
	if p.Material != "" && c.Name(field("material"), p.Material) {
		ps.material = p.Material
	}
	ps.uv = c.Enum(field("uv"), p.UV, uvModes, defaultUV[p.Shape])
	ps.flip = p.FlipNormals
	return ps
}

// validateMirror checks the fields of mirror part i and inherits material, uv and
// flip_normals from the mirrored part.
func validateMirror(c *asset.Checker, pp string, i int, p *asset.PartSource, prev []partSpec, ps *partSpec) {
	if p.Axis == "" {
		c.Errorf(asset.Path(pp, "axis"), "is required (one of %v)", axes)
	} else {
		switch c.Enum(asset.Path(pp, "axis"), p.Axis, axes, "") {
		case "x":
			ps.axis = 0
		case "y":
			ps.axis = 1
		case "z":
			ps.axis = 2
		}
	}
	switch {
	case p.Of == nil:
		c.Errorf(asset.Path(pp, "of"), "is required (the index of an earlier part)")
	case i == 0:
		c.Errorf(asset.Path(pp, "of"), "a mirror needs an earlier part to copy, and part 0 has none")
	case *p.Of < 0 || *p.Of >= i:
		c.Errorf(asset.Path(pp, "of"), "%d is not the index of an earlier part (want 0..%d)", *p.Of, i-1)
	default:
		src := prev[*p.Of]
		ps.of = *p.Of
		ps.material = src.material
		ps.uv = src.uv
		ps.flip = src.flip
	}
	if p.Material != "" && c.Name(asset.Path(pp, "material"), p.Material) {
		ps.material = p.Material
	}
}

// intField validates an optional integer in [lo, hi]. Unlike Checker.Int, an explicit 0
// in the source is an error rather than "absent".
func intField(c *asset.Checker, path string, v, lo, hi, def int) int {
	if v == 0 && c.Loc.Has(path) {
		c.Errorf(path, "0 out of range [%d, %d]", lo, hi)
		return def
	}
	return c.Int(path, v, lo, hi, def)
}

// positiveVec validates a required vector of n (2 or 3) positive finite numbers.
func positiveVec(c *asset.Checker, path string, v []float32, n int) dvec {
	out := dvec{1, 1, 1}
	switch {
	case v == nil:
		c.Errorf(path, "is required")
		return out
	case len(v) != n:
		c.Errorf(path, "want %d numbers, got %d", n, len(v))
		return out
	}
	for k, x := range v {
		if !gmath.IsFinite(x) || x <= 0 {
			c.Errorf(asset.Path(path, k), "must be a positive number, got %v", x)
			continue
		}
		out[k] = float64(x)
	}
	return out
}

// profilePoints validates the common shape of a profile: at least min points of two
// finite numbers each, at most MaxProfilePoints, and no point equal to the one before
// it (closing included when closed). It returns nil when the profile is unusable.
func profilePoints(c *asset.Checker, path string, prof [][]float32, min int, closed bool) []dvec2 {
	switch {
	case prof == nil:
		c.Errorf(path, "is required")
		return nil
	case len(prof) < min:
		c.Errorf(path, "want at least %d points, got %d", min, len(prof))
		return nil
	case len(prof) > MaxProfilePoints:
		c.Errorf(path, "want at most %d points, got %d", MaxProfilePoints, len(prof))
		return nil
	}
	pts := make([]dvec2, len(prof))
	ok := true
	for i, p := range prof {
		pi := asset.Path(path, i)
		if len(p) != 2 {
			c.Errorf(pi, "want 2 numbers, got %d", len(p))
			ok = false
			continue
		}
		for k, x := range p {
			if !gmath.IsFinite(x) {
				c.Errorf(asset.Path(pi, k), "not a finite number")
				ok = false
			}
		}
		pts[i] = dvec2{float64(p[0]), float64(p[1])}
	}
	if !ok {
		return nil
	}
	n := len(pts)
	for i := 1; i <= n; i++ {
		if i == n && !closed {
			break
		}
		if pts[i%n] == pts[i-1] {
			c.Errorf(asset.Path(path, i%n), "repeats point %d (%v, %v)", i-1, pts[i-1][0], pts[i-1][1])
			ok = false
		}
	}
	if !ok {
		return nil
	}
	return pts
}

// extrudeProfile validates an extrude profile (a simple polygon in the XY plane) and
// returns it counter-clockwise.
func extrudeProfile(c *asset.Checker, path string, prof [][]float32) []dvec2 {
	pts := profilePoints(c, path, prof, 3, true)
	if pts == nil {
		return nil
	}
	if i, j, bad := firstCrossing(pts); bad {
		c.Errorf(path, "not a simple polygon: edge %d-%d touches or crosses edge %d-%d", i, (i+1)%len(pts), j, (j+1)%len(pts))
		return nil
	}
	a := signedArea(pts)
	if a == 0 {
		c.Errorf(path, "encloses no area")
		return nil
	}
	if a < 0 {
		pts = reversed(pts)
	}
	return pts
}

// latheProfile validates a lathe profile ([r, y] points, r >= 0). A closed profile —
// both ends on the axis, or last point equal to the first — is returned in the order
// that makes its surface face outwards.
func latheProfile(c *asset.Checker, path string, prof [][]float32) []dvec2 {
	pts := profilePoints(c, path, prof, 2, false)
	if pts == nil {
		return nil
	}
	ok := true
	for i, p := range pts {
		if p[0] < 0 {
			c.Errorf(asset.Path(asset.Path(path, i), 0), "radius must be >= 0, got %v", p[0])
			ok = false
		}
	}
	if !ok {
		return nil
	}
	surface := false
	for i := 0; i+1 < len(pts); i++ {
		if pts[i][0] > 0 || pts[i+1][0] > 0 {
			surface = true
		}
	}
	if !surface {
		c.Errorf(path, "every point lies on the axis (r = 0): the profile makes no surface")
		return nil
	}
	if latheClosed(pts) && signedArea(pts) < 0 {
		pts = reversed(pts)
	}
	return pts
}

// latheClosed reports whether a lathe profile bounds a solid: both ends on the axis, or
// the last point equal to the first.
func latheClosed(pts []dvec2) bool {
	first, last := pts[0], pts[len(pts)-1]
	return (first[0] == 0 && last[0] == 0) || first == last
}

func reversed(pts []dvec2) []dvec2 {
	out := make([]dvec2, len(pts))
	for i, p := range pts {
		out[len(pts)-1-i] = p
	}
	return out
}
