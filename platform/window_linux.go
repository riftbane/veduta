//go:build linux

package platform

// The X11 player window. open connects to $DISPLAY, runs the connection setup, creates
// and maps a window and starts one reader goroutine that decodes events and errors into
// a queue; Poll converts the queue into Events on the caller's goroutine. Present writes
// the frame as ZPixmap PutImage requests in horizontal strips under the server's maximum
// request length (raised by BIG-REQUESTS when the server has it).

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/sim"
)

const (
	dialTimeout  = 10 * time.Second
	setupTimeout = 10 * time.Second // connection setup and window creation
	closeTimeout = time.Second      // writing the final requests in Close
	maxTitle     = 4096             // bytes of the title sent to the window manager
	maxReplySize = 16 << 20         // larger replies are treated as a broken connection

	// maxBigRequestUnits caps the BIG-REQUESTS maximum (Xorg's own value, 16 MiB) so
	// that strip sizes fit an int on 32-bit hosts whatever the server announces.
	maxBigRequestUnits = 1 << 22
)

var errClosed = errors.New("platform: X11 window is closed")

// open connects to the X server named by $DISPLAY.
func open(o Options) (Window, error) {
	w, err := openX11(o, os.Getenv("DISPLAY"), xauthPath())
	if err != nil {
		return nil, err
	}
	return w, nil
}

// x11Window is a Window on an X server. Poll, Present and Size must be called from one
// goroutine (the main one); Close may be called from any goroutine, any number of times.
type x11Window struct {
	conn net.Conn
	r    *bufio.Reader // owned by the reader goroutine once it runs

	// Set while opening, read-only afterwards.
	info                      *setupInfo
	win, gc                   uint32
	alphaOr                   uint32 // 0xff000000 for depth-32 visuals, so compositors see opaque pixels
	maxReqUnits               uint32 // maximum request length in 4-byte units
	bigReq                    bool   // BIG-REQUESTS is enabled
	bigReqOpcode              byte
	atomProtocols, atomDelete uint32
	codes                     [256]string    // keycode → W3C code ("" = unmapped)
	syms                      [256][2]uint32 // keycode → group-1 keysyms (unshifted, shifted)
	numLock                   uint16         // modifier mask of NumLock (0 = none)
	nextID                    uint32

	wmu sync.Mutex // serializes writes; guards seq and buf
	seq uint16     // sequence number of the last request sent
	buf []byte     // PutImage strip buffer, reused across frames

	closing atomic.Bool
	done    chan struct{} // closed when the reader goroutine has exited

	qmu  sync.Mutex
	q    []rawEvent // decoded by the reader, drained by Poll
	qerr error      // first X error or connection failure (sticky)

	// Poll state, owned by the goroutine that calls Poll, Present and Size.
	spare         []rawEvent
	held          rawEvent // trailing KeyRelease kept for one Poll (auto-repeat detection)
	hasHeld       bool
	down          [256]bool // keycodes currently down
	width, height int
}

// rawEvent is an X event for the window, reduced to the fields Poll needs. kind is the
// X event code (xKeyPress, …).
type rawEvent struct {
	kind   byte
	detail byte // keycode or button
	state  uint16
	time   uint32
	x, y   int16
	w, h   uint16
}

// openX11 connects to display (a $DISPLAY value) with the cookie from the Xauthority
// file at authFile and opens the window.
func openX11(o Options, display, authFile string) (*x11Window, error) {
	if o.Width > 32767 || o.Height > 32767 {
		return nil, fmt.Errorf("platform: window size %dx%d exceeds the X11 limit of 32767", o.Width, o.Height)
	}
	d, err := parseDisplay(display)
	if err != nil {
		return nil, fmt.Errorf("platform: no X11 display: %w", err)
	}
	conn, err := d.dial(dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("platform: connect to X11 display %q: %w", display, err)
	}
	w, err := newX11Window(conn, loadCookie(authFile, conn, d.display), d.screen, o)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("platform: X11 display %q: %w", display, err)
	}
	return w, nil
}

// batch accumulates requests to send with one write.
type batch struct {
	b []byte
	n int
}

// add appends a request and returns its index in the batch.
func (bt *batch) add(req []byte) int {
	bt.b = append(bt.b, req...)
	bt.n++
	return bt.n - 1
}

