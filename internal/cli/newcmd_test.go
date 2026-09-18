package cli

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestNew: every kind veduta new makes builds as it is, a scenario of each start passes,
// a module says how to require it, and a taken name, a bad name or a folder out of place
// is refused with nothing written.
func TestNew(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "game")
	env := &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}
	if _, err := Init(env, InitOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSession(dir, env)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		o    NewOptions
		want []string
	}{
		{NewOptions{Kind: "scene", Name: "level1"}, []string{"assets/scenes/level1.vscene"}},
		{NewOptions{Kind: "world", Name: "land"}, []string{"assets/worlds/land.vworld", "assets/materials/ground.vmat", "assets/textures/ground.vtex"}},
		{NewOptions{Kind: "world", Name: "caves", In: "under"}, []string{"assets/worlds/under/caves.vworld"}},
		{NewOptions{Kind: "prefab", Name: "tree", In: "nature/big"}, []string{"assets/prefabs/nature/big/tree.vprefab"}},
		{NewOptions{Kind: "model", Name: "crate"}, []string{"assets/models/crate.vmodel"}},
		{NewOptions{Kind: "material", Name: "wood", In: "props"}, []string{"assets/materials/props/wood.vmat"}},
		{NewOptions{Kind: "texture", Name: "bark"}, []string{"assets/textures/bark.vtex"}},
		{NewOptions{Kind: "scenario", Name: "first_level", Scene: "level1"}, []string{"tests/scenarios/first_level.vscenario"}},
		{NewOptions{Kind: "scenario", Name: "walk", World: "land"}, []string{"tests/scenarios/walk.vscenario"}},
		{NewOptions{Kind: "script", Name: "slime-ai", In: "enemies"}, []string{"enemies/slime-ai.lua"}},
	} {
		r, err := s.New(c.o)
		if err != nil {
			t.Fatalf("new %s %s: %v", c.o.Kind, c.o.Name, err)
		}
		if !reflect.DeepEqual(r.Files, c.want) || r.File != c.want[0] {
			t.Errorf("new %s %s wrote %v, want %v", c.o.Kind, c.o.Name, r.Files, c.want)
		}
	}
	lua, _ := os.ReadFile(filepath.Join(dir, "enemies", "slime-ai.lua"))
	if !strings.Contains(string(lua), `local slime_ai = require("enemies.slime-ai")`) {
		t.Errorf("the module:\n%s", lua)
	}
	if b, err := s.Build(false); err != nil || !b.OK {
		t.Fatalf("build with the new sources: %+v %v", b, err)
	}
	r, err := s.Test(false)
	if err != nil || !r.OK || len(r.Scenarios) != 3 {
		t.Fatalf("test with the new scenarios: %+v %v", r, err)
	}

	for _, c := range []struct {
		o    NewOptions
		want string
	}{
		{NewOptions{Kind: "prefab", Name: "tree"}, `the prefab name "tree" is taken by assets/prefabs/nature/big/tree.vprefab`},
		{NewOptions{Kind: "scene", Name: "Level2"}, `may only contain`},
		{NewOptions{Kind: "scene", Name: "x", In: "../out"}, `a relative path of visible folders`},
		{NewOptions{Kind: "scene", Name: "x", In: ".drafts"}, `a relative path of visible folders`},
		{NewOptions{Kind: "scenario", Name: "x", In: "more"}, `no folders`},
		{NewOptions{Kind: "scenario", Name: "walk"}, `tests/scenarios/walk.vscenario is there already`},
		{NewOptions{Kind: "script", Name: "x", In: "assets/lua"}, `not searched for`},
		{NewOptions{Kind: "script", Name: "x", In: "v1.2"}, `dots into folders`},
		{NewOptions{Kind: "sound", Name: "x"}, `unknown kind "sound"`},
		{NewOptions{Kind: "model", Name: "x", Scene: "level1"}, `for a scenario`},
	} {
		if _, err := s.New(c.o); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("new %+v: %v, want %q", c.o, err, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "assets", "scenes", "x.vscene")); err == nil {
		t.Error("a refused new wrote its file")
	}
}
