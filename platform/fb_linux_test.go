package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gfx"
)

// fakeSys builds a sysfs tree of framebuffers and points sysRoot at it. Each entry is
// "node name WxH bpp[ stride]".
func fakeSys(t *testing.T, entries ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, e := range entries {
		f := strings.Fields(e)
		if len(f) < 4 {
			t.Fatalf("bad fixture %q", e)
		}
		dir := filepath.Join(root, "sys", "class", "graphics", f[0])
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		size := strings.ReplaceAll(f[2], "x", ",")
		write := func(name, value string) {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("name", f[1])
		write("virtual_size", size)
		write("bits_per_pixel", f[3])
		if len(f) > 4 {
			write("stride", f[4])
		}
	}
	old := sysRoot
	sysRoot = root
	t.Cleanup(func() { sysRoot = old })
	return root
}

func TestFindFramebuffer(t *testing.T) {
	for _, c := range []struct {
		name    string
		entries []string
		want    string
		node    string
		errHas  string
	}{
		{
			// The panel is the 16-bit one; HDMI on the same board is 32-bit.
			name:    "panel beside HDMI",
			entries: []string{"fb0 vc4drmfb 1920x1080 32", "fb1 mi0283qtdrmfb 320x240 16"},
			node:    "fb1",
		},
		{
			// Probe order is not fixed: the panel can come first.
			name:    "panel first",
			entries: []string{"fb0 mi0283qtdrmfb 320x240 16", "fb1 vc4drmfb 1920x1080 32"},
			node:    "fb0",
		},
		{
			name:    "chosen by driver name",
			entries: []string{"fb0 mi0283qtdrmfb 320x240 16", "fb1 otherdrmfb 320x240 16"},
			want:    "mi0283qt",
			node:    "fb0",
		},
		{
			name:    "chosen by node",
			entries: []string{"fb0 mi0283qtdrmfb 320x240 16", "fb1 otherdrmfb 320x240 16"},
			want:    "fb1",
			node:    "fb1",
		},
		{
			name:    "ambiguous without a choice",
			entries: []string{"fb0 mi0283qtdrmfb 320x240 16", "fb1 otherdrmfb 320x240 16"},
			errHas:  "several framebuffers match",
		},
		{
			name:    "no panel",
			entries: []string{"fb0 vc4drmfb 1920x1080 32"},
			errHas:  "no 16-bit framebuffer",
		},
		{
			name:    "the choice matches nothing",
			entries: []string{"fb0 vc4drmfb 1920x1080 32"},
			want:    "fb9",
			errHas:  "no framebuffer matches",
		},
		{
			name:    "fbcon is not a device",
			entries: []string{"fbcon fbcon 0x0 0", "fb0 mi0283qtdrmfb 320x240 16"},
			node:    "fb0",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeSys(t, c.entries...)
			got, err := findFramebuffer(c.want)
			if c.errHas != "" {
				if err == nil || !strings.Contains(err.Error(), c.errHas) {
					t.Fatalf("err = %v, want one containing %q", err, c.errHas)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Node != c.node {
				t.Fatalf("chose %s, want %s", got, c.node)
			}
			if got.Dev != filepath.Join(sysRoot, "dev", c.node) {
				t.Errorf("device node %s", got.Dev)
			}
		})
	}
}

func TestFindFramebufferStride(t *testing.T) {
	fakeSys(t, "fb0 paneldrmfb 320x240 16 768")
	got, err := findFramebuffer("")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stride != 768 { // padded lines: 768 bytes for 640 bytes of picture
		t.Fatalf("stride %d, want 768", got.Stride)
	}
	fakeSys(t, "fb0 paneldrmfb 320x240 16")
	if got, err = findFramebuffer(""); err != nil || got.Stride != 640 {
		t.Fatalf("without a stride file: %+v %v, want stride 640", got, err)
	}
}

// image builds a tiny picture whose pixels are easy to recognise in the packed bytes.
func image(w, h int, colors ...uint32) *gfx.Image {
	img := gfx.NewImage(w, h)
	copy(img.Pix, colors)
	return img
}

func TestPackRGB565(t *testing.T) {
	const (
		red   = 0xffff0000
		green = 0xff00ff00
		blue  = 0xff0000ff
		white = 0xffffffff
	)
	img := image(2, 2, red, green, blue, white)
	dst := make([]byte, 2*2*2)
	if err := packRGB565(dst, img, 4, 1); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x00, 0xf8, 0xe0, 0x07, // red, green
		0x1f, 0x00, 0xff, 0xff, // blue, white
	}
	if string(dst) != string(want) {
		t.Fatalf("packed % x, want % x", dst, want)
	}
}

func TestPackRGB565StrideAndScale(t *testing.T) {
	img := image(2, 1, 0xffff0000, 0xff0000ff)
	// A padded line: the two bytes after the picture must be left alone.
	dst := []byte{9, 9, 9, 9, 9, 9}
	if err := packRGB565(dst, img, 6, 1); err != nil {
		t.Fatal(err)
	}
	want := []byte{0x00, 0xf8, 0x1f, 0x00, 9, 9}
	if string(dst) != string(want) {
		t.Fatalf("padded line % x, want % x", dst, want)
	}

	// Doubling: each source pixel covers two by two, so one line becomes two.
	dst = make([]byte, 2*8)
	if err := packRGB565(dst, img, 8, 2); err != nil {
		t.Fatal(err)
	}
	row := []byte{0x00, 0xf8, 0x00, 0xf8, 0x1f, 0x00, 0x1f, 0x00}
	if string(dst[:8]) != string(row) || string(dst[8:]) != string(row) {
		t.Fatalf("doubled % x, want the row % x twice", dst, row)
	}
}

func TestPackRGB565RefusesASmallBuffer(t *testing.T) {
	img := image(2, 2, 0, 0, 0, 0)
	if err := packRGB565(make([]byte, 7), img, 4, 1); err == nil {
		t.Fatal("a buffer too small was accepted")
	}
	if err := packRGB565(make([]byte, 8), img, 4, 2); err == nil {
		t.Fatal("a buffer too small for the doubled frame was accepted")
	}
}

// TestPackRGB565DoesNotAllocate pins the property the player loop depends on.
func TestPackRGB565DoesNotAllocate(t *testing.T) {
	img := gfx.NewImage(320, 240)
	dst := make([]byte, 320*240*2)
	if n := testing.AllocsPerRun(20, func() {
		if err := packRGB565(dst, img, 640, 1); err != nil {
			t.Fatal(err)
		}
	}); n != 0 {
		t.Fatalf("packRGB565 allocates %v times per frame", n)
	}
}
