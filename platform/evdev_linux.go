package platform

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"github.com/riftbane/veduta/v2/sim"
)

// Reading a pad needs no C: the kernel hands out fixed-size records on an ordinary file
// descriptor. Only their size varies, because the timestamp is two C longs, so it is
// computed rather than assumed — 24 bytes on a 64-bit kernel, 16 on a 32-bit one such as an
// ARMv6 board. The one thing the records do not say is the range of an axis, which is asked
// once, with an ioctl, when the device is opened (ioctl_linux.go).
var eventSize = 2*(strconv.IntSize/8) + 8

// padDecoder turns evdev records into platform events, keeping just enough state to
// release what is held when the pad goes away or the kernel drops events.
type padDecoder struct {
	size   int                      // bytes per record, eventSize unless a test says otherwise
	held   map[uint16]sim.Button    // evdev code → the button reported down for it
	axis   map[uint16]sim.Button    // axis → the direction currently down for it
	exit   [len(exitChords)][2]bool // the two members of each exit chord, held or not
	quit   bool                     // a chord was closed: emit Close once
	down   map[uint16]bool          // keys physically down, whatever was reported for them
	ranges [absHat0Y + 1]axisRange  // what the device says each axis reports; zero when unknown

	stickDPad bool // ABS_X and ABS_Y are read as the D-pad
}

// axisRange is the least and the greatest value an axis reports, as EVIOCGABS gives them.
type axisRange struct{ min, max int32 }

// setRange tells the decoder what an axis reports. A range with nothing in it (zeros, for
// an axis the device does not have) leaves the axis read as if no range were known.
func (d *padDecoder) setRange(code uint16, lo, hi int32) {
	if int(code) < len(d.ranges) {
		d.ranges[code] = axisRange{lo, hi}
	}
}

// exitChords close the player: Home (BTN_MODE or KEY_HOMEPAGE), a pad's Select, and a
// keyboard's Ctrl (either one) and Q. A console has no other way back,
// and a game must not be able to swallow it. The array is as long as its contents, and so
// is the state kept for it.
var exitChords = [...][2]uint16{padHome[0], padHome[1], padHome[2], padHome[3], keyboardExit[0], keyboardExit[1]}

// newPadDecoder returns a decoder whose ABS_X and ABS_Y are the D-pad, as they may be on a
// pad whose D-pad is not four buttons.
func newPadDecoder() *padDecoder {
	return &padDecoder{size: eventSize, held: map[uint16]sim.Button{}, axis: map[uint16]sim.Button{}, down: map[uint16]bool{}, stickDPad: true}
}

// pressed reports whether any key or button is physically down: a chord that closed the
// player is reported released to the game while its keys are still held.
func (d *padDecoder) pressed() bool { return len(d.down) > 0 }

// decode appends the events of one read to out. A partial record at the end is a
// programming error rather than something to tolerate: the kernel writes whole records.
func (d *padDecoder) decode(data []byte, out []Event) ([]Event, error) {
	if len(data)%d.size != 0 {
		return out, errors.New("platform: the pad returned a partial event record")
	}
	for i := 0; i+d.size <= len(data); i += d.size {
		rec := data[i : i+d.size]
		typ := binary.LittleEndian.Uint16(rec[d.size-8:])
		code := binary.LittleEndian.Uint16(rec[d.size-6:])
		value := int32(binary.LittleEndian.Uint32(rec[d.size-4:]))
		out = d.event(out, typ, code, value)
	}
	return out, nil
}

