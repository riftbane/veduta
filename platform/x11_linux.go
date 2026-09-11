//go:build linux

package platform

// This file is the X11 core-protocol client: $DISPLAY parsing, connection, Xauthority
// cookies, the connection setup, and the encoders of the few requests the window uses.
// The client announces little-endian byte order ('l'), so every request, reply and event
// field is little-endian whatever the host is. Image byte order is a separate server
// property (see setupInfo.imageMSB).

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Core protocol request opcodes used by the window.
const (
	opCreateWindow           = 1
	opChangeWindowAttributes = 2
	opDestroyWindow          = 4
	opMapWindow              = 8
	opInternAtom             = 16
	opChangeProperty         = 18
	opWarpPointer            = 41
	opGetInputFocus          = 43
	opCreatePixmap           = 53
	opCreateGC               = 55
	opFreeGC                 = 60
	opPolyFillRectangle      = 70
	opPutImage               = 72
	opCreateColormap         = 78
	opCreateCursor           = 93
	opQueryExtension         = 98
	opGetKeyboardMapping     = 101
	opGetModifierMapping     = 119
)

// Predefined atoms (X11 protocol, appendix B).
const (
	atomATOM          = 4
	atomSTRING        = 31
	atomWMName        = 39
	atomWMNormalHints = 40
	atomWMSizeHints   = 41
	atomWMClass       = 67
)

// Event codes (the high bit of byte 0 marks SendEvent and is masked off).
const (
	xError           = 0
	xReply           = 1
	xKeyPress        = 2
	xKeyRelease      = 3
	xButtonPress     = 4
	xButtonRelease   = 5
	xMotionNotify    = 6
	xFocusOut        = 10
	xDestroyNotify   = 17
	xConfigureNotify = 22
	xClientMessage   = 33
	xGenericEvent    = 35
)

// Window event mask: KeyPress|KeyRelease|ButtonPress|ButtonRelease|PointerMotion|
// Exposure|StructureNotify|FocusChange.
const windowEventMask = 0x1 | 0x2 | 0x4 | 0x8 | 0x40 | 0x8000 | 0x20000 | 0x200000

// Xauthority address families.
const (
	familyInternet  = 0
	familyInternet6 = 6
	familyLocal     = 256
	familyWild      = 65535
)

const cookieName = "MIT-MAGIC-COOKIE-1"

// pad4 rounds n up to a multiple of 4.
func pad4(n int) int { return (n + 3) &^ 3 }

func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func put16(b []byte, v uint16) {
	binary.LittleEndian.PutUint16(b, v)
}
func put32(b []byte, v uint32) {
	binary.LittleEndian.PutUint32(b, v)
}

// ---- $DISPLAY -------------------------------------------------------------------------

// displayAddr is a parsed $DISPLAY value.
type displayAddr struct {
	protocol string // "unix", "tcp", or "" (unix socket first, then TCP to localhost)
	host     string // TCP host; "" for the local machine
	path     string // explicit unix socket path (a $DISPLAY starting with '/')
	display  int
	screen   int
}

// dialTarget is one address to try when connecting.
type dialTarget struct{ network, address string }

