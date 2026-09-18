package asset

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"reflect"
	"strings"
	"testing"
)

var sampleChunks = []Chunk{
	{Type: "META", Data: []byte(`{"x":1}`)},
	{Type: "XTRA", Data: []byte{}},
	{Type: "MESH", Data: []byte{1, 2, 3, 4, 5}},
	{Type: "abc9", Data: bytes.Repeat([]byte{0xee}, 300)},
}

func writeVDA(t *testing.T, chunks []Chunk) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteVDA(&buf, chunks); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestVDARoundTrip(t *testing.T) {
	data := writeVDA(t, sampleChunks)
	if !bytes.Equal(data, writeVDA(t, sampleChunks)) {
		t.Fatal("WriteVDA is not deterministic")
	}
	got, err := ReadVDA(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(sampleChunks) {
		t.Fatalf("got %d chunks", len(got))
	}
	for i, c := range got {
		if c.Type != sampleChunks[i].Type || !bytes.Equal(c.Data, sampleChunks[i].Data) {
			t.Errorf("chunk %d = %s %v", i, c.Type, c.Data)
		}
		if cap(c.Data) != len(c.Data) {
			t.Errorf("chunk %d data capacity not clipped", i)
		}
	}
	empty := writeVDA(t, nil)
	if string(empty) != "VDA1" {
		t.Fatalf("empty file = %q", empty)
	}
	if cs, err := ReadVDA(empty); err != nil || len(cs) != 0 {
		t.Fatalf("empty file: %v %v", cs, err)
	}
}

// The byte layout matches docs/vda.md exactly.
func TestVDALayout(t *testing.T) {
	data := writeVDA(t, []Chunk{{Type: "MATL", Data: []byte("hi")}})
	want := []byte("VDA1MATL\x02\x00\x00\x00hi")
	want = binary.LittleEndian.AppendUint32(want, crc32.ChecksumIEEE([]byte("MATLhi")))
	if !bytes.Equal(data, want) {
		t.Fatalf("got % x\nwant % x", data, want)
	}
}

func TestVDAWriteErrors(t *testing.T) {
	for _, typ := range []string{"ME", "MESHY", "ME!H", "MÉS"} {
		if err := WriteVDA(&bytes.Buffer{}, []Chunk{{Type: typ}}); err == nil {
			t.Errorf("chunk type %q accepted", typ)
		}
	}
	err := WriteVDA(failWriter{}, sampleChunks)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("writer error not propagated: %v", err)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestVDACorruption(t *testing.T) {
	data := writeVDA(t, sampleChunks)
	// Flipping any bit after the magic is detected (CRC, type or length check).
	for i := 4; i < len(data); i++ {
		bad := append([]byte(nil), data...)
		bad[i] ^= 0x10
		if _, err := ReadVDA(bad); err == nil {
			t.Fatalf("corruption at byte %d not detected", i)
		}
	}
	bad := append([]byte(nil), data...)
	bad[len(bad)-1] ^= 1
	if _, err := ReadVDA(bad); err == nil || !strings.Contains(err.Error(), "CRC mismatch") {
		t.Fatalf("CRC error = %v", err)
	}
	for _, magic := range []string{"", "VDA", "VDA2", "PNG\x00"} {
		if _, err := ReadVDA([]byte(magic)); err == nil || !strings.Contains(err.Error(), "magic") {
			t.Errorf("magic %q: %v", magic, err)
		}
	}
}

func TestVDATruncation(t *testing.T) {
	data := writeVDA(t, sampleChunks)
	boundaries := map[int]int{4: 0} // offset → chunks complete before it
	off := 4
	for i, c := range sampleChunks {
		off += 12 + len(c.Data)
		boundaries[off] = i + 1
	}
	for n := 4; n < len(data); n++ {
		chunks, err := ReadVDA(data[:n])
		if want, ok := boundaries[n]; ok {
			if err != nil || len(chunks) != want {
				t.Fatalf("cut at chunk boundary %d: %d chunks, %v", n, len(chunks), err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("truncation at %d of %d not detected", n, len(data))
		}
	}
}

const testHash = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

func testMeta() Meta {
	return Meta{Kind: KindMaterial, Name: "crate_wood", Source: "materials/crate_wood.vmat",
		SourceHash: testHash, Compiler: CompilerVersion}
}

func TestMetaCanonical(t *testing.T) {
	m := testMeta()
	m.Deps = []string{"textures/src/b.png", "textures/src/a&b<c>.png", "textures/src/b.png"}
	c, err := EncodeMeta(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"compiler":"veduta-asset/0.5.0","deps":["textures/src/a&b<c>.png","textures/src/b.png"],"kind":"material","name":"crate_wood","source":"materials/crate_wood.vmat","source_hash":"` + testHash + `"}`
	if c.Type != ChunkMeta || string(c.Data) != want {
		t.Fatalf("META = %s\nwant   %s", c.Data, want)
	}
	back, err := DecodeMeta(c)
	if err != nil {
		t.Fatal(err)
	}
	m.Deps = []string{"textures/src/a&b<c>.png", "textures/src/b.png"}
	if !reflect.DeepEqual(back, m) {
		t.Fatalf("round trip %+v", back)
	}
	c, _ = EncodeMeta(testMeta())
	if !strings.Contains(string(c.Data), `"deps":[]`) {
		t.Fatalf("empty deps: %s", c.Data)
	}
	if back, err := DecodeMeta(c); err != nil || back.Deps != nil {
		t.Fatalf("empty deps round trip: %+v %v", back, err)
	}
}

func TestMetaErrors(t *testing.T) {
	bad := []func(*Meta){
		func(m *Meta) { m.Kind = KindScenario },
		func(m *Meta) { m.Name = "Crate" },
		func(m *Meta) { m.Source = "/abs/x.vmat" },
		func(m *Meta) { m.Source = "" },
		func(m *Meta) { m.SourceHash = "abc" },
		func(m *Meta) { m.SourceHash = strings.ToUpper(testHash) },
		func(m *Meta) { m.Compiler = "" },
		func(m *Meta) { m.Deps = []string{"../x.png"} },
	}
	for i, f := range bad {
		m := testMeta()
		f(&m)
		if _, err := EncodeMeta(m); err == nil {
			t.Errorf("case %d accepted: %+v", i, m)
		}
	}
	good, _ := EncodeMeta(testMeta())
	for _, data := range []string{
		strings.Replace(string(good.Data), ",", ", ", 1),                // whitespace: not canonical
		strings.Replace(string(good.Data), `"deps":[],`, "", 1),         // missing key: not canonical
		strings.Replace(string(good.Data), `{`, `{"extra":1,`, 1),       // unknown field
		strings.Replace(string(good.Data), `"material"`, `"shader"`, 1), // bad kind
		`[]`,
	} {
		if _, err := DecodeMeta(Chunk{Type: ChunkMeta, Data: []byte(data)}); err == nil {
			t.Errorf("DecodeMeta accepted %s", data)
		}
	}
	if _, err := DecodeMeta(Chunk{Type: "MESH", Data: good.Data}); err == nil {
		t.Error("DecodeMeta accepted a MESH chunk")
	}
}

// The META example in docs/vda.md is canonical.
func TestMetaDocExample(t *testing.T) {
	for _, ex := range docJSON(t, "vda.md") {
		m, err := DecodeMeta(Chunk{Type: ChunkMeta, Data: []byte(ex)})
		if err != nil {
			t.Fatalf("docs/vda.md META example: %v", err)
		}
		if m.Kind != KindModel || m.Compiler != CompilerVersion {
			t.Fatalf("example %+v", m)
		}
	}
}

func TestPackUnpackVDA(t *testing.T) {
	meta := testMeta()
	body := Chunk{Type: ChunkMaterial, Data: []byte{9, 8, 7}}
	data, err := PackVDA(meta, body)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := PackVDA(meta, body); !bytes.Equal(data, again) {
		t.Fatal("PackVDA is not deterministic")
	}
	gm, gb, err := UnpackVDA(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gm, meta) || gb.Type != body.Type || !bytes.Equal(gb.Data, body.Data) {
		t.Fatalf("unpacked %+v %+v", gm, gb)
	}
	if _, err := PackVDA(meta, Chunk{Type: ChunkMesh}); err == nil {
		t.Fatal("PackVDA accepted a body of the wrong kind")
	}

	mc, _ := EncodeMeta(meta)
	unknown := Chunk{Type: "XTRA", Data: []byte("future")}
	// Unknown chunks are skipped and order does not matter.
	gm, gb, err = UnpackVDA(writeVDA(t, []Chunk{unknown, body, unknown, mc}))
	if err != nil || gm.Name != meta.Name || !bytes.Equal(gb.Data, body.Data) {
		t.Fatalf("with unknown chunks: %+v %+v %v", gm, gb, err)
	}
	mesh := Chunk{Type: ChunkMesh}
	for name, chunks := range map[string][]Chunk{
		"missing META":   {body, unknown},
		"missing body":   {mc, unknown},
		"duplicate META": {mc, mc, body},
		"duplicate body": {mc, body, body},
		"kind mismatch":  {mc, mesh},
		"bad META":       {{Type: ChunkMeta, Data: []byte("{}")}, body},
	} {
		if _, _, err := UnpackVDA(writeVDA(t, chunks)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, _, err := UnpackVDA(data[:len(data)-1]); err == nil {
		t.Error("truncated file accepted")
	}
}
