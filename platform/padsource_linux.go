package platform

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Finding what the player holds, and surviving it being unplugged. Discovery is reading
// sysfs, so it is testable anywhere; opening a device is behind openPad so a test can hand
// back a fake that is unplugged on cue. TestEvdevUinput runs the real discovery and the real
// device through a virtual pad, where the kernel lets the test make one.
var openPad = func(path string) (events, error) { return openEvdev(path) }

// padEnv names a device to use instead of searching: an event node ("event3") or part of a
// device name ("Rii").
const padEnv = "VEDUTA_PAD"

// padRescan is how often the input devices are looked for again, whether or not some are
// already being read. A console is usually switched on before the pad is plugged in.
const padRescan = time.Second

// inputDevice is one /dev/input/eventN and what sysfs says about it.
type inputDevice struct {
	Node string // event3
	Dev  string // /dev/input/event3
	Name string // "Rii Gamepad"
}

// findInputs returns every device worth reading: gamepads first, then keyboards, each
// group in node order so the same devices are chosen every time. A keyboard counts because
// a console is often used before its pad is plugged in — on an emulated machine, at a text
// console, or on the bench the day the panel works and the pad's buttons are still
// unknown. What a device is, is read from its capabilities in sysfs rather than by opening
// it.
func findInputs(want string) ([]inputDevice, error) {
	dir := filepath.Join(sysRoot, "sys", "class", "input")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("platform: no input devices in %s: %w", dir, err)
	}
	var all, matching, pads, keyboards []inputDevice
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		d := inputDevice{
			Node: e.Name(),
			Dev:  filepath.Join(sysRoot, "dev", "input", e.Name()),
			Name: readAttr(dir, e.Name(), "device/name"),
		}
		all = append(all, d)
		keys := readAttr(dir, e.Name(), "device/capabilities/key")
		switch {
		case want != "":
			if d.Node == want || (d.Name != "" && strings.Contains(strings.ToLower(d.Name), strings.ToLower(want))) {
				matching = append(matching, d)
			}
		case hasPadButtons(keys):
			pads = append(pads, d)
		case hasKeyboardKeys(keys):
			keyboards = append(keyboards, d)
		}
	}
	byNode := func(l []inputDevice) { sort.Slice(l, func(i, j int) bool { return l[i].Node < l[j].Node }) }
	byNode(matching)
	byNode(pads)
	byNode(keyboards)
	if want == "" {
		matching = append(pads, keyboards...)
	}
	if len(matching) == 0 {
		if want != "" {
			return nil, fmt.Errorf("platform: no input device matches %q (found %s)", want, describeInputs(all))
		}
		return nil, fmt.Errorf("platform: no gamepad or keyboard among the input devices (found %s); set %s to name one", describeInputs(all), padEnv)
	}
	return matching, nil
}

func describeInputs(list []inputDevice) string {
	if len(list) == 0 {
		return "none"
	}
	s := make([]string, len(list))
	for i, d := range list {
		s[i] = d.Node
		if d.Name != "" {
			s[i] += " (" + d.Name + ")"
		}
	}
	return strings.Join(s, ", ")
}

