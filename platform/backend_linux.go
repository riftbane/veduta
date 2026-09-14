package platform

import (
	"fmt"
	"os"
)

// Backends of the player on Linux. There is one: the panel's framebuffer, with input from
// the kernel's event devices. The names stay so VEDUTA_BACKEND keeps its meaning.
const (
	BackendFB    = "fbdev"
	BackendAuto  = "auto"
	backendEnv   = "VEDUTA_BACKEND"
	fbDeviceEnv  = "VEDUTA_FB"
	renderScaleE = "VEDUTA_SCALE"
)

// backendX11 is the value VEDUTA_BACKEND had for the X11 window, recognised only to say
// that it is gone.
const backendX11 = "x11"

// open picks the backend. Only the framebuffer is left, so auto and fbdev are the same
// choice; x11 is named in its own error, because a script or a service file written for an
// older version should learn what happened rather than read that the value is unknown.
func open(o Options) (Window, error) {
	switch b := os.Getenv(backendEnv); b {
	case "", BackendAuto, BackendFB:
		return openFB(o)
	case backendX11:
		return nil, fmt.Errorf("platform: %s=%s: the X11 window was removed in v1.0.0; the player draws on a framebuffer (unset %s, or set it to %s)",
			backendEnv, b, backendEnv, BackendFB)
	default:
		return nil, fmt.Errorf("platform: %s %q (want %s or %s)", backendEnv, b, BackendAuto, BackendFB)
	}
}
