package platform

import (
	"fmt"
	"os"
)

// Backends of the player window on Linux. A desktop has a display server; an appliance has
// a panel and nothing else, and the same binary has to serve both.
const (
	BackendX11   = "x11"
	BackendFB    = "fbdev"
	BackendAuto  = "auto"
	backendEnv   = "VEDUTA_BACKEND"
	displayEnv   = "DISPLAY"
	waylandEnv   = "WAYLAND_DISPLAY"
	fbDeviceEnv  = "VEDUTA_FB"
	renderScaleE = "VEDUTA_SCALE"
)

// open picks the backend. VEDUTA_BACKEND forces one; by default the X11 window is used
// when a display server is there and the panel's framebuffer when it is not, so one
// binary runs on a developer's desktop and on the console.
func open(o Options) (Window, error) {
	switch b := os.Getenv(backendEnv); b {
	case "", BackendAuto:
		if os.Getenv(displayEnv) != "" || os.Getenv(waylandEnv) != "" {
			return openX11Window(o)
		}
		w, err := openFB(o)
		if err != nil {
			return nil, fmt.Errorf("%w (no %s or %s either; set %s=x11 to force the display server, or %s to name a framebuffer)",
				err, displayEnv, waylandEnv, backendEnv, fbDeviceEnv)
		}
		return w, nil
	case BackendX11:
		return openX11Window(o)
	case BackendFB:
		return openFB(o)
	default:
		return nil, fmt.Errorf("platform: %s %q (want %s, %s or %s)", backendEnv, b, BackendAuto, BackendX11, BackendFB)
	}
}

// openX11Window connects to the X server named by $DISPLAY.
func openX11Window(o Options) (Window, error) {
	w, err := openX11(o, os.Getenv(displayEnv), xauthPath())
	if err != nil {
		return nil, err
	}
	return w, nil
}
