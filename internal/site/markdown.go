// Package site builds the documentation website from docs/*.md: the same pages the docs
// tool gives agents, as HTML for people, with a navigation and the links between pages
// working. It reads the subset of Markdown the docs are written in: ATX headings, fenced
// code, pipe tables, bulleted and numbered lists with indented continuation lines,
// paragraphs, and inline code, bold, italic, links and autolinks.
package site

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Heading is a heading of a page, for the page's own contents.
type Heading struct {
	Level int
	Text  string
	ID    string
}

// Page is one rendered page.
type Page struct {
	Title    string
	HTML     string
	Headings []Heading
}

// Render converts Markdown to HTML. link rewrites a link's target (a page's .md name to its
// .html one, for example).
func Render(md string, link func(string) string) Page {
	r := &renderer{link: link, ids: map[string]int{}}
	r.blocks(strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n"))
	return Page{Title: r.title, HTML: r.b.String(), Headings: r.headings}
}

type renderer struct {
	b        strings.Builder
	link     func(string) string
	title    string
	headings []Heading
	ids      map[string]int
}

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	bulletRe  = regexp.MustCompile(`^(\s*)([-*]|\d+\.)\s+(.*)$`)
	fenceRe   = regexp.MustCompile("^(\\s*)(```+|~~~+)\\s*([\\w+-]*)")
	tableSep  = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$`)
)

func (r *renderer) blocks(lines []string) {
	for i := 0; i < len(lines); {
		line := lines[i]
		switch {
		case strings.TrimSpace(line) == "":
			i++
		case fenceRe.MatchString(line):
			i = r.code(lines, i)
		case headingRe.MatchString(line):
			m := headingRe.FindStringSubmatch(line)
			r.heading(len(m[1]), m[2])
			i++
		case strings.HasPrefix(strings.TrimSpace(line), "|") && i+1 < len(lines) && tableSep.MatchString(lines[i+1]):
			i = r.table(lines, i)
		case bulletRe.MatchString(line):
			i = r.list(lines, i)
		default:
			i = r.paragraph(lines, i)
		}
	}
}

func (r *renderer) code(lines []string, i int) int {
	m := fenceRe.FindStringSubmatch(lines[i])
	indent, fence, lang := len(m[1]), m[2], m[3]
	var body []string
	i++
	for ; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), fence[:3]) && strings.TrimSpace(strings.Trim(strings.TrimSpace(lines[i]), fence[:1])) == "" {
			i++
			break
		}
		l := lines[i]
		if len(l)-len(strings.TrimLeft(l, " ")) >= indent {
			l = l[indent:]
		}
		body = append(body, l)
	}
	class := ""
	if lang != "" {
		class = fmt.Sprintf(` class="language-%s"`, html.EscapeString(lang))
	}
	fmt.Fprintf(&r.b, "<pre><code%s>%s</code></pre>\n", class, html.EscapeString(strings.Join(body, "\n")))
	return i
}

func (r *renderer) heading(level int, text string) {
	plain := stripInline(text)
	id := slug(plain)
	if n := r.ids[id]; n > 0 {
		r.ids[id] = n + 1
		id = fmt.Sprintf("%s-%d", id, n)
	} else {
		r.ids[id] = 1
	}
	if level == 1 && r.title == "" {
		r.title = plain
	}
	r.headings = append(r.headings, Heading{Level: level, Text: plain, ID: id})
	fmt.Fprintf(&r.b, "<h%d id=\"%s\">%s</h%d>\n", level, id, r.inline(text), level)
}

func (r *renderer) table(lines []string, i int) int {
	cells := func(l string) []string {
		l = strings.TrimSpace(l)
		l = strings.TrimSuffix(strings.TrimPrefix(l, "|"), "|")
		// A pipe inside code or escaped is part of its cell.
		var out []string
		var cur strings.Builder
		inCode := false
		for j := 0; j < len(l); j++ {
			switch {
			case l[j] == '\\' && j+1 < len(l) && l[j+1] == '|':
				cur.WriteByte('|')
				j++
			case l[j] == '`':
				inCode = !inCode
				cur.WriteByte('`')
			case l[j] == '|' && !inCode:
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			default:
				cur.WriteByte(l[j])
			}
		}
		return append(out, strings.TrimSpace(cur.String()))
	}
	r.b.WriteString("<div class=\"table\"><table>\n<thead><tr>")
	for _, c := range cells(lines[i]) {
		fmt.Fprintf(&r.b, "<th>%s</th>", r.inline(c))
	}
	r.b.WriteString("</tr></thead>\n<tbody>\n")
	i += 2
	for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
		r.b.WriteString("<tr>")
		for _, c := range cells(lines[i]) {
			fmt.Fprintf(&r.b, "<td>%s</td>", r.inline(c))
		}
		r.b.WriteString("</tr>\n")
	}
	r.b.WriteString("</tbody></table></div>\n")
	return i
}

