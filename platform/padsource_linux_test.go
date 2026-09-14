package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeInputs builds a /sys/class/input tree. Each entry is "node name|key-bitmap".
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
		name, bitmap, _ := strings.Cut(rest, "|")
		dir := filepath.Join(root, "sys", "class", "input", node, "device", "capabilities")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "key"), []byte(strings.TrimSpace(bitmap)+"\n"), 0o644); err != nil {
			t.Fatal(err)
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
			name:    "nothing to read",
			entries: []string{"event0 Some Mouse|" + mouseBits},
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
		batches: [][]Event{{{Kind: KeyDown, Code: "Space"}}},
		err:     errors.New("device removed"),
	}
	opened := 0
	old := openPad
	openPad = func(string) (events, error) { opened++; return pad, nil }
	t.Cleanup(func() { openPad = old })

	now := time.Now()
	p := newInputSource("")
	p.now = func() time.Time { return now }

	if evs, err := p.poll(); err != nil || len(evs) != 1 || evs[0].Code != "Space" {
		t.Fatalf("first poll: %v %v", evs, err)
	}
	if p.Devices() != "event0" {
		t.Errorf("reading %q", p.Devices())
	}
	// Pulled out: the console keeps running and lets the device go.
	if evs, err := p.poll(); err != nil || len(evs) != 0 {
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
	pad.batches = [][]Event{{{Kind: KeyDown, Code: "Enter"}}}
	pad.err = nil
	if evs, err := p.poll(); err != nil || len(evs) != 1 || evs[0].Code != "Enter" {
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
	pad := &fakePad{batches: [][]Event{{{Kind: KeyDown, Code: "Space"}}}}
	keyboard := &fakePad{batches: [][]Event{{{Kind: KeyDown, Code: "ArrowDown"}}}}
	byNode := map[string]*fakePad{"event1": pad, "event0": keyboard}
	old := openPad
	openPad = func(path string) (events, error) { return byNode[filepath.Base(path)], nil }
	t.Cleanup(func() { openPad = old })

	p := newInputSource("")
	evs, err := p.poll()
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, e := range evs {
		codes = append(codes, e.Code)
	}
	// The pad is read first, being the one a console is meant to be played with.
	if strings.Join(codes, ",") != "Space,ArrowDown" {
		t.Fatalf("events %v, want the pad's then the keyboard's", codes)
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

// TestInputSourceWithNothingToRead: a console with no pad and no keyboard runs and waits,
// rather than failing to start.
func TestInputSourceWithNothingToRead(t *testing.T) {
	fakeInputs(t, "event0 Some Mouse|"+mouseBits)
	p := newInputSource("")
	now := time.Now()
	p.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if evs, err := p.poll(); err != nil || len(evs) != 0 {
			t.Fatalf("poll %d: %v %v", i, evs, err)
		}
	}
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
}
