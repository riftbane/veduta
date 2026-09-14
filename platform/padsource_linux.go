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

// Finding the pad and surviving it being unplugged. Discovery is reading sysfs, so it is
// testable anywhere; opening the device is behind openPad so a test can hand back
// something a regular file cannot be — a character device answers read deadlines, a file
// does not.
var openPad = func(path string) (events, error) { return openEvdev(path) }

// padEnv names a device to use instead of searching: an event node ("event3") or part of a
// device name ("Rii").
const padEnv = "VEDUTA_PAD"

// padRescan is how long to wait before looking for a pad again after finding none or
// losing one. A console is usually switched on before the pad is plugged in.
const padRescan = time.Second

// inputDevice is one /dev/input/eventN and what sysfs says about it.
type inputDevice struct {
	Node string // event3
	Dev  string // /dev/input/event3
	Name string // "Rii Gamepad"
}

// findPad returns the pad among the input devices: the one whose key capabilities carry
// gamepad or joystick buttons. A keyboard has none of them, which is how the two are told
// apart without opening either.
func findPad(want string) (inputDevice, error) {
	dir := filepath.Join(sysRoot, "sys", "class", "input")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return inputDevice{}, fmt.Errorf("platform: no input devices in %s: %w", dir, err)
	}
	var all, matching []inputDevice
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
		switch {
		case want != "":
			if d.Node == want || (d.Name != "" && strings.Contains(strings.ToLower(d.Name), strings.ToLower(want))) {
				matching = append(matching, d)
			}
		case hasPadButtons(readAttr(dir, e.Name(), "device/capabilities/key")):
			matching = append(matching, d)
		}
	}
	sort.Slice(matching, func(i, j int) bool { return matching[i].Node < matching[j].Node })
	if len(matching) == 0 {
		if want != "" {
			return inputDevice{}, fmt.Errorf("platform: no input device matches %q (found %s)", want, describeInputs(all))
		}
		return inputDevice{}, fmt.Errorf("platform: no gamepad among the input devices (found %s); set %s to name one", describeInputs(all), padEnv)
	}
	return matching[0], nil // the lowest node, so the same pad is chosen every time
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

// bitmapHas reports whether a bit is set in a sysfs capability bitmap. The kernel prints
// these as hexadecimal words from the most significant down, one per machine word, so the
// width is taken from the text itself rather than assumed: the same code reads a 64-bit
// board's files and an ARMv6 one's.
func bitmapHas(bitmap string, bit int) bool {
	words := strings.Fields(bitmap)
	off := 0
	for i := len(words) - 1; i >= 0; i-- {
		w := words[i]
		width := 4 * len(w)
		if bit < off+width {
			if bit < off {
				return false
			}
			v, err := strconv.ParseUint(w, 16, 64)
			if err != nil {
				return false
			}
			return v>>uint(bit-off)&1 == 1
		}
		off += width
	}
	return false
}

// padSource keeps a pad attached across unplugging: the console goes on running, and the
// keys the pad had down are reported released rather than left stuck.
type padSource struct {
	want  string
	src   events
	next  time.Time        // when to look again, while there is no pad
	now   func() time.Time // replaced in tests
	dev   string
	tried bool
}

func newPadSource(want string) *padSource {
	return &padSource{want: want, now: time.Now}
}

// Device is the node currently read, empty when no pad is attached.
func (p *padSource) Device() string { return p.dev }

func (p *padSource) poll() ([]Event, error) {
	if p.src == nil {
		if p.tried && p.now().Before(p.next) {
			return nil, nil
		}
		p.tried = true
		d, err := findPad(p.want)
		if err != nil {
			p.next = p.now().Add(padRescan)
			return nil, nil // no pad yet is a console waiting, not a failure
		}
		src, err := openPad(d.Dev)
		if err != nil {
			p.next = p.now().Add(padRescan)
			return nil, nil
		}
		p.src, p.dev = src, d.Node
	}
	evs, err := p.src.poll()
	if err != nil {
		// Unplugged mid-game: evs already carries the releases the decoder produced.
		p.src.close()
		p.src, p.dev = nil, ""
		p.next = p.now().Add(padRescan)
	}
	return evs, nil
}

func (p *padSource) close() error {
	if p.src == nil {
		return nil
	}
	err := p.src.close()
	p.src, p.dev = nil, ""
	return err
}
