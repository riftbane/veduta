package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/script"
)

// TestEditorFiles: init sets an editor up for a Lua game, and writing the files again keeps
// the project's own settings, adds back what is missing, and leaves a settings file with
// comments alone.
func TestEditorFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "game")
	if _, err := Init(&Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}, InitOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	read := func(p string) map[string]any {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		return v
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".veduta", "lua", "veduta.d.lua")); string(b) != string(script.Types) {
		t.Error("the Lua definitions are not the runtime's")
	}
	schemas := read(".vscode/settings.json")["json.schemas"].([]any)
	if len(schemas) != 9 {
		t.Fatalf("json.schemas %v", schemas)
	}
	for _, s := range schemas {
		url := s.(map[string]any)["url"].(string)
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(url))); err != nil {
			t.Errorf("settings name %s, which is not there", url)
		}
	}
	assoc := read(".vscode/settings.json")["files.associations"].(map[string]any)
	if len(assoc) != 8 || assoc["*.vmap"] != "json" || assoc["*.vmodel"] != "json" || assoc["*.vscenario"] != "json" {
		t.Errorf("files.associations %v", assoc)
	}
	if lib := read(".luarc.json")["workspace.library"]; !reflect.DeepEqual(lib, []any{".veduta/lua"}) {
		t.Errorf(".luarc.json library %v", lib)
	}
	if rec := read(".vscode/extensions.json")["recommendations"]; !reflect.DeepEqual(rec, []any{"sumneko.lua"}) {
		t.Errorf("recommendations %v", rec)
	}

	os.WriteFile(filepath.Join(dir, ".vscode", "settings.json"), []byte(`{"editor.tabSize": 2, "files.associations": {"*.map": "xml"}}`), 0o644)
	os.WriteFile(filepath.Join(dir, ".vscode", "extensions.json"), []byte(`{"recommendations": ["mine.ext"]}`), 0o644)
	os.WriteFile(filepath.Join(dir, ".luarc.json"), []byte("// mine\n{}"), 0o644)
	os.Remove(filepath.Join(dir, ".veduta", "schema", "world.schema.json"))
	changed, skipped, err := writeEditorFiles(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".veduta/schema/world.schema.json", ".vscode/extensions.json", ".vscode/settings.json"}; !reflect.DeepEqual(changed, want) {
		t.Errorf("changed %v, want %v", changed, want)
	}
	if !reflect.DeepEqual(skipped, []string{".luarc.json"}) {
		t.Errorf("skipped %v", skipped)
	}
	settings := read(".vscode/settings.json")
	if settings["editor.tabSize"] != 2.0 || settings["json.schemas"] == nil {
		t.Errorf("settings %v", settings)
	}
	if assoc := settings["files.associations"].(map[string]any); len(assoc) != 9 || assoc["*.map"] != "xml" || assoc["*.vtex"] != "json" {
		t.Errorf("files.associations %v", assoc)
	}
	if rec := read(".vscode/extensions.json")["recommendations"]; !reflect.DeepEqual(rec, []any{"sumneko.lua", "mine.ext"}) {
		t.Errorf("recommendations %v", rec)
	}
	if again, _, _ := writeEditorFiles(dir, true); len(again) != 0 {
		t.Errorf("a second write changed %v", again)
	}
}

// TestBuildRefreshesEditorFiles: a project set up by an older tool gets this tool's schemas
// when it builds, so VS Code checks sources against the formats the engine reads; settings
// that already say what the engine wants keep their own layout.
func TestBuildRefreshesEditorFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "game")
	env := &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}
	if _, err := Init(env, InitOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join(dir, ".veduta", "schema", "scene.schema.json")
	settings := filepath.Join(dir, ".vscode", "settings.json")
	os.WriteFile(schema, []byte(`{"old": true}`), 0o644)
	b, _ := os.ReadFile(settings)
	var obj map[string]any
	json.Unmarshal(b, &obj)
	mine, _ := json.Marshal(obj) // the same keys on one line: the project's own layout
	os.WriteFile(settings, mine, 0o644)
	s, err := OpenSession(dir, env)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.Build(false); err != nil || !r.OK {
		t.Fatalf("build: %+v %v", r, err)
	}
	if got, _ := os.ReadFile(schema); !strings.Contains(string(got), `"map"`) {
		t.Errorf("the scene schema was not refreshed:\n%s", got)
	}
	if got, _ := os.ReadFile(settings); string(got) != string(mine) {
		t.Errorf("settings rewritten:\n%s", got)
	}
}
