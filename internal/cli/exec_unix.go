//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// execSelf replaces the process with the (just updated) executable, same arguments.
func execSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
