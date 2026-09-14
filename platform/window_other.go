//go:build !linux

package platform

import (
	"fmt"
	"runtime"
)

// open fails everywhere but Linux. The player draws on a Linux framebuffer, the console's
// panel; Windows and macOS are machines to build, test and cross-compile on, and no window
// opens there. Headless mode works everywhere.
func open(o Options) (Window, error) {
	return nil, fmt.Errorf("platform: the player needs a Linux framebuffer, and this is %s/%s", runtime.GOOS, runtime.GOARCH)
}