// parseDisplay parses $DISPLAY in the forms libxcb accepts: [protocol/][host]:display[.screen]
// (":0", ":0.1", "unix:0", "localhost:10.0", "tcp/host:1", "[::1]:0") and unix socket
// paths ("/tmp/.X11-unix/X0", "/tmp/.X11-unix/X0.1", launchd's "/path/org.xquartz:0").
func parseDisplay(s string) (displayAddr, error) {
	if s == "" {
		return displayAddr{}, errors.New("$DISPLAY is not set")
	}
	if s[0] == '/' {
		slash := strings.LastIndexByte(s, '/')
		if i := strings.LastIndexByte(s, ':'); i > slash {
			n, scr, err := parseDisplayNumber(s[i+1:])
			if err != nil {
				return displayAddr{}, fmt.Errorf("$DISPLAY %q: %w", s, err)
			}
			return displayAddr{protocol: "unix", path: s[:i], display: n, screen: scr}, nil
		}
		d := displayAddr{protocol: "unix", path: s}
		if dot := strings.LastIndexByte(s, '.'); dot > slash && isDigits(s[dot+1:]) {
			d.screen, _ = strconv.Atoi(s[dot+1:])
			d.path = s[:dot]
		}
		// The display number only selects the Xauthority entry: take it from the
		// conventional socket name X<n>.
		if name := d.path[strings.LastIndexByte(d.path, '/')+1:]; len(name) > 1 && name[0] == 'X' && isDigits(name[1:]) {
			d.display, _ = strconv.Atoi(name[1:])
		}
		return d, nil
	}
	var d displayAddr
	rest := s
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		switch p := rest[:i]; p {
		case "unix", "local":
			d.protocol = "unix"
		case "tcp", "inet", "inet6":
			d.protocol = "tcp"
		default:
			return displayAddr{}, fmt.Errorf("$DISPLAY %q: unsupported protocol %q", s, p)
		}
		rest = rest[i+1:]
	}
	i := strings.LastIndexByte(rest, ':')
	if i < 0 {
		return displayAddr{}, fmt.Errorf("$DISPLAY %q: missing ':display'", s)
	}
	n, scr, err := parseDisplayNumber(rest[i+1:])
	if err != nil {
		return displayAddr{}, fmt.Errorf("$DISPLAY %q: %w", s, err)
	}
	d.display, d.screen = n, scr
	host := rest[:i]
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	switch {
	case host == "unix":
		if d.protocol == "tcp" {
			return displayAddr{}, fmt.Errorf("$DISPLAY %q: host \"unix\" with protocol tcp", s)
		}
		d.protocol = "unix"
	case host != "":
		if d.protocol == "unix" {
			return displayAddr{}, fmt.Errorf("$DISPLAY %q: unix protocol with remote host %q", s, host)
		}
		d.protocol = "tcp"
		d.host = host
	}
	return d, nil
}