// list renders the list starting at lines[i]: its items, each with the lines indented
// under it, which may hold paragraphs, code and nested lists.
func (r *renderer) list(lines []string, i int) int {
	m := bulletRe.FindStringSubmatch(lines[i])
	indent := len(m[1])
	ordered := m[2] != "-" && m[2] != "*"
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	fmt.Fprintf(&r.b, "<%s>\n", tag)
	for i < len(lines) {
		m := bulletRe.FindStringSubmatch(lines[i])
		if m == nil || len(m[1]) != indent || (m[2] != "-" && m[2] != "*") == !ordered {
			break
		}
		body := []string{m[3]}
		contentIndent := indent + len(m[2]) + 1
		i++
		for i < len(lines) {
			l := lines[i]
			if strings.TrimSpace(l) == "" {
				// A blank line ends the item unless indented content follows it.
				if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" && leading(lines[i+1]) >= contentIndent {
					body = append(body, "")
					i++
					continue
				}
				break
			}
			if n := leading(l); n < contentIndent && (n <= indent || bulletRe.MatchString(l)) {
				break // a sibling, or the end of the list
			}
			body = append(body, l[min(leading(l), contentIndent):])
			i++
		}
		r.b.WriteString("<li>")
		sub := &renderer{link: r.link, ids: r.ids}
		if strings.Contains(strings.Join(body, "\n"), "\n\n") {
			sub.blocks(body) // a loose item: paragraphs
		} else {
			// A tight item: its text inline, then whatever block follows it (a nested list).
			n := 1
			for n < len(body) && !hasBlock(body[n:n+1]) {
				n++
			}
			sub.b.WriteString(r.inline(strings.Join(body[:n], "\n")))
			sub.blocks(body[n:])
		}
		r.b.WriteString(sub.b.String())
		r.b.WriteString("</li>\n")
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" && i+1 < len(lines) && bulletRe.MatchString(lines[i+1]) && leading(lines[i+1]) == indent {
			i++
		}
	}
	fmt.Fprintf(&r.b, "</%s>\n", tag)
	return i
}

func hasBlock(lines []string) bool {
	for _, l := range lines {
		if bulletRe.MatchString(l) || fenceRe.MatchString(l) || strings.HasPrefix(strings.TrimSpace(l), "|") {
			return true
		}
	}
	return false
}

func leading(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }

func (r *renderer) paragraph(lines []string, i int) int {
	var body []string
	for ; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" || headingRe.MatchString(l) || fenceRe.MatchString(l) || (len(body) > 0 && bulletRe.MatchString(l) && leading(l) == 0) {
			break
		}
		body = append(body, strings.TrimSpace(l))
	}
	fmt.Fprintf(&r.b, "<p>%s</p>\n", r.inline(strings.Join(body, "\n")))
	return i
}

var (
	codeSpan = regexp.MustCompile("`+")
	linkRe   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	autoRe   = regexp.MustCompile(`&lt;(https?://[^&\s]+)&gt;`)
	boldRe   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	italicRe = regexp.MustCompile(`(^|[\s(])[*_]([^*_\s][^*_]*?)[*_]([\s).,;:!?]|$)`)
)

// inline renders the inline markup of a text: code spans first, whose content is literal,
// then links, bold and italic in the rest.
func (r *renderer) inline(text string) string {
	var out strings.Builder
	for text != "" {
		loc := codeSpan.FindStringIndex(text)
		if loc == nil {
			out.WriteString(r.span(text))
			break
		}
		ticks := text[loc[0]:loc[1]]
		end := strings.Index(text[loc[1]:], ticks)
		if end < 0 {
			out.WriteString(r.span(text))
			break
		}
		out.WriteString(r.span(text[:loc[0]]))
		code := strings.TrimSpace(text[loc[1] : loc[1]+end])
		fmt.Fprintf(&out, "<code>%s</code>", html.EscapeString(code))
		text = text[loc[1]+end+len(ticks):]
	}
	return out.String()
}

func (r *renderer) span(s string) string {
	// Links are found before escaping, and their text is rendered as inline markup too.
	var out strings.Builder
	for s != "" {
		loc := linkRe.FindStringSubmatchIndex(s)
		if loc == nil {
			out.WriteString(emphasis(html.EscapeString(s)))
			break
		}
		out.WriteString(emphasis(html.EscapeString(s[:loc[0]])))
		target := s[loc[4]:loc[5]]
		if r.link != nil {
			target = r.link(target)
		}
		fmt.Fprintf(&out, `<a href="%s">%s</a>`, html.EscapeString(target), r.inline(s[loc[2]:loc[3]]))
		s = s[loc[1]:]
	}
	return out.String()
}

func emphasis(s string) string {
	s = autoRe.ReplaceAllString(s, `<a href="$1">$1</a>`)
	s = boldRe.ReplaceAllString(s, "<strong>$1</strong>")
	return italicRe.ReplaceAllString(s, "$1<em>$2</em>$3")
}

// stripInline is a heading's text without its markup, for its title and anchor.
func stripInline(s string) string {
	s = linkRe.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("`", "", "**", "").Replace(s)
	return s
}

// slug makes an anchor as GitHub does: lower case, letters, digits, spaces as dashes and
// dashes kept, everything else dropped.
func slug(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		switch {
		case c == ' ':
			b.WriteByte('-')
		case c == '-' || c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c > 127 && isLetter(c):
			b.WriteRune(c)
		}
	}
	return b.String()
}

func isLetter(c rune) bool {
	return strings.ContainsRune("àáâäèéêëìíîïòóôöùúûüçñ", c)
}
