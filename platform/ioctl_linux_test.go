package platform

import (
	"runtime"
	"testing"
)

// TestIoctlRequests pins the request numbers against the values linux/input.h and
// linux/uinput.h give on the architecture running the test.
func TestIoctlRequests(t *testing.T) {
	generic := [][2]uintptr{
		{ioc(iocRead, 'E', 0x40+absX, 24), 0x80184540},     // EVIOCGABS(ABS_X)
		{ioc(iocRead, 'E', 0x40+absHat0Y, 24), 0x80184551}, // EVIOCGABS(ABS_HAT0Y)
		{ioc(iocWrite, 'U', 100, 4), uiSetEvBit},           // UI_SET_EVBIT
		{ioc(iocNone, 'U', 1, 0), uiDevCreate},             // UI_DEV_CREATE
	}
	other := [][2]uintptr{
		{ioc(iocRead, 'E', 0x40+absX, 24), 0x40184540},
		{ioc(iocWrite, 'U', 100, 4), 0x80045564},
		{ioc(iocNone, 'U', 1, 0), 0x20005501},
	}
	want := generic
	switch runtime.GOARCH {
	case "ppc64", "ppc64le", "mips", "mipsle", "mips64", "mips64le":
		want = other
	}
	for i, c := range want {
		if c[0] != c[1] {
			t.Errorf("request %d on %s: %#x, want %#x", i, runtime.GOARCH, c[0], c[1])
		}
	}
}
