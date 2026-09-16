package platform

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/riftbane/veduta/gfx"
)

// fbWindow presents frames on a panel through its framebuffer device. There is no window
// system on an appliance: the panel is the whole screen and it never resizes. Input arrives
// from elsewhere (the pad), which this window only passes on.
type fbWindow struct {
	info   fbInfo
	file   *os.File
	scale  int
	w, h   int    // the size a game renders at: the panel divided by scale
	buf    []byte // packed frame, reused every Present so presenting allocates nothing
	source events // where Poll takes its events from; nil until a pad is attached
	closed bool
}

// events is anything that can hand the window a batch of input events. The pad implements
// it; tests use a fake.
type events interface {
	poll() ([]Event, error)
	close() error
}

var errFBClosed = errors.New("platform: the panel is closed")

// closeSettle bounds how long Close waits for the buttons that closed the player to be let go.
const closeSettle = 500 * time.Millisecond

// openFB opens the panel named by VEDUTA_FB (or the one findFramebuffer picks) and renders
// at its size divided by VEDUTA_SCALE, which lets a slower board draw a quarter of the
// pixels and still fill the glass.
func openFB(o Options) (Window, error) {
	info, err := findFramebuffer(os.Getenv(fbDeviceEnv))
	if err != nil {
		return nil, err
	}
	switch info.Bits {
	case 16, 32: // RGB565 (the panel, or a Raspberry Pi's HDMI), or the 32 bits of an emulator or a PC
	default:
		return nil, fmt.Errorf("platform: %s is %d bits per pixel; 16 (RGB565) and 32 are supported", info, info.Bits)
	}
	scale := 1
	if s := os.Getenv(renderScaleE); s != "" {
		if scale, err = strconv.Atoi(s); err != nil || scale < 1 || scale > 8 {
			return nil, fmt.Errorf("platform: VEDUTA_SCALE %q (want 1 to 8)", s)
		}
	}
	f, err := os.OpenFile(info.Dev, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("platform: %s: %w", info.Dev, err)
	}
	w := &fbWindow{
		info: info, file: f, scale: scale,
		w: info.W / scale, h: info.H / scale,
		buf:    make([]byte, info.Stride*info.H),
		source: newInputSource(os.Getenv(padEnv)),
	}
	if w.w <= 0 || w.h <= 0 {
		f.Close()
		return nil, fmt.Errorf("platform: %s divided by %d leaves nothing to draw on", info, scale)
	}
	return w, nil
}

// Poll returns the input events since the last call. Without an input source there are
// none, which is a console nobody can play rather than an error.
func (w *fbWindow) Poll() ([]Event, error) {
	if w.closed {
		return nil, errFBClosed
	}
	if w.source == nil {
		return nil, nil
	}
	return w.source.poll()
}

// Present packs the frame into the panel's format and writes it in one go.
func (w *fbWindow) Present(img *gfx.Image) error {
	if w.closed {
		return errFBClosed
	}
	if img == nil {
		return errors.New("platform: Present of a nil image")
	}
	if img.W != w.w || img.H != w.h {
		return fmt.Errorf("platform: the panel takes %dx%d frames, got %dx%d", w.w, w.h, img.W, img.H)
	}
	pack := packRGB565
	if w.info.Bits == 32 {
		pack = packXRGB
	}
	if err := pack(w.buf, img, w.info.Stride, w.scale); err != nil {
		return err
	}
	if _, err := w.file.WriteAt(w.buf, 0); err != nil {
		return fmt.Errorf("platform: writing to %s: %w", w.info.Dev, err)
	}
	return nil
}

// Size is the panel's size divided by the render scale, and never changes.
func (w *fbWindow) Size() (int, int) {
	if w.closed {
		return 0, 0
	}
	return w.w, w.h
}

// terminal is the descriptor of the terminal the player may have been started from: its
// standard input. A test replaces it.
var terminal uintptr = 0

// Close releases the panel and the input source. It is safe to call more than once.
//
// It also throws away what was typed at the player's terminal and not read. The input
// source keeps keyboards from typing into the text console while it polls, but not before
// the first poll, nor while a stalled player has given them back; without this the shell
// would run those keys once the player quits. The console's cursor is left alone: hiding
// it could not be undone for a player killed outright, and a cursor that stays hidden at
// the shell is worse than one blinking over the game. Before the devices are given back,
// Close waits up to closeSettle for the buttons that closed the player to be let go, so
// neither they nor the kernel's repeats of them reach the console.
func (w *fbWindow) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	err := w.file.Close()
	if w.source != nil {
		if s, ok := w.source.(interface{ settle(time.Duration) }); ok {
			s.settle(closeSettle)
		}
		if cerr := w.source.close(); err == nil {
			err = cerr
		}
	}
	flushInput(terminal) // not a terminal, or not ours to flush: nothing to do
	return err
}
