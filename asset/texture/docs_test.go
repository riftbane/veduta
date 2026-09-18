package texture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type docBlock struct{ section, lang, body string }

// docBlocks returns the fenced code blocks of a markdown document with the "## "
// section each one appears in.
func docBlocks(doc string) []docBlock {
	var out []docBlock
	section := ""
	var cur *docBlock
	var body []string
	for _, line := range strings.Split(doc, "\n") {
		switch {
		case cur != nil && strings.HasPrefix(line, "```"):
			cur.body = strings.Join(body, "\n")
			out = append(out, *cur)
			cur, body = nil, nil
		case cur != nil:
			body = append(body, line)
		case strings.HasPrefix(line, "```"):
			cur = &docBlock{section: section, lang: strings.TrimPrefix(line, "```")}
		case strings.HasPrefix(line, "## "):
			section = strings.TrimPrefix(line, "## ")
		}
	}
	return out
}

// TestDocsExamples keeps docs/texture.md consistent with the compiler: every JSON
// example compiles (image paths resolve against testdata/, whose textures/src/logo.png
// the examples use), the example in "Errors" produces exactly the errors printed after
// it, and every layer type and field is documented.
func TestDocsExamples(t *testing.T) {
	dir := testdataDir(t)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "docs", "texture.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	blocks := docBlocks(doc)
	opt := Options{FS: os.DirFS(dir)}
	n := 0
	for i, b := range blocks {
		if b.lang != "json" {
			continue
		}
		n++
		if b.section == "Errors" {
			if i+1 >= len(blocks) || blocks[i+1].lang != "" {
				t.Fatal("the errors example is not followed by its output block")
			}
			_, err := Parse("wall.vtex", []byte(b.body), opt)
			if want := strings.TrimSpace(blocks[i+1].body); err == nil || err.Error() != want {
				t.Errorf("errors example reports:\n%v\ndocs say:\n%s", err, want)
			}
			continue
		}
		if _, err := Parse("example.vtex", []byte(b.body), opt); err != nil {
			t.Errorf("example %d in section %q: %v", n, b.section, err)
		}
	}
	if n < 5 {
		t.Fatalf("found only %d JSON examples in docs/texture.md", n)
	}
	for _, typ := range layerTypes {
		if !strings.Contains(doc, "### `"+typ+"`") {
			t.Errorf("layer type %q has no section", typ)
		}
		for _, f := range layerFields[typ] {
			if !strings.Contains(doc, "| `"+f+"` |") {
				t.Errorf("field %q of layer type %q is not in a field table", f, typ)
			}
		}
	}
	for _, s := range append(append([]string{"opacity", "blend", "size", "tiling", "mipmaps", "layers", "grid", "frames", "clips", "play", "edge", "fps", "loop", "next", "priority", "width", "roughness", "seed"}, blendNames...), fitNames...) {
		if !strings.Contains(doc, "`"+s+"`") {
			t.Errorf("%q is not documented", s)
		}
	}
}
