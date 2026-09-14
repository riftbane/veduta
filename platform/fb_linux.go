package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/gfx"
)

// This file is the pixel path of the appliance: finding the small panel among the
// framebuffers the kernel offers, and packing a rendered frame into its format. It is
// deliberately free of ioctls and of the window itself, so all of it is testable on a
// machine that has no framebuffer at all.

// sysRoot is the root the framebuffer is looked for under. Tests replace it with a
// directory holding a fake /sys and /dev.
var sysRoot = "/"

// fbInfo describes one framebuffer device.
type fbInfo struct {
	Dev    string // device node, /dev/fb0
	Node   string // sysfs node name, fb0
	Name   string // driver name, e.g. "mi0283qtdrmfb"
	W, H   int    // visible size in pixels
	Stride int    // bytes per line, which may be wider than W*Bits/8
	Bits   int    // bits per pixel
}

func (f fbInfo) String() string {
	return fmt.Sprintf("%s (%s, %dx%d, %d bpp, stride %d)", f.Dev, f.Name, f.W, f.H, f.Bits, f.Stride)
}

// findFramebuffer picks the panel among the framebuffers in sysfs. A small SPI panel is
// 16-bit, but depth alone does not single it out: a Raspberry Pi's HDMI framebuffer is
// 16-bit too, whether it comes from the KMS driver (vc4drmfb) or the firmware. Among
// several 16-bit framebuffers the board's own are passed over, and then the smallest wins;
// only two panels of one size are left in doubt. A 32-bit framebuffer is what an emulator
// or a PC at a text console offers, and it will do when there is no 16-bit one. want
// overrides the choice: it matches a node name ("fb1") or any part of a driver name
// ("mi0283qt"), which is what VEDUTA_FB carries. The device number is never assumed: it
// depends on probe order.
func findFramebuffer(want string) (fbInfo, error) {
	dir := filepath.Join(sysRoot, "sys", "class", "graphics")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fbInfo{}, fmt.Errorf("platform: no framebuffers in %s: %w", dir, err)
	}
	var all, matching, panels, others []fbInfo
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "fb") || strings.Contains(e.Name(), "con") {
			continue // fbcon is not a device
		}
		info, err := readFBInfo(dir, e.Name())
		if err != nil {
			continue // a framebuffer that will not describe itself is not one we can use
		}
		all = append(all, info)
		switch {
		case want != "":
			if info.Node == want || strings.Contains(info.Name, want) {
				matching = append(matching, info)
			}
		case info.Bits == 16:
			panels = append(panels, info)
		case info.Bits == 32:
			others = append(others, info)
		}
	}
	if want == "" {
		if matching = choosePanel(panels); len(matching) == 0 {
			matching = others
		}
	}
	sort.Slice(matching, func(i, j int) bool { return matching[i].Node < matching[j].Node })
	switch len(matching) {
	case 1:
		return matching[0], nil
	case 0:
		if want != "" {
			return fbInfo{}, fmt.Errorf("platform: no framebuffer matches %q (found %s)", want, describeFBs(all))
		}
		return fbInfo{}, fmt.Errorf("platform: no framebuffer with 16 or 32 bits per pixel (found %s); set VEDUTA_FB to choose one", describeFBs(all))
	default:
		return fbInfo{}, fmt.Errorf("platform: several framebuffers match (%s); set VEDUTA_FB to one of them", describeFBs(matching))
	}
}

// boardFramebuffers are the framebuffers of a Raspberry Pi itself rather than of a panel
// attached to it: HDMI through the KMS driver, the firmware's framebuffer, and simpledrm's
// version of the firmware's. All of them can be 16-bit, as the panel is.
var boardFramebuffers = map[string]bool{"vc4drmfb": true, "BCM2708 FB": true, "simpledrmdrmfb": true}

// choosePanel narrows the 16-bit framebuffers to the panel: the board's own framebuffers
// are dropped when anything else is left, and of the rest only the smallest remain. More
// than one comes back only when panels of the same size tie.
func choosePanel(list []fbInfo) []fbInfo {
	if len(list) < 2 {
		return list
	}
	var attached []fbInfo
	for _, f := range list {
		if !boardFramebuffers[f.Name] {
			attached = append(attached, f)
		}
	}
	if len(attached) > 0 {
		list = attached
	}
	var smallest []fbInfo
	for _, f := range list {
		switch {
		case len(smallest) == 0 || f.W*f.H < smallest[0].W*smallest[0].H:
			smallest = append(smallest[:0], f)
		case f.W*f.H == smallest[0].W*smallest[0].H:
			smallest = append(smallest, f)
		}
	}
	return smallest
}

func describeFBs(list []fbInfo) string {
	if len(list) == 0 {
		return "none"
	}
	s := make([]string, len(list))
	for i, f := range list {
		s[i] = f.String()
	}
	return strings.Join(s, ", ")
}

