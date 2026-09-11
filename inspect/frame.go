// Package inspect produces the numeric reports and small image sheets that let an agent
// see what it built: model, texture and scene reports, image diffs, and ID-buffer
// queries on frame bundles.
package inspect

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// A frame bundle (.vframe) holds one rendered frame with everything needed to answer
// queries without re-rendering: color, depth, entity ids, optional normals, the camera,
// and per-entity projected coverage. Layout: magic "VFR1", then chunks
// (type [4]byte, length u32, payload, crc32 IEEE of type+payload), little-endian:
//
//	FRAM  JSON header (FrameHeader), canonical key order
//	COLR  width*height u32 BGRA8 colors, rows top to bottom
//	DPTH  width*height f32 window depth (1 = background)
//	IDBF  width*height u32 entity ids (0 = none)
//	NRML  width*height u32 packed normals 0xFFRRGGBB, n*0.5+0.5 (optional; 0 = none)
const frameMagic = "VFR1"

// FrameHeader describes a bundled frame.
type FrameHeader struct {
	Width    int           `json:"width"`
	Height   int           `json:"height"`
	Scene    string        `json:"scene"`
	Tick     uint64        `json:"tick"`
	Seed     uint64        `json:"seed"`
	Mode     string        `json:"mode"`
	Camera   FrameCamera   `json:"camera"`
	View     gmath.Mat4    `json:"view"`
	Proj     gmath.Mat4    `json:"proj"`
	Entities []FrameEntity `json:"entities"`
}

// FrameCamera is the camera of a bundled frame.
type FrameCamera struct {
	Name     string     `json:"name"`
	Ortho    bool       `json:"ortho"`
	FovDeg   float32    `json:"fov_deg"`
	Size     float32    `json:"size"`
	Near     float32    `json:"near"`
	Far      float32    `json:"far"`
	Position gmath.Vec3 `json:"position"`
	Target   gmath.Vec3 `json:"target"`
}

// FrameEntity is an entity drawn in the frame with its projected (unoccluded) coverage.
type FrameEntity struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Projected int    `json:"projected"` // pixels the entity covers when drawn alone
	BBox      [4]int `json:"bbox"`      // projected screen bounds x0, y0, x1, y1 (exclusive); zeros when not on screen
}

// Frame is a decoded frame bundle.
type Frame struct {
	FrameHeader
	Color  []uint32
	Depth  []float32
	ID     []uint32
	Normal []uint32 // nil when not bundled
}

// Image returns the color buffer as an image (sharing memory).
func (f *Frame) Image() *gfx.Image { return &gfx.Image{W: f.Width, H: f.Height, Pix: f.Color} }

// Encode serializes the bundle.
func (f *Frame) Encode() ([]byte, error) {
	n := f.Width * f.Height
	if f.Width <= 0 || f.Height <= 0 || len(f.Color) != n || len(f.Depth) != n || len(f.ID) != n || (f.Normal != nil && len(f.Normal) != n) {
		return nil, errors.New("frame: buffers do not match the size")
	}
	var buf bytes.Buffer
	buf.WriteString(frameMagic)
	hdr, err := json.Marshal(f.FrameHeader)
	if err != nil {
		return nil, err
	}
	writeChunk(&buf, "FRAM", hdr)
	writeChunk(&buf, "COLR", u32Bytes(f.Color))
	depth := make([]uint32, n)
	for i, d := range f.Depth {
		depth[i] = math.Float32bits(d)
	}
	writeChunk(&buf, "DPTH", u32Bytes(depth))
	writeChunk(&buf, "IDBF", u32Bytes(f.ID))
	if f.Normal != nil {
		writeChunk(&buf, "NRML", u32Bytes(f.Normal))
	}
	return buf.Bytes(), nil
}

func writeChunk(w io.Writer, typ string, payload []byte) {
	var hdr [8]byte
	copy(hdr[:4], typ)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(len(payload)))
	w.Write(hdr[:])
	w.Write(payload)
	crc := crc32.NewIEEE()
	crc.Write(hdr[:4])
	crc.Write(payload)
	var c [4]byte
	binary.LittleEndian.PutUint32(c[:], crc.Sum32())
	w.Write(c[:])
}

func u32Bytes(v []uint32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], x)
	}
	return b
}

func bytesU32(b []byte, n int) ([]uint32, error) {
	if len(b) != 4*n {
		return nil, fmt.Errorf("frame: buffer has %d bytes, want %d", len(b), 4*n)
	}
	v := make([]uint32, n)
	for i := range v {
		v[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	return v, nil
}

// DecodeFrame parses a frame bundle, verifying magic, chunk lengths and CRCs.
func DecodeFrame(data []byte) (*Frame, error) {
	if len(data) < 4 || string(data[:4]) != frameMagic {
		return nil, errors.New("frame: not a .vframe bundle (bad magic)")
	}
	chunks := map[string][]byte{}
	off := 4
	for off < len(data) {
		if len(data)-off < 12 {
			return nil, errors.New("frame: truncated chunk header")
		}
		typ := string(data[off : off+4])
		n := int(binary.LittleEndian.Uint32(data[off+4:]))
		if n < 0 || n > len(data)-off-12 {
			return nil, fmt.Errorf("frame: chunk %s length %d exceeds the file", typ, n)
		}
		payload := data[off+8 : off+8+n]
		want := binary.LittleEndian.Uint32(data[off+8+n:])
		crc := crc32.NewIEEE()
		crc.Write(data[off : off+4])
		crc.Write(payload)
		if crc.Sum32() != want {
			return nil, fmt.Errorf("frame: chunk %s CRC mismatch", typ)
		}
		chunks[typ] = payload
		off += 12 + n
	}
	f := &Frame{}
	hdr, ok := chunks["FRAM"]
	if !ok {
		return nil, errors.New("frame: missing FRAM header")
	}
	if err := json.Unmarshal(hdr, &f.FrameHeader); err != nil {
		return nil, fmt.Errorf("frame: header: %w", err)
	}
	n := f.Width * f.Height
	if f.Width <= 0 || f.Height <= 0 || n > 8192*8192 {
		return nil, fmt.Errorf("frame: bad size %dx%d", f.Width, f.Height)
	}
	var err error
	if f.Color, err = bytesU32(chunks["COLR"], n); err != nil {
		return nil, err
	}
	depth, err := bytesU32(chunks["DPTH"], n)
	if err != nil {
		return nil, err
	}
	f.Depth = make([]float32, n)
	for i, d := range depth {
		f.Depth[i] = math.Float32frombits(d)
	}
	if f.ID, err = bytesU32(chunks["IDBF"], n); err != nil {
		return nil, err
	}
	if nb, ok := chunks["NRML"]; ok {
		if f.Normal, err = bytesU32(nb, n); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// WriteFrame writes a bundle file.
func WriteFrame(path string, f *Frame) error {
	data, err := f.Encode()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ReadFrame reads a bundle file.
func ReadFrame(path string) (*Frame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := DecodeFrame(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}
