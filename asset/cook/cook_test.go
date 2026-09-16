package cook

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/asset"
)

// copyTemplate copies the engine's test game (internal/testgame) into a temporary directory.
func copyTemplate(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "internal", "testgame")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if strings.HasPrefix(rel, filepath.Join("assets", ".cooked")) {
			return filepath.SkipDir
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func statuses(r *Report) map[string]Status {
	m := map[string]Status{}
	for _, it := range r.Items {
		m[string(it.Kind)+"/"+it.Name] = it.Status
	}
	return m
}

func TestCookIncremental(t *testing.T) {
	root := copyTemplate(t)
	r, err := Run(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if r.Failed != 0 || r.Compiled == 0 || r.Fresh != 0 {
		t.Fatalf("first cook: %+v", r)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("template has dangling references: %v", r.Warnings)
	}
	for _, it := range r.Items {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(it.Output))); err != nil {
			t.Fatalf("%s/%s: %v", it.Kind, it.Name, err)
		}
	}
	// Nothing changed: everything is fresh.
	r2, err := Run(Options{Root: root})
	if err != nil || r2.Compiled != 0 || r2.Fresh != r.Compiled {
		t.Fatalf("second cook: %+v %v", r2, err)
	}
	// Touch one source: only it recompiles.
	mat := filepath.Join(root, "assets", "materials", "hero.mat.json")
	os.WriteFile(mat, []byte(`{ "veduta": "material/1", "albedo": "#ff0000" }`), 0o644)
	r3, _ := Run(Options{Root: root})
	if st := statuses(r3); r3.Compiled != 1 || st["material/hero"] != StatusCompiled {
		t.Fatalf("after edit: %+v", st)
	}
	// Dry run after another edit reports stale without writing.
	os.WriteFile(mat, []byte(`{ "veduta": "material/1", "albedo": "#00ff00" }`), 0o644)
	r4, _ := Run(Options{Root: root, DryRun: true})
	if st := statuses(r4); r4.Stale != 1 || st["material/hero"] != StatusStale {
		t.Fatalf("dry run: %+v", st)
	}
	// Force recompiles everything.
	r5, _ := Run(Options{Root: root, Force: true})
	if r5.Compiled != r.Compiled {
		t.Fatalf("force: %+v", r5)
	}
	// A removed source removes its cooked file.
	os.Remove(filepath.Join(root, "assets", "models", "crate.model.json"))
	r6, _ := Run(Options{Root: root})
	if r6.Removed != 1 {
		t.Fatalf("prune: %+v", r6)
	}
	if _, err := os.Stat(filepath.Join(root, "assets", ".cooked", "models", "crate.vda")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("crate.vda survived its source")
	}
	if len(r6.Warnings) == 0 || !strings.Contains(strings.Join(r6.Warnings, "\n"), `model "crate" not found`) {
		t.Fatalf("missing model not warned: %v", r6.Warnings)
	}
}

func TestCookTextureDependency(t *testing.T) {
	root := copyTemplate(t)
	src := filepath.Join(root, "assets", "textures", "src")
	os.MkdirAll(src, 0o755)
	png, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "soft_cube_ids.png"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "logo.png"), png, 0o644)
	os.WriteFile(filepath.Join(root, "assets", "textures", "logo.tex.json"), []byte(`{
  "veduta": "texture/1", "size": [32, 32],
  "layers": [ { "type": "image", "path": "textures/src/logo.png", "fit": "stretch" } ]
}`), 0o644)
	if r, err := Run(Options{Root: root}); err != nil || r.Failed != 0 {
		t.Fatalf("cook: %+v %v", r, err)
	}
	// Changing the image (not the source) recompiles the texture.
	png[len(png)-20] ^= 0xff // corrupt the IEND neighbourhood: still a file change
	os.WriteFile(filepath.Join(src, "logo.png"), png, 0o644)
	r, _ := Run(Options{Root: root})
	if st := statuses(r); st["texture/logo"] == StatusFresh {
		t.Fatalf("image change not detected: %+v", st)
	}
}

