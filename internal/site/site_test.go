package site

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/docs"
)

// TestNavHasEveryTopic: every docs page is in the menu, once.
func TestNavHasEveryTopic(t *testing.T) {
	seen := map[string]int{}
	for _, s := range Nav {
		for _, e := range s.Pages {
			seen[e.Topic]++
		}
	}
	for _, topic := range docs.All() {
		if seen[topic] != 1 {
			t.Errorf("docs topic %s is in the menu %d times", topic, seen[topic])
		}
	}
	if seen["index"] != 1 {
		t.Error("no home page in the menu")
	}
}

// TestBuildLinks builds the site and follows every link between its pages to a page and,
// when it names one, a heading that is there.
func TestBuildLinks(t *testing.T) {
	dir := t.TempDir()
	if err := Build(dir); err != nil {
		t.Fatal(err)
	}
	pages := map[string]string{}
	files, _ := filepath.Glob(filepath.Join(dir, "*.html"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		pages[filepath.Base(f)] = string(b)
	}
	if len(pages) != len(docs.All())+1 {
		t.Fatalf("%d pages for %d topics and the home page", len(pages), len(docs.All()))
	}
	href := regexp.MustCompile(`href="([^"]+)"`)
	for name, body := range pages {
		for _, m := range href.FindAllStringSubmatch(body, -1) {
			target := m[1]
			if strings.Contains(target, "://") || target == "style.css" {
				continue
			}
			file, anchor, _ := strings.Cut(target, "#")
			if file == "" {
				file = name
			}
			page, ok := pages[file]
			if !ok {
				t.Errorf("%s links to %s, which is not a page", name, target)
				continue
			}
			if anchor != "" && !strings.Contains(page, `id="`+anchor+`"`) {
				t.Errorf("%s links to %s, which has no such heading", name, target)
			}
		}
	}
	if !strings.Contains(pages["first-game.html"], `<a href="start-windows.html">Getting started on Windows</a>`) {
		t.Error("a link between docs pages was not pointed at its page")
	}
}

func TestLink(t *testing.T) {
	for in, want := range map[string]string{
		"lua.md":                   "lua.html",
		"docs/scenario.md#expect":  "scenario.html#expect",
		"#the-game":                "#the-game",
		"https://example.com/a.md": "https://example.com/a.md",
		"../README.md":             Repo + "README.md",
		"editors/vscode/README.md": Repo + "editors/vscode/README.md",
	} {
		if got := link(in); got != want {
			t.Errorf("link(%q) = %q, want %q", in, got, want)
		}
	}
}
