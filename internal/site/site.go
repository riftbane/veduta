package site

import (
	"embed"
	"fmt"
	"html"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/riftbane/veduta/v2/docs"
)

//go:embed home.md style.css
var own embed.FS

// Repo is where links to files outside docs point.
const Repo = "https://github.com/riftbane/veduta/blob/main/"

// Section is a group of the navigation.
type Section struct {
	Title string
	Pages []Entry
}

// Entry is a page of the navigation: its docs topic ("index" is the home page) and the name
// the menu shows.
type Entry struct{ Topic, Name string }

// Nav is the site's menu, every docs topic in it once.
var Nav = []Section{
	{"Start", []Entry{{"index", "Home"}, {"start-windows", "Getting started on Windows"}, {"first-game", "Your first game"}}},
	{"Making games", []Entry{{"lua", "Lua API"}, {"2d", "2D games"}, {"project", "veduta.json"}, {"scenario", "Scenarios"}}},
	{"Assets", []Entry{{"scene", "Scenes"}, {"model", "Models"}, {"material", "Materials"}, {"texture", "Textures"}, {"prefab", "Prefabs"}, {"world", "Worlds"}, {"map", "Maps"}}},
	{"Tools", []Entry{{"inspect", "Inspection"}, {"config", "Tool settings"}}},
	{"Go games", []Entry{{"api", "Go API"}, {"vda", "Cooked assets"}}},
}

// source is a page's Markdown.
func source(topic string) (string, error) {
	if topic == "index" {
		b, err := own.ReadFile("home.md")
		return string(b), err
	}
	return docs.Get(topic)
}

var topicSuffix = regexp.MustCompile("(?m)^(# .*?) \\(`[\\w-]+`\\)[ \t]*$")

var mdLink = regexp.MustCompile(`^(?:\./)?(?:docs/)?([\w-]+)\.md(#.*)?$`)

// link points a link of a page at the site: another docs page at its .html, any other file
// of the repository at GitHub.
func link(target string) string {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
		return target
	}
	if m := mdLink.FindStringSubmatch(target); m != nil {
		if _, err := docs.Get(m[1]); err == nil {
			return m[1] + ".html" + m[2]
		}
	}
	return Repo + path.Clean(strings.TrimPrefix(strings.TrimPrefix(target, "../"), "./"))
}

// Build writes the site into dir: a page per topic, style.css.
func Build(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	css, _ := own.ReadFile("style.css")
	if err := os.WriteFile(filepath.Join(dir, "style.css"), css, 0o644); err != nil {
		return err
	}
	for _, s := range Nav {
		for _, e := range s.Pages {
			md, err := source(e.Topic)
			if err != nil {
				return err
			}
			// "# Lua games (`lua`)": the topic name is for agents asking the docs tool.
			md = topicSuffix.ReplaceAllString(md, "$1")
			p := Render(md, link)
			if err := os.WriteFile(filepath.Join(dir, e.Topic+".html"), []byte(layout(e, p)), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func layout(current Entry, p Page) string {
	var nav strings.Builder
	for _, s := range Nav {
		fmt.Fprintf(&nav, "<h3>%s</h3>\n<ul>\n", html.EscapeString(s.Title))
		for _, e := range s.Pages {
			class := ""
			if e == current {
				class = ` class="current"`
			}
			fmt.Fprintf(&nav, "<li><a href=\"%s.html\"%s>%s</a></li>\n", e.Topic, class, html.EscapeString(e.Name))
		}
		nav.WriteString("</ul>\n")
	}
	var toc strings.Builder
	for _, h := range p.Headings {
		if h.Level == 2 {
			fmt.Fprintf(&toc, "<li><a href=\"#%s\">%s</a></li>\n", h.ID, html.EscapeString(h.Text))
		}
	}
	title := p.Title
	if current.Topic != "index" {
		title = current.Name + " · Veduta"
	}
	onPage := ""
	if toc.Len() > 0 {
		onPage = "<nav class=\"toc\"><h3>On this page</h3><ul>\n" + toc.String() + "</ul></nav>\n"
	}
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title) + `</title>
<link rel="stylesheet" href="style.css">
</head>
<body>
<header><a class="brand" href="index.html">Veduta</a><a class="repo" href="https://github.com/riftbane/veduta">GitHub</a></header>
<div class="page">
<nav class="menu">
` + nav.String() + `</nav>
<main>
` + p.HTML + `</main>
` + onPage + `</div>
</body>
</html>
`
}
