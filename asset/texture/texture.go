// Package texture compiles texture sources (assets/textures/<name>.vtex, header
// "texture/1") into asset.Texture: a BGRA8 image produced by a layer program (solid
// fills, value noise, stripes, rectangles, circles, gradients, checkers and PNG images
// composited bottom to top with a blend mode and an opacity), with its mip chain. The
// format is documented in docs/texture.md.
//
// Compilation is deterministic: the same source and the same image files always produce
// the same bytes on a given GOOS/GOARCH. Every layer is evaluated with fixed sample
// patterns and pure arithmetic (no maps, no clock, no randomness beyond the seeded noise
// hash); pixels are independent, so the row-parallel renderer cannot change the result.
package texture

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// Limits and defaults of the texture format (docs/texture.md).
const (
	MaxSize        = 4096 // texture width and height, pixels
	MaxLayers      = 64   // layers per texture
	MaxImageSize   = 8192 // width and height of a PNG read by an image layer
	DefaultScale   = 8    // noise lattice cells across the width
	MaxScale       = 4096 // noise scale upper bound
	DefaultOctaves = 1    // noise octaves
	MaxOctaves     = 8    // noise octaves upper bound
	MaxCells       = 4096 // checker cells per side
	MaxColors      = 64   // stripes colors
)

// Options control compilation.
type Options struct {
	// FS is the project's assets directory; image layers read their PNG files from it.
	// When nil, any image layer is an error.
	FS fs.FS
	// Skip lists layer indices to leave out of the rendering (used by inspection to find
	// layers with no visible effect). Skipped layers are still validated. An index outside
	// the layer list is an error.
	Skip []int
}

var (
	layerTypes = []string{"solid", "noise", "stripes", "rect", "circle", "gradient", "checker", "image"}
	blendNames = []string{"normal", "multiply", "screen", "add"}
	fitNames   = []string{"contain", "cover", "stretch"}
)

// layerFields lists the type-specific JSON fields each layer type accepts. Every type
// also accepts the common fields "type", "opacity" and "blend".
var layerFields = map[string][]string{
	"solid":    {"color"},
	"noise":    {"seed", "scale", "octaves", "color"},
	"stripes":  {"width", "angle_deg", "colors"},
	"rect":     {"xy", "size", "color", "corner", "outline"},
	"circle":   {"center", "radius", "color", "outline"},
	"gradient": {"from", "to", "angle_deg"},
	"checker":  {"cells", "colors"},
	"image":    {"path", "fit"},
}

// Parse decodes and compiles the texture source file (for example
// "assets/textures/crate_wood.vtex"). The texture name is the file name without its
// ".vtex" suffix and must be a valid asset name. Errors are located
// *asset.SourceError values (one from decoding) or an asset.Errors list with every
// validation problem.
func Parse(file string, data []byte, opt Options) (*asset.Texture, error) {
	var src asset.TextureSource
	loc, err := asset.Decode(file, data, asset.TypeTexture, &src)
	if err != nil {
		return nil, err
	}
	base := path.Base(filepath.ToSlash(file))
	name, ok := asset.KindTexture.NameFromFile(base)
	if !ok {
		return nil, asset.Errors{&asset.SourceError{File: loc.File(), Msg: fmt.Sprintf("file name %q must end in %q with a non-empty name", base, asset.KindTexture.Ext())}}
	}
	if err := asset.ValidName(name); err != nil {
		return nil, asset.Errors{&asset.SourceError{File: loc.File(), Msg: fmt.Sprintf("file name: texture %v", err)}}
	}
	return Compile(name, &src, loc, opt)
}

