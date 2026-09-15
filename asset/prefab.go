package asset

import (
	"sort"

	"github.com/riftbane/veduta/gmath"
)

// Prefab limits (docs/prefab.md).
const (
	MaxFootprint = 1024 // meters on a side
	MaxDistance  = 4096 // meters of a min_distance rule
)

// ParsePrefab decodes and compiles the prefab source file (for example
// "assets/prefabs/house.prefab.json"). The prefab name is the file name without its
// ".prefab.json" suffix and must be a valid asset name.
func ParsePrefab(file string, data []byte) (*Prefab, error) {
	var src PrefabSource
	loc, err := Decode(file, data, TypePrefab, &src)
	if err != nil {
		return nil, err
	}
	name, err := nameFromFile(KindPrefab, file, loc)
	if err != nil {
		return nil, err
	}
	return CompilePrefab(name, &src, loc)
}

// CompilePrefab validates src and returns the compiled prefab called name. Every problem
// is reported, located through loc (nil reports positions as unknown), in one Errors
// value. Asset references (models, materials) are checked for syntax only.
func CompilePrefab(name string, src *PrefabSource, loc *Locator) (*Prefab, error) {
	c := NewChecker(ensureLoc(loc))
	if err := ValidName(name); err != nil {
		c.Errorf("", "prefab %v", err)
	}
	p := &Prefab{Name: name}
	if src.Footprint == nil {
		c.Errorf("footprint", "is required ([width, depth] in meters)")
		p.Footprint = gmath.V2(1, 1)
	} else {
		p.Footprint = c.Vec2("footprint", src.Footprint, gmath.V2(1, 1))
		for k, v := range [2]float32{p.Footprint.X, p.Footprint.Y} {
			if v <= 0 || v > MaxFootprint {
				c.Errorf(Path("footprint", k), "%v out of range (0, %d]", v, MaxFootprint)
			}
		}
	}
	p.Tags = compileTags(c, "tags", src.Tags)
	if src.Rules != nil {
		p.Biomes = compileTags(c, "rules.biomes", src.Rules.Biomes)
		p.Distances = compileDistances(c, "rules.min_distance", src.Rules.MinDistance)
	}
	p.Entities = compileEntities(c, src.Entities, "prefab")
	for i, e := range p.Entities {
		if e.Kind == "camera" || e.Kind == "light" {
			c.Errorf(Path(Path("entities", i), "kind"), "a prefab cannot hold a %s (the world has one camera and one light)", e.Kind)
		}
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	return p, nil
}

// compileTags validates an optional list of names without duplicates (nil for none).
func compileTags(c *Checker, path string, src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, 0, len(src))
	for k, tag := range src {
		tp := Path(path, k)
		if !c.Name(tp, tag) {
			continue
		}
		if indexOf(out, tag) >= 0 {
			c.Errorf(tp, "duplicate %q", tag)
			continue
		}
		out = append(out, tag)
	}
	return out
}

// compileDistances validates a tag → meters map and returns it sorted by tag.
func compileDistances(c *Checker, path string, src map[string]float32) []Distance {
	if len(src) == 0 {
		return nil
	}
	tags := make([]string, 0, len(src))
	for tag := range src {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	out := make([]Distance, 0, len(tags))
	for _, tag := range tags {
		tp := Path(path, tag)
		if !c.Name(tp, tag) {
			continue
		}
		v := src[tag]
		if !gmath.IsFinite(v) || v < 0 || v > MaxDistance {
			c.Errorf(tp, "%v out of range [0, %d]", v, MaxDistance)
			continue
		}
		out = append(out, Distance{Tag: tag, Meters: v})
	}
	return out
}

// Distance returns the prefab's minimum distance rule toward tag (0 when none).
func (p *Prefab) Distance(tag string) float32 {
	for _, d := range p.Distances {
		if d.Tag == tag {
			return d.Meters
		}
	}
	return 0
}

// MaxDistance returns the largest minimum distance the prefab asks for (0 when none).
func (p *Prefab) MaxDistance() float32 {
	var m float32
	for _, d := range p.Distances {
		m = max(m, d.Meters)
	}
	return m
}

// HasTag reports whether the prefab carries tag.
func (p *Prefab) HasTag(tag string) bool { return indexOf(p.Tags, tag) >= 0 }