func (d *padDecoder) event(out []Event, typ, code uint16, value int32) []Event {
	switch typ {
	case evSyn:
		if code == synDropped {
			// Whatever was held may have been released while we were not looking, the
			// buttons of an exit chord included: half a chord kept from before the drop
			// would let the other half alone quit the game. After lost events a chord has
			// to be pressed again in full.
			out = d.releaseAll(out)
			d.exit = [len(exitChords)][2]bool{}
			d.quit = false
			clear(d.down)
		}
	case evKey:
		if value == 2 {
			return out // auto-repeat: the engine reports a button once
		}
		down := value != 0
		if down {
			d.down[code] = true
		} else {
			delete(d.down, code)
		}
		// Every chord a key belongs to learns of it before any closes: Q is in two, and
		// stopping at the first would leave the other thinking Q is up.
		closed := false
		for ci, chord := range exitChords {
			for i, c := range chord {
				if c != code {
					continue
				}
				d.exit[ci][i] = down
				closed = closed || (d.exit[ci][0] && d.exit[ci][1])
				if !down {
					d.quit = false
				}
			}
		}
		if closed && !d.quit {
			d.quit = true
			out = d.releaseAll(out)
			return append(out, Event{Kind: Close})
		}
		if tool, ok := keyboardTools[code]; ok {
			if down {
				out = append(out, Event{Kind: Tool, Tool: tool})
			}
			return out
		}
		b, ok := padButtons[code]
		if !ok {
			if b, ok = padDPad[code]; !ok {
				// Not a pad at all: a keyboard, which a console falls back to when no
				// pad is plugged in.
				if b, ok = evdevKeys[code]; !ok {
					return out
				}
			}
		}
		out = d.set(out, code, b, down)
	case evAbs:
		switch code {
		case absHat0X:
			out = d.direction(out, code, value, sim.ButtonLeft, sim.ButtonRight)
		case absHat0Y:
			out = d.direction(out, code, value, sim.ButtonUp, sim.ButtonDown)
		case absX:
			if d.stickDPad {
				out = d.direction(out, code, value, sim.ButtonLeft, sim.ButtonRight)
			}
		case absY:
			if d.stickDPad {
				out = d.direction(out, code, value, sim.ButtonUp, sim.ButtonDown)
			}
		}
	}
	return out
}

// direction turns an axis into the two D-pad buttons it stands for. A stick rests near the
// middle of its range, so a value counts as a direction only once it is a quarter of the
// way from the middle to an end (and at least one step away). The range is the device's
// own: 0 to 255 resting at 127, -128 to 127, -32768 to 32767, or -1 to 1 for a hat, where
// any value but the rest is a direction. An axis whose range is not known is read as a hat
// (-1, 0, 1) or a stick centred on zero with a range of thousands.
func (d *padDecoder) direction(out []Event, code uint16, value int32, neg, pos sim.Button) []Event {
	const none = sim.Button(sim.NumButtons) // no direction
	want := none
	if r := d.ranges[code]; r.max > r.min {
		lo, hi, v := int64(r.min), int64(r.max), int64(value)
		mid, half := (lo+hi)/2, (hi-lo)/2
		dead := max(half/4, 1)
		switch {
		case v <= mid-dead:
			want = neg
		case v >= mid+dead:
			want = pos
		}
	} else {
		const deadZone = 8192 // a hat reports -1, 0, 1; a stick reports thousands
		switch {
		case value <= -1 && (value <= -deadZone || value == -1):
			want = neg
		case value >= 1 && (value >= deadZone || value == 1):
			want = pos
		}
	}
	now, ok := d.axis[code]
	if !ok {
		now = none
	}
	if now != want {
		if now != none {
			out = append(out, Event{Kind: Release, Button: now})
		}
		if want != none {
			out = append(out, Event{Kind: Press, Button: want})
			d.axis[code] = want
		} else {
			delete(d.axis, code)
		}
	}
	return out
}

// set reports a button, ignoring a press of something already down.
func (d *padDecoder) set(out []Event, code uint16, b sim.Button, down bool) []Event {
	_, was := d.held[code]
	kind := Press
	switch {
	case down && !was:
		d.held[code] = b
	case !down && was:
		delete(d.held, code)
		kind = Release
	default:
		return out
	}
	return append(out, Event{Kind: kind, Button: b})
}

// releaseAll reports every button it had said was down, in a fixed order so a replay of
// the same session gives the same events.
func (d *padDecoder) releaseAll(out []Event) []Event {
	for _, code := range sortedCodes(d.held) {
		out = append(out, Event{Kind: Release, Button: d.held[code]})
		delete(d.held, code)
	}
	for _, code := range sortedCodes(d.axis) {
		out = append(out, Event{Kind: Release, Button: d.axis[code]})
		delete(d.axis, code)
	}
	return out
}