// Compile validates src and renders the texture called name. Every source problem is
// reported, located through loc (nil reports positions as unknown, for sources built in
// code), in one asset.Errors value; no texture is returned when there is any. The
// "veduta" header is checked by asset.Decode, not here. An invalid opt.Skip index is
// reported as a plain error after the source validated.
func Compile(name string, src *asset.TextureSource, loc *asset.Locator, opt Options) (*asset.Texture, error) {
	if src == nil {
		return nil, errors.New("texture: nil source")
	}
	if loc == nil {
		loc, _ = asset.NewLocator("", nil)
	}
	c := asset.NewChecker(loc)
	s := validate(c, name, src, opt)
	if err := c.Err(); err != nil {
		return nil, err
	}
	skip := make([]bool, len(s.layers))
	for _, i := range opt.Skip {
		if i < 0 || i >= len(s.layers) {
			return nil, fmt.Errorf("texture %s: skip: layer index %d out of range [0, %d]", name, i, len(s.layers)-1)
		}
		skip[i] = true
	}
	base := render(s, skip)
	levels := []*gfx.Image{base}
	if s.mipmaps {
		levels = gfx.BuildMips(base)
	}
	wrap := gfx.WrapClamp
	if s.tiling {
		wrap = gfx.WrapRepeat
	}
	return &asset.Texture{
		Name:   name,
		Data:   gfx.TextureData{Levels: levels, Wrap: wrap},
		Tiling: s.tiling,
		Layers: len(src.Layers),
	}, nil
}

// Deps returns the image files (paths relative to the assets directory) that src reads:
// the paths of its image layers, sorted and without duplicates. Invalid paths are
// skipped, never reported: Deps works on sources that do not compile, so that cooking can
// hash what exists.
func Deps(src *asset.TextureSource) []string {
	if src == nil {
		return nil
	}
	var out []string
	for _, l := range src.Layers {
		if l.Type == "image" && checkImagePath(l.Path) == "" {
			out = append(out, l.Path)
		}
	}
	sort.Strings(out)
	n := 0
	for i, p := range out {
		if i == 0 || p != out[n-1] {
			out[n] = p
			n++
		}
	}
	return out[:n]
}

// spec is a validated texture source with every default resolved.
type spec struct {
	w, h    int
	tiling  bool
	mipmaps bool
	layers  []layerSpec
}

// rgba is a straight-alpha color with components in [0, 1].
type rgba [4]float32

// layerSpec is a validated layer. Lengths are pixels, angles degrees.
type layerSpec struct {
	typ     string
	opacity float32
	blend   blendMode
	color   rgba   // solid, noise, rect, circle
	colors  []rgba // stripes, checker
	from    rgba   // gradient
	to      rgba   // gradient
	seed    int64  // noise
	scale   float64
	octaves int
	width   float64 // stripes band width
	angle   float64 // stripes, gradient
	xy      [2]float64
	size    [2]float64
	corner  float64
	outline float64
	center  [2]float64
	radius  float64
	cells   int // checker
	fit     string
	img     *gfx.Image // image, decoded
}

func validate(c *asset.Checker, name string, src *asset.TextureSource, opt Options) *spec {
	if err := asset.ValidName(name); err != nil {
		c.Errorf("", "texture %v", err)
	}
	s := &spec{w: 1, h: 1, tiling: src.Tiling, mipmaps: src.Mipmaps == nil || *src.Mipmaps}
	switch {
	case src.Size == nil:
		c.Errorf("size", "is required ([width, height] in pixels)")
	case len(src.Size) != 2:
		c.Errorf("size", "want 2 integers [width, height], got %d", len(src.Size))
	default:
		ok := true
		for i, v := range src.Size {
			if v < 1 || v > MaxSize {
				c.Errorf(asset.Path("size", i), "%d out of range [1, %d]", v, MaxSize)
				ok = false
			}
		}
		if ok {
			s.w, s.h = src.Size[0], src.Size[1]
		}
	}
	switch {
	case len(src.Layers) == 0:
		c.Errorf("layers", "at least one layer is required")
	case len(src.Layers) > MaxLayers:
		c.Errorf("layers", "%d layers, want at most %d", len(src.Layers), MaxLayers)
	}
	s.layers = make([]layerSpec, len(src.Layers))
	for i := range src.Layers {
		s.layers[i] = validateLayer(c, i, &src.Layers[i], opt)
	}
	return s
}

