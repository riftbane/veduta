package asset

import (
	"fmt"
	"sort"

	"github.com/riftbane/veduta/v2/gmath"
)

// Library holds every compiled asset of a project by name. It is what cook.Load returns,
// what the engine renders from and what inspection reads. Iterate with the Names helpers:
// map order must never influence output.
type Library struct {
	Project   *Project
	Models    map[string]*Model
	Textures  map[string]*Texture
	Materials map[string]*Material
	Scenes    map[string]*Scene
	Prefabs   map[string]*Prefab
	Worlds    map[string]*World
}

// NewLibrary returns an empty library.
func NewLibrary(p *Project) *Library {
	return &Library{Project: p, Models: map[string]*Model{}, Textures: map[string]*Texture{},
		Materials: map[string]*Material{}, Scenes: map[string]*Scene{}, Prefabs: map[string]*Prefab{}, Worlds: map[string]*World{}}
}

// ModelBounds returns the local bounds of a model (a scene.BoundsFunc).
func (l *Library) ModelBounds(name string) (gmath.AABB, bool) {
	if m, ok := l.Models[name]; ok {
		return m.Mesh.Bounds, true
	}
	return gmath.AABB{}, false
}

// Names returns the sorted keys of an asset map.
func Names[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Prefab returns the prefab called name, or nil (a CompileWorld lookup).
func (l *Library) Prefab(name string) *Prefab { return l.Prefabs[name] }

// References returns a sorted warning for every name that does not resolve: material
// textures, model part materials, scene, prefab and world entity models and materials,
// world ground materials and prefabs.
func (l *Library) References() []string {
	w := []string{}
	for _, name := range Names(l.Materials) {
		if t := l.Materials[name].Texture; t != "" && l.Textures[t] == nil {
			w = append(w, fmt.Sprintf("material %s: texture %q not found", name, t))
		}
	}
	for _, name := range Names(l.Models) {
		for _, m := range l.Models[name].Materials {
			if m != "" && l.Materials[m] == nil {
				w = append(w, fmt.Sprintf("model %s: material %q not found", name, m))
			}
		}
		for i, lod := range l.Models[name].LODs {
			if lod.Model != "" && l.Models[lod.Model] == nil {
				w = append(w, fmt.Sprintf("model %s: lod[%d]: model %q not found", name, i, lod.Model))
			}
		}
	}
	entities := func(what, name string, ents []Entity) {
		for _, e := range ents {
			if e.Model != "" && l.Models[e.Model] == nil {
				w = append(w, fmt.Sprintf("%s %s: entity %s: model %q not found", what, name, e.Name, e.Model))
			}
			if e.Material != "" && l.Materials[e.Material] == nil {
				w = append(w, fmt.Sprintf("%s %s: entity %s: material %q not found", what, name, e.Name, e.Material))
			}
		}
	}
	for _, name := range Names(l.Scenes) {
		entities("scene", name, l.Scenes[name].Entities)
	}
	for _, name := range Names(l.Prefabs) {
		entities("prefab", name, l.Prefabs[name].Entities)
	}
	for _, name := range Names(l.Worlds) {
		wd := l.Worlds[name]
		for _, b := range wd.Biomes {
			if l.Materials[b.Ground] == nil {
				w = append(w, fmt.Sprintf("world %s: biome %s: ground material %q not found", name, b.Name, b.Ground))
			}
		}
		for _, v := range wd.Vegetation {
			if v.Prefab != "" && l.Prefabs[v.Prefab] == nil {
				w = append(w, fmt.Sprintf("world %s: vegetation %s: prefab %q not found", name, v.Name, v.Prefab))
			}
			if v.Model != "" && l.Models[v.Model] == nil {
				w = append(w, fmt.Sprintf("world %s: vegetation %s: model %q not found", name, v.Name, v.Model))
			}
		}
		if wd.Terrain.Water != "" && l.Materials[wd.Terrain.Water] == nil {
			w = append(w, fmt.Sprintf("world %s: terrain: water material %q not found", name, wd.Terrain.Water))
		}
		for i, s := range wd.Scatter {
			if l.Prefabs[s.Prefab] == nil {
				w = append(w, fmt.Sprintf("world %s: scatter[%d]: prefab %q not found", name, i, s.Prefab))
			}
		}
		for i, s := range wd.Sites {
			for _, p := range s.Prefabs {
				if l.Prefabs[p] == nil {
					w = append(w, fmt.Sprintf("world %s: sites[%d]: prefab %q not found", name, i, p))
				}
			}
		}
		for _, p := range wd.Places {
			if l.Prefabs[p.Prefab] == nil {
				w = append(w, fmt.Sprintf("world %s: place %s: prefab %q not found", name, p.Name, p.Prefab))
			}
		}
		entities("world", name, wd.Entities)
	}
	return w
}