func TestLoadUsesCookedAndCompilesStale(t *testing.T) {
	root := copyTemplate(t)
	lib, err := Load(root) // nothing cooked: compiles in memory, writes nothing
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "assets", ".cooked")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("Load wrote cooked files")
	}
	if lib.Scenes["main"] == nil || lib.Models["hero"] == nil || lib.Textures["grass"] == nil || lib.Materials["gem"] == nil {
		t.Fatal("library incomplete")
	}
	if _, err := Run(Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	lib2, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	a, b := lib.Models["hero"].Mesh, lib2.Models["hero"].Mesh
	if len(a.Vertices) != len(b.Vertices) || a.Vertices[7] != b.Vertices[7] || a.Bounds != b.Bounds {
		t.Fatal("cooked model differs from the compiled one")
	}
	if lib2.Project.Name != "demo" {
		t.Fatalf("project %+v", lib2.Project)
	}
}

func TestCookReportsLocatedErrors(t *testing.T) {
	root := copyTemplate(t)
	bad := filepath.Join(root, "assets", "models", "broken.model.json")
	os.WriteFile(bad, []byte("{\n  \"veduta\": \"model/1\",\n  \"parts\": [ { \"shape\": \"box\", \"size\": [1, 0, 1] } ]\n}"), 0o644)
	r, err := Run(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if r.Failed != 1 {
		t.Fatalf("failed = %d", r.Failed)
	}
	es := r.Errors()
	if len(es) == 0 || es[0].File != "assets/models/broken.model.json" || es[0].Line != 3 {
		t.Fatalf("errors %v", es)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("Load accepted a broken source")
	} else {
		var list asset.Errors
		if !errors.As(err, &list) {
			t.Fatalf("Load error %T is not asset.Errors", err)
		}
	}
}

// A world is cooked after its prefabs and depends on them: editing a prefab makes the
// world stale, and both are decoded back from their cooked files.
func TestCookPrefabAndWorld(t *testing.T) {
	root := copyTemplate(t)
	write := func(rel, data string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(data), 0o644)
	}
	write("assets/prefabs/shrub.prefab.json", `{"veduta": "prefab/1", "footprint": [1, 1], "tags": ["shrub"],
	  "entities": [{"name": "trunk", "kind": "static", "model": "crate", "position": [0.5, 0, 0.5]}]}`)
	write("assets/worlds/land.world.json", `{"veduta": "world/1", "camera": {"position": [0, 5, 10], "look_at": [0, 0, 0]},
	  "biomes": [{"name": "plain", "ground": "grass"}], "scatter": [{"prefab": "shrub", "density": 0.1}],
	  "places": [{"name": "home", "prefab": "shrub", "cell": [3, 4]}]}`)
	r, err := Run(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(r)
	if r.Failed != 0 || st["prefab/shrub"] != StatusCompiled || st["world/land"] != StatusCompiled {
		t.Fatalf("cook: %+v", r)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("warnings %v", r.Warnings)
	}
	lib, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if lib.Prefabs["shrub"] == nil || lib.Worlds["land"] == nil || lib.Worlds["land"].Places[0].Cell != [2]int32{3, 4} {
		t.Fatalf("library %+v %+v", lib.Prefabs, lib.Worlds)
	}
	data, _ := os.ReadFile(filepath.Join(root, "assets", ".cooked", "worlds", "land.vda"))
	meta, _, err := asset.UnpackVDA(data)
	if err != nil || len(meta.Deps) != 1 || meta.Deps[0] != "prefabs/shrub.prefab.json" {
		t.Fatalf("world meta %+v %v", meta, err)
	}
	// A larger shrub no longer fits a scatter cell: the world is recooked and fails.
	write("assets/prefabs/shrub.prefab.json", `{"veduta": "prefab/1", "footprint": [2, 2], "entities": []}`)
	r, err = Run(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	st = statuses(r)
	if st["prefab/shrub"] != StatusCompiled || st["world/land"] != StatusError || r.Failed != 1 {
		t.Fatalf("after edit: %+v", st)
	}
	if es := r.Errors(); len(es) != 1 || es[0].File != "assets/worlds/land.world.json" || es[0].Line != 2 {
		t.Fatalf("errors %v", r.Errors())
	}
	// A missing prefab is only a warning.
	write("assets/worlds/land.world.json", `{"veduta": "world/1", "camera": {"position": [0, 5, 10], "look_at": [0, 0, 0]},
	  "biomes": [{"name": "plain", "ground": "grass"}], "places": [{"name": "home", "prefab": "castle", "cell": [3, 4]}]}`)
	r, err = Run(Options{Root: root})
	if err != nil || r.Failed != 0 || len(r.Warnings) != 1 || r.Warnings[0] != `world land: place home: prefab "castle" not found` {
		t.Fatalf("missing prefab: %v %+v", err, r)
	}
}
