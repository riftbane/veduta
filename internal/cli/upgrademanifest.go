package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/asset"
)

// v0Defaults are the manifest defaults every engine before v1.0.0 filled in. They are
// frozen here rather than read from asset.DefaultProject, because v1.0.0 changed that for
// the console (320x240, 20 Hz): a project whose veduta.json left one of these fields to
// the default ran with the value below, and moving it across v1.0.0 would silently change
// its window, its inspection images and, through ctx.DT, its physics and every trace
// hash. Upgrade writes these values into such a manifest instead. They describe releases
// that already exist, so they never change. Listed in manifest key order.
var v0Defaults = []struct {
	key   string                          // manifest field
	value string                          // JSON text written for it
	unset func(*asset.ProjectSource) bool // the manifest leaves the field to its default
}{
	{"resolution", "[1280, 720]", func(p *asset.ProjectSource) bool { return p.Resolution == nil }},
	{"inspect_resolution", "[640, 360]", func(p *asset.ProjectSource) bool { return p.InspectResolution == nil }},
	{"tick_rate", "60", func(p *asset.ProjectSource) bool { return p.TickRate == 0 }},
}

// v0Engine reports whether version belongs to the 0.x series, whose manifest defaults are
// v0Defaults. The release candidates of v1.0.0 already have the new ones.
func v0Engine(version string) bool {
	major, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), ".")
	n, err := strconv.ParseUint(major, 10, 32)
	return err == nil && n == 0
}

// manifestField is where one top-level field of veduta.json sits in its text.
type manifestField struct {
	key              string
	lead             string // white space between the preceding "{" or "," and the key
	colon            string // text between the key and its value, such as ": "
	valStart, valEnd int    // byte offsets of the value
}

// manifestFields locates the top-level fields of a JSON object, in file order.
func manifestFields(data []byte) ([]manifestField, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("not a JSON object")
	}
	var fields []manifestField
	after := int(dec.InputOffset()) // just past the "{" or the previous value
	for dec.More() {
		// Between a value and the next key there is only white space and one comma.
		keyStart, lead := after, after
		for keyStart < len(data) && strings.IndexByte(" \t\r\n,", data[keyStart]) >= 0 {
			if data[keyStart] == ',' {
				lead = keyStart + 1
			}
			keyStart++
		}
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := t.(string)
		if !ok || keyStart >= len(data) || data[keyStart] != '"' {
			return nil, fmt.Errorf("unexpected %v at offset %d", t, keyStart)
		}
		keyEnd := int(dec.InputOffset())
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		f := manifestField{key: key, valEnd: int(dec.InputOffset())}
		f.valStart = f.valEnd - len(raw)
		if f.valStart < keyEnd || !bytes.Equal(data[f.valStart:f.valEnd], raw) {
			return nil, fmt.Errorf("cannot locate the value of %q", key)
		}
		f.lead, f.colon = string(data[lead:keyStart]), string(data[keyEnd:f.valStart])
		fields = append(fields, f)
		after = f.valEnd
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty object")
	}
	return fields, nil
}

// findField returns the index of the field the manifest decoder takes name from, or -1.
// Like encoding/json, that is the last key equal to name under Unicode case folding: the
// decoder also reads "Tick_Rate", and of a field given twice it keeps the later value.
func findField(fields []manifestField, name string) int {
	at := -1
	for i, f := range fields {
		if strings.EqualFold(f.key, name) {
			at = i
		}
	}
	return at
}

// manifestKeyOrder is the order in which veduta.json documents its fields.
func manifestKeyOrder() []string {
	t := reflect.TypeOf(asset.ProjectSource{})
	keys := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		keys = append(keys, name)
	}
	return keys
}

// upgradeManifest returns the manifest text with engine set to to and, when pin is set,
// every v0Defaults field the manifest leaves to its default written out; pinned lists
// those as `"key": value`, in key order. The text is edited in place, so formatting, key
// order and every other byte stay as they are: a zero value is replaced where it stands,
// and a missing field goes after the nearest field that precedes it in the documented key
// order, in the file's own indentation and key spacing. A key the decoder reads in another
// case (findField) is the one edited. The result is decoded again and must hold to and the
// pinned values, so a wrong edit is an error rather than a report of a pin that did not
// happen.
func upgradeManifest(data []byte, to string, pin bool) (out []byte, pinned []string, err error) {
	fields, err := manifestFields(data)
	if err != nil {
		return nil, nil, err
	}
	type edit struct {
		start, end int // replaced bytes; start == end inserts
		text       string
	}
	var edits []edit
	if i := findField(fields, "engine"); i >= 0 {
		edits = append(edits, edit{fields[i].valStart, fields[i].valEnd, strconv.Quote(to)})
	}
	if pin {
		var src asset.ProjectSource
		if err := json.Unmarshal(data, &src); err != nil {
			return nil, nil, err
		}
		order := manifestKeyOrder()
		anchors := map[string]int{} // inserted key → index in fields of the field it follows
		inserts := map[int]int{}    // index in fields → index in edits of the text after it
		for _, d := range v0Defaults {
			if !d.unset(&src) {
				continue
			}
			pinned = append(pinned, strconv.Quote(d.key)+": "+d.value)
			if i := findField(fields, d.key); i >= 0 { // present, with its zero value
				edits = append(edits, edit{fields[i].valStart, fields[i].valEnd, d.value})
				continue
			}
			a := len(fields) - 1 // after the last field when nothing precedes it
			for k := indexOf(order, d.key) - 1; k >= 0; k-- {
				if i := findField(fields, order[k]); i >= 0 {
					a = i
					break
				}
				if i, ok := anchors[order[k]]; ok {
					a = i
					break
				}
			}
			anchors[d.key] = a
			lead := fields[a].lead // the separator after the anchor, or before it when it is last
			if a+1 < len(fields) {
				lead = fields[a+1].lead
			}
			text := "," + lead + strconv.Quote(d.key) + fields[a].colon + d.value
			if e, ok := inserts[a]; ok { // after the keys already inserted there
				edits[e].text += text
				continue
			}
			inserts[a] = len(edits)
			edits = append(edits, edit{fields[a].valEnd, fields[a].valEnd, text})
		}
	}
	// Edits never overlap and no two start at the same byte: an insertion follows a value,
	// a replacement starts one.
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b bytes.Buffer
	last := 0
	for _, e := range edits {
		b.Write(data[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.Write(data[last:])
	var got asset.ProjectSource
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		return nil, nil, fmt.Errorf("the rewritten manifest does not decode: %w", err)
	}
	if got.Engine != to {
		return nil, nil, fmt.Errorf("the rewritten manifest has engine %q, want %q", got.Engine, to)
	}
	for _, d := range v0Defaults {
		if pin && d.unset(&got) {
			return nil, nil, fmt.Errorf("the rewritten manifest still leaves %q to the default", d.key)
		}
	}
	return b.Bytes(), pinned, nil
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}
