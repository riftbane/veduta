package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/internal/golden"
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

// TestDocsExamples keeps docs/model.md consistent with the compiler: every JSON example
// compiles, and the example in "Errors" produces exactly the errors printed after it.
func TestDocsExamples(t *testing.T) {
	root, err := golden.Root()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "model.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := docBlocks(string(data))
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
			_, err := Parse("crate.model.json", []byte(b.body))
			if want := strings.TrimSpace(blocks[i+1].body); err == nil || err.Error() != want {
				t.Errorf("errors example reports:\n%v\ndocs say:\n%s", err, want)
			}
			continue
		}
		var hdr struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal([]byte(b.body), &hdr)
		name := hdr.Name
		if name == "" {
			name = "example"
		}
		if _, err := Parse(name+".model.json", []byte(b.body)); err != nil {
			t.Errorf("example in section %q: %v", b.section, err)
		}
	}
	if n < 4 {
		t.Fatalf("found only %d JSON examples in docs/model.md", n)
	}
}
