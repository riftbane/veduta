package site

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	md := "# Title `x`\n\nA *para* with **bold**, `a|b <c>` and [a link](lua.md#the-game).\nSecond line.\n\n" +
		"## Same\n\n## Same\n\n" +
		"| Key | Does |\n|-----|------|\n| `F1` | shows \\| things |\n\n" +
		"- one\n  continued\n- two\n  - nested\n\n" +
		"1. first\n\n   ```lua\n   local x = 1\n   ```\n2. second\n\n" +
		"```json\n{ \"a\": \"<b>\" }\n```\n\nSee <https://example.com>.\n"
	p := Render(md, func(s string) string { return strings.Replace(s, ".md", ".html", 1) })
	for _, want := range []string{
		`<h1 id="title-x">Title <code>x</code></h1>`,
		`<p>A <em>para</em> with <strong>bold</strong>, <code>a|b &lt;c&gt;</code> and <a href="lua.html#the-game">a link</a>.` + "\nSecond line.</p>",
		`<h2 id="same">Same</h2>`, `<h2 id="same-1">Same</h2>`,
		`<th>Key</th><th>Does</th>`, `<td><code>F1</code></td><td>shows | things</td>`,
		"<ul>\n<li>one\ncontinued</li>\n<li>two<ul>\n<li>nested</li>\n</ul>\n</li>\n</ul>",
		"<ol>\n<li><p>first</p>\n<pre><code class=\"language-lua\">local x = 1</code></pre>\n</li>\n<li>second</li>\n</ol>",
		`<pre><code class="language-json">{ &#34;a&#34;: &#34;&lt;b&gt;&#34; }</code></pre>`,
		`<a href="https://example.com">https://example.com</a>`,
	} {
		if !strings.Contains(p.HTML, want) {
			t.Errorf("missing %q in\n%s", want, p.HTML)
		}
	}
	if p.Title != "Title x" || len(p.Headings) != 3 {
		t.Errorf("title %q headings %+v", p.Title, p.Headings)
	}
}
