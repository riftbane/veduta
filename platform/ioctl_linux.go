package platform

import (
	"runtime"
	"syscall"
	"unsafe"
)

// The few ioctls of the input path. Drawing needs none; input needs one when a device is
// opened, to learn the ranges of its axes.

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
