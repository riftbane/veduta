// Package platform opens the player window and turns operating-system input into engine
// events. It is used only by the player build (veduta.Run without -headless) and is
// never imported by the veduta tool.
//
// Implementations are pure Go with CGO_ENABLED=0: Linux speaks the X11 protocol over its
// unix socket (window_linux.go), Windows calls user32/gdi32 through syscall
// (window_windows.go); every other GOOS gets a stub that fails at runtime
// (window_other.go). The window is created and pumped on the main goroutine, which is
// locked to the main OS thread by this package's init.
package platform

import (
	"runtime"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/sim"
)

func init() {
	// Window systems (Win32 especially) require a window's messages to be pumped by the
	// thread that created it; main runs on the main thread when this runs in init.
	runtime.LockOSThread()
}

// EventKind classifies an input event.
type EventKind uint8

// Event kinds.
const (
	KeyDown    EventKind = iota + 1 // Code went down (auto-repeat is filtered out)
	KeyUp                           // Code went up
	MouseMove                       // cursor at X, Y (window pixels, origin top-left)
	ButtonDown                      // mouse Button went down (X, Y hold the cursor position)
	ButtonUp                        // mouse Button went up
	Text                            // Text was typed (UTF-8)
	Resize                          // the client area is now W×H pixels
	Close                           // the user asked to close the window
	FocusLost                       // the window lost keyboard focus: release everything
)

// Event is one input event.
type Event struct {
	Kind   EventKind
	Code   string        // KeyDown/KeyUp: W3C KeyboardEvent.code (asset.KeyCodes); "" when unmapped
	Button sim.ButtonSet // ButtonDown/ButtonUp: sim.ButtonLeft, ButtonMiddle or ButtonRight
	X, Y   float32       // MouseMove, ButtonDown, ButtonUp
	Text   string        // Text
	W, H   int           // Resize
}

// Options configures Open.
type Options struct {
	Title  string
	Width  int // client area width in pixels
	Height int // client area height in pixels
}

// Window is an open player window.
type Window interface {
	// Poll returns the events received since the previous call without blocking.
	Poll() ([]Event, error)
	// Present copies img to the client area. img is expected to have the current client
	// size (see Size); a different size is drawn at the top-left corner, clipped.
	Present(img *gfx.Image) error
	// Size returns the current client area size.
	Size() (w, h int)
	// Close destroys the window. It is safe to call more than once.
	Close() error
}

// Open creates and shows a window. It fails when the platform has no display (for
// example a Linux server without $DISPLAY) or is not supported.
func Open(o Options) (Window, error) {
	if o.Width <= 0 {
		o.Width = 1280
	}
	if o.Height <= 0 {
		o.Height = 720
	}
	if o.Title == "" {
		o.Title = "Veduta"
	}
	return open(o)
}
