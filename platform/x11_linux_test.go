//go:build linux

package platform

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/sim"
)

// ---- in-process fake X server ---------------------------------------------------------

const (
	fakeRoot     = 0x2b
	fakeVisual24 = 0x21
	fakeVisual32 = 0x22
	fakeVisual8  = 0x23 // PseudoColor
	fakeBigReqOp = 133
	fakeRIDBase  = 0x04000000
	fakeRIDMask  = 0x001fffff
	fakeWindowID = fakeRIDBase | 1
	fakeGCID     = fakeRIDBase | 2
)

type fakeConfig struct {
	cookie     []byte // required MIT-MAGIC-COOKIE-1 data; nil = no authorization
	maxReq     uint16 // setup maximum-request-length in units (default 65535)
	bigReq     uint32 // BIG-REQUESTS maximum in units; 0 = extension absent
	msb        bool   // image byte order MSBFirst
	bpp24      byte   // bits per pixel of depth 24 (default 32)
	pad24      byte   // scanline pad of depth 24 (default 32)
	rootVisual uint32 // default fakeVisual24
	errorOn    byte   // answer the first request with this opcode with BadMatch
}

type fakeReq struct {
	op, data byte
	big      bool // sent with the BIG-REQUESTS length encoding
	seq      uint16
	raw      []byte // the whole request
}

// body returns the request after its length field(s).
func (q fakeReq) body() []byte {
	if q.big {
		return q.raw[8:]
	}
	return q.raw[4:]
}

// units returns the request length field in 4-byte units.
func (q fakeReq) units() uint32 {
	if q.big {
		return le32(q.raw[4:])
	}
	return uint32(le16(q.raw[2:]))
}

type fakeX struct {
	t    *testing.T
	cfg  fakeConfig
	ln   net.Listener
	path string
	done chan struct{}

	mu       sync.Mutex
	conn     net.Conn
	reqs     []fakeReq
	authName string
	authData []byte
	atoms    map[string]uint32
	eof      bool
	stopped  bool

	wmu sync.Mutex
}

func newFakeX(t *testing.T, cfg fakeConfig) *fakeX {
	t.Helper()
	if cfg.maxReq == 0 {
		cfg.maxReq = 65535
	}
	if cfg.bpp24 == 0 {
		cfg.bpp24 = 32
	}
	if cfg.pad24 == 0 {
		cfg.pad24 = 32
	}
	if cfg.rootVisual == 0 {
		cfg.rootVisual = fakeVisual24
	}
	dir, err := os.MkdirTemp("", "vx11")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "X7")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeX{t: t, cfg: cfg, ln: ln, path: path, done: make(chan struct{}), atoms: map[string]uint32{}}
	go s.serve()
	t.Cleanup(func() {
		ln.Close()
		s.closeConn()
		<-s.done
	})
	return s
}

// closeConn drops the client; the server stops reporting read errors from then on.
func (s *fakeX) closeConn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	if s.conn != nil {
		s.conn.Close()
	}
}

// errorf reports a protocol violation by the client, unless the server was stopped.
func (s *fakeX) errorf(format string, args ...any) {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if !stopped {
		s.t.Errorf(format, args...)
	}
}

func (s *fakeX) write(b []byte) {
	s.mu.Lock()
	c := s.conn
	s.mu.Unlock()
	s.wmu.Lock()
	defer s.wmu.Unlock()
	c.Write(b)
}

// send writes messages (events, errors, replies) in one write.
func (s *fakeX) send(msgs ...[]byte) {
	s.write(bytes.Join(msgs, nil))
}

func (s *fakeX) requests() []fakeReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]fakeReq(nil), s.reqs...)
}

func (s *fakeX) opcodes() []byte {
	var ops []byte
	for _, q := range s.requests() {
		ops = append(ops, q.op)
	}
	return ops
}

func (s *fakeX) atom(name string) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.atoms[name]
	if !ok {
		a = uint32(300 + len(s.atoms))
		s.atoms[name] = a
	}
	return a
}

func (s *fakeX) serve() {
	defer close(s.done)
	c, err := s.ln.Accept()
	if err != nil {
		return
	}
	s.mu.Lock()
	s.conn = c
	s.mu.Unlock()
	defer c.Close()
	r := bufio.NewReader(c)
	var h [12]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		s.errorf("fake X: read setup: %v", err)
		return
	}
	if h[0] != 'l' || le16(h[2:]) != 11 || le16(h[4:]) != 0 {
		s.errorf("fake X: setup header % x", h)
		return
	}
	nl, dl := int(le16(h[6:])), int(le16(h[8:]))
	rest := make([]byte, pad4(nl)+pad4(dl))
	if _, err := io.ReadFull(r, rest); err != nil {
		s.errorf("fake X: read setup auth: %v", err)
		return
	}
	s.mu.Lock()
	s.authName, s.authData = string(rest[:nl]), rest[pad4(nl):pad4(nl)+dl]
	s.mu.Unlock()
	if s.cfg.cookie != nil && (string(rest[:nl]) != cookieName || !bytes.Equal(rest[pad4(nl):pad4(nl)+dl], s.cfg.cookie)) {
		reason := "Invalid MIT-MAGIC-COOKIE-1 key"
		b := make([]byte, 8+pad4(len(reason)))
		b[1] = byte(len(reason))
		put16(b[2:], 11)
		put16(b[6:], uint16(pad4(len(reason))/4))
		copy(b[8:], reason)
		s.write(b)
		return
	}
	s.write(s.setupReply())
	var seq uint16
	bigOn := false
	for {
		var hd [8]byte
		if _, err := io.ReadFull(r, hd[:4]); err != nil {
			s.mu.Lock()
			s.eof = true
			s.mu.Unlock()
			return
		}
		n, hdr, big := int(le16(hd[2:]))*4, 4, false
		if n == 0 && bigOn {
			if _, err := io.ReadFull(r, hd[4:8]); err != nil {
				return
			}
			n, hdr, big = int(le32(hd[4:]))*4, 8, true
		}
		if n < hdr {
			s.errorf("fake X: request opcode %d with length %d bytes", hd[0], n)
			return
		}
		raw := make([]byte, n)
		copy(raw, hd[:hdr])
		if _, err := io.ReadFull(r, raw[hdr:]); err != nil {
			s.errorf("fake X: read request opcode %d (%d bytes): %v", hd[0], n, err)
			return
		}
		seq++
		q := fakeReq{op: hd[0], data: hd[1], big: big, seq: seq, raw: raw}
		s.mu.Lock()
		s.reqs = append(s.reqs, q)
		s.mu.Unlock()
		if q.op == s.cfg.errorOn {
			s.cfg.errorOn = 0
			s.send(errorMsg(8, q.op, q.seq, 0))
			continue
		}
		if q.op == fakeBigReqOp {
			bigOn = true
		}
		s.reply(q)
	}
}