// send writes the batch and returns the sequence number of its first request.
func (w *x11Window) send(bt *batch) (uint16, error) {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	first := w.seq + 1
	if _, err := w.conn.Write(bt.b); err != nil {
		return 0, err
	}
	w.seq += uint16(bt.n)
	return first, nil
}

// newX11Window runs the connection setup on conn and creates the window. It does not
// close conn on failure.
func newX11Window(conn net.Conn, cookie []byte, screen int, o Options) (*x11Window, error) {
	if err := conn.SetDeadline(time.Now().Add(setupTimeout)); err != nil {
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	if _, err := conn.Write(setupRequest(cookieName, cookie)); err != nil {
		return nil, fmt.Errorf("send connection setup: %w", err)
	}
	r := bufio.NewReaderSize(conn, 64<<10)
	body, err := readSetupReply(r)
	if err != nil {
		return nil, err
	}
	info, err := parseSetup(body, screen)
	if err != nil {
		return nil, fmt.Errorf("connection setup: %w", err)
	}
	if info.minKeycode < 8 || info.minKeycode > info.maxKeycode {
		return nil, fmt.Errorf("connection setup: invalid keycode range %d..%d", info.minKeycode, info.maxKeycode)
	}
	w := &x11Window{
		conn: conn, r: r, info: info, maxReqUnits: info.maxReqUnits,
		width: o.Width, height: o.Height, done: make(chan struct{}),
	}
	if info.visual.depth == 32 {
		w.alphaOr = 0xff000000
	}

	// Queries, pipelined: one write, then the replies in order.
	var q batch
	iExt := q.add(encQueryExtension("BIG-REQUESTS"))
	atomNames := [...]string{"WM_PROTOCOLS", "WM_DELETE_WINDOW", "_NET_WM_NAME", "UTF8_STRING"}
	var iAtoms [len(atomNames)]int
	for i, name := range atomNames {
		iAtoms[i] = q.add(encInternAtom(name))
	}
	nKeycodes := int(info.maxKeycode) - int(info.minKeycode) + 1
	iKeys := q.add(encGetKeyboardMapping(info.minKeycode, nKeycodes))
	iMods := q.add(encGetModifierMapping())
	first, err := w.send(&q)
	if err != nil {
		return nil, fmt.Errorf("send queries: %w", err)
	}
	ext, err := w.await(first + uint16(iExt))
	if err != nil {
		return nil, fmt.Errorf("QueryExtension BIG-REQUESTS: %w", err)
	}
	var atoms [len(atomNames)]uint32
	for i, name := range atomNames {
		rep, err := w.await(first + uint16(iAtoms[i]))
		if err != nil {
			return nil, fmt.Errorf("InternAtom %s: %w", name, err)
		}
		if atoms[i] = le32(rep[8:]); atoms[i] == 0 {
			return nil, fmt.Errorf("InternAtom %s: server returned None", name)
		}
	}
	w.atomProtocols, w.atomDelete = atoms[0], atoms[1]
	atomNetWMName, atomUTF8 := atoms[2], atoms[3]
	keys, err := w.await(first + uint16(iKeys))
	if err != nil {
		return nil, fmt.Errorf("GetKeyboardMapping: %w", err)
	}
	if err := w.setKeymap(keys, info.minKeycode, nKeycodes); err != nil {
		return nil, err
	}
	mods, err := w.await(first + uint16(iMods))
	if err != nil {
		return nil, fmt.Errorf("GetModifierMapping: %w", err)
	}
	w.numLock = numLockMask(mods, &w.syms)

	if ext[8] != 0 { // BIG-REQUESTS present: enable it
		w.bigReqOpcode = ext[9]
		var e batch
		e.add(encBigReqEnable(w.bigReqOpcode))
		seq, err := w.send(&e)
		if err != nil {
			return nil, fmt.Errorf("send BigReqEnable: %w", err)
		}
		rep, err := w.await(seq)
		if err != nil {
			return nil, fmt.Errorf("BigReqEnable: %w", err)
		}
		w.bigReq = true
		if m := min(le32(rep[8:]), maxBigRequestUnits); m > w.maxReqUnits {
			w.maxReqUnits = m
		}
	}
	if w.maxReqUnits < 16 {
		return nil, fmt.Errorf("connection setup: maximum request length of %d bytes is too small", w.maxReqUnits*4)
	}

	// The window. Values follow the order of their mask bits.
	w.win, w.gc = w.newID(), w.newID()
	var c batch
	mask := uint32(0x1 | 0x8 | 0x10 | 0x800) // background-pixmap, border-pixel, bit-gravity, event-mask
	values := []uint32{0, 0, 1, windowEventMask}
	if info.visual.id != info.rootVisual {
		cmap := w.newID()
		c.add(encCreateColormap(cmap, info.root, info.visual.id))
		mask |= 0x2000 // colormap
		values = append(values, cmap)
	}
	c.add(encCreateWindow(info.visual.depth, w.win, info.root, o.Width, o.Height, info.visual.id, mask, values))
	title := clipUTF8(strings.ToValidUTF8(o.Title, "�"), maxTitle)
	c.add(encChangeProperty(w.win, atomWMName, atomSTRING, 8, latin1(title)))
	c.add(encChangeProperty(w.win, atomNetWMName, atomUTF8, 8, []byte(title)))
	instance := clipUTF8(filepath.Base(os.Args[0]), 256)
	c.add(encChangeProperty(w.win, atomWMClass, atomSTRING, 8, latin1(instance+"\x00"+title+"\x00")))
	c.add(encChangeProperty(w.win, w.atomProtocols, atomATOM, 32, uint32Bytes(w.atomDelete)))
	c.add(encChangeProperty(w.win, atomWMNormalHints, atomWMSizeHints, 32, uint32Bytes(sizeHints(o.Width, o.Height)...)))
	c.add(encCreateGC(w.gc, w.win))
	c.add(encResource(opMapWindow, w.win))
	iSync := c.add(encGetInputFocus()) // round trip: errors of the requests above arrive first
	first, err = w.send(&c)
	if err != nil {
		return nil, fmt.Errorf("send CreateWindow: %w", err)
	}
	if _, err := w.await(first + uint16(iSync)); err != nil {
		return nil, fmt.Errorf("create window: %w", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("clear deadline: %w", err)
	}
	go w.readLoop()
	return w, nil
}

// sizeHints returns WM_NORMAL_HINTS (WM_SIZE_HINTS, 18 CARD32) with PSize and PMinSize.
func sizeHints(width, height int) []uint32 {
	h := make([]uint32, 18)
	h[0] = 8 | 16 // PSize | PMinSize
	h[3], h[4] = uint32(width), uint32(height)
	h[5], h[6] = 1, 1
	return h
}

// newID allocates a resource id from the setup's base and mask.
func (w *x11Window) newID() uint32 {
	w.nextID += w.info.ridMask & -w.info.ridMask
	return w.info.ridBase | (w.nextID & w.info.ridMask)
}

// readMsg reads one reply, error or event (used before the reader goroutine starts).
func (w *x11Window) readMsg() ([]byte, error) {
	m := make([]byte, 32)
	if _, err := io.ReadFull(w.r, m); err != nil {
		return nil, err
	}
	n, err := extraLen(m)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		m = append(m, make([]byte, n)...)
		if _, err := io.ReadFull(w.r, m[32:]); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// extraLen returns the number of bytes that follow the first 32 of a message.
func extraLen(m []byte) (int, error) {
	if c := m[0] & 0x7f; c != xReply && c != xGenericEvent {
		return 0, nil
	}
	n := uint64(le32(m[4:])) * 4
	if n > maxReplySize {
		return 0, fmt.Errorf("implausible reply length %d bytes", n)
	}
	return int(n), nil
}

// await reads messages until the reply to request seq, queueing events and failing on
// the first X error.
func (w *x11Window) await(seq uint16) ([]byte, error) {
	for {
		m, err := w.readMsg()
		if err != nil {
			return nil, fmt.Errorf("read reply: %w", err)
		}
		switch m[0] & 0x7f {
		case xError:
			return nil, decodeXError(m, w.bigReqOpcode)
		case xReply:
			if le16(m[2:]) == seq {
				return m, nil
			}
		default:
			w.dispatch(m)
		}
	}
}

// setKeymap stores the group-1 keysyms and codes of a GetKeyboardMapping reply.
func (w *x11Window) setKeymap(m []byte, first byte, count int) error {
	per := int(m[1])
	if per == 0 || len(m) < 32+count*per*4 {
		return fmt.Errorf("GetKeyboardMapping: malformed reply (%d keysyms per keycode, %d bytes for %d keycodes)", per, len(m), count)
	}
	for i := 0; i < count; i++ {
		o := 32 + i*per*4
		k0, k1 := le32(m[o:]), uint32(0)
		if per > 1 {
			k1 = le32(m[o+4:])
		}
		kc := int(first) + i
		w.syms[kc] = [2]uint32{k0, k1}
		w.codes[kc] = keysymCode(k0)
	}
	return nil
}

// numLockMask returns the mask of the modifier (Mod1…Mod5) that has a Num_Lock key in a
// GetModifierMapping reply, or 0.
func numLockMask(m []byte, syms *[256][2]uint32) uint16 {
	n := int(m[1])
	if len(m) < 32+8*n {
		return 0
	}
	for mod := 3; mod < 8; mod++ {
		for _, kc := range m[32+mod*n : 32+(mod+1)*n] {
			if kc != 0 && (syms[kc][0] == keysymNumLock || syms[kc][1] == keysymNumLock) {
				return 1 << mod
			}
		}
	}
	return 0
}

// readLoop decodes messages until the connection closes.
func (w *x11Window) readLoop() {
	defer close(w.done)
	var m [32]byte
	for {
		if _, err := io.ReadFull(w.r, m[:]); err != nil {
			w.lost(err)
			return
		}
		n, err := extraLen(m[:])
		if err == nil && n > 0 {
			_, err = w.r.Discard(n) // replies to nothing we asked for, generic events
		}
		if err != nil {
			w.lost(err)
			return
		}
		w.dispatch(m[:])
	}
}

// lost records a connection failure unless the window is being closed.
func (w *x11Window) lost(err error) {
	if !w.closing.Load() {
		w.setErr(fmt.Errorf("platform: X11 connection lost: %w", err))
	}
}

func (w *x11Window) setErr(err error) {
	w.qmu.Lock()
	if w.qerr == nil {
		w.qerr = err
	}
	w.qmu.Unlock()
}

func (w *x11Window) stickyErr() error {
	w.qmu.Lock()
	defer w.qmu.Unlock()
	return w.qerr
}

// dispatch queues the events of interest for the window and records X errors.
func (w *x11Window) dispatch(m []byte) {
	var e rawEvent
	switch code := m[0] & 0x7f; code {
	case xError:
		w.setErr(fmt.Errorf("platform: %w", decodeXError(m, w.bigReqOpcode)))
		return
	case xKeyPress, xKeyRelease, xButtonPress, xButtonRelease, xMotionNotify:
		if le32(m[12:]) != w.win {
			return
		}
		e = rawEvent{kind: code, detail: m[1], time: le32(m[4:]),
			x: int16(le16(m[24:])), y: int16(le16(m[26:])), state: le16(m[28:])}
	case xFocusOut:
		if le32(m[4:]) != w.win || m[1] == 2 { // NotifyInferior: focus moved to a child
			return
		}
		e.kind = code
	case xConfigureNotify:
		if le32(m[8:]) != w.win {
			return
		}
		e = rawEvent{kind: code, w: le16(m[20:]), h: le16(m[22:])}
	case xClientMessage:
		if le32(m[4:]) != w.win || m[1] != 32 || le32(m[8:]) != w.atomProtocols || le32(m[12:]) != w.atomDelete {
			return
		}
		e.kind = code
	case xDestroyNotify:
		if le32(m[8:]) != w.win {
			return
		}
		e.kind = code
	default:
		return
	}
	w.qmu.Lock()
	w.q = append(w.q, e)
	w.qmu.Unlock()
}

// Poll returns the events received since the previous call without blocking. Key
// auto-repeat is filtered: a KeyRelease followed by a KeyPress of the same key within a
// millisecond is dropped as a pair (the repeat's Text is kept), and so is a KeyPress of
// a key that is already down. A KeyRelease that is the last event received is held
// until the next Poll, because its repeat KeyPress may still be in flight. An X error
// or a lost connection is returned (with the events decoded before it) by this and every
// later call.
func (w *x11Window) Poll() ([]Event, error) {
	if w.closing.Load() {
		return nil, errClosed
	}
	w.qmu.Lock()
	raw := w.q
	w.q = w.spare[:0]
	err := w.qerr
	w.qmu.Unlock()

	var out []Event
	i := 0
	if w.hasHeld {
		w.hasHeld = false
		if len(raw) > 0 && isRepeat(w.held, raw[0]) {
			out = w.keyDown(out, raw[0])
			i = 1
		} else {
			out = w.keyUp(out, w.held)
		}
	}
	for ; i < len(raw); i++ {
		e := raw[i]
		switch e.kind {
		case xKeyPress:
			out = w.keyDown(out, e)
		case xKeyRelease:
			switch {
			case i+1 < len(raw) && isRepeat(e, raw[i+1]):
				out = w.keyDown(out, raw[i+1])
				i++
			case i+1 == len(raw) && err == nil:
				w.held, w.hasHeld = e, true
			default:
				out = w.keyUp(out, e)
			}
		case xMotionNotify:
			if n := len(out); n > 0 && out[n-1].Kind == MouseMove {
				out[n-1].X, out[n-1].Y = float32(e.x), float32(e.y)
			} else {
				out = append(out, Event{Kind: MouseMove, X: float32(e.x), Y: float32(e.y)})
			}
		case xButtonPress, xButtonRelease:
			b := xButton(e.detail)
			if b == 0 {
				continue // wheel (4–7) and extra buttons
			}
			k := ButtonDown
			if e.kind == xButtonRelease {
				k = ButtonUp
			}
			out = append(out, Event{Kind: k, Button: b, X: float32(e.x), Y: float32(e.y)})
		case xFocusOut:
			w.down = [256]bool{}
			out = append(out, Event{Kind: FocusLost})
		case xConfigureNotify:
			width, height := int(e.w), int(e.h)
			if width == w.width && height == w.height {
				continue
			}
			w.width, w.height = width, height
			if n := len(out); n > 0 && out[n-1].Kind == Resize {
				out[n-1].W, out[n-1].H = width, height
			} else {
				out = append(out, Event{Kind: Resize, W: width, H: height})
			}
		case xClientMessage, xDestroyNotify:
			out = append(out, Event{Kind: Close})
		}
	}
	w.spare = raw[:0]
	return out, err
}

// isRepeat reports whether press is the auto-repeat KeyPress that follows release.
func isRepeat(release, press rawEvent) bool {
	return press.kind == xKeyPress && press.detail == release.detail && press.time-release.time < 2
}

// keyDown emits KeyDown unless the key is already down, then the key's Text.
func (w *x11Window) keyDown(out []Event, e rawEvent) []Event {
	if !w.down[e.detail] {
		w.down[e.detail] = true
		out = append(out, Event{Kind: KeyDown, Code: w.codes[e.detail]})
	}
	if t := keyText(w.syms[e.detail][0], w.syms[e.detail][1], e.state, w.numLock); t != "" {
		out = append(out, Event{Kind: Text, Text: t})
	}
	return out
}

func (w *x11Window) keyUp(out []Event, e rawEvent) []Event {
	w.down[e.detail] = false
	return append(out, Event{Kind: KeyUp, Code: w.codes[e.detail]})
}

// xButton maps X pointer buttons 1–3 to sim buttons; others map to 0.
func xButton(b byte) sim.ButtonSet {
	switch b {
	case 1:
		return sim.ButtonLeft
	case 2:
		return sim.ButtonMiddle
	case 3:
		return sim.ButtonRight
	}
	return 0
}

// Size returns the client area size of the last Resize delivered by Poll (initially the
// requested size).
func (w *x11Window) Size() (int, int) { return w.width, w.height }

// Present draws img at the top-left corner of the window, clipped to the window.
func (w *x11Window) Present(img *gfx.Image) error {
	if img == nil {
		return errors.New("platform: Present: nil image")
	}
	if img.W < 0 || img.H < 0 || len(img.Pix) < img.W*img.H {
		return fmt.Errorf("platform: Present: %dx%d image has %d pixels", img.W, img.H, len(img.Pix))
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	if w.closing.Load() {
		return errClosed
	}
	if err := w.stickyErr(); err != nil {
		return err
	}
	width, height := min(img.W, w.width), min(img.H, w.height)
	if width <= 0 || height <= 0 {
		return nil
	}
	info := w.info
	rowBytes := (width*info.bpp + info.scanlinePad - 1) / info.scanlinePad * info.scanlinePad / 8
	hdr := 24
	if w.bigReq {
		hdr = 28 // room for the extended length field
	}
	rows := (int(w.maxReqUnits)*4 - hdr - 3) / rowBytes // -3: room to pad the data to 4 bytes
	if rows < 1 {
		return fmt.Errorf("platform: Present: a %d-pixel row (%d bytes) exceeds the X server's maximum request length of %d bytes", width, rowBytes, w.maxReqUnits*4)
	}
	rows = min(rows, height)
	if need := hdr + pad4(rows*rowBytes); cap(w.buf) < need {
		w.buf = make([]byte, need)
	}
	for y := 0; y < height; y += rows {
		n := min(rows, height-y)
		size := 24 + pad4(n*rowBytes)
		big := size/4 > 0xffff
		if big {
			size += 4
		}
		b := w.buf[:size]
		b[0], b[1] = opPutImage, 2 // ZPixmap
		o := 4
		if big {
			put16(b[2:], 0)
			put32(b[4:], uint32(size/4))
			o = 8
		} else {
			put16(b[2:], uint16(size/4))
		}
		put32(b[o:], w.win)
		put32(b[o+4:], w.gc)
		put16(b[o+8:], uint16(width))
		put16(b[o+10:], uint16(n))
		put16(b[o+12:], 0) // dst-x
		put16(b[o+14:], uint16(y))
		b[o+16], b[o+17], b[o+18], b[o+19] = 0, info.visual.depth, 0, 0 // left-pad, depth
		px := b[o+20:]
		for r := 0; r < n; r++ {
			src := img.Pix[(y+r)*img.W:][:width]
			packRow(px[r*rowBytes:][:rowBytes], src, info.bpp, info.imageMSB, w.alphaOr)
		}
		if _, err := w.conn.Write(b); err != nil {
			if w.closing.Load() {
				return errClosed
			}
			return fmt.Errorf("platform: X11 PutImage: %w", err)
		}
		w.seq++
	}
	return nil
}

// packRow converts BGRA8 pixels (0xAARRGGBB) to the server's ZPixmap layout: 32 or 24
// bits per pixel, least or most significant byte first.
func packRow(d []byte, src []uint32, bpp int, msb bool, alphaOr uint32) {
	switch {
	case bpp == 32 && !msb:
		d = d[:4*len(src)]
		for i, p := range src {
			binary.LittleEndian.PutUint32(d[4*i:], p|alphaOr)
		}
	case bpp == 32:
		d = d[:4*len(src)]
		for i, p := range src {
			binary.BigEndian.PutUint32(d[4*i:], p|alphaOr)
		}
	case !msb:
		d = d[:3*len(src)]
		for i, p := range src {
			d[3*i], d[3*i+1], d[3*i+2] = byte(p), byte(p>>8), byte(p>>16)
		}
	default:
		d = d[:3*len(src)]
		for i, p := range src {
			d[3*i], d[3*i+1], d[3*i+2] = byte(p>>16), byte(p>>8), byte(p)
		}
	}
}

// Close destroys the window, closes the connection and waits for the reader goroutine
// to exit. Later calls return nil.
func (w *x11Window) Close() error {
	if !w.closing.CompareAndSwap(false, true) {
		<-w.done
		return nil
	}
	// Best effort: the server also destroys the client's resources when the connection
	// closes. Skip it when a Present is stuck writing (Close from another goroutine).
	if w.wmu.TryLock() {
		var b batch
		b.add(encResource(opFreeGC, w.gc))
		b.add(encResource(opDestroyWindow, w.win))
		if w.conn.SetWriteDeadline(time.Now().Add(closeTimeout)) == nil {
			if _, err := w.conn.Write(b.b); err == nil {
				w.seq += uint16(b.n)
			}
		}
		w.wmu.Unlock()
	}
	err := w.conn.Close()
	<-w.done
	if err != nil {
		return fmt.Errorf("platform: close X11 connection: %w", err)
	}
	return nil
}

// latin1 converts s to Latin-1 for STRING properties; other characters become '?'.
func latin1(s string) []byte {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r >= 0x100 {
			r = '?'
		}
		b = append(b, byte(r))
	}
	return b
}

// clipUTF8 cuts s to at most n bytes at a character boundary.
func clipUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
