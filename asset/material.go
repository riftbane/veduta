package asset

import (
	"fmt"
	"path/filepath"

	"github.com/riftbane/veduta/v2/gfx"
)

// Material enumerations, in the order their compiled values are numbered.
var (
	materialAlphas  = []string{"opaque", "blend", "cutout"}
	materialCulls   = []string{"back", "none"}
	materialFilters = []string{"bilinear", "nearest"}
)

// Material defaults (docs/material.md).
const (
	defaultAlbedo = 0xffffffff // #ffffff
	defaultCutoff = 0.5
)

// ParseMaterial decodes and compiles the material source file (for example
// "assets/materials/crate_wood.vmat"). The material name is the file name without
// its ".vmat" suffix and must be a valid asset name.
func ParseMaterial(file string, data []byte) (*Material, error) {
	var src MaterialSource
	loc, err := Decode(file, data, TypeMaterial, &src)
	if err != nil {
		return nil, err
	}
	name, err := nameFromFile(KindMaterial, file, loc)
	if err != nil {
		return nil, err
	}
	return CompileMaterial(name, &src, loc)
}

// CompileMaterial validates src and returns the compiled material called name. Every
// problem is reported, located through loc (nil reports positions as unknown), in one
// Errors value. The "veduta" header is checked by Decode, not here.
// MaxGrid is the most columns or rows of frames a material's grid may have.
const MaxGrid = 256

func CompileMaterial(name string, src *MaterialSource, loc *Locator) (*Material, error) {
	c := NewChecker(ensureLoc(loc))
	if err := ValidName(name); err != nil {
		c.Errorf("", "material %v", err)
	}
	m := &Material{
		Name:   name,
		Albedo: c.Color("albedo", src.Albedo, defaultAlbedo),
		Unlit:  src.Unlit,
		Alpha:  c.Enum("alpha", src.Alpha, materialAlphas, "opaque"),
		Cutoff: defaultCutoff,
	}
	if src.Texture != "" && c.Name("texture", src.Texture) {
		m.Texture = src.Texture
	}
	if src.Cutoff != nil {
		switch v := *src.Cutoff; {
		case src.Alpha != "cutout":
			c.Errorf("cutoff", "only allowed when alpha is \"cutout\"")
		case !(v > 0 && v <= 1):
			c.Errorf("cutoff", "%v out of range (0, 1]", v)
		default:
			m.Cutoff = v
		}
	}
	cull, _ := gfx.ParseCull(c.Enum("cull", src.Cull, materialCulls, "back"))
	m.Cull = cull
	filter, _ := gfx.ParseFilter(c.Enum("filter", src.Filter, materialFilters, "bilinear"))
	m.Filter = filter
	if src.Grid != nil {
		switch {
		case len(src.Grid) != 2:
			c.Errorf("grid", "must be [columns, rows]")
		case src.Texture == "":
			c.Errorf("grid", "needs a texture to cut into frames")
		default:
			for i, n := range src.Grid {
				if n < 1 || n > MaxGrid {
					c.Errorf(Path("grid", i), "%d out of range [1, %d]", n, MaxGrid)
				}
			}
			m.Grid = [2]int{src.Grid[0], src.Grid[1]}
		}
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// ensureLoc returns loc, or a locator over no data (every position 1:1) when loc is nil,
// so the Compile functions accept sources built in code.
func ensureLoc(loc *Locator) *Locator {
	if loc != nil {
		return loc
	}
	l, _ := NewLocator("", nil)
	return l
}

// nameFromFile derives the asset name of kind k from the base name of file and
// validates it, returning a SourceError for file when the suffix or the name is wrong.
func nameFromFile(k Kind, file string, loc *Locator) (string, error) {
	base := filepath.Base(file)
	name, ok := k.NameFromFile(base)
	if !ok {
		return "", Errors{&SourceError{File: loc.File(), Msg: fmt.Sprintf("file name %q must end in %q with a non-empty name", base, k.Ext())}}
	}
	if err := ValidName(name); err != nil {
		return "", Errors{&SourceError{File: loc.File(), Msg: fmt.Sprintf("file name: %s %v", k, err)}}
	}
	return name, nil
}
