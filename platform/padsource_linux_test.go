package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riftbane/veduta/sim"
)

// fakeInputs builds a /sys/class/input tree. Each entry is "node name|key-bitmap", optionally
// followed by "|rel-bitmap" and "|abs-bitmap".
func fakeInputs(t *testing.T, entries ...string) {
	t.Helper()
	root := sysRoot
	if root == "/" {
		root = t.TempDir()
		old := sysRoot
		sysRoot = root
		t.Cleanup(func() { sysRoot = old })
	}
	for _, e := range entries {
		node, rest, _ := strings.Cut(e, " ")
		name, bitmaps, _ := strings.Cut(rest, "|")
		dir := filepath.Join(root, "sys", "class", "input", node, "device", "capabilities")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		caps := append(strings.Split(bitmaps, "|"), "", "")
		for i, kind := range []string{"key", "rel", "abs"} {
			if caps[i] == "" && kind != "key" {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, kind), []byte(strings.TrimSpace(caps[i])+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "..", "name"), []byte(strings.TrimSpace(name)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// bitmapWith prints a capability bitmap with the given bits set, the way the kernel prints
// one for a reader whose words are wordBits wide: an unpadded hexadecimal number per word,
// most significant first, the empty words at the top left out.
func bitmapWith(wordBits int, bits ...int) string {
	top := 0
	for _, b := range bits {
		top = max(top, b/wordBits)
	}
	words := make([]uint64, top+1)
	for _, b := range bits {
		words[b/wordBits] |= 1 << uint(b%wordBits)
	}
	out := make([]string, 0, len(words))
	for i := len(words) - 1; i >= 0; i-- {
		out = append(out, strconv.FormatUint(words[i], 16))
	}
	return strings.Join(out, " ")
}

var (
	padBits      = bitmapWith(strconv.IntSize, btnSouth)
	joystickBits = bitmapWith(strconv.IntSize, btnTrigger)
	keyboardBits = bitmapWith(strconv.IntSize, keyEsc)
	mouseBits    = bitmapWith(strconv.IntSize, 0x110) // BTN_LEFT: neither a pad nor a keyboard
	dpadPadBits  = bitmapWith(strconv.IntSize, btnSouth, btnMode, btnDPadUp, btnDPadUp+1, btnDPadUp+2, btnDPadUp+3)
	powerBits    = bitmapWith(strconv.IntSize, 116)  // KEY_POWER alone: a power button
	xyBits       = bitmapWith(strconv.IntSize, 0, 1) // REL_X and REL_Y, or ABS_X and ABS_Y
)

func TestBitmapHas(t *testing.T) {
	// A 64-bit reader gets 64-bit words, a 32-bit one 32-bit words, on any kernel.
	for _, wordBits := range []int{32, 64} {
		for _, bit := range []int{0, 1, 31, 32, 63, 64, 0x130, 0x220} {
			b := bitmapWith(wordBits, bit)
			if !bitmapHasWords(b, bit, wordBits) {
				t.Errorf("%d-bit words: bit %#x not found in %q", wordBits, bit, b)
			}
			if bitmapHasWords(b, bit+1, wordBits) {
				t.Errorf("%d-bit words: bit %#x found in %q, where only %#x is set", wordBits, bit+1, b, bit)
			}
		}
		// The empty words in between are a lone "0", not a word's worth of zeros.
		b := bitmapWith(wordBits, keyEsc, btnSouth)
		if !bitmapHasWords(b, keyEsc, wordBits) || !bitmapHasWords(b, btnSouth, wordBits) || bitmapHasWords(b, btnSouth-1, wordBits) {
			t.Errorf("%d-bit words: %q read wrongly", wordBits, b)
		}
	}
	// As the kernel printed them for a 64-bit reader: /sys/class/input/eventN/device/
	// capabilities/key of a VM's power button (KEY_POWER, 116), its AT keyboard, and a
	// uinput gamepad with A, B, Select and Start (0x130, 0x131, 0x13a, 0x13b); then that
	// gamepad as a 32-bit reader sees it.
	padButtons := []int{btnSouth, btnSouth + 1, 0x13a, 0x13b}
	for _, c := range []struct {
		bitmap   string
		wordBits int
		set      []int
		unset    []int
	}{
		{"8000 10000000000000 0", 64, []int{116}, []int{keyEsc, btnSouth, 115, 117}},
		{"402000002 3803078f800d001 feffffdfffefffff fffffffffffffffe", 64, []int{keyEsc, keyQ, 57}, []int{0, btnSouth}},
		{"c03000000000000 0 0 0 0", 64, padButtons, []int{keyEsc, btnTrigger, btnSouth + 2}},
		{"c030000 0 0 0 0 0 0 0 0 0", 32, padButtons, []int{keyEsc, btnTrigger, btnSouth + 2}},
	} {
		for _, bit := range c.set {
			if !bitmapHasWords(c.bitmap, bit, c.wordBits) {
				t.Errorf("%q (%d-bit words): bit %#x not found", c.bitmap, c.wordBits, bit)
			}
		}
		for _, bit := range c.unset {
			if bitmapHasWords(c.bitmap, bit, c.wordBits) {
				t.Errorf("%q (%d-bit words): bit %#x found", c.bitmap, c.wordBits, bit)
			}
		}
	}
	if bitmapHas("", 0x130) || bitmapHas("zzz", 0x130) || bitmapHas("zzz", 0) || bitmapHas("1", -1) {
		t.Error("nonsense accepted as a bitmap")
	}
}

func TestFindInputs(t *testing.T) {
	for _, c := range []struct {
		name    string
		entries []string
		want    string
		nodes   []string
		errHas  string
	}{
		{
			// Both are read, and the pad comes first.
			name:    "a pad and a keyboard",
			entries: []string{"event0 AT Keyboard|" + keyboardBits, "event3 Rii Gamepad|" + padBits},
			nodes:   []string{"event3", "event0"},
		},
		{
			// A console with no pad is driven from the keyboard rather than left deaf.
			name:    "only a keyboard",
			entries: []string{"event0 AT Keyboard|" + keyboardBits},
			nodes:   []string{"event0"},
		},
		{
			name:    "a joystick-style pad counts as a pad",
			entries: []string{"event0 Clone Joystick|" + joystickBits},
			nodes:   []string{"event0"},
		},
		{
			name:    "pads in node order, then keyboards",
			entries: []string{"event5 Pad|" + padBits, "event1 Keyboard|" + keyboardBits, "event2 Pad|" + padBits},
			nodes:   []string{"event2", "event5", "event1"},
		},
		{
			name:    "chosen by name, whatever it is",
			entries: []string{"event0 One Pad|" + padBits, "event1 Other Pad|" + padBits},
			want:    "other",
			nodes:   []string{"event1"},
		},
		{
			// A mouse or a tablet presses no button of the console.
			name:    "pointers are not read",
			entries: []string{"event0 Mouse|" + mouseBits + "|" + xyBits, "event1 Tablet|" + mouseBits + "||" + xyBits, "event2 Keyboard|" + keyboardBits, "event3 Pad|" + padBits},
			nodes:   []string{"event3", "event2"},
		},
		{
			name:    "nothing to read",
			entries: []string{"event0 Power Button|" + powerBits, "event1 Mouse|" + mouseBits + "|" + xyBits},
			errHas:  "no gamepad or keyboard",
		},
		{
			name:    "the choice matches nothing",
			entries: []string{"event0 Pad|" + padBits},
			want:    "event9",
			errHas:  "no input device matches",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeInputs(t, c.entries...)
			got, err := findInputs(c.want)
			if c.errHas != "" {
				if err == nil || !strings.Contains(err.Error(), c.errHas) {
					t.Fatalf("err = %v, want one containing %q", err, c.errHas)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var nodes []string
			for _, d := range got {
				nodes = append(nodes, d.Node)
			}
			if strings.Join(nodes, ",") != strings.Join(c.nodes, ",") {
				t.Fatalf("found %v, want %v", nodes, c.nodes)
			}
			if got[0].Dev != filepath.Join(sysRoot, "dev", "input", c.nodes[0]) {
				t.Errorf("device node %s", got[0].Dev)
			}
		})
	}
}

// TestFindInputsDPadButtons: what ABS_X and ABS_Y are depends on the pad. On a pad whose
// D-pad is four buttons they are a stick, which the console does not have; on any other pad
// they may be its D-pad, as on many cheap pads.
func TestFindInputsDPadButtons(t *testing.T) {
	fakeInputs(t,
		"event0 Console Pad|"+dpadPadBits+"||"+xyBits,
		"event1 Clone Pad|"+padBits+"||"+xyBits,
		"event2 Keyboard|"+keyboardBits,
	)
	got, err := findInputs("")
	if err != nil {
		t.Fatal(err)
	}
	var s []string
	for _, d := range got {
		s = append(s, fmt.Sprintf("%s:%v", d.Node, d.DPadButtons))
	}
	if want := "event0:true,event1:false,event2:false"; strings.Join(s, ",") != want {
		t.Fatalf("D-pad buttons on %s, want %s", strings.Join(s, ","), want)
	}
}

// fakePad is an input device a test can drive.
type fakePad struct {
	batches [][]Event
	err     error
	closed  int
}

func (f *fakePad) poll() ([]Event, error) {
	if len(f.batches) == 0 {
		return nil, f.err
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b, nil
}

func (f *fakePad) close() error { f.closed++; return nil }

func TestInputSourceAttachesAndSurvivesUnplugging(t *testing.T) {
	fakeInputs(t, "event0 Rii Gamepad|"+padBits)
	pad := &fakePad{
		batches: [][]Event{{{Kind: Press, Button: sim.ButtonA}}},
		err:     errors.New("device removed"),
	}
	opened := 0
	old := openPad
	openPad = func(inputDevice) (events, error) { opened++; return pad, nil }
	t.Cleanup(func() { openPad = old })

	now := time.Now()
	p := newInputSource("")
	p.now = func() time.Time { return now }

	if evs, err := p.poll(); err != nil || describePad(evs) != "down a" {
		t.Fatalf("first poll: %v %v", evs, err)
	}
	if p.Devices() != "event0" {
		t.Errorf("reading %q", p.Devices())
	}
	// Pulled out: the console keeps running and lets the device go, and the button it held
	// comes back up even though this fake, unlike a real device, did not say so.
	if evs, err := p.poll(); err != nil || describePad(evs) != "up a" {
		t.Fatalf("unplugged poll: %v %v", evs, err)
	}
	if p.Devices() != "" || pad.closed != 1 {
		t.Fatalf("after unplugging: reading %q, closed %d", p.Devices(), pad.closed)
	}
	// It is not reopened over and over in between.
	if _, err := p.poll(); err != nil || opened != 1 {
		t.Fatalf("polls during the wait opened it %d times", opened)
	}
	// Once the wait is over it is looked for again.
	now = now.Add(2 * padRescan)
	pad.batches = [][]Event{{{Kind: Press, Button: sim.ButtonSelect}}}
	pad.err = nil
	if evs, err := p.poll(); err != nil || describePad(evs) != "down select" {
		t.Fatalf("after replugging: %v %v", evs, err)
	}
	if opened != 2 {
		t.Errorf("opened %d times, want a second open", opened)
	}
}

// TestInputSourceReadsPadAndKeyboard is the case that makes the console usable before its
// pad exists: both devices are read, and what either sends arrives.
func TestInputSourceReadsPadAndKeyboard(t *testing.T) {
	fakeInputs(t, "event0 AT Keyboard|"+keyboardBits, "event1 Rii Gamepad|"+padBits)
	pad := &fakePad{batches: [][]Event{{{Kind: Press, Button: sim.ButtonA}}}}
	keyboard := &fakePad{batches: [][]Event{{{Kind: Press, Button: sim.ButtonDown}}}}
	byNode := map[string]*fakePad{"event1": pad, "event0": keyboard}
	old := openPad
	openPad = func(dev inputDevice) (events, error) { return byNode[dev.Node], nil }
	t.Cleanup(func() { openPad = old })

	p := newInputSource("")
	evs, err := p.poll()
	if err != nil {
		t.Fatal(err)
	}
	// The pad is read first, being the one a console is meant to be played with.
	if got := describePad(evs); got != "down a,down down" {
		t.Fatalf("events %q, want the pad's then the keyboard's", got)
	}
	if p.Devices() != "event1,event0" {
		t.Fatalf("reading %q", p.Devices())
	}
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
	if pad.closed != 1 || keyboard.closed != 1 {
		t.Fatalf("closed pad %d keyboard %d", pad.closed, keyboard.closed)
	}
}

// TestInputSourcePicksUpALatePad: a console is often switched on with only a keyboard, or
// some other device that reads as one, and the pad plugged in afterwards. The pad has to
// be found at the next rescan while the keyboard is being read, not only once nothing is,
// and what is already open is neither opened twice nor moved behind the newcomer.
func TestInputSourcePicksUpALatePad(t *testing.T) {
	fakeInputs(t, "event0 AT Keyboard|"+keyboardBits)
	keyboard := &fakePad{batches: [][]Event{{{Kind: Press, Button: sim.ButtonDown}}}}
	pad := &fakePad{batches: [][]Event{{{Kind: Press, Button: sim.ButtonA}}}}
	byNode := map[string]*fakePad{"event0": keyboard, "event1": pad}
	opened := map[string]int{}
	old := openPad
	openPad = func(dev inputDevice) (events, error) {
		node := dev.Node
		opened[node]++
		return byNode[node], nil
	}
	t.Cleanup(func() { openPad = old })

	now := time.Now()
	p := newInputSource("")
	p.now = func() time.Time { return now }
	codes := func() string {
		t.Helper()
		evs, err := p.poll()
		if err != nil {
			t.Fatal(err)
		}
		return describePad(evs)
	}

	if got := codes(); got != "down down" || p.Devices() != "event0" {
		t.Fatalf("keyboard alone: events %q, reading %q", got, p.Devices())
	}
	fakeInputs(t, "event1 Rii Gamepad|"+padBits)
	// Not before the rescan: sysfs is not read on every frame.
	if got := codes(); got != "" || p.Devices() != "event0" {
		t.Fatalf("before the rescan: events %q, reading %q", got, p.Devices())
	}
	now = now.Add(2 * padRescan)
	if got := codes(); got != "down a" {
		t.Fatalf("after the rescan: events %q, want the pad's", got)
	}
	if p.Devices() != "event1,event0" {
		t.Fatalf("reading %q, want the pad first", p.Devices())
	}
	// Later rescans find both again and open neither a second time.
	now = now.Add(2 * padRescan)
	codes()
	if opened["event0"] != 1 || opened["event1"] != 1 || keyboard.closed != 0 || pad.closed != 0 {
		t.Fatalf("opened %v, closed keyboard %d pad %d", opened, keyboard.closed, pad.closed)
	}
	// Gone from sysfs before its read fails: it is kept, not dropped with keys still down,
	// and let go when the read does fail.
	if err := os.RemoveAll(filepath.Join(sysRoot, "sys", "class", "input", "event1")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * padRescan)
	codes()
	if p.Devices() != "event0,event1" || pad.closed != 0 {
		t.Fatalf("unlisted but still open: reading %q, pad closed %d", p.Devices(), pad.closed)
	}
	pad.err = errors.New("device removed")
	codes()
	if p.Devices() != "event0" || pad.closed != 1 {
		t.Fatalf("after the failed read: reading %q, pad closed %d", p.Devices(), pad.closed)
	}
}

// decodingPad is an input device whose records go through the real decoder, one poll's
// worth at a time. Once err is set it is unplugged: it releases what it holds, as a real
// device does, unless vanish says it goes without a word.
type decodingPad struct {
	d      *padDecoder
	next   []byte
	err    error
	vanish bool
	onPoll func() // called before each read, to change what the next one returns
}

func (f *decodingPad) poll() ([]Event, error) {
	if f.onPoll != nil {
		f.onPoll()
	}
	if f.err != nil {
		if f.vanish {
			return nil, f.err
		}
		return f.d.releaseAll(nil), f.err
	}
	b := f.next
	f.next = nil
	return f.d.decode(b, nil)
}

func (f *decodingPad) close() error { return nil }

func (f *decodingPad) pressed() bool { return f.d.pressed() }

// TestInputSourceCountsHolds: a pad's hat, its stick and its D-pad buttons, and the
// keyboard beside it, all press the same few buttons. A button is held while anything holds
// it: it goes down with the first and up with the last, whichever key, axis or device that
// is. Before, letting the stick back to the middle released left while the hat still held
// it, and the hero stopped with the D-pad pressed.
func TestInputSourceCountsHolds(t *testing.T) {
	fakeInputs(t, "event0 AT Keyboard|"+keyboardBits, "event1 Rii Gamepad|"+padBits)
	const size = 24
	pad := &decodingPad{d: newPadDecoder()}
	keyboard := &decodingPad{d: newPadDecoder()}
	pad.d.size, keyboard.d.size = size, size
	byNode := map[string]events{"event1": pad, "event0": keyboard}
	old := openPad
	openPad = func(dev inputDevice) (events, error) { return byNode[dev.Node], nil }
	t.Cleanup(func() { openPad = old })

	p := newInputSource("")
	rec := func(typ, code uint16, value int32) []byte { return record(size, typ, code, value) }
	const keySpace, keyLeft = 57, 105
	for _, s := range []struct {
		name          string
		pad, keyboard []byte
		want          string
	}{
		{name: "hat left", pad: rec(evAbs, absHat0X, -1), want: "down left"},
		{name: "stick left as well", pad: rec(evAbs, absX, -30000), want: ""},
		{name: "stick back to the middle", pad: rec(evAbs, absX, 0), want: ""},
		{name: "D-pad button left as well", pad: rec(evKey, btnDPadUp+2, 1), want: ""},
		{name: "hat back", pad: rec(evAbs, absHat0X, 0), want: ""},
		{name: "D-pad button back", pad: rec(evKey, btnDPadUp+2, 0), want: "up left"},
		{name: "Space on the keyboard", keyboard: rec(evKey, keySpace, 1), want: "down a"},
		{name: "A on the pad as well", pad: rec(evKey, btnSouth, 1), want: ""},
		{name: "the keyboard lets go", keyboard: rec(evKey, keySpace, 0), want: ""},
		{name: "the pad lets go", pad: rec(evKey, btnSouth, 0), want: "up a"},
		{name: "left on both at once", pad: rec(evAbs, absHat0X, -1), keyboard: rec(evKey, keyLeft, 1), want: "down left"},
	} {
		pad.next, keyboard.next = s.pad, s.keyboard
		evs, err := p.poll()
		if err != nil {
			t.Fatal(err)
		}
		if got := describePad(evs); got != s.want {
			t.Fatalf("%s: %q, want %q", s.name, got, s.want)
		}
	}
	// The pad is unplugged while both hold left: the keyboard still does.
	pad.err = errors.New("device removed")
	if evs, _ := p.poll(); describePad(evs) != "" || p.Devices() != "event0" {
		t.Fatalf("unplugged while the keyboard holds the key: %q, reading %q", describePad(evs), p.Devices())
	}
	keyboard.next = rec(evKey, keyLeft, 0)
	if evs, _ := p.poll(); describePad(evs) != "up left" {
		t.Fatalf("the keyboard lets go: %q, want up left", describePad(evs))
	}
	// A device that goes without releasing what it held still lets go of it: no button
	// outlives the device holding it.
	keyboard.next = rec(evKey, 17, 1) // KEY_W
	if evs, _ := p.poll(); describePad(evs) != "down up" {
		t.Fatalf("W: %q", describePad(evs))
	}
	keyboard.err, keyboard.vanish = errors.New("gone"), true
	if evs, _ := p.poll(); describePad(evs) != "up up" || p.Devices() != "" {
		t.Fatalf("a device gone without a word: %q, reading %q", describePad(evs), p.Devices())
	}
}

// TestInputSourceIdlePollAllocatesNothing: the player polls every tick, and a tick where
// nothing is pressed must not feed the collector.
func TestInputSourceIdlePollAllocatesNothing(t *testing.T) {
	fakeInputs(t, "event0 AT Keyboard|"+keyboardBits)
	idle := &fakePad{}
	old := openPad
	openPad = func(inputDevice) (events, error) { return idle, nil }
	t.Cleanup(func() { openPad = old })
	p := newInputSource("")
	now := time.Now()
	p.now = func() time.Time { return now }
	if _, err := p.poll(); err != nil || p.Devices() != "event0" {
		t.Fatalf("first poll: %v, reading %q", err, p.Devices())
	}
	if n := testing.AllocsPerRun(100, func() { p.poll() }); n != 0 {
		t.Errorf("an idle poll allocates %v times", n)
	}
}

// grabbingPad is a fake device that can be taken for one process alone, and remembers
// every time it was taken or given back. The watchdog calls it from its own goroutine.
type grabbingPad struct {
	fakePad
	mu    sync.Mutex
	grabs []string
}

func (g *grabbingPad) grab(take bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.grabs = append(g.grabs, map[bool]string{true: "take", false: "give back"}[take])
	return nil
}

func (g *grabbingPad) history() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return strings.Join(g.grabs, ",")
}

// TestInputSourceGrabsWhilePolled: a device is taken for the player alone as soon as it is
// opened, so a keyboard stops typing into the text console under the game. It is given
// back when the player stops polling for grabStall, so that a game stuck in a loop does not
// take Ctrl+C and the console switch with it, and taken again at the next poll.
func TestInputSourceGrabsWhilePolled(t *testing.T) {
	fakeInputs(t, "event0 AT Keyboard|"+keyboardBits)
	keyboard := &grabbingPad{}
	old, oldStall := openPad, grabStall
	openPad = func(inputDevice) (events, error) { return keyboard, nil }
	t.Cleanup(func() { openPad, grabStall = old, oldStall })
	grabStall = time.Minute

	p := newInputSource("")
	now := time.Now()
	p.now = func() time.Time { return now }
	if _, err := p.poll(); err != nil {
		t.Fatal(err)
	}
	if got := keyboard.history(); got != "take" {
		t.Fatalf("after the first poll: %q, want the keyboard taken", got)
	}
	// Keeping it costs an idle poll nothing.
	if n := testing.AllocsPerRun(100, func() { p.poll() }); n != 0 {
		t.Errorf("an idle poll of a taken device allocates %v times", n)
	}

	grabStall = 20 * time.Millisecond
	p.poll() // the watchdog now waits the short time
	deadline := time.Now().Add(5 * time.Second)
	for keyboard.history() != "take,give back" {
		if time.Now().After(deadline) {
			t.Fatalf("no poll for far longer than grabStall: %q, want the keyboard given back", keyboard.history())
		}
		time.Sleep(5 * time.Millisecond)
	}
	grabStall = time.Minute
	if _, err := p.poll(); err != nil {
		t.Fatal(err)
	}
	if got := keyboard.history(); got != "take,give back,take" {
		t.Fatalf("polling again: %q, want the keyboard taken again", got)
	}

	// Closing gives it back by closing it, and stops the watchdog.
	grabStall = 200 * time.Millisecond
	p.poll()
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if got := keyboard.history(); got != "take,give back,take" || keyboard.closed != 1 {
		t.Fatalf("after close: %q, closed %d", got, keyboard.closed)
	}
}

// TestInputSourceWithNothingToRead: a console with no pad or keyboard runs and waits,
// rather than failing to start, and says once why nothing answers.
func TestInputSourceWithNothingToRead(t *testing.T) {
	fakeInputs(t, "event0 Some Mouse|"+mouseBits)
	p := newInputSource("")
	var log strings.Builder
	p.log = &log
	now := time.Now()
	p.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if evs, err := p.poll(); err != nil || len(evs) != 0 {
			t.Fatalf("poll %d: %v %v", i, evs, err)
		}
		now = now.Add(2 * padRescan)
	}
	if got := log.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "no gamepad or keyboard") {
		t.Fatalf("logged %q, want the reason once", got)
	}
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
}

// TestInputSourceReportsProblems: a player that reads no input shows the game and answers
// nothing, and cannot even be left, since the exit chords arrive through that input. The
// reason goes to the log once when it appears or changes, not once per rescan, and the
// log says so when input is read again.
func TestInputSourceReportsProblems(t *testing.T) {
	fakeInputs(t, "event0 AT Keyboard|"+keyboardBits, "event1 Rii Gamepad|"+padBits)
	refuse := true
	pad := &fakePad{}
	old := openPad
	openPad = func(dev inputDevice) (events, error) {
		if refuse {
			return nil, &os.PathError{Op: "open", Path: dev.Dev, Err: os.ErrPermission}
		}
		return pad, nil
	}
	t.Cleanup(func() { openPad = old })

	p := newInputSource("")
	var log strings.Builder
	p.log = &log
	now := time.Now()
	p.now = func() time.Time { return now }
	poll := func() string {
		t.Helper()
		before := log.Len()
		if _, err := p.poll(); err != nil {
			t.Fatal(err)
		}
		now = now.Add(2 * padRescan)
		return log.String()[before:]
	}

	got := poll()
	for _, want := range []string{"no input device could be opened", "event1: permission denied", "event0: permission denied", "input group"} {
		if !strings.Contains(got, want) {
			t.Fatalf("every device refused: logged %q, want it to say %q", got, want)
		}
	}
	if got := poll(); got != "" {
		t.Fatalf("the same refusal at the next rescan was logged again: %q", got)
	}
	refuse = false
	if got := poll(); !strings.Contains(got, "reading input from event1,event0") {
		t.Fatalf("once the devices open: logged %q", got)
	}
	if got := poll(); got != "" {
		t.Fatalf("nothing changed, but logged %q", got)
	}

	// A VEDUTA_PAD that names nothing says what there is instead.
	named := newInputSource("event9")
	named.log = &log
	before := log.Len()
	named.poll()
	if got := log.String()[before:]; !strings.Contains(got, `no input device matches "event9"`) || !strings.Contains(got, "Rii Gamepad") {
		t.Fatalf("a name that matches nothing: logged %q", got)
	}
}

// TestInputSourceSettles: the player closes with the chord still held, and waits for it to
// be let go before giving the devices back, so the keys never reach the text console. A
// key nobody lets go of does not keep the player from closing.
func TestInputSourceSettles(t *testing.T) {
	fakeInputs(t, "event0 AT Keyboard|"+keyboardBits)
	const size = 24
	keyboard := &decodingPad{d: newPadDecoder()}
	keyboard.d.size = size
	old := openPad
	openPad = func(inputDevice) (events, error) { return keyboard, nil }
	t.Cleanup(func() { openPad = old })
	p := newInputSource("")
	keyboard.next = append(record(size, evKey, keyLeftCtrl, 1), record(size, evKey, keyQ, 1)...)
	if evs, _ := p.poll(); !strings.Contains(describePad(evs), "close") {
		t.Fatalf("Ctrl+Q: %q", describePad(evs))
	}
	polls := 0
	keyboard.onPoll = func() {
		if polls++; polls == 3 {
			keyboard.next = append(record(size, evKey, keyQ, 0), record(size, evKey, keyLeftCtrl, 0)...)
		}
	}
	start := time.Now()
	p.settle(time.Second)
	if p.holding() || polls < 3 || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("settle: holding %v after %d polls in %v", p.holding(), polls, time.Since(start))
	}
	keyboard.onPoll = nil
	keyboard.next = record(size, evKey, 17, 1) // W, never released
	p.poll()
	start = time.Now()
	p.settle(50 * time.Millisecond)
	if !p.holding() || time.Since(start) > 400*time.Millisecond {
		t.Fatalf("a key never let go: holding %v, waited %v", p.holding(), time.Since(start))
	}
}
