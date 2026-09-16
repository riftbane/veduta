package cli

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/docs"
)

// TestFirstGameTutorial follows docs/first-game.md: a new Lua game, each file a block names
// written as shown, main.lua made of the Lua blocks in order. The game must build and pass
// the scenarios the page writes, as the page says it does.
func TestFirstGameTutorial(t *testing.T) {
	page, err := docs.Get("first-game")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "catcher")
	env := &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}
	if _, err := Init(env, InitOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	var main []string
	written := 0
	for _, m := range regexp.MustCompile("(?s)```(\\w+)([^\\n]*)\\n(.*?)```").FindAllStringSubmatch(page, -1) {
		lang, file, body := m[1], strings.TrimSpace(m[2]), m[3]
		switch {
		case file != "":
			p := filepath.Join(dir, filepath.FromSlash(file))
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			written++
		case lang == "lua":
			main = append(main, body)
		}
	}
	if written != 6 || len(main) != 6 {
		t.Fatalf("the page has %d files and %d Lua blocks, want 6 and 6", written, len(main))
	}
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(strings.Join(main, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSession(dir, env)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Test(false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, sc := range r.Scenarios {
		names = append(names, sc.Name+" "+sc.Verdict)
	}
	if !r.OK || strings.Join(names, ", ") != "game_over pass, move pass, restart pass, start pass" {
		t.Fatalf("the tutorial's game: %s (failed %v)", strings.Join(names, ", "), r.Failed)
	}
}