// parseDisplayNumber parses "display[.screen]".
func parseDisplayNumber(s string) (display, screen int, err error) {
	num, scr, hasScreen := strings.Cut(s, ".")
	if !isDigits(num) || len(num) > 5 {
		return 0, 0, fmt.Errorf("invalid display number %q", num)
	}
	display, _ = strconv.Atoi(num)
	if hasScreen {
		if !isDigits(scr) || len(scr) > 5 {
			return 0, 0, fmt.Errorf("invalid screen number %q", scr)
		}
		screen, _ = strconv.Atoi(scr)
	}
	return display, screen, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// targets returns the addresses to try, in order: on Linux the abstract socket
// "@/tmp/.X11-unix/X<n>" before the filesystem socket (what libxcb does), and TCP port
// 6000+n for remote hosts or as the last resort of a plain ":n".
func (d displayAddr) targets() []dialTarget {
	if d.path != "" {
		return []dialTarget{{"unix", d.path}}
	}
	host := d.host
	if host == "" {
		host = "localhost"
	}
	tcp := dialTarget{"tcp", net.JoinHostPort(host, strconv.Itoa(6000+d.display))}
	if d.protocol == "tcp" {
		return []dialTarget{tcp}
	}
	sock := "/tmp/.X11-unix/X" + strconv.Itoa(d.display)
	t := []dialTarget{{"unix", "@" + sock}, {"unix", sock}}
	if d.protocol == "" {
		t = append(t, tcp)
	}
	return t
}

// dial connects to the first reachable target.
func (d displayAddr) dial(timeout time.Duration) (net.Conn, error) {
	var errs []error
	for _, t := range d.targets() {
		c, err := net.DialTimeout(t.network, t.address, timeout)
		if err == nil {
			return c, nil
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

// ---- Xauthority ----------------------------------------------------------------------

// xauthEntry is one record of an Xauthority file.
type xauthEntry struct {
	family uint16
	addr   []byte
	number string // display number as decimal text; "" matches every display
	name   string // authorization protocol, e.g. MIT-MAGIC-COOKIE-1
	data   []byte
}

// xauthPath returns $XAUTHORITY, or ~/.Xauthority.
func xauthPath() string {
	if p := os.Getenv("XAUTHORITY"); p != "" {
		return p
	}
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".Xauthority")
}

// parseXauth decodes an Xauthority file: records of a big-endian uint16 family followed
// by four big-endian uint16-length-prefixed fields (address, display number, auth name,
// auth data).
func parseXauth(b []byte) ([]xauthEntry, error) {
	var out []xauthEntry
	field := func() ([]byte, error) {
		if len(b) < 2 {
			return nil, io.ErrUnexpectedEOF
		}
		n := int(binary.BigEndian.Uint16(b))
		if len(b) < 2+n {
			return nil, io.ErrUnexpectedEOF
		}
		f := b[2 : 2+n]
		b = b[2+n:]
		return f, nil
	}
	for len(b) > 0 {
		if len(b) < 2 {
			return out, fmt.Errorf("xauthority record %d: %w", len(out), io.ErrUnexpectedEOF)
		}
		e := xauthEntry{family: binary.BigEndian.Uint16(b)}
		b = b[2:]
		var f [4][]byte
		for i := range f {
			var err error
			if f[i], err = field(); err != nil {
				return out, fmt.Errorf("xauthority record %d: %w", len(out), err)
			}
		}
		e.addr, e.number, e.name, e.data = f[0], string(f[1]), string(f[2]), f[3]
		out = append(out, e)
	}
	return out, nil
}

// findCookie returns the first MIT-MAGIC-COOKIE-1 entry for the address and display, as
// libXau's XauGetBestAuthByAddr does: the entry's family is Wild, or equal to family with
// an equal address; its display number is empty or equal.
func findCookie(entries []xauthEntry, family uint16, addr []byte, display int) ([]byte, bool) {
	num := strconv.Itoa(display)
	for _, e := range entries {
		if e.name != cookieName {
			continue
		}
		if e.family != familyWild && (e.family != family || !bytes.Equal(e.addr, addr)) {
			continue
		}
		if e.number != "" && e.number != num {
			continue
		}
		return e.data, true
	}
	return nil, false
}

// authFor returns the Xauthority family and address that identify the server at the
// other end of c: FamilyLocal with the host name for unix sockets and loopback TCP (as
// libxcb does), FamilyInternet/Internet6 with the peer address otherwise.
func authFor(c net.Conn, hostname string) (uint16, []byte) {
	if a, ok := c.RemoteAddr().(*net.TCPAddr); ok && !a.IP.IsLoopback() {
		if ip4 := a.IP.To4(); ip4 != nil {
			return familyInternet, ip4
		}
		return familyInternet6, a.IP.To16()
	}
	return familyLocal, []byte(hostname)
}

// loadCookie reads the Xauthority file at path and returns the cookie for the server
// behind c, or nil (no authorization) when there is none.
func loadCookie(path string, c net.Conn, display int) []byte {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil // no file: try without authorization, as Xlib does
	}
	entries, _ := parseXauth(b) // a truncated file still yields its complete records
	host, _ := os.Hostname()
	fam, addr := authFor(c, host)
	data, _ := findCookie(entries, fam, addr, display)
	return data
}

// ---- connection setup ----------------------------------------------------------------

// pixmapFormat is one entry of the setup's pixmap formats.
type pixmapFormat struct {
	depth, bpp, scanlinePad byte
}

// visualInfo is a TrueColor visual usable for 8-bit-per-channel BGRA pixels.
type visualInfo struct {
	id    uint32
	depth byte
}

// setupInfo is what the window needs from the connection setup reply.
type setupInfo struct {
	vendor                 string
	ridBase, ridMask       uint32
	maxReqUnits            uint32 // maximum request length in 4-byte units
	imageMSB               bool   // server image byte order is MSBFirst
	minKeycode, maxKeycode byte
	formats                []pixmapFormat
	root, rootVisual       uint32
	rootDepth              byte
	visual                 visualInfo // chosen visual
	bpp, scanlinePad       int        // pixmap format of the chosen depth, in bits
}

// setupRequest encodes the connection setup request.
func setupRequest(authName string, authData []byte) []byte {
	if len(authData) == 0 {
		authName = ""
	}
	nn, dn := pad4(len(authName)), pad4(len(authData))
	b := make([]byte, 12+nn+dn)
	b[0] = 'l'
	put16(b[2:], 11) // protocol-major-version
	put16(b[4:], 0)  // protocol-minor-version
	put16(b[6:], uint16(len(authName)))
	put16(b[8:], uint16(len(authData)))
	copy(b[12:], authName)
	copy(b[12+nn:], authData)
	return b
}

// readSetupReply reads the setup reply and returns its body (everything after the
// 8-byte header) on success.
func readSetupReply(r io.Reader) ([]byte, error) {
	var h [8]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, fmt.Errorf("read setup reply: %w", err)
	}
	body := make([]byte, int(le16(h[6:]))*4)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read setup reply: %w", err)
	}
	switch h[0] {
	case 1:
		return body, nil
	case 0:
		n := int(h[1])
		if n > len(body) {
			n = len(body)
		}
		return nil, fmt.Errorf("server refused the connection (protocol %d.%d): %s",
			le16(h[2:]), le16(h[4:]), strings.TrimSpace(string(body[:n])))
	case 2:
		return nil, fmt.Errorf("server requires further authentication: %s",
			strings.TrimSpace(string(bytes.TrimRight(body, "\x00"))))
	default:
		return nil, fmt.Errorf("invalid setup reply status %d", h[0])
	}
}

