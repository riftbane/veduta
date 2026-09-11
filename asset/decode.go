package asset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Decode strictly decodes the JSON source data of file into v (a pointer to one of the
// *Source types) after checking that its "veduta" header equals header (for example
// TypeModel). Unknown fields, duplicate keys, wrong types, trailing data and a missing
// or different header are errors located by line and column.
//
// The returned Locator maps JSON paths to positions for later validation errors.
func Decode(file string, data []byte, header string, v any) (*Locator, error) {
	loc, err := NewLocator(file, data)
	if err != nil {
		return loc, err
	}
	if loc.rootKind != '{' {
		return loc, loc.Errorf("", "source must be a JSON object")
	}
	var h struct {
		Veduta *string `json:"veduta"`
	}
	_ = json.Unmarshal(data, &h)
	switch {
	case h.Veduta == nil:
		return loc, loc.Errorf("", "missing \"veduta\" header (want %q)", header)
	case *h.Veduta != header:
		return loc, loc.Errorf("veduta", "header is %q, want %q", *h.Veduta, header)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return loc, loc.convert(err)
	}
	return loc, nil
}

// Locator maps JSON paths such as `parts[2].radius` to line and column positions in a
// source file. The root path is "".
type Locator struct {
	file     string
	data     []byte
	lines    []int          // byte offset of each line start
	pos      map[string]int // path → offset of the value
	byOff    map[int]string // offset of a value → its path (innermost value wins)
	keys     []keyPos       // every object key, in file order
	rootKind byte
}

type keyPos struct {
	name   string
	path   string // path of the value the key introduces
	offset int
}

// NewLocator indexes data. It fails with a located error when data is not valid JSON or
// contains duplicate object keys.
func NewLocator(file string, data []byte) (*Locator, error) {
	l := &Locator{file: file, data: data, lines: []int{0}, pos: map[string]int{}, byOff: map[int]string{}}
	for i, b := range data {
		if b == '\n' {
			l.lines = append(l.lines, i+1)
		}
	}
	if err := l.index(); err != nil {
		return l, err
	}
	return l, nil
}

// File returns the file name the locator reports.
func (l *Locator) File() string { return l.file }

type frame struct {
	path  string
	array bool
	index int
	key   string // pending object key
	seen  map[string]bool
}

func (l *Locator) index() error {
	dec := json.NewDecoder(bytes.NewReader(l.data))
	dec.UseNumber()
	var stack []*frame
	next := func() int { // offset of the next token start
		o := int(dec.InputOffset())
		for o < len(l.data) {
			switch l.data[o] {
			case ' ', '\t', '\r', '\n', ',', ':':
				o++
				continue
			}
			break
		}
		return o
	}
	valuePath := func() string {
		if len(stack) == 0 {
			return ""
		}
		f := stack[len(stack)-1]
		if f.array {
			return f.path + "[" + strconv.Itoa(f.index) + "]"
		}
		if f.path == "" {
			return f.key
		}
		return f.path + "." + f.key
	}
	afterValue := func() {
		if len(stack) == 0 {
			return
		}
		f := stack[len(stack)-1]
		if f.array {
			f.index++
		} else {
			f.key = ""
		}
	}
	rootDone := false
	for {
		if rootDone && dec.More() {
			return l.errAt(next(), "unexpected data after the JSON value")
		}
		start := next()
		tok, err := dec.Token()
		if err == io.EOF {
			if len(stack) != 0 {
				return l.errAt(len(l.data), "unexpected end of JSON input")
			}
			return nil
		}
		if err != nil {
			return l.convert(err)
		}
		// An object key?
		if len(stack) > 0 {
			if f := stack[len(stack)-1]; !f.array && f.key == "" {
				if d, ok := tok.(json.Delim); ok && d == '}' {
					stack = stack[:len(stack)-1]
					afterValue()
					rootDone = len(stack) == 0
					continue
				}
				name := tok.(string)
				if f.seen[name] {
					return l.errAt(start, fmt.Sprintf("duplicate key %q", name))
				}
				f.seen[name] = true
				f.key = name
				l.keys = append(l.keys, keyPos{name: name, path: valuePath(), offset: start})
				continue
			}
		}
		switch d := tok.(type) {
		case json.Delim:
			switch d {
			case '{', '[':
				p := valuePath()
				l.pos[p] = start
				l.byOff[start] = p
				if len(stack) == 0 {
					l.rootKind = byte(d)
				}
				stack = append(stack, &frame{path: p, array: d == '[', seen: map[string]bool{}})
			case ']':
				stack = stack[:len(stack)-1]
				afterValue()
			}
		default:
			vp := valuePath()
			l.pos[vp] = start
			l.byOff[start] = vp
			if len(stack) == 0 {
				l.rootKind = 'v'
			}
			afterValue()
		}
		rootDone = len(stack) == 0
	}
}

// Pos returns the 1-based line and column of the value at path. Unknown paths fall back
// to the closest existing ancestor, and finally to the start of the file.
func (l *Locator) Pos(path string) (line, col int) {
	for {
		if o, ok := l.pos[path]; ok {
			return l.lineCol(o)
		}
		if path == "" {
			return 1, 1
		}
		path = parentPath(path)
	}
}

// Has reports whether the source contains a value at path.
func (l *Locator) Has(path string) bool {
	_, ok := l.pos[path]
	return ok
}

// Errorf returns a SourceError located at the value at path.
func (l *Locator) Errorf(path, format string, args ...any) *SourceError {
	line, col := l.Pos(path)
	msg := fmt.Sprintf(format, args...)
	if path != "" {
		msg = path + ": " + msg
	}
	return &SourceError{File: l.file, Line: line, Col: col, Msg: msg}
}

func (l *Locator) errAt(offset int, msg string) *SourceError {
	line, col := l.lineCol(offset)
	return &SourceError{File: l.file, Line: line, Col: col, Msg: msg}
}

func (l *Locator) lineCol(offset int) (int, int) {
	i := sort.Search(len(l.lines), func(i int) bool { return l.lines[i] > offset }) - 1
	if i < 0 {
		i = 0
	}
	return i + 1, offset - l.lines[i] + 1
}

// convert turns encoding/json errors into located SourceErrors.
func (l *Locator) convert(err error) error {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		// Go releases disagree by one on where Offset points; normalize to the first
		// non-blank byte at or after offset-1 so positions do not depend on the toolchain.
		off := int(syn.Offset) - 1
		if off < 0 {
			off = 0
		}
		for off < len(l.data) && (l.data[off] == ' ' || l.data[off] == '\t' || l.data[off] == '\r' || l.data[off] == '\n') {
			off++
		}
		return l.errAt(off, "invalid JSON: "+strings.TrimPrefix(syn.Error(), "json: "))
	case errors.As(err, &typ):
		// Field is a dotted Go-side path without array indices; the offset is exact.
		off := int(typ.Offset)
		if off > 0 {
			off-- // Offset points just past the offending value
		}
		for off > 0 && off < len(l.data) && (l.data[off] == ' ' || l.data[off] == '\n' || l.data[off] == '\t' || l.data[off] == '\r') {
			off--
		}
		// The path comes from our own index (Go releases differ in whether Field carries
		// array indices), falling back to encoding/json's field path.
		start := l.valueStartBefore(off)
		field, ok := l.byOff[start]
		if !ok {
			field = goPath(typ.Field)
		}
		if field == "" {
			field = "value"
		}
		return l.errAt(start, fmt.Sprintf("%s: cannot use JSON %s as %s", field, typ.Value, typ.Type))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		name, _ := strconv.Unquote(strings.TrimPrefix(err.Error(), "json: unknown field "))
		for _, k := range l.keys {
			if k.name == name {
				e := l.errAt(k.offset, fmt.Sprintf("unknown field %q", name))
				e.Msg = k.path + ": unknown field"
				return e
			}
		}
		return &SourceError{File: l.file, Msg: fmt.Sprintf("unknown field %q", name)}
	case errors.Is(err, io.ErrUnexpectedEOF):
		return l.errAt(len(l.data), "unexpected end of JSON input")
	}
	return &SourceError{File: l.file, Msg: err.Error()}
}

