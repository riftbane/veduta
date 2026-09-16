package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// findCards returns where the volumes labelled as a console's card are mounted, from
// /dev/disk/by-label and the mount table.
func findCards() []string {
	mounts, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil
	}
	var found []string
	for _, label := range cardLabels {
		dev, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-label", label))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(mounts), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			if d, err := filepath.EvalSymlinks(f[0]); err == nil && d == dev {
				found = append(found, unescapeMount(f[1]))
				break
			}
		}
	}
	return found
}

// unescapeMount undoes the octal escapes (\040 for a space) of the mount table.
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
