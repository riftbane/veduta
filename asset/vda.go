package asset

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"math"
	"sort"
)

// VDAMagic starts every .vda file.
const VDAMagic = "VDA1"

// Chunk types (PRFB and WRLD since v1.2.0).
const (
	ChunkMeta     = "META" // Meta as canonical JSON
	ChunkMesh     = "MESH" // compiled Model (EncodeModel)
	ChunkTexture  = "TEXR" // compiled Texture (EncodeTexture)
	ChunkMaterial = "MATL" // compiled Material (EncodeMaterial)
	ChunkScene    = "SCEN" // compiled Scene (EncodeScene)
	ChunkPrefab   = "PRFB" // compiled Prefab (EncodePrefab)
	ChunkWorld    = "WRLD" // compiled World (EncodeWorld)
)

// Chunk is one chunk of a .vda file. Type is exactly 4 ASCII letters or digits.
type Chunk struct {
	Type string
	Data []byte
}

// BodyChunkType returns the chunk type holding a compiled asset of kind k.
func BodyChunkType(k Kind) (string, bool) {
	switch k {
	case KindModel:
		return ChunkMesh, true
	case KindTexture:
		return ChunkTexture, true
	case KindMaterial:
		return ChunkMaterial, true
	case KindScene:
		return ChunkScene, true
	case KindPrefab:
		return ChunkPrefab, true
	case KindWorld:
		return ChunkWorld, true
	}
	return "", false
}

func isBodyChunk(t string) bool {
	return t == ChunkMesh || t == ChunkTexture || t == ChunkMaterial || t == ChunkScene || t == ChunkPrefab || t == ChunkWorld
}

func checkChunkType(t string) error {
	if len(t) != 4 {
		return fmt.Errorf("chunk type %q must be 4 characters", t)
	}
	for i := 0; i < 4; i++ {
		c := t[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return fmt.Errorf("chunk type %q may only contain ASCII letters and digits", t)
		}
	}
	return nil
}

// chunkCRC is the CRC-32 (IEEE) of the chunk type followed by its payload.
func chunkCRC(typ string, data []byte) uint32 {
	return crc32.Update(crc32.ChecksumIEEE([]byte(typ)), crc32.IEEETable, data)
}

// WriteVDA writes a .vda file: the magic "VDA1" followed by each chunk as type (4 bytes),
// payload length (u32 little-endian), payload, and CRC-32 IEEE of type+payload (u32
// little-endian). Chunks are written in the given order.
func WriteVDA(w io.Writer, chunks []Chunk) error {
	for i, c := range chunks {
		if err := checkChunkType(c.Type); err != nil {
			return fmt.Errorf("write vda: chunk %d: %w", i, err)
		}
		if uint64(len(c.Data)) > math.MaxUint32 {
			return fmt.Errorf("write vda: chunk %d (%s): payload of %d bytes exceeds 4 GiB", i, c.Type, len(c.Data))
		}
	}
	if _, err := io.WriteString(w, VDAMagic); err != nil {
		return fmt.Errorf("write vda: %w", err)
	}
	var hdr [8]byte
	var tail [4]byte
	for _, c := range chunks {
		copy(hdr[:4], c.Type)
		binary.LittleEndian.PutUint32(hdr[4:], uint32(len(c.Data)))
		binary.LittleEndian.PutUint32(tail[:], chunkCRC(c.Type, c.Data))
		for _, b := range [][]byte{hdr[:], c.Data, tail[:]} {
			if _, err := w.Write(b); err != nil {
				return fmt.Errorf("write vda: chunk %s: %w", c.Type, err)
			}
		}
	}
	return nil
}

