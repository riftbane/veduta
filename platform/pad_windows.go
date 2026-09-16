//go:build windows

package platform

import (
	"syscall"
	"time"
	"unsafe"
)

// Reading pads on Windows: XInput for Xbox controllers, WinMM for every other pad. Both are
// system DLLs called through syscall; a machine without them simply has no pads.

var (
	xinput          = firstDLL("xinput1_4.dll", "xinput9_1_0.dll")
	procXInputState *syscall.LazyProc

	winmm           = syscall.NewLazyDLL("winmm.dll")
	procJoyGetNum   = winmm.NewProc("joyGetNumDevs")
	procJoyGetPosEx = winmm.NewProc("joyGetPosEx")
)

func init() {
	if xinput != nil {
		procXInputState = xinput.NewProc("XInputGetState")
	}
}

// firstDLL returns the first of the DLLs that loads, or nil.
func firstDLL(names ...string) *syscall.LazyDLL {
	for _, n := range names {
		if d := syscall.NewLazyDLL(n); d.Load() == nil {
			return d
		}
	}
	return nil
}

// xinputState is XINPUT_STATE.
type xinputState struct {
	packet  uint32
	buttons uint16
	lt, rt  uint8
	lx, ly  int16
	rx, ry  int16
}

// joyInfoEx is JOYINFOEX.
type joyInfoEx struct {
	size, flags                            uint32
	x, y, z, r, u, v                       uint32
	buttons, buttonNumber, pov, res1, res2 uint32
}

const (
	joyReturnAll = 0xFF
	maxJoysticks = 16
	padRescanWin = time.Second
)

// pads reads every connected pad at each poll. WinMM pads are looked for again once a second
// (asking for a missing joystick is slow); while an Xbox controller is connected WinMM is
// not read, since it reports the same controller again.
type pads struct {
	xinput  [4]padTracker
	joy     [maxJoysticks]padTracker
	present []uint32 // WinMM ids answering
	next    time.Time
	state   xinputState
	info    joyInfoEx
}

func (p *pads) poll(out []Event) []Event {
	anyXInput := false
	if procXInputState != nil && procXInputState.Find() == nil {
		for i := range p.xinput {
			p.state = xinputState{}
			r, _, _ := syscall.SyscallN(procXInputState.Addr(), uintptr(i), uintptr(unsafe.Pointer(&p.state)))
			if uint32(r) != 0 { // not connected: let go of whatever it held
				out = p.xinput[i].update(out, 0, false)
				continue
			}
			anyXInput = true
			b, home := xinputButtons(p.state.buttons)
			out = p.xinput[i].update(out, b, home)
		}
	}
	if procJoyGetPosEx.Find() != nil {
		return out
	}
	if now := time.Now(); now.After(p.next) {
		p.next = now.Add(padRescanWin)
		p.present = p.present[:0]
		n, _, _ := syscall.SyscallN(procJoyGetNum.Addr())
		for id := uint32(0); id < min(uint32(n), maxJoysticks); id++ {
			if p.read(id) {
				p.present = append(p.present, id)
			}
		}
	}
	for id := uint32(0); id < maxJoysticks; id++ {
		if anyXInput || !p.listed(id) || !p.read(id) {
			out = p.joy[id].update(out, 0, false)
			continue
		}
		b, home := joystickButtons(joystick{buttons: p.info.buttons, x: p.info.x, y: p.info.y, pov: p.info.pov})
		out = p.joy[id].update(out, b, home)
	}
	return out
}

func (p *pads) listed(id uint32) bool {
	for _, x := range p.present {
		if x == id {
			return true
		}
	}
	return false
}

// read fills p.info for joystick id and reports whether it answered.
func (p *pads) read(id uint32) bool {
	p.info = joyInfoEx{flags: joyReturnAll}
	p.info.size = uint32(unsafe.Sizeof(p.info))
	r, _, _ := syscall.SyscallN(procJoyGetPosEx.Addr(), uintptr(id), uintptr(unsafe.Pointer(&p.info)))
	return uint32(r) == 0
}
