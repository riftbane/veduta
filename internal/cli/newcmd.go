package cli

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/cook"
	projtemplate "github.com/riftbane/veduta/v2/template"
)

// NewKinds are what veduta new makes: every source format, and a Lua module.
var NewKinds = []string{"scene", "world", "prefab", "model", "material", "texture", "scenario", "script"}

// NewOptions configures new.
type NewOptions struct {
	Kind string // one of NewKinds
	Name string // the asset's name, or the module's file name without .lua
	// In is the folder the file goes in: under the kind's directory for an asset
	// (assets/prefabs/<In>), under the project root for a script. Scenarios have none.
	In string
	// Scene or World is where a new scenario starts (default: the project's start).
	Scene, World string
}

// NewReport lists what new wrote.
type NewReport struct {
	OK    bool     `json:"ok"`
	Kind  string   `json:"kind"`
	Name  string   `json:"name"`
	File  string   `json:"file"`  // the new file, relative to the project root, slash-separated
	Files []string `json:"files"` // every file written, File first (a world also gets its ground material and texture when the project has none)
}

// Human names the files.
func (r *NewReport) Human() string {
	var b strings.Builder
	for _, f := range r.Files {
		fmt.Fprintln(&b, "created", f)
	}
	return b.String()
}

// New writes a new source of a kind, valid as it is, so that the game builds with it: an
// empty scene, a 1 m box, a grey texture and material, an empty prefab, a flat world
// (with a "ground" material and texture if the project has none), a scenario that runs 20 ticks and
// checks the invariants, or an empty Lua module. It never overwrites: an asset name must
// be free in every folder of its kind.
func (s *Session) New(o NewOptions) (*NewReport, error) {
	if err := asset.ValidName(o.Name); err != nil {
		return nil, fmt.Errorf("new: %w", err)
	}
	in, err := newFolder(o.In)
	if err != nil {
		return nil, err
	}
	r := &NewReport{OK: true, Kind: o.Kind, Name: o.Name, Files: []string{}}
	files := map[string][]byte{}
	var order []string
	add := func(rel string, data []byte) {
		files[rel] = data
		order = append(order, rel)
	}
	switch o.Kind {
	case "script":
		if !s.IsScript() {
			return nil, fmt.Errorf("new: a Go game has no Lua scripts")
		}
		rel := path.Join(in, o.Name+".lua")
		if strings.Contains(in, ".") {
			return nil, fmt.Errorf("new: folder %q: require turns dots into folders, so a script's folders have none", in)
		}
		first, _, _ := strings.Cut(rel, "/")
		if first == "out" || first == "bin" || first == "node_modules" || strings.HasPrefix(rel, s.Project.Assets+"/") {
			return nil, fmt.Errorf("new: %s: scripts are not searched for in %s", rel, first)
		}
		module, err := filepath.Rel(filepath.FromSlash(path.Dir(s.Project.Script)), filepath.FromSlash(strings.TrimSuffix(rel, ".lua")))
		if err != nil || strings.HasPrefix(filepath.ToSlash(module), "../") {
			return nil, fmt.Errorf("new: %s: modules load from the folder of %s and below", rel, s.Project.Script)
		}
		module = strings.ReplaceAll(filepath.ToSlash(module), "/", ".")
		data, err := newTemplate("script.lua", map[string]string{"Module": module, "Var": strings.ReplaceAll(o.Name, "-", "_")})
		if err != nil {
			return nil, err
		}
		add(rel, data)
	case "scenario":
		if in != "" {
			return nil, fmt.Errorf("new: tests/scenarios has no folders")
		}
		if o.Scene != "" && o.World != "" {
			return nil, usagef("new: --scene or --world, not both")
		}
		key, start := "scene", s.Project.DefaultScene
		if s.Project.DefaultWorld != "" {
			key, start = "world", s.Project.DefaultWorld
		}
		switch {
		case o.Scene != "":
			key, start = "scene", o.Scene
		case o.World != "":
			key, start = "world", o.World
		}
		data, err := newTemplate("scenario.vscenario", map[string]string{"StartKey": key, "StartName": start})
		if err != nil {
			return nil, err
		}
		add(path.Join(asset.KindScenario.Dir(), o.Name+asset.KindScenario.Ext()), data)
	default:
		k := asset.Kind(o.Kind)
		if k == asset.KindScenario || !isCookedKind(k) {
			return nil, usagef("new: unknown kind %q (%s)", o.Kind, strings.Join(NewKinds, ", "))
		}
		if o.Scene != "" || o.World != "" {
			return nil, usagef("new: --scene and --world are for a scenario")
		}
		if taken := s.sourceOf(k, o.Name); taken != "" {
			return nil, fmt.Errorf("new: the %s name %q is taken by %s: an asset's name is its file name, whatever its folder", k, o.Name, taken)
		}
		data, err := projtemplate.FS.ReadFile("new/" + string(k) + k.Ext())
		if err != nil {
			return nil, err
		}
		add(path.Join(s.Project.Assets, k.Dir(), in, o.Name+k.Ext()), data)
		// A world stands on a ground material with a tiling texture: both come with the
		// first world.
		for _, g := range []asset.Kind{asset.KindMaterial, asset.KindTexture} {
			if k != asset.KindWorld || s.sourceOf(g, "ground") != "" {
				continue
			}
			ground, err := projtemplate.FS.ReadFile("new/ground" + g.Ext())
			if err != nil {
				return nil, err
			}
			add(path.Join(s.Project.Assets, g.Dir(), "ground"+g.Ext()), ground)
		}
	}
	for _, rel := range order {
		if _, err := os.Stat(filepath.Join(s.Root, filepath.FromSlash(rel))); err == nil {
			return nil, fmt.Errorf("new: %s is there already", rel)
		}
	}
	for _, rel := range order {
		full := filepath.Join(s.Root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, files[rel], 0o644); err != nil {
			return nil, err
		}
		r.Files = append(r.Files, rel)
	}
	r.File = r.Files[0]
	return r, nil
}