// ReadVDA parses a .vda file. It verifies the magic, every chunk header, length and CRC,
// and returns all chunks in file order, including types it does not know. Each
// Chunk.Data aliases data (with its capacity clipped, so appending copies).
func ReadVDA(data []byte) ([]Chunk, error) {
	if len(data) < len(VDAMagic) || string(data[:len(VDAMagic)]) != VDAMagic {
		return nil, errors.New("read vda: not a .vda file (missing \"VDA1\" magic)")
	}
	chunks := []Chunk{}
	off := len(VDAMagic)
	for off < len(data) {
		if len(data)-off < 8 {
			return nil, fmt.Errorf("read vda: chunk at offset %d: truncated header (%d bytes left, need 8)", off, len(data)-off)
		}
		typ := string(data[off : off+4])
		if err := checkChunkType(typ); err != nil {
			return nil, fmt.Errorf("read vda: chunk at offset %d: %w", off, err)
		}
		n := uint64(binary.LittleEndian.Uint32(data[off+4:]))
		if rest := uint64(len(data) - off - 8); n+4 > rest {
			return nil, fmt.Errorf("read vda: chunk %s at offset %d: length %d plus CRC exceeds the %d bytes left (truncated file?)", typ, off, n, rest)
		}
		start, end := off+8, off+8+int(n)
		payload := data[start:end:end]
		stored := binary.LittleEndian.Uint32(data[end:])
		if got := chunkCRC(typ, payload); got != stored {
			return nil, fmt.Errorf("read vda: chunk %s at offset %d: CRC mismatch (stored %08x, computed %08x): file is corrupt", typ, off, stored, got)
		}
		chunks = append(chunks, Chunk{Type: typ, Data: payload})
		off = end + 4
	}
	return chunks, nil
}

// Meta describes a compiled asset; it is stored in the META chunk of every .vda file.
type Meta struct {
	Kind       Kind     // model, texture, material, scene, prefab or world
	Name       string   // asset name
	Source     string   // source path relative to the assets directory, e.g. "models/crate.model.json"
	SourceHash string   // lowercase hex SHA-256 (64 characters) of the compiler inputs, see docs/vda.md
	Compiler   string   // CompilerVersion that produced the file
	Deps       []string // other input files (relative to the assets directory); written sorted and unique
}

