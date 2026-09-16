package cli

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetLogicalDrives     = kernel32.NewProc("GetLogicalDrives")
	procGetVolumeInformation = kernel32.NewProc("GetVolumeInformationW")
)

// findCards returns the roots of the drives whose volume label is a console card's.
func findCards() []string {
	mask, _, _ := procGetLogicalDrives.Call()
	var found []string
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		var label [261]uint16
		// A drive without a medium (an empty card reader) fails, and is not a card.
		if ok, _, _ := procGetVolumeInformation.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&label[0])), uintptr(len(label)), 0, 0, 0, 0, 0); ok == 0 {
			continue
		}
		name := syscall.UTF16ToString(label[:])
		for _, l := range cardLabels {
			if strings.EqualFold(name, l) {
				found = append(found, root)
			}
		}
	}
	return found
}
