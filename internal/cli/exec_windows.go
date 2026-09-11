//go:build windows

package cli

import "errors"

// execSelf is not available on Windows (the tool does not self-update there).
func execSelf() error { return errors.New("re-exec is not supported on Windows") }