// wireReader decodes little-endian fields with bounds checking: reading past the end
// yields zeros and sets bad.
type wireReader struct {
	b   []byte
	off int
	bad bool
}

func (r *wireReader) take(n int) []byte {
	if n < 0 || r.off+n > len(r.b) {
		r.bad = true
		r.off = len(r.b)
		return nil
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}

func (r *wireReader) u8() byte {
	if s := r.take(1); s != nil {
		return s[0]
	}
	return 0
}

func (r *wireReader) u16() uint16 {
	if s := r.take(2); s != nil {
		return le16(s)
	}
	return 0
}

func (r *wireReader) u32() uint32 {
	if s := r.take(4); s != nil {
		return le32(s)
	}
	return 0
}

// parseSetup decodes the body of a successful setup reply (the bytes after its 8-byte
// header) and picks the visual for screen: the root visual when it is TrueColor with
// 8-bit red, green and blue masks at depth 24 or 32, otherwise such a visual of depth 24.
func parseSetup(body []byte, screen int) (*setupInfo, error) {
	r := &wireReader{b: body}
	s := &setupInfo{}
	r.u32() // release number
	s.ridBase = r.u32()
	s.ridMask = r.u32()
	r.u32() // motion buffer size
	vendorLen := int(r.u16())
	s.maxReqUnits = uint32(r.u16())
	nScreens := int(r.u8())
	nFormats := int(r.u8())
	s.imageMSB = r.u8() == 1
	r.u8() // bitmap bit order
	r.u8() // bitmap scanline unit
	r.u8() // bitmap scanline pad
	s.minKeycode = r.u8()
	s.maxKeycode = r.u8()
	r.take(4)
	if v := r.take(pad4(vendorLen)); v != nil {
		s.vendor = string(v[:vendorLen])
	}
	for i := 0; i < nFormats; i++ {
		f := r.take(8)
		if f != nil {
			s.formats = append(s.formats, pixmapFormat{depth: f[0], bpp: f[1], scanlinePad: f[2]})
		}
	}
	if r.bad {
		return nil, errors.New("setup reply is truncated")
	}
	if nScreens == 0 {
		return nil, errors.New("server has no screens")
	}
	if s.ridMask == 0 {
		return nil, errors.New("server gave an empty resource-id mask")
	}
	if screen >= nScreens {
		screen = 0
	}
	for i := 0; i <= screen; i++ {
		root := r.u32()
		r.take(16) // default colormap, white/black pixel, current input masks
		r.take(12) // width/height in pixels and mm, min/max installed maps
		rootVisual := r.u32()
		r.take(2) // backing stores, save unders
		rootDepth := r.u8()
		nDepths := int(r.u8())
		var rootOK, any24 visualInfo
		for j := 0; j < nDepths; j++ {
			depth := r.u8()
			r.u8()
			nVisuals := int(r.u16())
			r.take(4)
			for k := 0; k < nVisuals; k++ {
				id := r.u32()
				class := r.u8()
				r.take(3) // bits per rgb value, colormap entries
				red, green, blue := r.u32(), r.u32(), r.u32()
				r.take(4)
				usable := class == 4 && red == 0xff0000 && green == 0xff00 && blue == 0xff
				if !usable {
					continue
				}
				if id == rootVisual && (depth == 24 || depth == 32) {
					rootOK = visualInfo{id, depth}
				}
				if depth == 24 && any24.id == 0 {
					any24 = visualInfo{id, depth}
				}
			}
			if r.bad {
				return nil, errors.New("setup reply is truncated")
			}
		}
		if i < screen {
			continue
		}
		s.root, s.rootVisual, s.rootDepth = root, rootVisual, rootDepth
		switch {
		case rootOK.id != 0:
			s.visual = rootOK
		case any24.id != 0:
			s.visual = any24
		default:
			return nil, fmt.Errorf("screen %d has no 24-bit TrueColor visual (root visual 0x%x, depth %d)", screen, rootVisual, rootDepth)
		}
	}
	if r.bad {
		return nil, errors.New("setup reply is truncated")
	}
	for _, f := range s.formats {
		if f.depth == s.visual.depth {
			s.bpp, s.scanlinePad = int(f.bpp), int(f.scanlinePad)
		}
	}
	if s.bpp != 32 && s.bpp != 24 {
		return nil, fmt.Errorf("unsupported pixmap format for depth %d: %d bits per pixel", s.visual.depth, s.bpp)
	}
	if s.scanlinePad != 8 && s.scanlinePad != 16 && s.scanlinePad != 32 {
		return nil, fmt.Errorf("unsupported scanline pad %d for depth %d", s.scanlinePad, s.visual.depth)
	}
	return s, nil
}

// ---- requests ------------------------------------------------------------------------

// request starts a request of n bytes (a multiple of 4) with its opcode, data byte and
// length field set.
func request(op, data byte, n int) []byte {
	b := make([]byte, n)
	b[0] = op
	b[1] = data
	put16(b[2:], uint16(n/4))
	return b
}

func encInternAtom(name string) []byte {
	b := request(opInternAtom, 0, 8+pad4(len(name)))
	put16(b[4:], uint16(len(name)))
	copy(b[8:], name)
	return b
}

func encQueryExtension(name string) []byte {
	b := request(opQueryExtension, 0, 8+pad4(len(name)))
	put16(b[4:], uint16(len(name)))
	copy(b[8:], name)
	return b
}

// encBigReqEnable encodes BIG-REQUESTS' only request (minor opcode 0).
func encBigReqEnable(major byte) []byte { return request(major, 0, 4) }

func encGetKeyboardMapping(first byte, count int) []byte {
	b := request(opGetKeyboardMapping, 0, 8)
	b[4] = first
	b[5] = byte(count)
	return b
}

func encGetModifierMapping() []byte { return request(opGetModifierMapping, 0, 4) }

func encGetInputFocus() []byte { return request(opGetInputFocus, 0, 4) }

// encCreateWindow encodes CreateWindow with values ordered by their mask bits.
func encCreateWindow(depth byte, wid, parent uint32, width, height int, visual uint32, mask uint32, values []uint32) []byte {
	b := request(opCreateWindow, depth, 32+4*len(values))
	put32(b[4:], wid)
	put32(b[8:], parent)
	// x, y = 0: the window manager places the window.
	put16(b[16:], uint16(width))
	put16(b[18:], uint16(height))
	put16(b[20:], 0) // border width
	put16(b[22:], 1) // InputOutput
	put32(b[24:], visual)
	put32(b[28:], mask)
	for i, v := range values {
		put32(b[32+4*i:], v)
	}
	return b
}

func encCreateColormap(mid, window, visual uint32) []byte {
	b := request(opCreateColormap, 0, 16) // alloc None
	put32(b[4:], mid)
	put32(b[8:], window)
	put32(b[12:], visual)
	return b
}

// encChangeProperty encodes ChangeProperty(Replace) with data of the given format (8 or
// 32 bits per item).
func encChangeProperty(window, property, typ uint32, format byte, data []byte) []byte {
	b := request(opChangeProperty, 0, 24+pad4(len(data)))
	put32(b[4:], window)
	put32(b[8:], property)
	put32(b[12:], typ)
	b[16] = format
	put32(b[20:], uint32(len(data)/int(format/8)))
	copy(b[24:], data)
	return b
}

// uint32Bytes encodes 32-bit property items.
func uint32Bytes(v ...uint32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		put32(b[4*i:], x)
	}
	return b
}

