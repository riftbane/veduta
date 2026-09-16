package world

import (
	"fmt"
	"math"
	"sort"

	"github.com/riftbane/veduta/asset"
)

// Budget is the console's triangle budget per frame (spec §15.8).
const Budget = 1200

// Report is what Check found: issues ranked errors first, then warnings, in a stable
// order, and the numbers behind the budget warnings.
type Report struct {
	Issues  []Issue
	Metrics map[string]any
}

// Check inspects the world against its library: missing prefabs and assets, ground
// materials that do not tile, places against the rules, the triangle budget of the
// densest sampled chunk scaled to what the camera sees, and a camera seeing past the
// loaded chunks.
func (g *Gen) Check(lib *asset.Library) *Report {
	w := g.W
	rep := &Report{Metrics: map[string]any{}}
	add := func(code, sev, msg string, where map[string]any) {
		rep.Issues = append(rep.Issues, Issue{code, sev, msg, where})
	}
	for _, name := range g.Missing {
		add(CodeMissingPrefab, "error", fmt.Sprintf("prefab %q does not exist (assets/prefabs/%s.prefab.json)", name, name), map[string]any{"prefab": name})
	}
	for _, b := range w.Biomes {
		m := lib.Materials[b.Ground]
		switch {
		case m == nil:
			add(CodeMissingAsset, "error", fmt.Sprintf("biome %q: ground material %q does not exist", b.Name, b.Ground), map[string]any{"biome": b.Name, "material": b.Ground})
		case m.Texture == "":
			add(CodeNotTiling, "error", fmt.Sprintf("biome %q: ground material %q has no texture; the ground tiles one texture repeat per cell", b.Name, b.Ground), map[string]any{"biome": b.Name, "material": b.Ground})
		case lib.Textures[m.Texture] != nil && !lib.Textures[m.Texture].Tiling:
			add(CodeNotTiling, "error", fmt.Sprintf("biome %q: texture %q of ground material %q is not tiling (set \"tiling\": true)", b.Name, m.Texture, b.Ground), map[string]any{"biome": b.Name, "material": b.Ground, "texture": m.Texture})
		}
	}
	if name := w.Terrain.Water; name != "" && lib.Materials[name] == nil {
		add(CodeMissingAsset, "error", fmt.Sprintf("terrain: water material %q does not exist", name), map[string]any{"material": name})
	}
	for _, e := range w.Entities {
		if e.Model != "" && lib.Models[e.Model] == nil {
			add(CodeMissingAsset, "error", fmt.Sprintf("entity %q: model %q does not exist", e.Name, e.Model), map[string]any{"entity": e.Name, "model": e.Model})
		}
		if e.Material != "" && lib.Materials[e.Material] == nil {
			add(CodeMissingAsset, "error", fmt.Sprintf("entity %q: material %q does not exist", e.Name, e.Material), map[string]any{"entity": e.Name, "material": e.Material})
		}
	}
	// Places: each against the others; a pair is reported once.
	for i := range g.places {
		p := &g.places[i]
		for _, is := range g.Validate(p.Prefab, p.Cell, p.Rotation, p.Place) {
			if other, ok := is.Where["place"].(string); ok {
				j := g.placeIndex(other)
				if j < i {
					continue
				}
			}
			is.Where["place"] = p.Place
			is.Msg = fmt.Sprintf("place %q: %s", p.Place, is.Msg)
			rep.Issues = append(rep.Issues, is)
		}
	}
	// Budget: the densest sampled chunk, scaled to the ground the camera sees.
	tris := func(m string) int {
		if md := lib.Models[m]; md != nil {
			return len(md.Mesh.Indices) / 3
		}
		return 0
	}
	structTris := func(s *Struct) int {
		n := 0
		for _, e := range s.Prefab.Entities {
			if e.Model != "" {
				n += tris(e.Model)
			}
		}
		return n
	}
	maxTris, maxChunk := 0, [2]int32{}
	seen := map[[2]int32]bool{}
	sample := func(cx, cz int32) {
		if seen[[2]int32{cx, cz}] || cx < -int32(w.Extent) || cx >= int32(w.Extent) || cz < -int32(w.Extent) || cz >= int32(w.Extent) {
			return
		}
		seen[[2]int32{cx, cz}] = true
		c := g.Chunk(cx, cz)
		n := len(g.Ground(c).Mesh.Indices) / 3
		for i := range c.Scatter {
			n += structTris(&c.Scatter[i])
		}
		for _, s := range g.Structures(cx, cz) {
			n += structTris(&s)
		}
		if n > maxTris {
			maxTris, maxChunk = n, [2]int32{cx, cz}
		}
	}
	for cz := int32(-1); cz <= 1; cz++ {
		for cx := int32(-1); cx <= 1; cx++ {
			sample(cx, cz)
		}
	}
	for i := range g.places {
		cx, cz := g.ChunkOf(g.places[i].Rect.Center())
		sample(cx, cz)
	}
	for i := int64(0); i < 12; i++ {
		h := hash(w.Seed, 0x73616d70, i, 0, 0)
		e := uint64(2 * w.Extent)
		sample(int32(h%e)-int32(w.Extent), int32((h>>32)%e)-int32(w.Extent))
	}
	chunkArea := float64(w.Chunk) * float64(w.Chunk) * float64(w.Cell) * float64(w.Cell)
	loaded := float64(2*w.View+1) * float64(2*w.View+1) * chunkArea
	visible := loaded
	if w.Camera.Ortho {
		visible = math.Min(loaded, float64(w.Camera.Size)*float64(w.Camera.Size)*4/3)
	}
	estimate := int(math.Ceil(float64(maxTris) * visible / chunkArea))
	rep.Metrics["max_chunk_triangles"] = maxTris
	rep.Metrics["max_chunk"] = maxChunk
	rep.Metrics["visible_triangles"] = estimate
	rep.Metrics["sampled_chunks"] = len(seen)
	if estimate > Budget {
		add(CodeBudget, "warning", fmt.Sprintf("chunk [%d, %d] has %d triangles; the camera sees about %d, more than the console's budget of %d: lower scatter density or use simpler models",
			maxChunk[0], maxChunk[1], maxTris, estimate, Budget), map[string]any{"chunk": maxChunk, "triangles": maxTris, "visible": estimate, "budget": Budget})
	}
	reach := float64(w.View) * float64(w.Chunk) * float64(w.Cell)
	if w.Camera.Ortho {
		half := math.Hypot(float64(w.Camera.Size)/2, float64(w.Camera.Size)*4/3/2)
		if half > reach {
			add(CodeViewShort, "warning", fmt.Sprintf("the camera sees %.1f m from the focus but only %.0f m of chunks are loaded (view %d × chunk %d × cell %v): raise view or lower size",
				half, reach, w.View, w.Chunk, w.Cell), map[string]any{"sees": half, "loaded": reach})
		}
	} else if float64(w.Camera.Far) > reach {
		add(CodeViewShort, "warning", fmt.Sprintf("the camera's far plane (%v m) is beyond the loaded chunks (%.0f m from the focus: view %d × chunk %d × cell %v): the horizon shows unloaded ground; lower far or raise view",
			w.Camera.Far, reach, w.View, w.Chunk, w.Cell), map[string]any{"far": w.Camera.Far, "loaded": reach})
	}
	sort.SliceStable(rep.Issues, func(i, j int) bool { return rep.Issues[i].Severity == "error" && rep.Issues[j].Severity != "error" })
	return rep
}

func (g *Gen) placeIndex(name string) int {
	for i := range g.places {
		if g.places[i].Place == name {
			return i
		}
	}
	return -1
}