// validateLayer checks layer i and loads its image, if any.
func validateLayer(c *asset.Checker, i int, l *asset.LayerSource, opt Options) layerSpec {
	lp := asset.Path("layers", i)
	field := func(name string) string { return asset.Path(lp, name) }
	ls := layerSpec{typ: l.Type, opacity: 1}
	switch {
	case l.Type == "":
		c.Errorf(field("type"), "is required (one of %v)", layerTypes)
	case layerFields[l.Type] == nil:
		c.Errorf(field("type"), "unknown value %q (want one of %v)", l.Type, layerTypes)
	default:
		// Fields present in the source (or set in code) that this type does not use.
		has := func(name string, set bool) bool { return set || c.Loc.Has(field(name)) }
		set := map[string]bool{
			"color":     has("color", l.Color != ""),
			"colors":    has("colors", l.Colors != nil),
			"seed":      has("seed", l.Seed != nil),
			"scale":     has("scale", l.Scale != nil),
			"octaves":   has("octaves", l.Octaves != 0),
			"width":     has("width", l.Width != nil),
			"angle_deg": has("angle_deg", l.AngleDeg != nil),
			"xy":        has("xy", l.XY != nil),
			"size":      has("size", l.Size != nil),
			"corner":    has("corner", l.Corner != nil),
			"outline":   has("outline", l.Outline != nil),
			"center":    has("center", l.Center != nil),
			"radius":    has("radius", l.Radius != nil),
			"from":      has("from", l.From != ""),
			"to":        has("to", l.To != ""),
			"cells":     has("cells", l.Cells != 0),
			"path":      has("path", l.Path != ""),
			"fit":       has("fit", l.Fit != ""),
		}
		for _, f := range layerFields[l.Type] {
			delete(set, f)
		}
		c.Forbid(lp, "layer type "+l.Type, set)
		validateTyped(c, field, l, &ls, opt)
	}
	ls.opacity = c.Float(field("opacity"), l.Opacity, 0, 1, 1)
	switch c.Enum(field("blend"), l.Blend, blendNames, "normal") {
	case "multiply":
		ls.blend = blendMultiply
	case "screen":
		ls.blend = blendScreen
	case "add":
		ls.blend = blendAdd
	}
	return ls
}

// validateTyped checks the type-specific fields of a layer whose type is known.
func validateTyped(c *asset.Checker, field func(string) string, l *asset.LayerSource, ls *layerSpec, opt Options) {
	switch l.Type {
	case "solid":
		ls.color = requireColor(c, field("color"), l.Color)
	case "noise":
		if l.Seed != nil {
			ls.seed = *l.Seed
		}
		ls.scale = DefaultScale
		if l.Scale != nil {
			if v := *l.Scale; !gmath.IsFinite(v) || v <= 0 || v > MaxScale {
				c.Errorf(field("scale"), "%v out of range (0, %d]", v, MaxScale)
			} else {
				ls.scale = float64(v)
			}
		}
		ls.octaves = intField(c, field("octaves"), l.Octaves, 1, MaxOctaves, DefaultOctaves)
		ls.color = requireColor(c, field("color"), l.Color)
	case "stripes":
		ls.width = float64(c.RequirePositive(field("width"), l.Width))
		ls.angle = finite(c, field("angle_deg"), l.AngleDeg)
		ls.colors = colorList(c, field("colors"), l.Colors, 2, MaxColors)
	case "rect":
		ls.xy = requireVec2(c, field("xy"), l.XY)
		ls.size = positiveVec2(c, field("size"), l.Size)
		ls.color = requireColor(c, field("color"), l.Color)
		ls.corner = nonNegative(c, field("corner"), l.Corner)
		ls.outline = nonNegative(c, field("outline"), l.Outline)
	case "circle":
		ls.center = requireVec2(c, field("center"), l.Center)
		ls.radius = float64(c.RequirePositive(field("radius"), l.Radius))
		ls.color = requireColor(c, field("color"), l.Color)
		ls.outline = nonNegative(c, field("outline"), l.Outline)
	case "gradient":
		ls.from = requireColor(c, field("from"), l.From)
		ls.to = requireColor(c, field("to"), l.To)
		ls.angle = finite(c, field("angle_deg"), l.AngleDeg)
	case "checker":
		if l.Cells == 0 && !c.Loc.Has(field("cells")) {
			c.Errorf(field("cells"), "is required (cells per side, 1 to %d)", MaxCells)
		} else {
			ls.cells = intField(c, field("cells"), l.Cells, 1, MaxCells, 1)
		}
		ls.colors = colorList(c, field("colors"), l.Colors, 2, 2)
	case "image":
		ls.fit = c.Enum(field("fit"), l.Fit, fitNames, "contain")
		if msg := checkImagePath(l.Path); msg != "" {
			c.Errorf(field("path"), "%s", msg)
			return
		}
		if opt.FS == nil {
			c.Errorf(field("path"), "image layers need the assets directory (no filesystem was given)")
			return
		}
		img, err := loadImage(opt.FS, l.Path)
		if err != nil {
			c.Errorf(field("path"), "%v", err)
			return
		}
		ls.img = img
	}
}

