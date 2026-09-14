//go:build linux

package platform

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riftbane/veduta/gfx"
)

// TestDisplaySmoke opens a real window on $DISPLAY. It runs only with
// VEDUTA_DISPLAY_TEST=1 (CI runs it under xvfb-run); elsewhere it is skipped.
func TestDisplaySmoke(t *testing.T) {
	if os.Getenv("VEDUTA_DISPLAY_TEST") != "1" {
		t.Skip("set VEDUTA_DISPLAY_TEST=1 to open a real window (needs an X server, e.g. xvfb-run)")
	}
	w, err := Open(Options{Title: "Veduta display smoke test", Width: 320, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if cw, ch := w.Size(); cw != 320 || ch != 240 {
		t.Errorf("Size right after Open = %dx%d, want 320x240", cw, ch)
	}
	img := gfx.NewImage(320, 240)
	for f := 0; f < 30; f++ {
		events, err := w.Poll()
		if err != nil {
			t.Fatalf("frame %d: Poll: %v", f, err)
		}
		for _, ev := range events {
			t.Logf("frame %d: event %+v", f, ev)
		}
		cw, ch := w.Size()
		if cw <= 0 || ch <= 0 {
			t.Fatalf("frame %d: Size = %dx%d", f, cw, ch)
		}
		if img.W != cw || img.H != ch {
			img = gfx.NewImage(cw, ch)
		}
		for y := 0; y < img.H; y++ {
			for x := 0; x < img.W; x++ {
				img.Pix[y*img.W+x] = gfx.RGBA(uint8(x+8*f), uint8(y), uint8(128+4*f), 255)
			}
		}
		if err := w.Present(img); err != nil {
			t.Fatalf("frame %d: Present: %v", f, err)
		}
		time.Sleep(16 * time.Millisecond)
	}
	// Pointer lock: the cursor is hidden and warped back to the middle of the window.
	if err := w.SetPointerLock(true); err != nil {
		t.Fatalf("SetPointerLock(true): %v", err)
	}
	for f := 0; f < 5; f++ {
		events, err := w.Poll()
		if err != nil {
			t.Fatalf("locked frame %d: Poll: %v", f, err)
		}
		for _, ev := range events {
			t.Logf("locked frame %d: event %+v", f, ev)
		}
		time.Sleep(16 * time.Millisecond)
	}
	if err := w.SetPointerLock(false); err != nil {
		t.Fatalf("SetPointerLock(false): %v", err)
	}

	// Errors of the last frames arrive asynchronously.
	time.Sleep(100 * time.Millisecond)
	if _, err := w.Poll(); err != nil {
		t.Fatalf("final Poll: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := w.Present(img); err == nil {
		t.Fatal("Present after Close succeeded")
	}
}

// TestOpenWithoutDisplay covers both ways out when no display server is running: asking
// for X11 still says plainly that it is not there, while the default falls through to the
// panel's framebuffer — and on a machine with neither, as this one is, the error has to
// name both paths rather than only the one it tried last.
func TestOpenWithoutDisplay(t *testing.T) {
	t.Setenv(displayEnv, "")
	t.Setenv(waylandEnv, "")
	t.Setenv(backendEnv, BackendX11)
	if _, err := Open(Options{}); err == nil || !strings.Contains(err.Error(), "$DISPLAY is not set") {
		t.Fatalf("forcing x11 without DISPLAY: %v", err)
	}
	t.Setenv(backendEnv, "")
	t.Setenv(fbDeviceEnv, "")
	_, err := Open(Options{})
	if err == nil {
		t.Fatal("Open succeeded with no display server and no framebuffer")
	}
	for _, want := range []string{"framebuffer", backendEnv, displayEnv} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %s: %v", want, err)
		}
	}
}
