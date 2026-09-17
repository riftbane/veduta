// Package platform puts the player on the screen and turns input devices into the
// console's buttons. It is used only by the player build (veduta.Run without -headless) and
// is never imported by the veduta tool.
//
// There is one backend, pure Go with CGO_ENABLED=0: frames are written to a Linux
// framebuffer (the console's panel, or the 32-bit framebuffer of a PC at a text console)
// and gamepads and keyboards are read from the kernel's event devices, their buttons and
// keys translated to the eight game buttons (sim.Button) and Home, and the devices taken
// for the player alone while it polls, so a keyboard does not also type into the text
// console (window_fb_linux.go, fb_linux.go, evdev_linux.go, padsource_linux.go). On
// Windows the player opens the simulator, a window that shows the panel at a whole scale in
// its 16-bit colors and reads the keyboard as the console's buttons (window_windows.go,
// sim.go). On every other GOOS, Open fails at runtime (window_other.go).
package platform

import (
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/sim"
)

// EventKind classifies an input event.
type EventKind uint8

// Event kinds.
const (
	Press     EventKind = iota + 1 // Button went down (auto-repeat is filtered out)
	Release                        // Button went up
	FocusLost                      // input was lost: release everything; not produced by the framebuffer backend
	Close                          // the player asked to leave: Home, Select and Start held together on a pad, Ctrl+Q on a keyboard
	Tool                           // a keyboard's tool key went down: the player's own functions, never the game's
)

// ToolKey is a key of a keyboard that works the player rather than the game.
type ToolKey uint8

// The tool keys.
const (
	ToolOverlay ToolKey = iota + 1 // F1: show the timings and triangles against the console's budget
	ToolRecord                     // F5: restart and record the buttons, F5 again to save them as a scenario
	ToolReload                     // F9: read the scripts and assets again and restart from the start
)

// Event is one input event.
type Event struct {
	Kind   EventKind
	Button sim.Button // Press, Release
	Tool   ToolKey    // Tool
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