// sourceOf returns the source of an asset, relative to the project root, or "" when the
// name is free in every folder of its kind.
func (s *Session) sourceOf(k asset.Kind, name string) string {
	files, _ := cook.SourceFiles(filepath.Join(s.Root, filepath.FromSlash(s.Project.Assets)), k)
	for _, f := range files {
		if path.Base(f) == name+k.Ext() {
			return path.Join(s.Project.Assets, k.Dir(), f)
		}
	}
	return ""
}

func isCookedKind(k asset.Kind) bool {
	for _, c := range asset.CookedKinds {
		if c == k {
			return true
		}
	}
	return false
}

// newFolder checks the folder a new file goes in: relative, forward slashes, no hidden
// folder and no way out.
func newFolder(in string) (string, error) {
	in = strings.Trim(filepath.ToSlash(in), "/")
	if in == "" || in == "." {
		return "", nil
	}
	for _, seg := range strings.Split(in, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") || strings.ContainsAny(seg, `:\`) {
			return "", fmt.Errorf("new: folder %q: a relative path of visible folders", in)
		}
	}
	return in, nil
}

func newTemplate(name string, data map[string]string) ([]byte, error) {
	src, err := projtemplate.FS.ReadFile("new/" + name)
	if err != nil {
		return nil, err
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func init() {
	register(command{
		name: "new", usage: "new KIND NAME [--in FOLDER] [--scene S | --world W]", summary: "write a new source that builds as it is: scene, world, prefab, model, material, texture, scenario (--scene or --world: where it starts) or script (a Lua module); --in puts it in a folder of its kind's directory (of the project, for a script)", project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("new", env.Stderr)
			in := fs.String("in", "", "folder: under the kind's directory (assets/prefabs/FOLDER), or the project's for a script")
			scene := fs.String("scene", "", "a scenario's scene")
			world := fs.String("world", "", "a scenario's world")
			var pos []string
			for {
				if err := fs.Parse(args); err != nil {
					return nil, UsageError{err}
				}
				if fs.NArg() == 0 {
					break
				}
				pos = append(pos, fs.Arg(0))
				args = fs.Args()[1:]
			}
			if len(pos) != 2 {
				return nil, usagef("new takes a kind (%s) and a name", strings.Join(NewKinds, ", "))
			}
			return s.New(NewOptions{Kind: pos[0], Name: pos[1], In: *in, Scene: *scene, World: *world})
		},
	})
}
