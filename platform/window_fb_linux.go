package platform

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/riftbane/veduta/gfx"
)

// fbWindow presents frames on a panel through its framebuffer device. There is no window
// system on an appliance: the panel is the whole screen, it never resizes, and the cursor
// it does not have cannot be locked. Input arrives from elsewhere (the pad), which this
// window only passes on.
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

// openFB opens the panel named by VEDUTA_FB (or the only 16-bit framebuffer) and renders
// at its size divided by VEDUTA_SCALE, which lets a slower board draw a quarter of the
// pixels and still fill the glass.
func openFB(o Options) (Window, error) {
	info, err := findFramebuffer(os.Getenv("VEDUTA_FB"))
	if err != nil {
		return nil, err
	}
	switch info.Bits {
	case 16, 32: // the panel's RGB565, or the 32-bit framebuffer of an emulator or a PC
	default:
		return nil, fmt.Errorf("platform: %s is %d bits per pixel; 16 (RGB565) and 32 are supported", info, info.Bits)
	}
	scale := 1
	if s := os.Getenv("VEDUTA_SCALE"); s != "" {
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

// SetPointerLock does nothing: a panel has no cursor to hide or to keep anywhere.
func (w *fbWindow) SetPointerLock(bool) error {
	if w.closed {
		return errFBClosed
	}
	return nil
}

// Close releases the panel and the input source. It is safe to call more than once.
func (w *fbWindow) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	err := w.file.Close()
	if w.source != nil {
		if cerr := w.source.close(); err == nil {
			err = cerr
		}
	}
	return err
}