// metaJSON is Meta with its fields in sorted key order, which encoding/json follows.
type metaJSON struct {
	Compiler   string   `json:"compiler"`
	Deps       []string `json:"deps"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	SourceHash string   `json:"source_hash"`
}

func (m *Meta) validate() error {
	if _, ok := BodyChunkType(m.Kind); !ok {
		return fmt.Errorf("kind %q is not cooked (want model, texture, material, scene, prefab or world)", m.Kind)
	}
	if err := ValidName(m.Name); err != nil {
		return err
	}
	if !fs.ValidPath(m.Source) || m.Source == "." {
		return fmt.Errorf("source %q is not a clean relative slash path", m.Source)
	}
	if len(m.SourceHash) != 64 {
		return fmt.Errorf("source_hash %q is not 64 hex digits", m.SourceHash)
	}
	for _, c := range m.SourceHash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return fmt.Errorf("source_hash %q is not lowercase hex", m.SourceHash)
		}
	}
	if m.Compiler == "" {
		return errors.New("compiler is empty")
	}
	for _, d := range m.Deps {
		if !fs.ValidPath(d) || d == "." {
			return fmt.Errorf("dep %q is not a clean relative slash path", d)
		}
	}
	return nil
}

// EncodeMeta validates m and returns its META chunk: canonical JSON (keys in sorted
// order, no whitespace, no HTML escaping, deps sorted and unique, [] when empty).
func EncodeMeta(m Meta) (Chunk, error) {
	if err := m.validate(); err != nil {
		return Chunk{}, fmt.Errorf("encode META: %w", err)
	}
	deps := append([]string{}, m.Deps...)
	sort.Strings(deps)
	uniq := deps[:0]
	for i, d := range deps {
		if i == 0 || d != deps[i-1] {
			uniq = append(uniq, d)
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(metaJSON{
		Compiler: m.Compiler, Deps: uniq, Kind: string(m.Kind),
		Name: m.Name, Source: m.Source, SourceHash: m.SourceHash,
	})
	if err != nil {
		return Chunk{}, fmt.Errorf("encode META: %w", err)
	}
	return Chunk{Type: ChunkMeta, Data: bytes.TrimSuffix(buf.Bytes(), []byte("\n"))}, nil
}

// DecodeMeta parses a META chunk. The JSON must be canonical (exactly what EncodeMeta
// writes) and valid.
func DecodeMeta(c Chunk) (Meta, error) {
	if c.Type != ChunkMeta {
		return Meta{}, fmt.Errorf("decode META: chunk type is %q", c.Type)
	}
	var mj metaJSON
	dec := json.NewDecoder(bytes.NewReader(c.Data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&mj); err != nil {
		return Meta{}, fmt.Errorf("decode META: %w", err)
	}
	m := Meta{Kind: Kind(mj.Kind), Name: mj.Name, Source: mj.Source, SourceHash: mj.SourceHash, Compiler: mj.Compiler, Deps: mj.Deps}
	if len(m.Deps) == 0 {
		m.Deps = nil
	}
	canon, err := EncodeMeta(m)
	if err != nil {
		return Meta{}, fmt.Errorf("decode META: %w", err)
	}
	if !bytes.Equal(canon.Data, c.Data) {
		return Meta{}, fmt.Errorf("decode META: JSON is not canonical (want %s)", canon.Data)
	}
	return m, nil
}

// PackVDA returns a complete .vda file holding meta and the compiled asset body, whose
// chunk type must match meta.Kind (see BodyChunkType). META is written first.
func PackVDA(meta Meta, body Chunk) ([]byte, error) {
	mc, err := EncodeMeta(meta)
	if err != nil {
		return nil, fmt.Errorf("pack vda: %w", err)
	}
	if want, _ := BodyChunkType(meta.Kind); body.Type != want {
		return nil, fmt.Errorf("pack vda: %s asset needs a %s chunk, got %q", meta.Kind, want, body.Type)
	}
	var buf bytes.Buffer
	if err := WriteVDA(&buf, []Chunk{mc, body}); err != nil {
		return nil, fmt.Errorf("pack vda: %w", err)
	}
	return buf.Bytes(), nil
}

// UnpackVDA reads a .vda file and returns its META and body chunk (MESH, TEXR, MATL, SCEN,
// PRFB or WRLD, matching Meta.Kind). Chunks of unknown types are skipped. A missing or repeated
// META or body chunk is an error.
func UnpackVDA(data []byte) (Meta, Chunk, error) {
	chunks, err := ReadVDA(data)
	if err != nil {
		return Meta{}, Chunk{}, fmt.Errorf("unpack vda: %w", err)
	}
	var meta Meta
	var body Chunk
	haveMeta, haveBody := false, false
	for _, c := range chunks {
		switch {
		case c.Type == ChunkMeta:
			if haveMeta {
				return Meta{}, Chunk{}, errors.New("unpack vda: more than one META chunk")
			}
			if meta, err = DecodeMeta(c); err != nil {
				return Meta{}, Chunk{}, fmt.Errorf("unpack vda: %w", err)
			}
			haveMeta = true
		case isBodyChunk(c.Type):
			if haveBody {
				return Meta{}, Chunk{}, fmt.Errorf("unpack vda: more than one body chunk (%s and %s)", body.Type, c.Type)
			}
			body, haveBody = c, true
		}
	}
	switch {
	case !haveMeta:
		return Meta{}, Chunk{}, errors.New("unpack vda: missing META chunk")
	case !haveBody:
		return Meta{}, Chunk{}, errors.New("unpack vda: missing body chunk (MESH, TEXR, MATL, SCEN, PRFB or WRLD)")
	}
	if want, _ := BodyChunkType(meta.Kind); body.Type != want {
		return Meta{}, Chunk{}, fmt.Errorf("unpack vda: META kind %s needs a %s chunk, found %s", meta.Kind, want, body.Type)
	}
	return meta, body, nil
}
