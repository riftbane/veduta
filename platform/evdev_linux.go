package platform

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

// Reading a pad needs no ioctl and no C: the kernel hands out fixed-size records on an
// ordinary file descriptor. Only their size varies, because the timestamp is two C longs,
// so it is computed rather than assumed — 24 bytes on a 64-bit kernel, 16 on a 32-bit one
// such as an ARMv6 board.
var eventSize = 2*(strconv.IntSize/8) + 8

// padDecoder turns evdev records into platform events, keeping just enough state to
// release what is held when the pad goes away or the kernel drops events.
type padDecoder struct {
	size int               // bytes per record, eventSize unless a test says otherwise
	held map[uint16]string // evdev code → the W3C code reported down for it
	axis map[uint16]string // axis → the direction currently down for it
	exit [2][2]bool        // the two members of each exit chord, held or not
	quit bool              // a chord was closed: emit Close once
}

// exitChords close the window: the pad's Select and Start, and a keyboard's Ctrl and Q.
// A console has no other way back, and a game must not be able to swallow it.
var exitChords = [2][2]uint16{exitChord, keyboardExit}

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
		for ci, chord := range exitChords {
			for i, c := range chord {
				if c != code {
					continue
				}
				d.exit[ci][i] = down
				if d.exit[ci][0] && d.exit[ci][1] && !d.quit {
					d.quit = true
					out = d.releaseAll(out)
					return append(out, Event{Kind: Close})
				}
				if !down {
					d.quit = false
				}
			}
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

// evdevSource reads one pad device.
type evdevSource struct {
	f   *os.File
	d   *padDecoder
	buf []byte
	out []Event
}

// openEvdev opens a device node. Reads are kept from blocking by giving each one a
// deadline that has already passed: it takes whatever the kernel has and returns at once,
// so a frame is never held up by a player who is pressing nothing. Opening the file with
// O_NONBLOCK would not do it — the runtime registers the descriptor with its poller and
// waits anyway.
func openEvdev(path string) (*evdevSource, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	if err := f.SetReadDeadline(time.Now()); err != nil {
		f.Close()
		return nil, fmt.Errorf("platform: %s cannot be read without waiting: %w", path, err)
	}
	return &evdevSource{f: f, d: newPadDecoder(), buf: make([]byte, 64*eventSize)}, nil
}

// poll returns the events since the last call. A pad that was unplugged reports every key
// it had down and then the error, so a game never keeps walking into a wall.
func (s *evdevSource) poll() ([]Event, error) {
	s.out = s.out[:0]
	if err := s.f.SetReadDeadline(time.Now()); err != nil {
		return s.out, err
	}
	for {
		n, err := s.f.Read(s.buf)
		if n > 0 {
			var derr error
			if s.out, derr = s.d.decode(s.buf[:n], s.out); derr != nil {
				return s.out, derr
			}
		}
		switch {
		case err == nil && n == len(s.buf):
			continue // there may be more waiting
		case err == nil, errors.Is(err, io.EOF):
			return s.out, nil
		case isWouldBlock(err):
			return s.out, nil
		default:
			return s.d.releaseAll(s.out), err
		}
	}
}

func (s *evdevSource) close() error { return s.f.Close() }

// isWouldBlock reports the "nothing to read yet" error of a non-blocking descriptor
// without naming the syscall package's constant, which differs between platforms.
func isWouldBlock(err error) bool {
	var errno interface{ Timeout() bool }
	if errors.As(err, &errno) && errno.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}
