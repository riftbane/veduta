package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/model"
	"github.com/riftbane/veduta/v2/asset/texture"
	"github.com/riftbane/veduta/v2/lua"
)

// The wiki's pages (wiki/*.md, published to the GitHub wiki by the wiki workflow) teach by
// example, so their examples are checked like the docs'.

var wikiBlock = regexp.MustCompile("(?s)```(\\w+)([^\\n]*)\\n(.*?)```")

func wikiPages(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "wiki", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no wiki pages: %v", err)
	}
	pages := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		pages[strings.TrimSuffix(filepath.Base(f), ".md")] = string(b)
	}
	return pages
}

// TestWikiExamples: every Lua block compiles, and every JSON block with a "veduta" header
// compiles with the parser of its format. A block the page marks as a fragment (```lua
// fragment) is left out.
func TestWikiExamples(t *testing.T) {
	vm := lua.New(lua.Options{})
	n := 0
	for name, page := range wikiPages(t) {
		for i, m := range wikiBlock.FindAllStringSubmatch(page, -1) {
			lang, info, body := m[1], strings.TrimSpace(m[2]), m[3]
			if info == "fragment" {
				continue
			}
			where := fmt.Sprintf("%s block %d", name, i)
			switch lang {
			case "lua":
				if _, err := vm.Load(name, body); err != nil {
					t.Errorf("%s: %v\n%s", where, err, body)
				}
				n++
			case "json":
				var h struct{ Veduta string }
				if json.Unmarshal([]byte(body), &h) != nil || h.Veduta == "" || h.Veduta == "card/1" {
					continue
				}
				if err := parseWikiAsset(h.Veduta, body); err != nil {
					t.Errorf("%s (%s): %v\n%s", where, h.Veduta, err, body)
				}
				n++
			}
		}
	}
	if n < 20 {
		t.Fatalf("checked %d examples, want at least 20", n)
	}
}

func parseWikiAsset(kind, body string) error {
	b := []byte(body)
	var err error
	switch kind {
	case asset.TypeScene:
		_, err = asset.ParseScene("main.scene.json", b)
	case asset.TypeMaterial:
		_, err = asset.ParseMaterial("x.mat.json", b)
	case asset.TypeScenario:
		_, err = asset.ParseScenario("x.scenario.json", b)
	case asset.TypeProject:
		_, err = asset.ParseProject("veduta.json", b)
	case asset.TypeTexture:
		_, err = texture.Parse("x.tex.json", b, texture.Options{FS: wikiPNGs(body)})
	case asset.TypeModel:
		_, err = model.Parse("x.model.json", b)
	case asset.TypePrefab:
		_, err = asset.ParsePrefab("x.prefab.json", b)
	case asset.TypeWorld:
		_, err = asset.ParseWorld("x.world.json", b, nil)
	}
	return err
}

// wikiPNGs is an assets directory holding a small PNG at every path the example names.
func wikiPNGs(body string) fs.FS {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 16, 16)))
	files := fstest.MapFS{}
	for _, m := range regexp.MustCompile(`"path": "([^"]+\.png)"`).FindAllStringSubmatch(body, -1) {
		files[m[1]] = &fstest.MapFile{Data: buf.Bytes()}
	}
	return files
}

// TestWikiFirstGame follows wiki/Your-First-Game.md: a new Lua game, each file a block names
// written as shown, main.lua made of the unnamed Lua blocks in order. The game must pass the
// scenarios the page lists, as the page says it does.
func TestWikiFirstGame(t *testing.T) {
	page := wikiPages(t)["Your-First-Game"]
	dir := filepath.Join(t.TempDir(), "gemcave")
	env := &Env{Version: "dev", Stdout: io.Discard, Stderr: io.Discard}
	if _, err := Init(env, InitOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	var main []string
	written := 0
	for _, m := range wikiBlock.FindAllStringSubmatch(page, -1) {
		lang, file, body := m[1], strings.TrimSpace(m[2]), m[3]
		switch {
		case lang == "sh":
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
	if written != 14 || len(main) != 7 {
		t.Fatalf("the page has %d files and %d Lua blocks, want 14 and 7", written, len(main))
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
	want := "bat_hit pass, first_gem pass, pause pass, start pass, title pass, walk pass"
	if !r.OK || strings.Join(names, ", ") != want {
		t.Fatalf("the wiki's game: %s (failed %v)", strings.Join(names, ", "), r.Failed)
	}
	// The output the page shows is the one the tool prints.
	for _, line := range strings.Split(want, ", ") {
		f := strings.Fields(line)
		if !regexp.MustCompile(`scenario ` + f[0] + ` +pass`).MatchString(page) {
			t.Errorf("the page does not show %q passing", f[0])
		}
	}
}
