//go:build !linux && !windows

package platform

import (
	"fmt"
	"runtime"
)

// open fails on platforms without a window implementation (macOS needs cgo and is
// deferred; see the spec's non-goals).
func open(o Options) (Window, error) {
	return nil, fmt.Errorf("platform: no player window on %s/%s in this version (headless mode works everywhere)", runtime.GOOS, runtime.GOARCH)
}