// valueStartBefore returns the offset of the indexed value that starts closest before
// (or at) off.
func (l *Locator) valueStartBefore(off int) int {
	best := -1
	for _, o := range l.pos {
		if o <= off && o > best {
			best = o
		}
	}
	if best < 0 {
		return off
	}
	return best
}

// goPath converts encoding/json's field path ("parts.0.shape") to Veduta's
// ("parts[0].shape").
func goPath(f string) string {
	var b strings.Builder
	for i, seg := range strings.Split(f, ".") {
		if _, err := strconv.Atoi(seg); err == nil {
			b.WriteString("[" + seg + "]")
			continue
		}
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(seg)
	}
	return b.String()
}

// parentPath strips the last segment of a path: "a.b[2]" → "a.b", "a.b" → "a".
func parentPath(p string) string {
	if strings.HasSuffix(p, "]") {
		if i := strings.LastIndexByte(p, '['); i >= 0 {
			return p[:i]
		}
	}
	if i := strings.LastIndexByte(p, '.'); i >= 0 {
		return p[:i]
	}
	return ""
}

// Path joins a parent path and a child key or index: Path("parts", 2) = "parts[2]",
// Path("parts[2]", "radius") = "parts[2].radius".
func Path(parent string, child any) string {
	switch c := child.(type) {
	case int:
		return parent + "[" + strconv.Itoa(c) + "]"
	case string:
		if parent == "" {
			return c
		}
		return parent + "." + c
	}
	panic(fmt.Sprintf("asset.Path: bad child %T", child))
}
