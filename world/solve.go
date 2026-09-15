package world

import (
	"fmt"

	"github.com/riftbane/veduta/asset"
)

// Issue codes of Validate and Check.
const (
	CodePlaceOutside  = "WORLD_PLACE_OUTSIDE"
	CodePlaceBiome    = "WORLD_PLACE_BIOME"
	CodePlaceOverlap  = "WORLD_PLACE_OVERLAP"
	CodePlaceTooClose = "WORLD_PLACE_TOO_CLOSE"
	CodeMissingPrefab = "WORLD_MISSING_PREFAB"
	CodeMissingAsset  = "WORLD_MISSING_ASSET"
	CodeNotTiling     = "WORLD_GROUND_NOT_TILING"
	CodeBudget        = "WORLD_CHUNK_BUDGET"
	CodeViewShort     = "WORLD_VIEW_SHORT"
)

// Issue is one problem found by Validate or Check.
type Issue struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"` // error or warning
	Msg      string         `json:"msg"`
	Where    map[string]any `json:"where,omitempty"`
}

// Validate checks a place of prefab p with its footprint's min corner at cell, turned by
// rotation, against the world and the other places (except the one called except): it
// must lie inside the world, stand on an allowed biome, and neither overlap another
// place nor come closer to one than their min_distance rules allow. Sites and scatter
// are not checked: they yield to places.
func (g *Gen) Validate(p *asset.Prefab, cell [2]int32, rotation int, except string) []Issue {
	fw, fd := g.W.Footprint(p, rotation)
	s := Struct{Prefab: p, Rect: Rect{cell[0], cell[1], int32(fw), int32(fd)}, Rotation: rotation, Tags: p.Tags, Site: -1, Cell: cell}
	var out []Issue
	where := func(kv ...any) map[string]any {
		m := map[string]any{"cell": cell, "footprint": [2]int{fw, fd}, "rotation": rotation}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	if !s.Rect.Inside(g.Bounds()) {
		span := g.W.Span()
		out = append(out, Issue{CodePlaceOutside, "error",
			fmt.Sprintf("the %d×%d cell footprint at [%d, %d] leaves the world (cells -%d to %d)", fw, fd, cell[0], cell[1], span, span-1), where()})
		return out
	}
	if len(p.Biomes) > 0 {
		cx, cz := s.Rect.Center()
		b := g.W.Biomes[g.Biome(cx, cz)].Name
		if !contains(p.Biomes, b) {
			out = append(out, Issue{CodePlaceBiome, "warning",
				fmt.Sprintf("cell [%d, %d] is in biome %q; prefab %q allows %v", cx, cz, b, p.Name, p.Biomes), where("biome", b)})
		}
	}
	for i := range g.places {
		o := &g.places[i]
		if o.Place == except {
			continue
		}
		switch {
		case s.Rect.Overlaps(o.Rect):
			out = append(out, Issue{CodePlaceOverlap, "error",
				fmt.Sprintf("overlaps place %q (%s at [%d, %d], %d×%d cells)", o.Place, o.Prefab.Name, o.Rect.X, o.Rect.Z, o.Rect.W, o.Rect.D),
				where("place", o.Place)})
		case g.conflicts(&s, o):
			out = append(out, Issue{CodePlaceTooClose, "warning",
				fmt.Sprintf("%d cells from place %q (%s), rules ask for %d", s.Rect.Gap(o.Rect), o.Place, o.Prefab.Name, g.need(&s, o)),
				where("place", o.Place, "gap", s.Rect.Gap(o.Rect), "need", g.need(&s, o))})
		}
	}
	return out
}

// Displaced returns the keys of the sites a place with footprint s would remove (they
// yield to places), sorted.
func (g *Gen) Displaced(p *asset.Prefab, cell [2]int32, rotation int) []string {
	fw, fd := g.W.Footprint(p, rotation)
	s := Struct{Prefab: p, Rect: Rect{cell[0], cell[1], int32(fw), int32(fd)}, Tags: p.Tags, Site: -1}
	var out []string
	for r := range g.sites {
		g.anySite(r, &s, func(t *Struct) bool {
			if g.conflicts(&s, t) {
				out = append(out, t.Key)
			}
			return false
		})
	}
	sortStrings(out)
	return out
}

// Solve searches for the first cell where a place of p, turned by rotation, is valid:
// rings of growing Chebyshev distance around near, up to within cells away, each ring
// starting east (+X) of the centre and going clockwise (south, west, north). It returns
// the cell, whether one was found, and how many candidates each issue code rejected.
func (g *Gen) Solve(p *asset.Prefab, near [2]int32, within int32, rotation int) (cell [2]int32, ok bool, rejected map[string]int) {
	rejected = map[string]int{}
	try := func(x, z int32) bool {
		c := [2]int32{x, z}
		issues := g.Validate(p, c, rotation, "")
		if len(issues) == 0 {
			cell, ok = c, true
			return true
		}
		for _, is := range issues {
			rejected[is.Code]++
		}
		return false
	}
	if try(near[0], near[1]) {
		return cell, true, rejected
	}
	for k := int32(1); k <= within; k++ {
		x0, z0, x1, z1 := near[0]-k, near[1]-k, near[0]+k, near[1]+k
		// East edge going south, south edge going west, west edge going north, north
		// edge going east; each edge stops before the next one's first cell.
		for z := near[1]; z < z1; z++ {
			if try(x1, z) {
				return cell, true, rejected
			}
		}
		for x := x1; x > x0; x-- {
			if try(x, z1) {
				return cell, true, rejected
			}
		}
		for z := z1; z > z0; z-- {
			if try(x0, z) {
				return cell, true, rejected
			}
		}
		for x := x0; x < x1; x++ {
			if try(x, z0) {
				return cell, true, rejected
			}
		}
		for z := z0; z < near[1]; z++ {
			if try(x1, z) {
				return cell, true, rejected
			}
		}
	}
	return cell, false, rejected
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
