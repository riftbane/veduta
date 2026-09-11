//go:build windows

package platform

import (
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/riftbane/veduta/gfx"
)

var procGetPixel = gdi32.NewProc("GetPixel")

// gradient fills img with a gradient that moves with frame.
func gradient(img *gfx.Image, frame int) {
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			img.Pix[y*img.W+x] = gfx.RGBA(uint8(x+frame*8), uint8(y), uint8(frame*8), 255)
		}
	}
}

// TestDisplaySmoke opens a real window. It needs a desktop session, so it runs only when
// VEDUTA_DISPLAY_TEST=1 (the Windows CI runner, a developer's machine). With
// VEDUTA_DISPLAY_TEST_SECONDS=N it also keeps the window open for N seconds, or until it
// is closed, and logs every event (run with -v) for checking input by hand.
func TestDisplaySmoke(t *testing.T) {
	if os.Getenv("VEDUTA_DISPLAY_TEST") != "1" {
		t.Skip("set VEDUTA_DISPLAY_TEST=1 to open a real window")
	}
	// A window belongs to the OS thread that creates it: keep this goroutine on one.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	win, err := Open(Options{Title: "Veduta smoke test – ✓ 𝄞", Width: 320, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	defer win.Close()
	if w, h := win.Size(); w != 320 || h != 240 {
		t.Errorf("Size() = %dx%d, want 320x240", w, h)
	}

	img := gfx.NewImage(320, 240)
	frame := func(n int) (closed bool) {
		events, err := win.Poll()
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		for _, e := range events {
			t.Logf("frame %d: %s", n, describe([]Event{e})[0])
			closed = closed || e.Kind == Close
		}
		if w, h := win.Size(); w > 0 && h > 0 && (w != img.W || h != img.H) {
			img = gfx.NewImage(w, h)
		}
		gradient(img, n)
		if err := win.Present(img); err != nil {
			t.Fatalf("Present: %v", err)
		}
		time.Sleep(16 * time.Millisecond)
		return closed
	}
	for n := 0; n < 30; n++ {
		frame(n)
	}
	if s, _ := strconv.Atoi(os.Getenv("VEDUTA_DISPLAY_TEST_SECONDS")); s > 0 {
		t.Logf("interactive for %d s: press keys, type, click, drag, resize; close the window to stop", s)
		deadline := time.Now().Add(time.Duration(s) * time.Second)
		for n := 30; time.Now().Before(deadline) && !frame(n); n++ {
		}
	}

	// Steady state without input: polling and presenting allocate nothing.
	if n := testing.AllocsPerRun(20, func() {
		if _, err := win.Poll(); err != nil {
			t.Error(err)
		}
		if err := win.Present(img); err != nil {
			t.Error(err)
		}
	}); n != 0 {
		t.Errorf("Poll+Present allocate %v times per frame", n)
	}

	// Byte order and orientation, read back through GDI: top half red with a green
	// right quarter, bottom half blue. GetPixel returns COLORREF 0x00BBGGRR.
	const red, green, blue = 0xFFFF0000, 0xFF00FF00, 0xFF0000FF
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			c := uint32(blue)
			if y < img.H/2 {
				c = red
				if x >= img.W*3/4 {
					c = green
				}
			}
			img.Pix[y*img.W+x] = c
		}
	}
	if err := win.Present(img); err != nil {
		t.Fatalf("Present: %v", err)
	}
	dc := win.(*window).dc
	for _, p := range []struct {
		x, y int
		want uint32
	}{
		{4, 4, 0x0000FF},                 // red
		{img.W - 4, 4, 0x00FF00},         // green
		{4, img.H - 4, 0xFF0000},         // blue
		{img.W - 4, img.H - 4, 0xFF0000}, // blue
	} {
		r, _, _ := procGetPixel.Call(dc, uintptr(p.x), uintptr(p.y))
		if c := uint32(r); c == 0xFFFFFFFF {
			t.Logf("GetPixel(%d,%d): pixel not readable (window hidden?), check skipped", p.x, p.y)
		} else if c != p.want {
			t.Errorf("pixel %d,%d = COLORREF %#06x, want %#06x", p.x, p.y, c, p.want)
		}
	}

	// A different size is drawn clipped at the top-left corner.
	if err := win.Present(gfx.NewImage(img.W/2, img.H*2)); err != nil {
		t.Errorf("Present of a mismatched size: %v", err)
	}
	if err := win.Present(&gfx.Image{W: 4, H: 4, Pix: make([]uint32, 15)}); err == nil {
		t.Error("Present accepted an image with too few pixels")
	}
	if _, err := win.Poll(); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	// Another OS thread gets an error instead of silently losing the window's input.
	errc := make(chan error)
	go func() {
		_, err := win.Poll()
		errc <- err
	}()
	if err := <-errc; err == nil {
		t.Error("Poll from another OS thread succeeded")
	}

	if err := win.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := win.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := win.Present(img); err == nil {
		t.Error("Present after Close succeeded")
	}
	if _, err := win.Poll(); err == nil {
		t.Error("Poll after Close succeeded")
	}
	if w, h := win.Size(); w != 0 || h != 0 {
		t.Errorf("Size after Close = %dx%d", w, h)
	}

	// The shared window procedure and class registration work for a second window.
	win2, err := Open(Options{Title: "second", Width: 64, Height: 48})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := win2.Poll(); err != nil {
		t.Error(err)
	}
	if err := win2.Present(gfx.NewImage(64, 48)); err != nil {
		t.Error(err)
	}
	if err := win2.Close(); err != nil {
		t.Errorf("Close second window: %v", err)
	}
}