func readAttr(dir, node, attr string) string {
	b, err := os.ReadFile(filepath.Join(dir, node, attr))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// hasPadButtons reports whether a key capability bitmap carries the buttons of a gamepad,
// a joystick-style pad or a D-pad.
func hasPadButtons(bitmap string) bool {
	for _, bit := range []int{btnSouth, btnTrigger, 0x220} {
		if bitmapHas(bitmap, bit) {
			return true
		}
	}
	return false
}

// hasKeyboardKeys reports whether a bitmap is a keyboard's. Escape is on every one of them
// and on no gamepad.
func hasKeyboardKeys(bitmap string) bool { return bitmapHas(bitmap, keyEsc) }

// bitmapHas reports whether a bit is set in a sysfs capability bitmap, as this program
// reads it. The kernel prints one hexadecimal number per word, most significant first,
// without leading zeros (an empty word is "0", and the empty words at the top are left
// out), so the text says nothing about how wide a word is: "8000 10000000000000 0" is
// three 64-bit words. The width is that of a long in the reading process — 64 bits for a
// 64-bit program, 32 for a 32-bit one, even on a 64-bit kernel, which splits its words for
// such a reader — and that is this program's own int.
func bitmapHas(bitmap string, bit int) bool {
	return bitmapHasWords(bitmap, bit, strconv.IntSize)
}

// bitmapHasWords is bitmapHas for words of wordBits bits.
func bitmapHasWords(bitmap string, bit, wordBits int) bool {
	if bit < 0 {
		return false
	}
	words := strings.Fields(bitmap)
	i := len(words) - 1 - bit/wordBits
	if i < 0 {
		return false
	}
	v, err := strconv.ParseUint(words[i], 16, wordBits)
	if err != nil {
		return false
	}
	return v>>uint(bit%wordBits)&1 == 1
}

// openDevice is one device being read.
type openDevice struct {
	node string
	src  events
	down map[string]int // W3C code → how many of this device's buttons and axes hold it
}

// inputSource reads every gamepad and keyboard at once and hands their events on together.
// Reading all of them rather than choosing one is what lets a console be driven from a
// keyboard while its pad is unplugged, and lets the pad take over the moment it appears,
// without anything having to be restarted.
//
// Several things report the same key: a pad's hat, its stick and its D-pad buttons all
// press the arrows, and a keyboard's Space is the pad's A. A key is therefore held while
// anything holds it, and reported down with the first press and up with the last release.
type inputSource struct {
	want  string
	open  []openDevice
	down  map[string]int   // W3C code → how many buttons and axes, over every device, hold it
	out   []Event          // what the last poll returned, reused so an idle poll allocates nothing
	next  time.Time        // when to look again
	now   func() time.Time // replaced in tests
	tried bool

	log     io.Writer // where a change in what can be read is reported: stderr
	problem string    // why nothing is being read, as last reported; empty while something is
}

func newInputSource(want string) *inputSource {
	return &inputSource{want: want, now: time.Now, down: map[string]int{}, log: os.Stderr}
}

// Devices names what is being read, for diagnostics; empty when nothing is.
func (p *inputSource) Devices() string {
	nodes := make([]string, len(p.open))
	for i, d := range p.open {
		nodes[i] = d.node
	}
	return strings.Join(nodes, ",")
}

// poll returns the events of every device since the last call. The slice is reused by the
// next call.
func (p *inputSource) poll() ([]Event, error) {
	p.attach()
	p.out = p.out[:0]
	for i := 0; i < len(p.open); {
		d := p.open[i]
		evs, err := d.src.poll()
		p.pass(d, evs)
		if err != nil {
			// Unplugged: evs already carried the keys it had down, released. Whatever it
			// did not release is let go of here, so no key outlives the device holding it.
			p.forget(d)
			d.src.close()
			p.open = append(p.open[:i], p.open[i+1:]...)
			p.next = p.now().Add(padRescan)
			continue
		}
		i++
	}
	return p.out, nil
}

// pass hands on what one device sent, counting presses by W3C code: a key goes down when
// the first button or axis anywhere holds it and up when the last one lets go. A release
// from a device that never pressed the key is not a release at all.
func (p *inputSource) pass(d openDevice, evs []Event) {
	for _, e := range evs {
		switch e.Kind {
		case KeyDown:
			d.down[e.Code]++
			if p.down[e.Code]++; p.down[e.Code] == 1 {
				p.out = append(p.out, e)
			}
		case KeyUp:
			n := d.down[e.Code]
			if n == 0 {
				continue
			}
			if n == 1 {
				delete(d.down, e.Code)
			} else {
				d.down[e.Code] = n - 1
			}
			p.release(e.Code, 1)
		default:
			p.out = append(p.out, e)
		}
	}
}

// release lets go of n presses of a key and reports it up when none is left.
func (p *inputSource) release(code string, n int) {
	left := p.down[code] - n
	if left > 0 {
		p.down[code] = left
		return
	}
	delete(p.down, code)
	p.out = append(p.out, Event{Kind: KeyUp, Code: code})
}

// forget lets go of everything a departing device still holds, in code order so the same
// session gives the same events.
func (p *inputSource) forget(d openDevice) {
	codes := make([]string, 0, len(d.down))
	for code := range d.down {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		p.release(code, d.down[code])
		delete(d.down, code)
	}
}

// attach looks for devices at most once per rescan interval, whether or not something is
// already being read, and opens the ones not yet open. Finding nothing is a console waiting
// for a pad to be plugged in, not a failure.
//
// The open devices end up in discovery's order, pads first, so a pad plugged in after a
// keyboard is read before it as it would have been at start-up. One still open that
// discovery no longer lists has most likely just been unplugged: it is kept until its next
// read says so, which is what releases the keys it had down.
func (p *inputSource) attach() {
	if p.tried && p.now().Before(p.next) {
		return
	}
	p.tried = true
	p.next = p.now().Add(padRescan)
	devices, err := findInputs(p.want)
	if err != nil {
		p.report(err, nil)
		return
	}
	var refused []error
	kept := make([]bool, len(p.open))
	list := make([]openDevice, 0, len(devices)+len(p.open))
	for _, d := range devices {
		i := 0
		for i < len(p.open) && p.open[i].node != d.Node {
			i++
		}
		if i < len(p.open) {
			kept[i] = true
			list = append(list, p.open[i])
			continue
		}
		src, oerr := openPad(d.Dev)
		if oerr != nil {
			refused = append(refused, oerr) // a device that will not open is one we do without
			continue
		}
		list = append(list, openDevice{node: d.Node, src: src, down: map[string]int{}})
	}
	for i, d := range p.open {
		if !kept[i] {
			list = append(list, d)
		}
	}
	p.open = list
	p.report(nil, refused)
}

// report writes to the log what reading input has come to, once when it changes rather
// than at every rescan: why nothing is being read, or, after such a message, what is read
// again. A player reading nothing shows the game and answers nothing, and it cannot even be
// left, since the exit chords arrive through the same input, so the reason has to be
// somewhere a person looking at the log will find it. Some devices refusing while another
// is read is not a problem worth a line.
func (p *inputSource) report(findErr error, refused []error) {
	problem := ""
	switch {
	case len(p.open) > 0:
	case findErr != nil:
		problem = findErr.Error()
	case len(refused) > 0:
		msgs := make([]string, len(refused))
		denied := false
		for i, err := range refused {
			msgs[i] = err.Error()
			denied = denied || errors.Is(err, fs.ErrPermission)
		}
		problem = "platform: no input device could be opened: " + strings.Join(msgs, "; ")
		if denied {
			problem += " (reading /dev/input takes root or membership of the input group)"
		}
	}
	if problem == p.problem {
		return
	}
	p.problem = problem
	if problem != "" {
		fmt.Fprintf(p.log, "%s; looking again every %v\n", problem, padRescan)
	} else {
		fmt.Fprintf(p.log, "platform: reading input from %s\n", p.Devices())
	}
}

func (p *inputSource) close() error {
	var err error
	for _, d := range p.open {
		if cerr := d.src.close(); err == nil {
			err = cerr
		}
	}
	p.open = nil
	return err
}