// readFBInfo reads one framebuffer's description from its sysfs attributes.
func readFBInfo(dir, node string) (fbInfo, error) {
	read := func(attr string) (string, error) {
		b, err := os.ReadFile(filepath.Join(dir, node, attr))
		return strings.TrimSpace(string(b)), err
	}
	name, err := read("name")
	if err != nil {
		return fbInfo{}, err
	}
	size, err := read("virtual_size")
	if err != nil {
		return fbInfo{}, err
	}
	w, h, ok := strings.Cut(size, ",")
	if !ok {
		return fbInfo{}, fmt.Errorf("virtual_size %q", size)
	}
	info := fbInfo{Dev: filepath.Join(sysRoot, "dev", node), Node: node, Name: name}
	if info.W, err = strconv.Atoi(w); err != nil {
		return fbInfo{}, err
	}
	if info.H, err = strconv.Atoi(h); err != nil {
		return fbInfo{}, err
	}
	bits, err := read("bits_per_pixel")
	if err != nil {
		return fbInfo{}, err
	}
	if info.Bits, err = strconv.Atoi(bits); err != nil {
		return fbInfo{}, err
	}
	// stride is what a line really costs; it is padded on some drivers. When the file is
	// missing, the line is exactly as wide as the picture.
	if s, err := read("stride"); err == nil {
		if info.Stride, err = strconv.Atoi(s); err != nil {
			return fbInfo{}, err
		}
	}
	if info.Stride == 0 {
		info.Stride = info.W * info.Bits / 8
	}
	if info.W <= 0 || info.H <= 0 || info.Bits <= 0 {
		return fbInfo{}, fmt.Errorf("%s describes itself as %dx%d at %d bpp", node, info.W, info.H, info.Bits)
	}
	return info, nil
}

// packXRGB writes img into dst as 32-bit pixels, the format of an ordinary framebuffer —
// an emulated one, or a PC in text mode. The engine's own pixels are already 0xAARRGGBB,
// so each one is stored as it stands, low byte first. Like the 16-bit packer it honours a
// padded stride and an integer scale, and allocates nothing.
func packXRGB(dst []byte, img *gfx.Image, stride, scale int) error {
	if scale < 1 {
		scale = 1
	}
	w, h := img.W*scale, img.H*scale
	if need := stride*(h-1) + w*4; len(dst) < need {
		return fmt.Errorf("platform: the framebuffer holds %d bytes, a %dx%d frame needs %d", len(dst), w, h, need)
	}
	for y := 0; y < img.H; y++ {
		src := img.Pix[y*img.W : y*img.W+img.W]
		line := dst[y*scale*stride:]
		for x, c := range src {
			b0, b1, b2, b3 := byte(c), byte(c>>8), byte(c>>16), byte(c>>24)
			for i := 0; i < scale; i++ {
				o := (x*scale + i) * 4
				line[o], line[o+1], line[o+2], line[o+3] = b0, b1, b2, b3
			}
		}
		first := line[:w*4]
		for i := 1; i < scale; i++ {
			copy(dst[(y*scale+i)*stride:][:w*4], first)
		}
	}
	return nil
}

// packRGB565 writes img into dst as RGB565, the format of these panels: five bits of red
// and blue, six of green, two bytes per pixel in the machine's own order. Each destination
// line starts every stride bytes, and each source pixel covers scale by scale destination
// pixels, which is how a small board renders a quarter of the pixels and still fills the
// panel. It allocates nothing, so the player loop stays allocation-free.
func packRGB565(dst []byte, img *gfx.Image, stride, scale int) error {
	if scale < 1 {
		scale = 1
	}
	w, h := img.W*scale, img.H*scale
	if need := stride*(h-1) + w*2; len(dst) < need {
		return fmt.Errorf("platform: the framebuffer holds %d bytes, a %dx%d frame needs %d", len(dst), w, h, need)
	}
	for y := 0; y < img.H; y++ {
		src := img.Pix[y*img.W : y*img.W+img.W]
		line := dst[y*scale*stride:]
		for x, c := range src {
			// 0xAARRGGBB to RRRRRGGG GGGBBBBB, little endian.
			v := uint16(c>>8&0xf800 | c>>5&0x07e0 | c>>3&0x001f)
			lo, hi := byte(v), byte(v>>8)
			for i := 0; i < scale; i++ {
				o := (x*scale + i) * 2
				line[o], line[o+1] = lo, hi
			}
		}
		// The other lines of a scaled row are copies of the first.
		first := line[:w*2]
		for i := 1; i < scale; i++ {
			copy(dst[(y*scale+i)*stride:][:w*2], first)
		}
	}
	return nil
}
