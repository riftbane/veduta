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
	"sync"
	"time"

	"github.com/riftbane/veduta/v2/sim"
)

// Finding what the player holds, and surviving it being unplugged. Discovery is reading
// sysfs, so it is testable anywhere; opening a device is behind openPad so a test can hand
// back a fake that is unplugged on cue. TestEvdevUinput runs the real discovery and the real
// device through a virtual pad, where the kernel lets the test make one.
var openPad = func(d inputDevice) (events, error) {
	s, err := openEvdev(d.Dev)
	if err != nil {
		return nil, err
	}
	s.d.stickDPad = !d.DPadButtons
	return s, nil
}

// padEnv names a device to use instead of searching: an event node ("event3") or part of a
// device name ("Rii").
const padEnv = "VEDUTA_PAD"

// padRescan is how often the input devices are looked for again, whether or not some are
// already being read. A console is usually switched on before the pad is plugged in.
const padRescan = time.Second

// inputDevice is one /dev/input/eventN and what sysfs says about it.
type inputDevice struct {
	Node        string     // event3
	Dev         string     // /dev/input/event3
	Name        string     // "Rii Gamepad"
	Kind        deviceKind // what its capabilities make it
	DPadButtons bool       // its D-pad is four buttons (BTN_DPAD_UP…RIGHT)
}

// deviceKind is what a device's capabilities make it.
type deviceKind uint8

const (
	kindOther    deviceKind = iota // nothing the player reads, unless it is named
	kindPad                        // buttons of a gamepad, a joystick-style pad or a D-pad
	kindKeyboard                   // Escape: every keyboard has it and no pad does
)

// classify tells what a device is from its key capability bitmap.
func classify(keys string) deviceKind {
	switch {
	case hasPadButtons(keys):
		return kindPad
	case hasKeyboardKeys(keys):
		return kindKeyboard
	}
	return kindOther
}

