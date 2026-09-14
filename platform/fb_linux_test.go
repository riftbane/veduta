package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gfx"
)

// fakeSys builds a sysfs tree of framebuffers and points sysRoot at it. Each entry is
// "node name WxH bpp[ stride]", where a + in the name stands for a space ("BCM2708+FB").
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
		write("name", strings.ReplaceAll(f[1], "+", " "))
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
		errNot  string
	}{
		{
			// The panel is 16-bit, and so is a Raspberry Pi's HDMI framebuffer: the
			// board's own framebuffer is passed over.
			name:    "panel beside HDMI",
			entries: []string{"fb0 vc4drmfb 1920x1080 16", "fb1 mi0283qtdrmfb 320x240 16"},
			node:    "fb1",
		},
		{
			// Probe order is not fixed: the panel can come first.
			name:    "panel first",
			entries: []string{"fb0 ili9341drmfb 320x240 16", "fb1 vc4drmfb 1920x1080 16"},
			node:    "fb0",
		},
		{
			// With no display attached, vc4 makes up a 1024x768 framebuffer.
			name:    "panel beside HDMI with nothing plugged in",
			entries: []string{"fb0 vc4drmfb 1024x768 16", "fb1 ili9341drmfb 320x240 16"},
			node:    "fb1",
		},
		{
			// The firmware's framebuffer (a Pi without the KMS driver), and simpledrm's
			// version of it, are the board's own too.
			name:    "panel beside the firmware framebuffer",
			entries: []string{"fb0 BCM2708+FB 1024x768 16", "fb1 simpledrmdrmfb 656x416 16", "fb2 mi0283qtdrmfb 320x240 16"},
			node:    "fb2",
		},
		{
			// The board's framebuffer loses even at the panel's size.
			name:    "HDMI as small as the panel",
			entries: []string{"fb0 vc4drmfb 320x240 16", "fb1 ili9341drmfb 320x240 16"},
			node:    "fb1",
		},
		{
			// An HDMI driver this does not know by name is still larger than the panel.
			name:    "the smaller of two unknown framebuffers",
			entries: []string{"fb0 otherhdmidrmfb 1920x1080 16", "fb1 paneldrmfb 480x320 16"},
			node:    "fb1",
		},
		{
			// A board with HDMI and no panel plays on HDMI.
			name:    "HDMI alone",
			entries: []string{"fb0 vc4drmfb 1920x1080 16"},
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
			// Two panels of one size: nothing tells them apart.
			name:    "ambiguous without a choice",
			entries: []string{"fb0 mi0283qtdrmfb 320x240 16", "fb1 otherdrmfb 320x240 16", "fb2 vc4drmfb 1920x1080 16"},
			errHas:  "several framebuffers match",
			errNot:  "vc4drmfb", // the error names only what is really in doubt
		},
		{
			// No panel, but an ordinary framebuffer: an emulator, or a PC in text mode.
			name:    "a 32-bit framebuffer will do",
			entries: []string{"fb0 virtio_gpudrmfb 1280x800 32"},
			node:    "fb0",
		},
		{
			// The panel still wins when both are there.
			name:    "the panel is preferred",
			entries: []string{"fb0 i915drmfb 1920x1080 32", "fb1 paneldrmfb 320x240 16"},
			node:    "fb1",
		},
		{
			// Two 32-bit framebuffers are not narrowed by size: there is no panel among
			// them to find.
			name:    "two 32-bit framebuffers",
			entries: []string{"fb0 virtio_gpudrmfb 1280x800 32", "fb1 ramfbdrmfb 1024x768 32"},
			errHas:  "several framebuffers match",
		},
		{
			name:    "nothing usable",
			entries: []string{"fb0 ancientfb 640x480 8"},
			errHas:  "16 or 32 bits per pixel",
		},
		{
			name:    "the choice matches nothing",
			entries: []string{"fb0 vc4drmfb 1920x1080 16"},
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
				if c.errNot != "" && strings.Contains(err.Error(), c.errNot) {
					t.Fatalf("err = %v, want one not naming %q", err, c.errNot)
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

func TestPackXRGB(t *testing.T) {
	img := image(2, 1, 0xffff0000, 0xff0000ff) // red, blue
	dst := make([]byte, 2*4)
	if err := packXRGB(dst, img, 8, 1); err != nil {
		t.Fatal(err)
	}
	// 0xAARRGGBB stored low byte first: blue, green, red, alpha.
	want := []byte{0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0xff}
	if string(dst) != string(want) {
		t.Fatalf("packed % x, want % x", dst, want)
	}
	// A padded line leaves the bytes past the picture alone.
	dst = []byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
	if err := packXRGB(dst, image(1, 1, 0xff00ff00), 10, 1); err != nil {
		t.Fatal(err)
	}
	if string(dst[:4]) != string([]byte{0x00, 0xff, 0x00, 0xff}) || dst[4] != 9 {
		t.Fatalf("padded line % x", dst)
	}
	// Doubling covers two by two.
	dst = make([]byte, 2*16)
	if err := packXRGB(dst, image(2, 1, 0xffff0000, 0xff0000ff), 16, 2); err != nil {
		t.Fatal(err)
	}
	if string(dst[:16]) != string(dst[16:]) {
		t.Fatal("the doubled row differs from the first")
	}
	if err := packXRGB(make([]byte, 4), image(2, 1, 0, 0), 8, 1); err == nil {
		t.Fatal("a buffer too small was accepted")
	}
	big := gfx.NewImage(320, 240)
	buf := make([]byte, 320*240*4)
	if n := testing.AllocsPerRun(20, func() {
		if err := packXRGB(buf, big, 320*4, 1); err != nil {
			t.Fatal(err)
		}
	}); n != 0 {
		t.Fatalf("packXRGB allocates %v times per frame", n)
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
