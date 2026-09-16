//go:build !linux && !windows

package platform

import (
	"fmt"
	"runtime"
)

// open fails everywhere but Linux, where the player draws on a framebuffer (the console's
// panel), and Windows, where it opens the simulator window. Headless mode works everywhere.
func open(o Options) (Window, error) {
	return nil, fmt.Errorf("platform: the player needs a Linux framebuffer or the Windows simulator, and this is %s/%s", runtime.GOOS, runtime.GOARCH)
}