func (s *fakeX) reply(q fakeReq) {
	rep := func(b1 byte, extra []byte, fill func(b []byte)) {
		b := make([]byte, 32+len(extra))
		b[0], b[1] = 1, b1
		put16(b[2:], q.seq)
		put32(b[4:], uint32(len(extra)/4))
		if fill != nil {
			fill(b)
		}
		copy(b[32:], extra)
		s.write(b)
	}
	switch q.op {
	case opInternAtom:
		name := string(q.raw[8 : 8+le16(q.raw[4:])])
		a := s.atom(name)
		rep(0, nil, func(b []byte) { put32(b[8:], a) })
	case opQueryExtension:
		name := string(q.raw[8 : 8+le16(q.raw[4:])])
		rep(0, nil, func(b []byte) {
			if name == "BIG-REQUESTS" && s.cfg.bigReq != 0 {
				b[8], b[9] = 1, fakeBigReqOp
			}
		})
	case fakeBigReqOp:
		rep(0, nil, func(b []byte) { put32(b[8:], s.cfg.bigReq) })
	case opGetKeyboardMapping:
		first, count := int(q.raw[4]), int(q.raw[5])
		const per = 4
		syms := make([]byte, count*per*4)
		for i := 0; i < count; i++ {
			k := usKeymap[byte(first+i)]
			put32(syms[i*per*4:], k[0])
			put32(syms[i*per*4+4:], k[1])
		}
		rep(per, syms, nil)
	case opGetModifierMapping:
		const per = 4
		kc := make([]byte, 8*per)
		for mod, keys := range usModmap {
			copy(kc[mod*per:], keys)
		}
		rep(per, kc, nil)
	case opGetInputFocus:
		rep(1, nil, func(b []byte) { put32(b[8:], 1) })
	}
}

func (s *fakeX) setupReply() []byte {
	var b []byte
	u8 := func(v byte) { b = append(b, v) }
	u16 := func(v uint16) { b = binary.LittleEndian.AppendUint16(b, v) }
	u32 := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }
	vendor := "Veduta fake X"
	b = make([]byte, 8)
	u32(12101011) // release
	u32(fakeRIDBase)
	u32(fakeRIDMask)
	u32(256) // motion buffer
	u16(uint16(len(vendor)))
	u16(s.cfg.maxReq)
	u8(1) // screens
	u8(3) // formats
	if s.cfg.msb {
		u8(1)
	} else {
		u8(0)
	}
	u8(0)  // bitmap bit order
	u8(32) // scanline unit
	u8(32) // scanline pad
	u8(8)  // min keycode
	u8(255)
	u32(0)
	b = append(b, vendor...)
	b = append(b, make([]byte, pad4(len(vendor))-len(vendor))...)
	for _, f := range [][3]byte{{1, 1, 32}, {24, s.cfg.bpp24, s.cfg.pad24}, {32, 32, 32}} {
		b = append(b, f[0], f[1], f[2], 0, 0, 0, 0, 0)
	}
	rootDepth := byte(24)
	switch s.cfg.rootVisual {
	case fakeVisual32:
		rootDepth = 32
	case fakeVisual8:
		rootDepth = 8
	}
	u32(fakeRoot)
	u32(0x20)     // default colormap
	u32(0xffffff) // white
	u32(0)        // black
	u32(0)        // current input masks
	u16(1920)     // width
	u16(1080)     // height
	u16(508)      // mm
	u16(286)      // mm
	u16(1)        // min installed maps
	u16(1)        // max
	u32(s.cfg.rootVisual)
	u8(0) // backing stores
	u8(0) // save unders
	u8(rootDepth)
	u8(4) // depths
	visual := func(id uint32, class byte, r, g, bl uint32) {
		u32(id)
		u8(class)
		u8(8)
		u16(256)
		u32(r)
		u32(g)
		u32(bl)
		u32(0)
	}
	depth := func(d byte, n uint16) { u8(d); u8(0); u16(n); u32(0) }
	depth(1, 0)
	depth(8, 1)
	visual(fakeVisual8, 3, 0, 0, 0)
	depth(24, 2)
	visual(0x30, 4, 0xff, 0xff00, 0xff0000) // BGR TrueColor: not usable
	visual(fakeVisual24, 4, 0xff0000, 0xff00, 0xff)
	depth(32, 1)
	visual(fakeVisual32, 4, 0xff0000, 0xff00, 0xff)
	b[0] = 1
	put16(b[2:], 11)
	put16(b[6:], uint16((len(b)-8)/4))
	return b
}

// ---- message builders ------------------------------------------------------------------

func keyMsg(code, keycode byte, time uint32, state uint16, win uint32) []byte {
	b := make([]byte, 32)
	b[0], b[1] = code, keycode
	put32(b[4:], time)
	put32(b[8:], fakeRoot)
	put32(b[12:], win)
	put16(b[28:], state)
	b[30] = 1
	return b
}

func pointerMsg(code, button byte, x, y int16, win uint32) []byte {
	b := keyMsg(code, button, 1, 0, win)
	put16(b[24:], uint16(x))
	put16(b[26:], uint16(y))
	return b
}

func configureMsg(win uint32, w, h uint16) []byte {
	b := make([]byte, 32)
	b[0] = xConfigureNotify
	put32(b[4:], win)
	put32(b[8:], win)
	put16(b[20:], w)
	put16(b[22:], h)
	return b
}

func clientMsg(win, typ, data0 uint32) []byte {
	b := make([]byte, 32)
	b[0], b[1] = xClientMessage, 32
	put32(b[4:], win)
	put32(b[8:], typ)
	put32(b[12:], data0)
	return b
}

func focusOutMsg(win uint32, detail byte) []byte {
	b := make([]byte, 32)
	b[0], b[1] = xFocusOut, detail
	put32(b[4:], win)
	return b
}

func errorMsg(code, major byte, seq uint16, value uint32) []byte {
	b := make([]byte, 32)
	b[1] = code
	put16(b[2:], seq)
	put32(b[4:], value)
	b[10] = major
	return b
}

// ---- helpers ---------------------------------------------------------------------------

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (w *x11Window) queued() int {
	w.qmu.Lock()
	defer w.qmu.Unlock()
	return len(w.q)
}

func encodeXauth(entries ...xauthEntry) []byte {
	var b []byte
	field := func(f []byte) {
		b = binary.BigEndian.AppendUint16(b, uint16(len(f)))
		b = append(b, f...)
	}
	for _, e := range entries {
		b = binary.BigEndian.AppendUint16(b, e.family)
		field(e.addr)
		field([]byte(e.number))
		field([]byte(e.name))
		field(e.data)
	}
	return b
}