func encCreateGC(gc, drawable uint32) []byte {
	b := request(opCreateGC, 0, 20)
	put32(b[4:], gc)
	put32(b[8:], drawable)
	put32(b[12:], 0x10000) // graphics-exposures
	put32(b[16:], 0)       // false
	return b
}

func encResource(op byte, id uint32) []byte {
	b := request(op, 0, 8)
	put32(b[4:], id)
	return b
}

// encChangeWindowAttributes encodes ChangeWindowAttributes with values ordered by their
// mask bits.
func encChangeWindowAttributes(window, mask uint32, values ...uint32) []byte {
	b := request(opChangeWindowAttributes, 0, 12+4*len(values))
	put32(b[4:], window)
	put32(b[8:], mask)
	for i, v := range values {
		put32(b[12+4*i:], v)
	}
	return b
}

func encCreatePixmap(depth byte, pid, drawable uint32, width, height int) []byte {
	b := request(opCreatePixmap, depth, 16)
	put32(b[4:], pid)
	put32(b[8:], drawable)
	put16(b[12:], uint16(width))
	put16(b[14:], uint16(height))
	return b
}

// encPolyFillRectangle encodes PolyFillRectangle with a single rectangle.
func encPolyFillRectangle(drawable, gc uint32, x, y, width, height int) []byte {
	b := request(opPolyFillRectangle, 0, 20)
	put32(b[4:], drawable)
	put32(b[8:], gc)
	put16(b[12:], uint16(int16(x)))
	put16(b[14:], uint16(int16(y)))
	put16(b[16:], uint16(width))
	put16(b[18:], uint16(height))
	return b
}