// checkImagePath returns why p is not an acceptable image path, or "".
func checkImagePath(p string) string {
	switch {
	case p == "":
		return "is required (a .png file relative to the assets directory)"
	case strings.Contains(p, `\`):
		return fmt.Sprintf("%q: use forward slashes", p)
	case strings.HasPrefix(p, "/") || strings.Contains(p, ":"):
		return fmt.Sprintf("%q: must be relative to the assets directory", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Sprintf("%q: must not leave the assets directory (\"..\")", p)
		}
	}
	if !fs.ValidPath(p) {
		return fmt.Sprintf("%q: not a clean relative path (no \".\" or empty segments, no trailing \"/\")", p)
	}
	if !strings.EqualFold(path.Ext(p), ".png") {
		return fmt.Sprintf("%q: must be a .png file", p)
	}
	return ""
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

// requireColor validates a mandatory color.
func requireColor(c *asset.Checker, path, s string) rgba {
	if s == "" {
		if c.Loc.Has(path) {
			c.Errorf(path, "empty color (want #RRGGBB or #RRGGBBAA)")
		} else {
			c.Errorf(path, "is required (#RRGGBB or #RRGGBBAA)")
		}
		return rgba{}
	}
	return colorOf(c.Color(path, s, 0))
}

// colorList validates a mandatory list of lo..hi colors.
func colorList(c *asset.Checker, path string, v []string, lo, hi int) []rgba {
	switch {
	case v == nil:
		c.Errorf(path, "is required")
		return nil
	case len(v) < lo || len(v) > hi:
		if lo == hi {
			c.Errorf(path, "want exactly %d colors, got %d", lo, len(v))
		} else {
			c.Errorf(path, "want %d to %d colors, got %d", lo, hi, len(v))
		}
		return nil
	}
	out := make([]rgba, len(v))
	for i, s := range v {
		p := asset.Path(path, i)
		if s == "" {
			c.Errorf(p, "empty color (want #RRGGBB or #RRGGBBAA)")
			continue
		}
		out[i] = colorOf(c.Color(p, s, 0))
	}
	return out
}

// colorOf converts a packed 0xAARRGGBB color to components in [0, 1].
func colorOf(v uint32) rgba {
	r, g, b, a := gfx.UnpackRGBA(v)
	return rgba{float32(r) / 255, float32(g) / 255, float32(b) / 255, float32(a) / 255}
}

// finite validates an optional finite number (nil yields 0).
func finite(c *asset.Checker, path string, v *float32) float64 {
	if v == nil {
		return 0
	}
	if !gmath.IsFinite(*v) {
		c.Errorf(path, "not a finite number")
		return 0
	}
	return float64(*v)
}

// nonNegative validates an optional number >= 0 (nil yields 0).
func nonNegative(c *asset.Checker, path string, v *float32) float64 {
	if v == nil {
		return 0
	}
	if !gmath.IsFinite(*v) || *v < 0 {
		c.Errorf(path, "must be a number >= 0, got %v", *v)
		return 0
	}
	return float64(*v)
}

// requireVec2 validates a mandatory [x, y] of finite numbers.
func requireVec2(c *asset.Checker, path string, v []float32) [2]float64 {
	if v == nil {
		c.Errorf(path, "is required ([x, y] in pixels)")
		return [2]float64{}
	}
	p := c.Vec2(path, v, gmath.Vec2{})
	return [2]float64{float64(p.X), float64(p.Y)}
}

// positiveVec2 validates a mandatory [w, h] of positive finite numbers.
func positiveVec2(c *asset.Checker, path string, v []float32) [2]float64 {
	out := [2]float64{1, 1}
	switch {
	case v == nil:
		c.Errorf(path, "is required ([width, height] in pixels)")
		return out
	case len(v) != 2:
		c.Errorf(path, "want 2 numbers, got %d", len(v))
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
