package platform

import (
	"runtime"
	"syscall"
	"unsafe"
)

// The few ioctls of the input path. Drawing needs none. Input asks a device for the ranges
// of its axes when it is opened, takes it for the player alone while the player is
// polling, and, when the player closes, throws away what was typed at its terminal.

// Directions of an ioctl request's argument.
const (
	iocNone = iota
	iocRead
	iocWrite
)

// ioc builds an ioctl request the way linux/ioctl.h does: the direction of the argument,
// its size, a type letter and a number. Most architectures, the console's among them, put
// two direction bits above a 14-bit size; PowerPC and MIPS put three bits above a 13-bit
// size and number the directions differently.
func ioc(dir int, typ byte, nr, size uintptr) uintptr {
	bits, shift := [3]uintptr{0, 2, 1}, 30
	switch runtime.GOARCH {
	case "ppc64", "ppc64le", "mips", "mipsle", "mips64", "mips64le":
		bits, shift = [3]uintptr{1, 2, 4}, 29
	}
	return bits[dir]<<shift | size<<16 | uintptr(typ)<<8 | nr
}

// readAxisRange asks an event device what one of its axes reports (EVIOCGABS). The answer
// is a struct input_absinfo, six 32-bit integers in the machine's own order: the current
// value, the least, the greatest, then fuzz, flat and resolution. A device without axes
// refuses; one without this axis answers zeros, which the decoder ignores.
func readAxisRange(fd uintptr, code uint16) (axisRange, bool) {
	var info [6]int32
	req := ioc(iocRead, 'E', 0x40+uintptr(code), unsafe.Sizeof(info))
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(&info))); errno != 0 {
		return axisRange{}, false
	}
	return axisRange{min: info[1], max: info[2]}, true
}

// grabDevice takes an event device for this process alone, or gives it back (EVIOCGRAB,
// whose argument is the flag itself rather than a pointer to it). While it is taken no
// other reader sees its events, the kernel's text console included: at a Linux text
// console a keyboard would otherwise also type into the terminal, echoing every key onto
// the framebuffer the game draws on, scrolling it at every Enter and leaving the keystrokes
// for the shell to run once the player quits. The kernel gives the device back when the
// descriptor is closed, however the process ends.
func grabDevice(fd uintptr, take bool) error {
	arg := uintptr(0)
	if take {
		arg = 1
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, ioc(iocWrite, 'E', 0x90, 4), arg); errno != 0 {
		return errno
	}
	return nil
}

// flushInput throws away what was typed at a terminal and not yet read (TCFLSH with
// TCIFLUSH), so that keys that reached it while the player ran are not run by the shell
// afterwards. It does nothing when fd is not a terminal. It also does nothing when fd is
// the controlling terminal of a process group other than this one's: the kernel would
// stop a background process for flushing its terminal.
func flushInput(fd uintptr) error {
	var fg int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&fg)))
	if errno == 0 && int(fg) != syscall.Getpgrp() {
		return nil // in the background: leave the terminal to the foreground
	}
	// TIOCGPGRP also refuses a terminal that is not this process's controlling one, which
	// may be flushed without being stopped, and anything that is not a terminal, which
	// refuses TCFLSH in turn.
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, tcflsh(), 0 /* TCIFLUSH */); errno != 0 {
		return errno
	}
	return nil
}

// tcflsh is the TCFLSH request, which predates the encoding ioc builds and is numbered per
// architecture.
func tcflsh() uintptr {
	switch runtime.GOARCH {
	case "ppc64", "ppc64le":
		return 0x2000741f
	case "mips", "mipsle", "mips64", "mips64le":
		return 0x5407
	}
	return 0x540b
}
