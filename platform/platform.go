// Package platform puts the player on the screen and turns input devices into engine
// events. It is used only by the player build (veduta.Run without -headless) and is never
// imported by the veduta tool.
//
// There is one backend, pure Go with CGO_ENABLED=0: frames are written to a Linux
// framebuffer (the console's panel, or the 32-bit framebuffer of a PC at a text console)
// and gamepads, keyboards and mice are read from the kernel's event devices, their buttons
// and keys translated to W3C key codes, their sticks and pointers to the one analog stick,
// and the devices taken for the player alone while it polls, so a keyboard does not also
// type into the text console (window_fb_linux.go, fb_linux.go, evdev_linux.go,
// stick_linux.go, padsource_linux.go). No window system is involved, so
// nothing here needs a particular OS thread. On every other GOOS, Open fails at runtime
// (window_other.go): Windows and macOS build, test and cross-compile games, and play them
// only headless.
package platform

import (
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/sim"
)

// EventKind classifies an input event.
type EventKind uint8

// Event kinds. The framebuffer backend produces only KeyDown, KeyUp, Stick and Close: a
// console has a pad and perhaps a keyboard and a mouse (which stands in for the stick), no
// text input and a panel that never resizes or loses focus. The other kinds stay in the
// API because the player loop and recorded input handle them, and a backend that has them
// reports them this way.
const (
	KeyDown    EventKind = iota + 1 // Code went down (auto-repeat is filtered out)
	KeyUp                           // Code went up
	MouseMove                       // cursor at X, Y (frame pixels, origin top-left); not produced by the framebuffer backend
	ButtonDown                      // mouse Button went down (X, Y hold the cursor position); not produced by the framebuffer backend
	ButtonUp                        // mouse Button went up; not produced by the framebuffer backend
	Text                            // Text was typed (UTF-8); not produced by the framebuffer backend
	Resize                          // the frame is now W×H pixels; not produced by the framebuffer backend
	Close                           // the player asked to quit (Home or Select+Start on a pad, Ctrl+Q on a keyboard)
	FocusLost                       // input focus was lost: release everything; not produced by the framebuffer backend
	Stick                           // the analog stick is at X, Y (each -1…1, +Y up): a pad's stick, a mouse or a tablet
)

// Event is one input event.
type Event struct {
	Kind   EventKind
	Code   string        // KeyDown/KeyUp: W3C KeyboardEvent.code (asset.KeyCodes); "" when unmapped
	Button sim.ButtonSet // ButtonDown/ButtonUp: sim.ButtonLeft, ButtonMiddle or ButtonRight
	X, Y   float32       // MouseMove, ButtonDown, ButtonUp; Stick
	Text   string        // Text
	W, H   int           // Resize
}

// Options configures Open.
type Options struct {
	Title  string
	Width  int // requested frame width in pixels; the framebuffer backend uses the panel's
	Height int // requested frame height in pixels; the framebuffer backend uses the panel's
}

// Window is where the player draws and where its input comes from. On a console it is the
// whole panel.
type Window interface {
	// Poll returns the events received since the previous call without blocking.
	Poll() ([]Event, error)
	// Present copies img to the screen. img must have the current frame size (see Size).
	Present(img *gfx.Image) error
	// Size returns the current frame size.
	Size() (w, h int)
	// SetPointerLock asks for the cursor to be hidden and kept inside the frame, which is
	// what mouse look needs. No backend implements it: a console has no pointer, and the
	// framebuffer backend accepts the call and does nothing. It is kept so that
	// veduta.Context.LockPointer still has somewhere to go.
	SetPointerLock(on bool) error
	// Close releases the screen and the input devices. It is safe to call more than once.
	Close() error
}

// Open starts the player on this machine's framebuffer. It fails when there is no usable
// framebuffer (no /sys/class/graphics/fbN with 16 or 32 bits per pixel, or one that cannot
// be opened), when VEDUTA_BACKEND names something else, and on every GOOS but Linux. The
// framebuffer backend ignores o.Width and o.Height: the panel decides the size.
func Open(o Options) (Window, error) {
	// A caller that asks for no size gets the reference panel's.
	if o.Width <= 0 {
		o.Width = 320
	}
	if o.Height <= 0 {
		o.Height = 240
	}
	if o.Title == "" {
		o.Title = "Veduta"
	}
	return open(o)
}
