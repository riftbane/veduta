package cli

import (
	"debug/elf"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestDeployScriptGame: a Lua game lands on the card as its release unpacks, replacing what
// was there, and nothing lands on a folder that is not a card.
func TestDeployScriptGame(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lua")
	env := &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}
	if _, err := Init(env, InitOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.WriteFile(filepath.Join(dir, "lib", "util.lua"), []byte("return {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "icon.png"), []byte("png"), 0o644)
	os.MkdirAll(filepath.Join(dir, "assets", ".cooked"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", ".cooked", "x.vda"), []byte("cooked"), 0o644)
	manifest, _ := os.ReadFile(filepath.Join(dir, "veduta.json"))
	os.WriteFile(filepath.Join(dir, "veduta.json"), []byte(strings.Replace(string(manifest), `"script"`, `"icon": "icon.png", "script"`, 1)), 0o644)
	s, err := OpenSession(dir, env)
	if err != nil {
		t.Fatal(err)
	}

	card := t.TempDir()
	if _, err := s.Deploy(card); err == nil || !strings.Contains(err.Error(), "not a console card") {
		t.Fatalf("a folder that is not a card: %v", err)
	}
	os.MkdirAll(filepath.Join(card, "games", "lua", "old"), 0o755)
	r, err := s.Deploy(card)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	filepath.WalkDir(r.Dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(r.Dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	sort.Strings(files)
	for _, want := range []string{"README.md", "card.json", "icon.png", "lib/util.lua", "main.lua", "veduta.json", "assets/models/quad.model.json"} {
		if !contains(files, want) {
			t.Errorf("the card lacks %s: %v", want, files)
		}
	}
	for _, f := range files {
		if strings.HasPrefix(f, ".") || strings.Contains(f, ".cooked") || strings.HasPrefix(f, "tests/") || strings.HasPrefix(f, "old") {
			t.Errorf("the card holds %s", f)
		}
	}
	if r.Files != len(files) {
		t.Errorf("report counts %d files, the folder has %d", r.Files, len(files))
	}
	var desc map[string]any
	b, _ := os.ReadFile(filepath.Join(r.Dir, "card.json"))
	if err := json.Unmarshal(b, &desc); err != nil || desc["version"] != r.Version || desc["icon"] != "icon.png" || desc["veduta"] != "card/1" {
		t.Errorf("card.json %s (%v)", b, err)
	}
	if entries, _ := os.ReadDir(filepath.Join(card, "games")); len(entries) != 1 {
		t.Errorf("games holds %d entries, want the game alone", len(entries))
	}

	os.Remove(filepath.Join(dir, "card.json"))
	if _, err := s.Deploy(card); err == nil || !strings.Contains(err.Error(), "card.json") {
		t.Errorf("a game without card.json: %v", err)
	}
}

// TestDeployGoGame: a Go game lands with its program built for the console.
func TestDeployGoGame(t *testing.T) {
	dir, env := newProject(t)
	s, err := OpenSession(dir, env)
	if err != nil {
		t.Fatal(err)
	}
	card := t.TempDir()
	os.MkdirAll(filepath.Join(card, "vedutaos"), 0o755)
	r, err := s.Deploy(card)
	if err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(filepath.Join(r.Dir, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Machine != elf.EM_AARCH64 {
		t.Errorf("the program is for %v", f.Machine)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