func sortedCodes(m map[uint16]sim.Button) []uint16 {
	codes := make([]uint16, 0, len(m))
	for c := range m {
		codes = append(codes, c)
	}
	for i := 1; i < len(codes); i++ { // few keys are ever held: insertion sort is enough
		for j := i; j > 0 && codes[j] < codes[j-1]; j-- {
			codes[j], codes[j-1] = codes[j-1], codes[j]
		}
	}
	return codes
}

// evdevSource reads one device node.
type evdevSource struct {
	f    *os.File
	raw  syscall.RawConn
	d    *padDecoder
	buf  []byte
	out  []Event
	n    int                   // what the last read returned
	err  error                 // and its error
	read func(fd uintptr) bool // one read(2) into buf, made once so a poll allocates nothing
}

// openEvdev opens a device node for reading without ever waiting. The descriptor is
// non-blocking, and each poll calls read(2) on it directly until the kernel answers EAGAIN:
// it takes whatever the kernel has and returns at once, so a frame is never held up by a
// player who is pressing nothing.
//
// Neither of the obvious ways does this. A plain Read on the file waits, because the
// runtime registers the descriptor with its poller and parks the goroutine on EAGAIN. A
// read deadline that has already passed does not wait, but it does not read either: the
// runtime refuses the call before asking the kernel, so a pad read that way never reports
// a press.
func openEvdev(path string) (*evdevSource, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	raw, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("platform: %s: %w", path, err)
	}
	s := &evdevSource{f: f, raw: raw, d: newPadDecoder(), buf: make([]byte, 64*eventSize)}
	// An axis is read against the range its device reports. Where the device will not say
	// (not an event device, or one with no axes) the decoder falls back to a hat or a stick
	// centred on zero.
	raw.Control(func(fd uintptr) {
		for _, code := range [...]uint16{absX, absY, absHat0X, absHat0Y} {
			if r, ok := readAxisRange(fd, code); ok {
				s.d.setRange(code, r.min, r.max)
			}
		}
	})
	s.read = func(fd uintptr) bool {
		for {
			s.n, s.err = syscall.Read(int(fd), s.buf)
			if !errors.Is(s.err, syscall.EINTR) {
				return true // done, whatever the answer: never let the runtime wait for more
			}
		}
	}
	return s, nil
}

// poll returns the events since the last call. A pad that was unplugged reports every key
// it had down and then the error, so a game never keeps walking into a wall.
func (s *evdevSource) poll() ([]Event, error) {
	s.out = s.out[:0]
	for {
		if err := s.raw.Read(s.read); err != nil {
			return s.d.releaseAll(s.out), fmt.Errorf("platform: reading %s: %w", s.f.Name(), err)
		}
		n, err := s.n, s.err
		if n > 0 {
			var derr error
			if s.out, derr = s.d.decode(s.buf[:n], s.out); derr != nil {
				return s.out, derr
			}
		}
		switch {
		case err == nil && n == len(s.buf):
			continue // there may be more waiting
		case err == nil:
			return s.out, nil // all there was, or the end of an ordinary file
		case errors.Is(err, syscall.EAGAIN):
			return s.out, nil // nothing more yet
		default:
			// ENODEV once the device is gone.
			return s.d.releaseAll(s.out), fmt.Errorf("platform: reading %s: %w", s.f.Name(), err)
		}
	}
}

// grab takes the device for this process alone, or gives it back.
func (s *evdevSource) grab(take bool) error {
	var gerr error
	if err := s.raw.Control(func(fd uintptr) { gerr = grabDevice(fd, take) }); err != nil {
		return err
	}
	return gerr
}

func (s *evdevSource) close() error { return s.f.Close() }

// pressed reports whether a key of the device is physically down.
func (s *evdevSource) pressed() bool { return s.d.pressed() }
