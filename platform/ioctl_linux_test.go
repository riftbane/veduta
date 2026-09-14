package platform

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
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
	if runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64" {
		if tcflsh() != 0x540b {
			t.Errorf("TCFLSH on %s: %#x, want 0x540b", runtime.GOARCH, tcflsh())
		}
	}
}

// newPty opens a pseudo-terminal and returns its two ends. Neither becomes the test's
// controlling terminal.
func newPty(t *testing.T) (master, slave int) {
	t.Helper()
	m, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}
	t.Cleanup(func() { syscall.Close(m) })
	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(m), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Skipf("unlocking the pseudo-terminal: %v", errno)
	}
	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(m), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); errno != 0 {
		t.Skipf("numbering the pseudo-terminal: %v", errno)
	}
	s, err := syscall.Open(fmt.Sprintf("/dev/pts/%d", n), syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("opening /dev/pts/%d: %v", n, err)
	}
	t.Cleanup(func() { syscall.Close(s) })
	return m, s
}

// pendingInput is how many bytes a terminal has for its reader.
func pendingInput(t *testing.T, fd int) int {
	t.Helper()
	var n int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCINQ, uintptr(unsafe.Pointer(&n))); errno != 0 {
		t.Fatalf("TIOCINQ: %v", errno)
	}
	return int(n)
}

// typeAhead types a line at a terminal and waits, a bounded time, for it to be readable.
func typeAhead(t *testing.T, master, slave int) {
	t.Helper()
	if _, err := syscall.Write(master, []byte("wwwdddd\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for pendingInput(t, slave) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("what was typed never reached the terminal")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestFlushInput: keys that reached the player's terminal are thrown away rather than left
// for the shell to run, and a descriptor that is no terminal is left alone.
func TestFlushInput(t *testing.T) {
	master, slave := newPty(t)
	typeAhead(t, master, slave)
	if err := flushInput(uintptr(slave)); err != nil {
		t.Fatal(err)
	}
	if n := pendingInput(t, slave); n != 0 {
		t.Fatalf("%d bytes still waiting for the shell after the flush", n)
	}
	f, err := os.CreateTemp(t.TempDir(), "notatty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := flushInput(f.Fd()); err == nil {
		t.Error("a plain file was flushed like a terminal")
	}
}
