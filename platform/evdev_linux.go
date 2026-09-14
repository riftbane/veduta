package platform

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// Reading a pad needs no ioctl and no C: the kernel hands out fixed-size records on an
// ordinary file descriptor. Only their size varies, because the timestamp is two C longs,
// so it is computed rather than assumed — 24 bytes on a 64-bit kernel, 16 on a 32-bit one
// such as an ARMv6 board.
var eventSize = 2*(strconv.IntSize/8) + 8

// padDecoder turns evdev records into platform events, keeping just enough state to
// release what is held when the pad goes away or the kernel drops events.
type padDecoder struct {
	size int                      // bytes per record, eventSize unless a test says otherwise
	held map[uint16]string        // evdev code → the W3C code reported down for it
	axis map[uint16]string        // axis → the direction currently down for it
	exit [len(exitChords)][2]bool // the two members of each exit chord, held or not
	quit bool                     // a chord was closed: emit Close once
}

// exitChords close the player: Select and Start on either kind of pad, and a keyboard's
// Ctrl (either one) and Q. A console has no other way back, and a game must not be able to
// swallow it. The array is as long as its contents, and so is the state kept for it.
var exitChords = [...][2]uint16{padExitChords[0], padExitChords[1], keyboardExit[0], keyboardExit[1]}

func newPadDecoder() *padDecoder {
	return &padDecoder{size: eventSize, held: map[uint16]string{}, axis: map[uint16]string{}}
}

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
			// Whatever was held may have been released while we were not looking.
			out = d.releaseAll(out)
		}
	case evKey:
		if value == 2 {
			return out // auto-repeat: the engine reports a key once
		}
		down := value != 0
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
		name, ok := padButtons[code]
		if !ok {
			if name, ok = padDPad[code]; !ok {
				// Not a pad at all: a keyboard, which a console falls back to when no
				// pad is plugged in.
				if name, ok = evdevKeys[code]; !ok {
					return out
				}
			}
		}
		out = d.set(out, code, name, down)
	case evAbs:
		switch code {
		case absHat0X, absX:
			out = d.direction(out, code, value, dirLeft, dirRight)
		case absHat0Y, absY:
			out = d.direction(out, code, value, dirUp, dirDown)
		}
	}
	return out
}

// direction turns an axis into the two keys it stands for. A stick rests near the middle
// of its range, so anything inside the dead zone counts as released.
func (d *padDecoder) direction(out []Event, code uint16, value int32, neg, pos string) []Event {
	const deadZone = 8192 // a hat reports -1, 0, 1; a stick reports thousands
	want := ""
	switch {
	case value <= -1 && (value <= -deadZone || value == -1):
		want = neg
	case value >= 1 && (value >= deadZone || value == 1):
		want = pos
	}
	if now := d.axis[code]; now != want {
		if now != "" {
			out = append(out, Event{Kind: KeyUp, Code: now})
		}
		if want != "" {
			out = append(out, Event{Kind: KeyDown, Code: want})
		}
		if want == "" {
			delete(d.axis, code)
		} else {
			d.axis[code] = want
		}
	}
	return out
}

// set reports a button, ignoring a press of something already down.
func (d *padDecoder) set(out []Event, code uint16, name string, down bool) []Event {
	_, was := d.held[code]
	switch {
	case down && !was:
		d.held[code] = name
		return append(out, Event{Kind: KeyDown, Code: name})
	case !down && was:
		delete(d.held, code)
		return append(out, Event{Kind: KeyUp, Code: name})
	}
	return out
}

// releaseAll reports every key it had said was down, in a fixed order so a replay of the
// same session gives the same events.
func (d *padDecoder) releaseAll(out []Event) []Event {
	for _, code := range sortedCodes(d.held) {
		out = append(out, Event{Kind: KeyUp, Code: d.held[code]})
		delete(d.held, code)
	}
	for _, code := range sortedCodes(d.axis) {
		out = append(out, Event{Kind: KeyUp, Code: d.axis[code]})
		delete(d.axis, code)
	}
	return out
}

func sortedCodes(m map[uint16]string) []uint16 {
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

func (s *evdevSource) close() error { return s.f.Close() }
