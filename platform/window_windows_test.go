//go:build windows

package platform

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/riftbane/veduta/v2/gfx"
)

var procGetPixel = gdi32.NewProc("GetPixel")

// TestSimulatorWindow opens the simulator. It needs a desktop session, so it runs only when
// VEDUTA_DISPLAY_TEST=1 (the Windows CI runner, a developer's machine): the frame reaches
// the window scaled and centered, in the panel's colors and the right byte order, and
// polling and presenting allocate nothing.
func TestSimulatorWindow(t *testing.T) {
	if os.Getenv("VEDUTA_DISPLAY_TEST") != "1" {
		t.Skip("set VEDUTA_DISPLAY_TEST=1 to open a real window")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.Setenv(renderScaleEnv, "2")
	win, err := Open(Options{Title: "simulator test ✓", Width: 320, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	defer win.Close()
	if _, err := Open(Options{Width: 320, Height: 240}); err == nil {
		t.Error("a second simulator window opened")
	}
	if w, h := win.Size(); w != 320 || h != 240 {
		t.Fatalf("Size() = %dx%d, want the frame, 320x240", w, h)
	}
	// Top half red with a green right quarter, bottom half a blue the panel cannot show
	// exactly (0x13 has bits RGB565 drops).
	img := gfx.NewImage(320, 240)
	const red, green, blue = 0xFFFF0000, 0xFF00FF00, 0xFF000013
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
	for i := 0; i < 10; i++ {
		if _, err := win.Poll(); err != nil {
			t.Fatal(err)
		}
		if err := win.Present(img); err != nil {
			t.Fatal(err)
		}
		time.Sleep(16 * time.Millisecond)
	}
	sw := win.(*window)
	scale, ox, oy := fitScale(sw.cw, sw.ch, 320, 240)
	for _, p := range []struct {
		x, y int
		want uint32 // COLORREF 0x00BBGGRR
	}{
		{10, 10, 0x0000FF},
		{310, 10, 0x00FF00},
		{10, 230, 0x100000}, // 0x13 → 0x10 in RGB565 (blue 2 of 31, widened)
	} {
		px, _, _ := procGetPixel.Call(sw.dc, uintptr(ox+p.x*scale), uintptr(oy+p.y*scale))
		if uint32(px) != p.want {
			t.Errorf("frame pixel (%d, %d) on screen is %#06x, want %#06x", p.x, p.y, uint32(px), p.want)
		}
	}
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
}
