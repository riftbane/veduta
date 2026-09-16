package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/gfx"
)

// fakePanel builds a sysfs tree with one 16-bit framebuffer and a file standing in for its
// device node, and returns that file's path. Everything the framebuffer window does is a
// read or a write of ordinary files, so the whole backend runs here.
func fakePanel(t *testing.T, w, h, stride int) string {
	t.Helper()
	entry := "fb0 paneldrmfb " + itoa(w) + "x" + itoa(h) + " 16 " + itoa(stride)
	root := fakeSys(t, entry)
	dev := filepath.Join(root, "dev", "fb0")
	if err := os.MkdirAll(filepath.Dir(dev), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dev, make([]byte, stride*h), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(backendEnv, BackendFB)
	t.Setenv(fbDeviceEnv, "")
	t.Setenv(renderScaleE, "")
	return dev
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestFBWindow(t *testing.T) {
	dev := fakePanel(t, 320, 240, 640)
	win, err := Open(Options{Title: "panel", Width: 1280, Height: 720})
	if err != nil {
		t.Fatal(err)
	}
	defer win.Close()
	// The panel decides the size, not the caller.
	if w, h := win.Size(); w != 320 || h != 240 {
		t.Fatalf("Size = %dx%d, want the panel's 320x240", w, h)
	}
	// With nothing to read the window still polls, and says once, on the player's stderr,
	// why nothing will answer.
	src, ok := win.(*fbWindow).source.(*inputSource)
	if !ok {
		t.Fatalf("the panel reads input from %T, want an input source", win.(*fbWindow).source)
	}
	if src.log != os.Stderr {
		t.Fatalf("the input source logs to %v, want stderr", src.log)
	}
	var log strings.Builder
	src.log = &log
	if evs, err := win.Poll(); err != nil || len(evs) != 0 {
		t.Fatalf("Poll without a pad = %v, %v", evs, err)
	}
	if !strings.Contains(log.String(), "no input devices in") {
		t.Errorf("Poll with nothing to read logged %q", log.String())
	}

	img := gfx.NewImage(320, 240)
	for i := range img.Pix {
		img.Pix[i] = 0xff0000ff // blue
	}
	if err := win.Present(img); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 640*240 {
		t.Fatalf("the panel holds %d bytes", len(data))
	}
	for i := 0; i+1 < len(data); i += 2 {
		if data[i] != 0x1f || data[i+1] != 0x00 {
			t.Fatalf("byte %d is %#02x %#02x, want blue in RGB565", i, data[i], data[i+1])
		}
	}
	// A frame of the wrong size is refused rather than written askew.
	if err := win.Present(gfx.NewImage(64, 64)); err == nil {
		t.Error("a frame of the wrong size was accepted")
	}
	if n := testing.AllocsPerRun(10, func() {
		if err := win.Present(img); err != nil {
			t.Fatal(err)
		}
	}); n > 1 { // the write itself may take one
		t.Errorf("Present allocates %v times per frame", n)
	}
	if err := win.Close(); err != nil {
		t.Fatal(err)
	}
	if err := win.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := win.Present(img); err == nil {
		t.Error("Present after Close succeeded")
	}
	if w, h := win.Size(); w != 0 || h != 0 {
		t.Errorf("Size after Close = %dx%d", w, h)
	}
}

// TestFBWindowCloseFlushesTerminal: at a Linux text console, keys typed while the player
// ran must not be left for the shell to run once it quits.
func TestFBWindowCloseFlushesTerminal(t *testing.T) {
	fakePanel(t, 320, 240, 640)
	master, slave := newPty(t)
	old := terminal
	terminal = uintptr(slave)
	t.Cleanup(func() { terminal = old })
	win, err := Open(Options{})
	if err != nil {
		t.Fatal(err)
	}
	typeAhead(t, master, slave)
	if err := win.Close(); err != nil {
		t.Fatal(err)
	}
	if n := pendingInput(t, slave); n != 0 {
		t.Fatalf("%d bytes typed at the terminal are still waiting for the shell", n)
	}
}

// TestFBWindowScale covers the small board: the game renders a quarter of the pixels and
// each one covers two by two on the glass.
func TestFBWindowScale(t *testing.T) {
	dev := fakePanel(t, 320, 240, 640)
	t.Setenv(renderScaleE, "2")
	win, err := Open(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer win.Close()
	if w, h := win.Size(); w != 160 || h != 120 {
		t.Fatalf("Size = %dx%d, want 160x120 at scale 2", w, h)
	}
	img := gfx.NewImage(160, 120)
	img.Pix[0] = 0xffff0000 // one red pixel at the corner
	if err := win.Present(img); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dev)
	// It must cover two by two: both of the first two pixels of the first two lines.
	for _, off := range []int{0, 2, 640, 642} {
		if data[off] != 0x00 || data[off+1] != 0xf8 {
			t.Fatalf("offset %d is %#02x %#02x, want red", off, data[off], data[off+1])
		}
	}
	if data[4] != 0 || data[5] != 0 {
		t.Error("the doubled pixel spilled past its two columns")
	}
}

func TestFBWindowRejections(t *testing.T) {
	fakePanel(t, 320, 240, 640)
	t.Setenv(renderScaleE, "0")
	if _, err := Open(Options{}); err == nil || !strings.Contains(err.Error(), "VEDUTA_SCALE") {
		t.Fatalf("scale 0: %v", err)
	}
	t.Setenv(renderScaleE, "")
	t.Setenv(fbDeviceEnv, "fb7")
	if _, err := Open(Options{}); err == nil || !strings.Contains(err.Error(), "no framebuffer matches") {
		t.Fatalf("unknown framebuffer: %v", err)
	}
}

func TestBackendChoice(t *testing.T) {
	fakePanel(t, 320, 240, 640)
	// An unknown backend is refused by name.
	t.Setenv(backendEnv, "wayland")
	if _, err := Open(Options{}); err == nil || !strings.Contains(err.Error(), "want auto or fbdev") {
		t.Fatalf("unknown backend: %v", err)
	}
	// The X11 window is gone, and asking for it says so rather than calling it unknown.
	t.Setenv(backendEnv, "x11")
	if _, err := Open(Options{}); err == nil || !strings.Contains(err.Error(), "X11 window was removed") ||
		!strings.Contains(err.Error(), "framebuffer") {
		t.Fatalf("x11: %v, want an error saying the X11 window was removed", err)
	}
	// Unset, auto and fbdev all land on the panel, whatever display server is around.
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	for _, b := range []string{"", BackendAuto, BackendFB} {
		t.Setenv(backendEnv, b)
		win, err := Open(Options{})
		if err != nil {
			t.Fatalf("%s=%q: %v", backendEnv, b, err)
		}
		if _, ok := win.(*fbWindow); !ok {
			t.Errorf("%s=%q chose %T, want the panel", backendEnv, b, win)
		}
		win.Close()
	}
}
