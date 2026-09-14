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

// bitmapWith prints a capability bitmap with one bit set, the way the kernel prints them:
// hexadecimal words of the machine's width, most significant first.
func bitmapWith(bit int, wordChars int) string {
	words := bit/(wordChars*4) + 1
	out := make([]string, words)
	for i := range out {
		out[i] = strings.Repeat("0", wordChars)
	}
	w := bit / (wordChars * 4)
	hex := strconv.FormatUint(1<<uint(bit%(wordChars*4)), 16)
	out[len(out)-1-w] = strings.Repeat("0", wordChars-len(hex)) + hex
	return strings.Join(out, " ")
}

var (
	padBits      = bitmapWith(btnSouth, 16)
	joystickBits = bitmapWith(btnTrigger, 16)
	keyboardBits = bitmapWith(keyEsc, 16)
	mouseBits    = bitmapWith(0x110, 16) // BTN_LEFT: neither a pad nor a keyboard
)

func TestBitmapHas(t *testing.T) {
	// A 64-bit kernel prints sixteen characters per word, a 32-bit one eight.
	for _, chars := range []int{8, 16} {
		for _, bit := range []int{0, 1, 63, 64, 0x130, 0x220} {
			if !bitmapHas(bitmapWith(bit, chars), bit) {
				t.Errorf("%d-char words: bit %#x not found in %q", chars, bit, bitmapWith(bit, chars))
			}
			if bitmapHas(bitmapWith(bit, chars), bit+1) {
				t.Errorf("%d-char words: bit %#x found where only %#x is set", chars, bit+1, bit)
			}
		}
	}
	if bitmapHas("", 0x130) || bitmapHas("zzz", 0x130) {
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
