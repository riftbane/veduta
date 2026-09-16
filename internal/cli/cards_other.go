//go:build !linux && !windows

package cli

// findCards finds no card: name its folder.
func findCards() []string { return nil }
