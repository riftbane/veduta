package asset

import (
	"fmt"
	"sort"

	"github.com/riftbane/veduta/gmath"
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
}

// NewLibrary returns an empty library.
func NewLibrary(p *Project) *Library {
	return &Library{Project: p, Models: map[string]*Model{}, Textures: map[string]*Texture{},
		Materials: map[string]*Material{}, Scenes: map[string]*Scene{}}
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

// References returns a sorted warning for every name that does not resolve: material
// textures, model part materials, scene entity models and materials.
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
	}
	for _, name := range Names(l.Scenes) {
		for _, e := range l.Scenes[name].Entities {
			if e.Model != "" && l.Models[e.Model] == nil {
				w = append(w, fmt.Sprintf("scene %s: entity %s: model %q not found", name, e.Name, e.Model))
			}
			if e.Material != "" && l.Materials[e.Material] == nil {
				w = append(w, fmt.Sprintf("scene %s: entity %s: material %q not found", name, e.Name, e.Material))
			}
		}
	}
	return w
}