// findInputs returns every device worth reading: gamepads first, then keyboards, each group
// in node order so the same devices are chosen every time. A keyboard counts because a
// console is often used before its pad is plugged in — on an emulated machine, at a text
// console, or on the bench the day the panel works and the pad's buttons are still unknown.
// What a device is, is read from its capabilities in sysfs rather than by opening it.
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
		keys := readAttr(dir, e.Name(), "device/capabilities/key")
		d := inputDevice{
			Node:        e.Name(),
			Dev:         filepath.Join(sysRoot, "dev", "input", e.Name()),
			Name:        readAttr(dir, e.Name(), "device/name"),
			Kind:        classify(keys),
			DPadButtons: bitmapHas(keys, btnDPadUp),
		}
		all = append(all, d)
		switch {
		case want != "":
			if d.Node == want || (d.Name != "" && strings.Contains(strings.ToLower(d.Name), strings.ToLower(want))) {
				matching = append(matching, d)
			}
		case d.Kind == kindPad:
			pads = append(pads, d)
		case d.Kind == kindKeyboard:
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
	for _, bit := range []int{btnSouth, btnTrigger, btnDPadUp} {
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
	node    string
	src     events
	down    [sim.NumButtons]int // button → how many of this device's buttons and axes hold it
	grabbed bool                // taken for this process alone
}

// grabber is a device that can be taken for this process alone (EVIOCGRAB) and given back.
type grabber interface {
	grab(take bool) error
}

// grabStall is how long the player may go without polling before the devices are given
// back. A variable so a test need not wait that long.
var grabStall = 2 * time.Second

// inputSource reads every gamepad and keyboard at once and hands their events on together.
// Reading all of them rather than choosing one is what lets a console be driven from a
// keyboard while its pad is unplugged, and lets the pad take over the moment it appears,
// without anything having to be restarted.
//
// Several things press the same button: a pad's hat, its stick and its D-pad buttons all
// press the D-pad, and a keyboard's Space is the pad's A. A button is therefore held while
// anything holds it, and reported down with the first press and up with the last release.
//
// Every device is taken for the player alone while it is being read, so a keyboard does not
// also type into the text console the player draws over. The player must keep polling to
// keep them: when it has not polled for grabStall, a watchdog gives them back, so that a
// game stuck in a loop does not take Ctrl+C, the console switch and SysRq down with it.
// The next poll takes them again.
type inputSource struct {
	want  string
	open  []openDevice
	down  [sim.NumButtons]int // button → how many buttons and axes, over every device, hold it
	out   []Event             // what the last poll returned, reused so an idle poll allocates nothing
	next  time.Time           // when to look again
	now   func() time.Time    // replaced in tests
	tried bool

	log     io.Writer // where a change in what can be read is reported: stderr
	problem string    // why nothing is being read, as last reported; empty while something is

	mu       sync.Mutex  // held by poll and close, and by the watchdog while it gives devices back
	stall    *time.Timer // the watchdog, started with the first device taken and reset by every poll
	released bool        // the watchdog gave the devices back: the next poll takes them again
}

func newInputSource(want string) *inputSource {
	return &inputSource{want: want, now: time.Now, log: os.Stderr}
}

// Devices names what is being read, for diagnostics; empty when nothing is.
func (p *inputSource) Devices() string {
	nodes := make([]string, len(p.open))
	for i := range p.open {
		nodes[i] = p.open[i].node // the node alone: the watchdog may be changing the rest
	}
	return strings.Join(nodes, ",")
}

// poll returns the events of every device since the last call. The slice is reused by the
// next call.
func (p *inputSource) poll() ([]Event, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stall != nil {
		p.stall.Reset(grabStall)
	}
	if p.released {
		p.released = false
		for i := range p.open {
			p.take(&p.open[i])
		}
	}
	p.attach()
	p.out = p.out[:0]
	for i := 0; i < len(p.open); {
		d := &p.open[i]
		evs, err := d.src.poll()
		p.pass(d, evs)
		if err != nil {
			// Unplugged: evs already carried the buttons it had down, released. Whatever it
			// did not release is let go of here, so no button outlives the device holding it.
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

// settle keeps reading, for at most d, until nothing is held. The player closes while the
// keys that closed it (Select and Start, Ctrl and Q) are still down; letting go of the
// devices then would hand those keys, and the kernel's repeats of them, to the text console
// and the shell behind it. Waiting for their release while the devices are still taken
// keeps them away.
func (p *inputSource) settle(d time.Duration) {
	deadline := p.now().Add(d)
	for p.holding() && p.now().Before(deadline) {
		time.Sleep(settleStep)
		p.poll()
	}
}

// settleStep is how often settle reads.
const settleStep = 10 * time.Millisecond

// holding reports whether any button is down, as the game sees it, or any key physically on
// a device that can tell.
func (p *inputSource) holding() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range p.down {
		if n > 0 {
			return true
		}
	}
	for _, d := range p.open {
		if s, ok := d.src.(interface{ pressed() bool }); ok && s.pressed() {
			return true
		}
	}
	return false
}

// pass hands on what one device sent, counting presses by button: a button goes down when
// the first key or axis anywhere holds it and up when the last one lets go. A release from
// a device that never pressed the button is not a release at all.
func (p *inputSource) pass(d *openDevice, evs []Event) {
	for _, e := range evs {
		switch e.Kind {
		case Press:
			if e.Button >= sim.NumButtons {
				continue
			}
			d.down[e.Button]++
			if p.down[e.Button]++; p.down[e.Button] == 1 {
				p.out = append(p.out, e)
			}
		case Release:
			if e.Button >= sim.NumButtons || d.down[e.Button] == 0 {
				continue
			}
			d.down[e.Button]--
			p.release(e.Button, 1)
		default:
			p.out = append(p.out, e)
		}
	}
}

// take grabs a device for this process alone, if it can be grabbed and is not already. A
// device that refuses (not an event device, or one another process holds) is still read.
func (p *inputSource) take(d *openDevice) {
	g, ok := d.src.(grabber)
	if !ok || d.grabbed || g.grab(true) != nil {
		return
	}
	d.grabbed = true
	if p.stall == nil {
		p.stall = time.AfterFunc(grabStall, p.giveBack)
	}
}

// giveBack is the watchdog: the player has stopped polling, so every device it took is
// given back until it polls again.
func (p *inputSource) giveBack() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.open {
		d := &p.open[i]
		if d.grabbed {
			d.src.(grabber).grab(false)
			d.grabbed = false
			p.released = true
		}
	}
}

// release lets go of n presses of a button and reports it up when none is left.
func (p *inputSource) release(b sim.Button, n int) {
	if p.down[b] == 0 {
		return
	}
	p.down[b] = max(p.down[b]-n, 0)
	if p.down[b] == 0 {
		p.out = append(p.out, Event{Kind: Release, Button: b})
	}
}

// forget lets go of everything a departing device still holds, in button order so the same
// session gives the same events.
func (p *inputSource) forget(d *openDevice) {
	for b := sim.Button(0); b < sim.NumButtons; b++ {
		if n := d.down[b]; n > 0 {
			p.release(b, n)
			d.down[b] = 0
		}
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
		src, oerr := openPad(d)
		if oerr != nil {
			refused = append(refused, oerr) // a device that will not open is one we do without
			continue
		}
		list = append(list, openDevice{node: d.Node, src: src})
		p.take(&list[len(list)-1])
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

// close lets every device go; closing a descriptor also gives back what it had taken.
func (p *inputSource) close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stall != nil {
		p.stall.Stop()
	}
	var err error
	for _, d := range p.open {
		if cerr := d.src.close(); err == nil {
			err = cerr
		}
	}
	p.open = nil
	return err
}