// encCreateCursor encodes CreateCursor from a depth-1 source and its mask. With an
// all-zero mask no pixel is drawn, which is the invisible cursor of a locked pointer;
// the colors and the hotspot are left zero.
func encCreateCursor(cid, source, mask uint32) []byte {
	b := request(opCreateCursor, 0, 32)
	put32(b[4:], cid)
	put32(b[8:], source)
	put32(b[12:], mask)
	return b
}

// encWarpPointer encodes WarpPointer with no source window: the cursor jumps to x, y
// relative to dst wherever it is.
func encWarpPointer(dst uint32, x, y int) []byte {
	b := request(opWarpPointer, 0, 24)
	put32(b[8:], dst)
	put16(b[20:], uint16(int16(x)))
	put16(b[22:], uint16(int16(y)))
	return b
}

// ---- errors --------------------------------------------------------------------------

var xErrorNames = [...]string{
	1: "BadRequest", 2: "BadValue", 3: "BadWindow", 4: "BadPixmap", 5: "BadAtom",
	6: "BadCursor", 7: "BadFont", 8: "BadMatch", 9: "BadDrawable", 10: "BadAccess",
	11: "BadAlloc", 12: "BadColormap", 13: "BadGContext", 14: "BadIDChoice", 15: "BadName",
	16: "BadLength", 17: "BadImplementation",
}

var xRequestNames = map[byte]string{
	opCreateWindow: "CreateWindow", opDestroyWindow: "DestroyWindow", opMapWindow: "MapWindow",
	opInternAtom: "InternAtom", opChangeProperty: "ChangeProperty", opGetInputFocus: "GetInputFocus",
	opCreateGC: "CreateGC", opFreeGC: "FreeGC", opPutImage: "PutImage",
	opCreateColormap: "CreateColormap", opQueryExtension: "QueryExtension",
	opGetKeyboardMapping: "GetKeyboardMapping", opGetModifierMapping: "GetModifierMapping",
}

// protoError is an error reported by the X server for one of the window's requests.
type protoError struct {
	Code     byte   // error code (8 = BadMatch, …)
	Major    byte   // major opcode of the failed request
	Minor    uint16 // minor opcode (extension requests)
	Sequence uint16 // sequence number of the failed request
	Value    uint32 // bad resource id, atom or value, when the error has one
	request  string // request name when known
}

func decodeXError(m []byte, bigReqOpcode byte) *protoError {
	e := &protoError{Code: m[1], Sequence: le16(m[2:]), Value: le32(m[4:]), Minor: le16(m[8:]), Major: m[10]}
	e.request = xRequestNames[e.Major]
	if e.request == "" && bigReqOpcode != 0 && e.Major == bigReqOpcode {
		e.request = "BigReqEnable"
	}
	return e
}

func (e *protoError) Error() string {
	name := "error"
	if int(e.Code) < len(xErrorNames) && xErrorNames[e.Code] != "" {
		name = xErrorNames[e.Code]
	}
	req := e.request
	if req == "" {
		req = "request"
	}
	return fmt.Sprintf("X11 %s (error code %d) in %s (opcode %d.%d, sequence %d, value 0x%x)",
		name, e.Code, req, e.Major, e.Minor, e.Sequence, e.Value)
}
