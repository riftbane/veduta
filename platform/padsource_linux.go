package platform

import (
	"fmt"
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

// padRescan is how long to wait before looking again after finding nothing or losing a
// device. A console is usually switched on before the pad is plugged in.
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
}

// inputSource reads every gamepad and keyboard at once and hands their events on together.
// Reading all of them rather than choosing one is what lets a console be driven from a
// keyboard while its pad is unplugged, and lets the pad take over the moment it appears,
// without anything having to be restarted.
type inputSource struct {
	want  string
	open  []openDevice
	next  time.Time        // when to look again
	now   func() time.Time // replaced in tests
	tried bool
}

func newInputSource(want string) *inputSource {
	return &inputSource{want: want, now: time.Now}
}

// Devices names what is being read, for diagnostics; empty when nothing is.
func (p *inputSource) Devices() string {
	nodes := make([]string, len(p.open))
	for i, d := range p.open {
		nodes[i] = d.node
	}
	return strings.Join(nodes, ",")
}

func (p *inputSource) poll() ([]Event, error) {
	p.attach()
	var out []Event
	for i := 0; i < len(p.open); {
		d := p.open[i]
		evs, err := d.src.poll()
		out = append(out, evs...)
		if err != nil {
			// Unplugged: evs already carries the keys it had down, released.
			d.src.close()
			p.open = append(p.open[:i], p.open[i+1:]...)
			p.next = p.now().Add(padRescan)
			continue
		}
		i++
	}
	return out, nil
}

// attach opens whatever is there, at most once per rescan interval. Finding nothing is a
// console waiting for a pad to be plugged in, not a failure.
func (p *inputSource) attach() {
	if len(p.open) > 0 || (p.tried && p.now().Before(p.next)) {
		return
	}
	p.tried = true
	p.next = p.now().Add(padRescan)
	devices, err := findInputs(p.want)
	if err != nil {
		return
	}
	for _, d := range devices {
		src, err := openPad(d.Dev)
		if err != nil {
			continue // a device that will not open is one we do without
		}
		p.open = append(p.open, openDevice{node: d.Node, src: src})
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