func hostname(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// openFake starts a fake server and opens a window on it. When cfg requires a cookie, an
// Xauthority file with a decoy entry and the right entry is used.
func openFake(t *testing.T, cfg fakeConfig, o Options) (*fakeX, *x11Window) {
	t.Helper()
	s := newFakeX(t, cfg)
	auth := ""
	if cfg.cookie != nil {
		auth = filepath.Join(filepath.Dir(s.path), "Xauthority")
		file := encodeXauth(
			xauthEntry{family: familyLocal, addr: []byte("not-" + hostname(t)), number: "7", name: cookieName, data: []byte("decoy-cookie-000")},
			xauthEntry{family: familyLocal, addr: []byte(hostname(t)), number: "7", name: cookieName, data: cfg.cookie},
		)
		if err := os.WriteFile(auth, file, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if o.Width == 0 {
		o = Options{Title: "Veduta test", Width: 320, Height: 240}
	}
	w, err := openX11(o, s.path, auth)
	if err != nil {
		t.Fatalf("openX11: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return s, w
}

// ---- tests -----------------------------------------------------------------------------

func TestParseDisplay(t *testing.T) {
	x0 := []dialTarget{{"unix", "@/tmp/.X11-unix/X0"}, {"unix", "/tmp/.X11-unix/X0"}, {"tcp", "localhost:6000"}}
	cases := []struct {
		in      string
		want    displayAddr
		targets []dialTarget
	}{
		{":0", displayAddr{}, x0},
		{":0.0", displayAddr{}, x0},
		{":1.2", displayAddr{display: 1, screen: 2}, []dialTarget{{"unix", "@/tmp/.X11-unix/X1"}, {"unix", "/tmp/.X11-unix/X1"}, {"tcp", "localhost:6001"}}},
		{"unix:0", displayAddr{protocol: "unix"}, x0[:2]},
		{"unix/:3", displayAddr{protocol: "unix", display: 3}, []dialTarget{{"unix", "@/tmp/.X11-unix/X3"}, {"unix", "/tmp/.X11-unix/X3"}}},
		{"localhost:10.0", displayAddr{protocol: "tcp", host: "localhost", display: 10}, []dialTarget{{"tcp", "localhost:6010"}}},
		{"host.example:3", displayAddr{protocol: "tcp", host: "host.example", display: 3}, []dialTarget{{"tcp", "host.example:6003"}}},
		{"tcp/localhost:1", displayAddr{protocol: "tcp", host: "localhost", display: 1}, []dialTarget{{"tcp", "localhost:6001"}}},
		{"tcp/:1", displayAddr{protocol: "tcp", display: 1}, []dialTarget{{"tcp", "localhost:6001"}}},
		{"[::1]:0", displayAddr{protocol: "tcp", host: "::1"}, []dialTarget{{"tcp", "[::1]:6000"}}},
		{"/tmp/.X11-unix/X3", displayAddr{protocol: "unix", path: "/tmp/.X11-unix/X3", display: 3}, []dialTarget{{"unix", "/tmp/.X11-unix/X3"}}},
		{"/tmp/.X11-unix/X3.1", displayAddr{protocol: "unix", path: "/tmp/.X11-unix/X3", display: 3, screen: 1}, []dialTarget{{"unix", "/tmp/.X11-unix/X3"}}},
		{"/private/tmp/com.apple.launchd.x/org.xquartz:0", displayAddr{protocol: "unix", path: "/private/tmp/com.apple.launchd.x/org.xquartz"}, []dialTarget{{"unix", "/private/tmp/com.apple.launchd.x/org.xquartz"}}},
	}
	for _, c := range cases {
		got, err := parseDisplay(c.in)
		if err != nil {
			t.Errorf("parseDisplay(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseDisplay(%q) = %+v, want %+v", c.in, got, c.want)
		}
		if tg := got.targets(); !equalTargets(tg, c.targets) {
			t.Errorf("parseDisplay(%q).targets() = %v, want %v", c.in, tg, c.targets)
		}
	}
	for _, bad := range []string{"", "0", ":", ":x", ":0.", ":0.x", "foo/:0", "tcp/unix:0", "unix/host:0", ":123456"} {
		if d, err := parseDisplay(bad); err == nil {
			t.Errorf("parseDisplay(%q) = %+v, want an error", bad, d)
		}
	}
}

func equalTargets(a, b []dialTarget) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestXauthority(t *testing.T) {
	host := "box"
	entries := []xauthEntry{
		{family: familyInternet, addr: []byte{10, 0, 0, 1}, number: "7", name: cookieName, data: []byte("aaaa")},
		{family: familyLocal, addr: []byte("otherhost"), number: "7", name: cookieName, data: []byte("bbbb")},
		{family: familyLocal, addr: []byte(host), number: "1", name: cookieName, data: []byte("cccc")},
		{family: familyLocal, addr: []byte(host), number: "7", name: "XDM-AUTHORIZATION-1", data: []byte("dddd")},
		{family: familyLocal, addr: []byte(host), number: "7", name: cookieName, data: []byte("eeee")},
		{family: familyWild, number: "", name: cookieName, data: []byte("ffff")},
	}
	file := encodeXauth(entries...)
	got, err := parseXauth(file)
	if err != nil || len(got) != len(entries) {
		t.Fatalf("parseXauth: %d entries, %v", len(got), err)
	}
	for i, e := range got {
		w := entries[i]
		if e.family != w.family || !bytes.Equal(e.addr, w.addr) || e.number != w.number || e.name != w.name || !bytes.Equal(e.data, w.data) {
			t.Errorf("entry %d = %+v, want %+v", i, e, w)
		}
	}
	cases := []struct {
		family  uint16
		addr    []byte
		display int
		want    string
	}{
		{familyLocal, []byte(host), 7, "eeee"},
		{familyLocal, []byte(host), 1, "cccc"},
		{familyLocal, []byte(host), 3, "ffff"}, // wild
		{familyInternet, []byte{10, 0, 0, 1}, 7, "aaaa"},
		{familyInternet, []byte{10, 0, 0, 2}, 7, "ffff"},
	}
	for _, c := range cases {
		d, ok := findCookie(got, c.family, c.addr, c.display)
		if !ok || string(d) != c.want {
			t.Errorf("findCookie(%d, %q, %d) = %q, %v; want %q", c.family, c.addr, c.display, d, ok, c.want)
		}
	}
	if d, ok := findCookie(got[:5], familyLocal, []byte(host), 3); ok {
		t.Errorf("findCookie without a wild entry = %q, want none", d)
	}
	// A truncated file keeps its complete records and reports the damage.
	part, err := parseXauth(file[:len(file)-3])
	if err == nil || len(part) != len(entries)-1 {
		t.Errorf("truncated parseXauth: %d entries, %v", len(part), err)
	}
}

func TestSetupRequest(t *testing.T) {
	cookie := []byte("0123456789abcdef")
	b := setupRequest(cookieName, cookie)
	if len(b) != 12+20+16 || b[0] != 'l' || le16(b[2:]) != 11 || le16(b[4:]) != 0 || le16(b[6:]) != 18 || le16(b[8:]) != 16 {
		t.Fatalf("setup request % x", b)
	}
	if string(b[12:30]) != cookieName || !bytes.Equal(b[32:], cookie) {
		t.Errorf("setup request auth % x", b[12:])
	}
	if b := setupRequest(cookieName, nil); len(b) != 12 || le16(b[6:]) != 0 || le16(b[8:]) != 0 {
		t.Errorf("setup request without cookie % x", b)
	}
}

func TestParseSetupTruncated(t *testing.T) {
	s := &fakeX{cfg: fakeConfig{maxReq: 65535, bpp24: 32, pad24: 32, rootVisual: fakeVisual24}}
	body := s.setupReply()[8:]
	info, err := parseSetup(body, 0)
	if err != nil {
		t.Fatal(err)
	}
	if info.vendor != "Veduta fake X" || info.root != fakeRoot || info.visual != (visualInfo{fakeVisual24, 24}) ||
		info.bpp != 32 || info.scanlinePad != 32 || info.maxReqUnits != 65535 || info.minKeycode != 8 || info.maxKeycode != 255 {
		t.Errorf("parseSetup = %+v", info)
	}
	if info, err := parseSetup(body, 3); err != nil || info.root != fakeRoot {
		t.Errorf("parseSetup for a missing screen should use screen 0: %+v, %v", info, err)
	}
	for n := 0; n < len(body); n++ {
		if _, err := parseSetup(body[:n], 0); err == nil {
			t.Fatalf("parseSetup of %d/%d bytes succeeded", n, len(body))
		}
	}
}

func TestOpenRequests(t *testing.T) {
	for _, bigReq := range []bool{false, true} {
		cfg := fakeConfig{cookie: []byte("0123456789abcdef")}
		if bigReq {
			cfg.bigReq = 4194303
		}
		title := "Veduta ✓ é"
		s, w := openFake(t, cfg, Options{Title: title, Width: 320, Height: 240})

		want := []byte{opQueryExtension, opInternAtom, opInternAtom, opInternAtom, opInternAtom,
			opGetKeyboardMapping, opGetModifierMapping}
		if bigReq {
			want = append(want, fakeBigReqOp)
		}
		want = append(want, opCreateWindow, opChangeProperty, opChangeProperty, opChangeProperty,
			opChangeProperty, opChangeProperty, opCreateGC, opMapWindow, opGetInputFocus)
		if got := s.opcodes(); !bytes.Equal(got, want) {
			t.Fatalf("bigReq=%v: requests %v, want %v", bigReq, got, want)
		}
		s.mu.Lock()
		if s.authName != cookieName || !bytes.Equal(s.authData, cfg.cookie) {
			t.Errorf("auth %q % x", s.authName, s.authData)
		}
		s.mu.Unlock()
		if w.bigReq != bigReq || (bigReq && w.maxReqUnits != 4194303) || (!bigReq && w.maxReqUnits != 65535) {
			t.Errorf("bigReq=%v: window bigReq %v, max %d", bigReq, w.bigReq, w.maxReqUnits)
		}
		if w.win != fakeWindowID || w.gc != fakeGCID {
			t.Errorf("ids 0x%x 0x%x", w.win, w.gc)
		}
		if w.numLock != maskMod2 {
			t.Errorf("NumLock mask 0x%x, want Mod2", w.numLock)
		}
		reqs := s.requests()
		for _, q := range reqs {
			if q.big || int(q.units())*4 != len(q.raw) {
				t.Errorf("request %d: length field %d units for %d bytes", q.op, q.units(), len(q.raw))
			}
		}
		for _, q := range reqs[1:5] {
			name := string(q.raw[8 : 8+le16(q.raw[4:])])
			if q.data != 0 || !strings.Contains("WM_PROTOCOLS WM_DELETE_WINDOW _NET_WM_NAME UTF8_STRING", name) {
				t.Errorf("InternAtom %q only-if-exists=%d", name, q.data)
			}
		}
		if q := reqs[5]; q.raw[4] != 8 || q.raw[5] != 248 {
			t.Errorf("GetKeyboardMapping first %d count %d", q.raw[4], q.raw[5])
		}
		i := 7
		if bigReq {
			i = 8
		}
		cw := reqs[i].raw
		if cw[1] != 24 || le32(cw[4:]) != fakeWindowID || le32(cw[8:]) != fakeRoot || le16(cw[16:]) != 320 || le16(cw[18:]) != 240 ||
			le16(cw[20:]) != 0 || le16(cw[22:]) != 1 || le32(cw[24:]) != fakeVisual24 || le32(cw[28:]) != 0x819 || len(cw) != 48 {
			t.Errorf("CreateWindow % x", cw)
		}
		if v := [4]uint32{le32(cw[32:]), le32(cw[36:]), le32(cw[40:]), le32(cw[44:])}; v != [4]uint32{0, 0, 1, 0x22804f} {
			t.Errorf("CreateWindow values %x", v)
		}
		props := map[uint32]fakeReq{}
		for _, q := range reqs[i+1 : i+6] {
			if q.data != 0 || le32(q.raw[4:]) != fakeWindowID {
				t.Errorf("ChangeProperty mode %d window 0x%x", q.data, le32(q.raw[4:]))
			}
			props[le32(q.raw[8:])] = q
		}
		prop := func(atom, typ uint32, format byte) []byte {
			q, ok := props[atom]
			if !ok {
				t.Errorf("no ChangeProperty for atom %d", atom)
				return nil
			}
			if le32(q.raw[12:]) != typ || q.raw[16] != format {
				t.Errorf("property %d: type %d format %d", atom, le32(q.raw[12:]), q.raw[16])
			}
			n := int(le32(q.raw[20:])) * int(format/8)
			if 24+pad4(n) != len(q.raw) {
				t.Errorf("property %d: %d data bytes in a %d-byte request", atom, n, len(q.raw))
				return nil
			}
			return q.raw[24 : 24+n]
		}
		if d := prop(atomWMName, atomSTRING, 8); string(d) != "Veduta ? \xe9" {
			t.Errorf("WM_NAME %q", d)
		}
		if d := prop(s.atom("_NET_WM_NAME"), s.atom("UTF8_STRING"), 8); string(d) != title {
			t.Errorf("_NET_WM_NAME %q", d)
		}
		if d := prop(atomWMClass, atomSTRING, 8); !bytes.HasSuffix(d, []byte("\x00Veduta ? \xe9\x00")) || bytes.Count(d, []byte{0}) != 2 {
			t.Errorf("WM_CLASS %q", d)
		}
		if d := prop(s.atom("WM_PROTOCOLS"), atomATOM, 32); len(d) != 4 || le32(d) != s.atom("WM_DELETE_WINDOW") {
			t.Errorf("WM_PROTOCOLS % x", d)
		}
		if d := prop(atomWMNormalHints, atomWMSizeHints, 32); len(d) != 72 || le32(d) != 24 || le32(d[12:]) != 320 || le32(d[16:]) != 240 || le32(d[20:]) != 1 || le32(d[24:]) != 1 {
			t.Errorf("WM_NORMAL_HINTS % x", d)
		}
		if gc := reqs[i+6].raw; len(gc) != 20 || le32(gc[4:]) != fakeGCID || le32(gc[8:]) != fakeWindowID || le32(gc[12:]) != 0x10000 || le32(gc[16:]) != 0 {
			t.Errorf("CreateGC % x", gc)
		}
		if m := reqs[i+7].raw; len(m) != 8 || le32(m[4:]) != fakeWindowID {
			t.Errorf("MapWindow % x", m)
		}
		if w, h := w.Size(); w != 320 || h != 240 {
			t.Errorf("Size = %dx%d", w, h)
		}
	}
}

func TestBigRequestsMaximumClamped(t *testing.T) {
	_, w := openFake(t, fakeConfig{bigReq: 0xffffffff}, Options{Title: "t", Width: 64, Height: 48})
	if !w.bigReq || w.maxReqUnits != maxBigRequestUnits {
		t.Errorf("bigReq %v, max %d units, want %d", w.bigReq, w.maxReqUnits, maxBigRequestUnits)
	}
	if err := w.Present(testImage(64, 48)); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAuthRejected(t *testing.T) {
	s := newFakeX(t, fakeConfig{cookie: []byte("right-cookie-123")})
	auth := filepath.Join(filepath.Dir(s.path), "Xauthority")
	os.WriteFile(auth, encodeXauth(xauthEntry{family: familyLocal, addr: []byte(hostname(t)), number: "7", name: cookieName, data: []byte("wrong-cookie-123")}), 0o600)
	_, err := openX11(Options{Title: "t", Width: 10, Height: 10}, s.path, auth)
	if err == nil || !strings.Contains(err.Error(), "Invalid MIT-MAGIC-COOKIE-1 key") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenWithoutXauthority(t *testing.T) {
	s, _ := openFake(t, fakeConfig{}, Options{})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.authName != "" || len(s.authData) != 0 {
		t.Errorf("auth %q % x, want none", s.authName, s.authData)
	}
}

func TestOpenFailsOnXError(t *testing.T) {
	s := newFakeX(t, fakeConfig{errorOn: opCreateWindow})
	_, err := openX11(Options{Title: "t", Width: 10, Height: 10}, s.path, "")
	if err == nil || !strings.Contains(err.Error(), "BadMatch") || !strings.Contains(err.Error(), "CreateWindow") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := openX11(Options{Width: 10, Height: 10}, "", ""); err == nil || !strings.Contains(err.Error(), "$DISPLAY is not set") {
		t.Errorf("no DISPLAY: %v", err)
	}
	if _, err := openX11(Options{Width: 40000, Height: 10}, ":0", ""); err == nil {
		t.Errorf("40000-pixel window: no error")
	}
	dir := t.TempDir()
	if _, err := openX11(Options{Width: 10, Height: 10}, filepath.Join(dir, "X9"), ""); err == nil || !strings.Contains(err.Error(), "connect") {
		t.Errorf("missing socket: %v", err)
	}
}

func TestOpenNonTrueColorRoot(t *testing.T) {
	s, w := openFake(t, fakeConfig{rootVisual: fakeVisual8}, Options{})
	reqs := s.requests()
	var cmap, cw []byte
	for _, q := range reqs {
		switch q.op {
		case opCreateColormap:
			cmap = q.raw
		case opCreateWindow:
			cw = q.raw
		}
	}
	if cmap == nil || cw == nil {
		t.Fatalf("requests %v", s.opcodes())
	}
	id := le32(cmap[4:])
	if cmap[1] != 0 || le32(cmap[8:]) != fakeRoot || le32(cmap[12:]) != fakeVisual24 {
		t.Errorf("CreateColormap % x", cmap)
	}
	if cw[1] != 24 || le32(cw[24:]) != fakeVisual24 || le32(cw[28:]) != 0x2819 || len(cw) != 52 || le32(cw[48:]) != id {
		t.Errorf("CreateWindow % x", cw)
	}
	if w.win == id || w.gc == id {
		t.Errorf("colormap id 0x%x reused", id)
	}
}

// testImage returns an image whose pixels encode their coordinates.
func testImage(w, h int) *gfx.Image {
	img := gfx.NewImage(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[y*w+x] = 0x40000000 | uint32(y)<<12 | uint32(x)
		}
	}
	return img
}

// expectPixel returns the bytes the server must receive for pixel p.
func expectPixel(p uint32, bpp int, msb bool, alpha byte) []byte {
	b, g, r, a := byte(p), byte(p>>8), byte(p>>16), byte(p>>24)|alpha
	switch {
	case bpp == 32 && !msb:
		return []byte{b, g, r, a}
	case bpp == 32:
		return []byte{a, r, g, b}
	case !msb:
		return []byte{b, g, r}
	default:
		return []byte{r, g, b}
	}
}

// checkStrips validates the PutImage requests of one frame and returns the strip heights
// and which strips used the big-request encoding.
func checkStrips(t *testing.T, s *fakeX, img *gfx.Image, width, height int, depth byte, bpp, pad int, msb bool, alpha byte, maxUnits uint32) (heights []int, bigs []bool) {
	t.Helper()
	var puts []fakeReq
	waitFor(t, "PutImage requests", func() bool {
		puts = puts[:0]
		rows := 0
		for _, q := range s.requests() {
			if q.op == opPutImage {
				puts = append(puts, q)
				rows += int(le16(q.body()[10:]))
			}
		}
		return rows >= height
	})
	rowBytes := (width*bpp + pad - 1) / pad * pad / 8
	y := 0
	for i, q := range puts {
		b := q.body()
		h := int(le16(b[10:]))
		heights = append(heights, h)
		bigs = append(bigs, q.big)
		if q.data != 2 || le32(b[0:]) != fakeWindowID || le32(b[4:]) != fakeGCID || int(le16(b[8:])) != width ||
			le16(b[12:]) != 0 || int(le16(b[14:])) != y || b[16] != 0 || b[17] != depth {
			t.Fatalf("strip %d header % x", i, q.raw[:28])
		}
		if int(q.units())*4 != len(q.raw) || q.units() > maxUnits {
			t.Fatalf("strip %d: %d units, %d bytes, max %d units", i, q.units(), len(q.raw), maxUnits)
		}
		if !q.big && q.units() > 0xffff || q.big && q.units() <= 0xffff {
			t.Errorf("strip %d: big=%v with %d units", i, q.big, q.units())
		}
		data := b[20:]
		if len(data) != pad4(h*rowBytes) {
			t.Fatalf("strip %d: %d data bytes, want %d", i, len(data), pad4(h*rowBytes))
		}
		for r := 0; r < h; r++ {
			for x := 0; x < width; x++ {
				want := expectPixel(img.Pix[(y+r)*img.W+x], bpp, msb, alpha)
				got := data[r*rowBytes+x*len(want):][:len(want)]
				if !bytes.Equal(got, want) {
					t.Fatalf("strip %d pixel (%d,%d) = % x, want % x", i, x, y+r, got, want)
				}
			}
		}
		y += h
	}
	if y != height {
		t.Errorf("strips cover %d rows, want %d", y, height)
	}
	return heights, bigs
}

func TestPresentStrips(t *testing.T) {
	rep := func(n, h int) []int {
		s := make([]int, n)
		for i := range s {
			s[i] = h
		}
		return s
	}
	cases := []struct {
		name    string
		maxReq  uint16
		bigReq  uint32
		w, h    int
		heights []int
		bigs    []bool
	}{
		{"setup max 256KiB", 65535, 0, 320, 240, []int{204, 36}, []bool{false, false}},
		{"setup max 16KiB", 4096, 0, 320, 240, rep(20, 12), make([]bool, 20)},
		{"big requests, one big strip", 65535, 4194303, 320, 240, []int{240}, []bool{true}},
		{"big requests, small frame not big", 65535, 4194303, 64, 48, []int{48}, []bool{false}},
		{"big requests, mixed", 4096, 70000, 320, 240, []int{218, 22}, []bool{true, false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, w := openFake(t, fakeConfig{maxReq: c.maxReq, bigReq: c.bigReq}, Options{Title: "t", Width: c.w, Height: c.h})
			img := testImage(c.w, c.h)
			if err := w.Present(img); err != nil {
				t.Fatal(err)
			}
			max := uint32(c.maxReq)
			if c.bigReq > max {
				max = c.bigReq
			}
			heights, bigs := checkStrips(t, s, img, c.w, c.h, 24, 32, 32, false, 0, max)
			if !equalInts(heights, c.heights) || !equalBools(bigs, c.bigs) {
				t.Errorf("strips %v big %v, want %v %v", heights, bigs, c.heights, c.bigs)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPresentFormats(t *testing.T) {
	cases := []struct {
		name  string
		cfg   fakeConfig
		depth byte
		bpp   int
		pad   int
		msb   bool
		alpha byte
	}{
		{"32bpp LSB", fakeConfig{}, 24, 32, 32, false, 0},
		{"32bpp MSB", fakeConfig{msb: true}, 24, 32, 32, true, 0},
		{"24bpp LSB pad 32", fakeConfig{bpp24: 24}, 24, 24, 32, false, 0},
		{"24bpp MSB pad 8", fakeConfig{bpp24: 24, pad24: 8, msb: true}, 24, 24, 8, true, 0},
		{"depth 32 root forces alpha", fakeConfig{rootVisual: fakeVisual32}, 32, 32, 32, false, 0xff},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A 3x3 window and a 5x2 image: drawn clipped to 3x2.
			s, w := openFake(t, c.cfg, Options{Title: "t", Width: 3, Height: 3})
			img := testImage(5, 2)
			if err := w.Present(img); err != nil {
				t.Fatal(err)
			}
			heights, _ := checkStrips(t, s, img, 3, 2, c.depth, c.bpp, c.pad, c.msb, c.alpha, 65535)
			if !equalInts(heights, []int{2}) {
				t.Errorf("strips %v", heights)
			}
		})
	}
}

func TestPresentErrors(t *testing.T) {
	_, w := openFake(t, fakeConfig{}, Options{})
	if err := w.Present(nil); err == nil {
		t.Error("Present(nil): no error")
	}
	if err := w.Present(&gfx.Image{W: 10, H: 10, Pix: make([]uint32, 5)}); err == nil {
		t.Error("Present with short Pix: no error")
	}
	if err := w.Present(gfx.NewImage(0, 0)); err != nil {
		t.Errorf("Present of an empty image: %v", err)
	}
	// A row that cannot fit a request.
	w.maxReqUnits = 64
	if err := w.Present(gfx.NewImage(320, 1)); err == nil || !strings.Contains(err.Error(), "maximum request length") {
		t.Errorf("oversized row: %v", err)
	}
}

func TestPresentDoesNotAllocate(t *testing.T) {
	_, w := openFake(t, fakeConfig{maxReq: 4096}, Options{Title: "t", Width: 64, Height: 48})
	img := testImage(64, 48)
	if err := w.Present(img); err != nil {
		t.Fatal(err)
	}
	// AllocsPerRun counts every goroutine's allocations, and the fake server allocates
	// per request: measure against a connection that discards the frames.
	real := w.conn
	w.conn = discardConn{real}
	defer func() { w.conn = real }()
	if n := testing.AllocsPerRun(20, func() {
		if err := w.Present(img); err != nil {
			t.Fatal(err)
		}
	}); n != 0 {
		t.Errorf("Present allocates %.1f times per frame", n)
	}
	if n := testing.AllocsPerRun(100, func() { w.Poll() }); n != 0 {
		t.Errorf("Poll without events allocates %.1f times", n)
	}
}

// discardConn is a connection whose writes succeed without doing anything.
type discardConn struct{ net.Conn }

func (discardConn) Write(b []byte) (int, error) { return len(b), nil }

func TestPollEvents(t *testing.T) {
	s, w := openFake(t, fakeConfig{}, Options{})
	win := w.win
	other := uint32(0x999)
	unknownReply := make([]byte, 40)
	unknownReply[0] = xReply
	put16(unknownReply[2:], 999)
	put32(unknownReply[4:], 2)
	generic := make([]byte, 36)
	generic[0] = xGenericEvent
	put32(generic[4:], 1)
	s.send(
		keyMsg(xKeyPress, 38, 10, 0, win),                                      // a: KeyDown KeyA, Text a
		keyMsg(xKeyRelease, 38, 50, 0, win), keyMsg(xKeyPress, 38, 50, 0, win), // repeat: Text a
		keyMsg(xKeyRelease, 38, 90, 0, win), keyMsg(xKeyPress, 38, 91, 0, win), // repeat 1 ms apart: Text a
		keyMsg(xKeyRelease, 38, 120, 0, win),         // KeyUp KeyA
		keyMsg(xKeyPress, 50, 130, 0, win),           // KeyDown ShiftLeft
		keyMsg(xKeyPress, 25, 140, maskShift, win),   // KeyDown KeyW, Text W
		keyMsg(xKeyPress, 25, 170, maskShift, win),   // detectable auto-repeat: Text W
		keyMsg(xKeyRelease, 25, 180, maskShift, win), // KeyUp KeyW
		keyMsg(xKeyRelease, 50, 190, maskShift, win), // KeyUp ShiftLeft
		keyMsg(xKeyPress, 38, 200, 0, other),         // another window: ignored
		pointerMsg(xMotionNotify, 0, 5, 6, win),      // coalesced
		pointerMsg(xMotionNotify, 0, 7, 8, win),      // MouseMove 7,8
		pointerMsg(xButtonPress, 1, 7, 8, win),       // ButtonDown left
		unknownReply,                                 // skipped
		pointerMsg(xButtonPress, 4, 7, 8, win),       // wheel: ignored
		pointerMsg(xButtonRelease, 3, 9, 10, win),    // ButtonUp right
		generic,                                       // skipped
		configureMsg(win, 400, 300),                   // Resize 400x300
		configureMsg(win, 400, 300),                   // unchanged: nothing
		configureMsg(other, 10, 10),                   // ignored
		focusOutMsg(win, 2),                           // NotifyInferior: ignored
		focusOutMsg(win, 3),                           // FocusLost
		clientMsg(win, s.atom("WM_PROTOCOLS"), 12345), // another protocol: ignored
		clientMsg(win, s.atom("WM_PROTOCOLS"), s.atom("WM_DELETE_WINDOW")), // Close
	)
	waitFor(t, "20 queued events", func() bool { return w.queued() >= 20 })
	got, err := w.Poll()
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{
		{Kind: KeyDown, Code: "KeyA"}, {Kind: Text, Text: "a"}, {Kind: Text, Text: "a"}, {Kind: Text, Text: "a"},
		{Kind: KeyUp, Code: "KeyA"}, {Kind: KeyDown, Code: "ShiftLeft"}, {Kind: KeyDown, Code: "KeyW"},
		{Kind: Text, Text: "W"}, {Kind: Text, Text: "W"}, {Kind: KeyUp, Code: "KeyW"}, {Kind: KeyUp, Code: "ShiftLeft"},
		{Kind: MouseMove, X: 7, Y: 8}, {Kind: ButtonDown, Button: sim.ButtonLeft, X: 7, Y: 8},
		{Kind: ButtonUp, Button: sim.ButtonRight, X: 9, Y: 10}, {Kind: Resize, W: 400, H: 300},
		{Kind: FocusLost}, {Kind: Close},
	}
	if !equalEvents(got, want) {
		t.Errorf("Poll:\n got %+v\nwant %+v", got, want)
	}
	if cw, ch := w.Size(); cw != 400 || ch != 300 {
		t.Errorf("Size = %dx%d after Resize", cw, ch)
	}
	if got, err := w.Poll(); len(got) != 0 || err != nil {
		t.Errorf("second Poll = %+v, %v", got, err)
	}
}

func equalEvents(a, b []Event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPointerLock checks the requests a lock sends and the virtual position it reports:
// the warp's own MotionNotify is dropped and the rest accumulate.
func TestPointerLock(t *testing.T) {
	s, w := openFake(t, fakeConfig{}, Options{Width: 320, Height: 240})
	n := len(s.requests())
	if err := w.SetPointerLock(true); err != nil {
		t.Fatal(err)
	}
	want := []byte{opCreatePixmap, opCreateGC, opPolyFillRectangle, opCreateCursor, opFreeGC,
		opChangeWindowAttributes, opWarpPointer}
	waitFor(t, "the pointer lock requests", func() bool { return len(s.requests()) >= n+len(want) })
	reqs := s.requests()[n:]
	got := make([]byte, len(reqs))
	for i, q := range reqs {
		got[i] = q.op
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("lock requests %v, want %v", got, want)
	}
	if cwa := reqs[5]; le32(cwa.raw[8:]) != 0x4000 || le32(cwa.raw[12:]) == 0 {
		t.Errorf("ChangeWindowAttributes mask 0x%x cursor 0x%x, want the cursor attribute set",
			le32(cwa.raw[8:]), le32(cwa.raw[12:]))
	}
	if warp := reqs[6]; int16(le16(warp.raw[20:])) != 160 || int16(le16(warp.raw[22:])) != 120 {
		t.Errorf("warped to %d,%d, want the middle of the 320x240 client area",
			int16(le16(warp.raw[20:])), int16(le16(warp.raw[22:])))
	}

	s.send(
		pointerMsg(xMotionNotify, 0, 160, 120, w.win), // the warp's own event: dropped
		pointerMsg(xMotionNotify, 0, 170, 130, w.win), // +10, +10
		pointerMsg(xMotionNotify, 0, 175, 130, w.win), // +5, 0
		pointerMsg(xButtonPress, 1, 175, 130, w.win),  // reported at the virtual position
	)
	waitFor(t, "4 queued events", func() bool { return w.queued() >= 4 })
	evs, err := w.Poll()
	if err != nil {
		t.Fatal(err)
	}
	wantEvents := []Event{{Kind: MouseMove, X: 15, Y: 10}, {Kind: ButtonDown, Button: sim.ButtonLeft, X: 15, Y: 10}}
	if !equalEvents(evs, wantEvents) {
		t.Fatalf("locked Poll:\n got %+v\nwant %+v", evs, wantEvents)
	}
	// Movement recenters the pointer for the next tick.
	waitFor(t, "the warp after the movement", func() bool { return len(s.requests()) > n+len(want) })
	if last := s.requests()[len(s.requests())-1]; last.op != opWarpPointer {
		t.Errorf("request after a locked Poll is %d, want WarpPointer", last.op)
	}

	// Unlocking puts the normal cursor back; locking again reuses the hidden one.
	m := len(s.requests())
	if err := w.SetPointerLock(false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the unlock request", func() bool { return len(s.requests()) > m })
	if q := s.requests()[m]; q.op != opChangeWindowAttributes || le32(q.raw[12:]) != 0 {
		t.Errorf("unlock request %d cursor 0x%x, want ChangeWindowAttributes with None", q.op, le32(q.raw[12:]))
	}
	m = len(s.requests())
	if err := w.SetPointerLock(true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second lock", func() bool { return len(s.requests()) >= m+2 })
	if q := s.requests()[m]; q.op != opChangeWindowAttributes {
		t.Errorf("second lock starts with request %d, want no new cursor", q.op)
	}
	if err := w.SetPointerLock(true); err != nil {
		t.Fatal(err)
	}
}

func TestPollHoldsTrailingRelease(t *testing.T) {
	s, w := openFake(t, fakeConfig{}, Options{})
	poll := func(send [][]byte, want ...Event) {
		t.Helper()
		if len(send) > 0 {
			n := w.queued() + len(send)
			s.send(send...)
			waitFor(t, "queued events", func() bool { return w.queued() >= n })
		}
		got, err := w.Poll()
		if err != nil {
			t.Fatal(err)
		}
		if !equalEvents(got, want) {
			t.Errorf("Poll:\n got %+v\nwant %+v", got, want)
		}
	}
	ev := func(code byte, time uint32) []byte { return keyMsg(code, 25, time, 0, w.win) }
	keyDown, keyUp, text := Event{Kind: KeyDown, Code: "KeyW"}, Event{Kind: KeyUp, Code: "KeyW"}, Event{Kind: Text, Text: "w"}

	poll([][]byte{ev(xKeyPress, 100)}, keyDown, text)
	poll([][]byte{ev(xKeyRelease, 200)}) // held: its repeat press may follow
	poll(nil, keyUp)                     // nothing followed: a real release
	poll([][]byte{ev(xKeyPress, 300), ev(xKeyRelease, 400)}, keyDown, text)
	poll([][]byte{ev(xKeyPress, 400)}, text) // the held release and this press are a repeat pair
	poll([][]byte{ev(xKeyRelease, 500)})
	poll([][]byte{pointerMsg(xMotionNotify, 0, 1, 2, w.win)}, keyUp, Event{Kind: MouseMove, X: 1, Y: 2})
}

func TestPollKeyRepeatAfterFocusLoss(t *testing.T) {
	s, w := openFake(t, fakeConfig{}, Options{})
	s.send(keyMsg(xKeyPress, 38, 10, 0, w.win), focusOutMsg(w.win, 3),
		keyMsg(xKeyRelease, 38, 50, 0, w.win), keyMsg(xKeyPress, 38, 50, 0, w.win),
		pointerMsg(xMotionNotify, 0, 1, 1, w.win))
	waitFor(t, "queued events", func() bool { return w.queued() >= 5 })
	got, _ := w.Poll()
	want := []Event{{Kind: KeyDown, Code: "KeyA"}, {Kind: Text, Text: "a"}, {Kind: FocusLost},
		{Kind: KeyDown, Code: "KeyA"}, {Kind: Text, Text: "a"}, {Kind: MouseMove, X: 1, Y: 1}}
	if !equalEvents(got, want) {
		t.Errorf("Poll:\n got %+v\nwant %+v", got, want)
	}
}

func TestXErrorSurfaced(t *testing.T) {
	s, w := openFake(t, fakeConfig{}, Options{})
	s.send(pointerMsg(xMotionNotify, 0, 3, 4, w.win), errorMsg(8, opPutImage, 42, fakeWindowID))
	waitFor(t, "the error", func() bool { return w.stickyErr() != nil })
	got, err := w.Poll()
	if err == nil || !strings.Contains(err.Error(), "BadMatch") || !strings.Contains(err.Error(), "PutImage") ||
		!strings.Contains(err.Error(), "opcode 72") || !strings.Contains(err.Error(), "sequence 42") {
		t.Fatalf("Poll error = %v", err)
	}
	var pe *protoError
	if !errors.As(err, &pe) || pe.Code != 8 || pe.Major != opPutImage || pe.Value != fakeWindowID {
		t.Errorf("error %#v", pe)
	}
	if !equalEvents(got, []Event{{Kind: MouseMove, X: 3, Y: 4}}) {
		t.Errorf("events with the error: %+v", got)
	}
	if err2 := w.Present(gfx.NewImage(4, 4)); err2 != err {
		t.Errorf("Present error = %v, want %v", err2, err)
	}
	if _, err2 := w.Poll(); err2 != err {
		t.Errorf("error is not sticky: %v", err2)
	}
	if e := decodeXError(errorMsg(1, 140, 5, 0), 140); !strings.Contains(e.Error(), "BigReqEnable") || !strings.Contains(e.Error(), "BadRequest") {
		t.Errorf("extension error: %v", e)
	}
	if e := decodeXError(errorMsg(200, 200, 5, 0), 0); !strings.Contains(e.Error(), "error code 200") {
		t.Errorf("unknown error: %v", e)
	}
}

func TestConnectionLost(t *testing.T) {
	s, w := openFake(t, fakeConfig{}, Options{})
	s.closeConn()
	waitFor(t, "the connection error", func() bool { return w.stickyErr() != nil })
	if _, err := w.Poll(); err == nil || !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("Poll error = %v", err)
	}
	if err := w.Present(gfx.NewImage(4, 4)); err == nil {
		t.Error("Present after connection loss: no error")
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close after connection loss: %v", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	s, w := openFake(t, fakeConfig{}, Options{})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.done:
	default:
		t.Fatal("reader goroutine still running after Close")
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := w.Poll(); err != errClosed {
		t.Errorf("Poll after Close: %v", err)
	}
	if err := w.Present(gfx.NewImage(4, 4)); err != errClosed {
		t.Errorf("Present after Close: %v", err)
	}
	if cw, ch := w.Size(); cw != 320 || ch != 240 {
		t.Errorf("Size after Close = %dx%d", cw, ch)
	}
	waitFor(t, "server EOF", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.eof
	})
	reqs := s.requests()
	n := len(reqs)
	if n < 2 || reqs[n-2].op != opFreeGC || le32(reqs[n-2].raw[4:]) != fakeGCID ||
		reqs[n-1].op != opDestroyWindow || le32(reqs[n-1].raw[4:]) != fakeWindowID {
		t.Errorf("last requests %v", s.opcodes())
	}
}

func TestCloseConcurrent(t *testing.T) {
	_, w := openFake(t, fakeConfig{}, Options{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	select {
	case <-w.done:
	default:
		t.Fatal("reader goroutine still running")
	}
}
