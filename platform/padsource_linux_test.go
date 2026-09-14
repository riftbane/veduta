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

// fakeInputs builds a /sys/class/input tree. Each entry is "node name key-bitmap".
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

// A key bitmap with one bit set, printed the way the kernel prints it.
func bitmapWith(bit int, wordChars int) string {
	words := bit/(wordChars*4) + 1
	out := make([]string, words)
	for i := range out {
		out[i] = strings.Repeat("0", wordChars)
	}
	w := bit / (wordChars * 4)
	var v uint64 = 1 << uint(bit%(wordChars*4))
	hex := strconv.FormatUint(v, 16)
	out[len(out)-1-w] = strings.Repeat("0", wordChars-len(hex)) + hex
	return strings.Join(out, " ")
}

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

func TestFindPad(t *testing.T) {
	keyboard := bitmapWith(0x20, 16) // an ordinary key, no gamepad buttons
	pad := bitmapWith(btnSouth, 16)
	joystick := bitmapWith(btnTrigger, 16)

	t.Run("the pad among keyboards", func(t *testing.T) {
		fakeInputs(t, "event0 AT Keyboard|"+keyboard, "event3 Rii Gamepad|"+pad)
		d, err := findPad("")
		if err != nil || d.Node != "event3" {
			t.Fatalf("found %+v, %v", d, err)
		}
		if d.Dev != filepath.Join(sysRoot, "dev", "input", "event3") {
			t.Errorf("device node %s", d.Dev)
		}
	})
	t.Run("a joystick-style pad counts", func(t *testing.T) {
		fakeInputs(t, "event0 Clone Joystick|"+joystick)
		if d, err := findPad(""); err != nil || d.Node != "event0" {
			t.Fatalf("found %+v, %v", d, err)
		}
	})
	t.Run("chosen by name", func(t *testing.T) {
		fakeInputs(t, "event0 One Pad|"+pad, "event1 Other Pad|"+pad)
		if d, err := findPad("other"); err != nil || d.Node != "event1" {
			t.Fatalf("found %+v, %v", d, err)
		}
	})
	t.Run("the lowest node when several match", func(t *testing.T) {
		fakeInputs(t, "event5 Pad|"+pad, "event2 Pad|"+pad)
		if d, err := findPad(""); err != nil || d.Node != "event2" {
			t.Fatalf("found %+v, %v", d, err)
		}
	})
	t.Run("no pad", func(t *testing.T) {
		fakeInputs(t, "event0 AT Keyboard|"+keyboard)
		_, err := findPad("")
		if err == nil || !strings.Contains(err.Error(), "no gamepad") {
			t.Fatalf("err = %v", err)
		}
	})
}

// fakePad is an input source a test can drive.
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

func TestPadSourceAttachesAndSurvivesUnplugging(t *testing.T) {
	fakeInputs(t, "event0 Rii Gamepad|"+bitmapWith(btnSouth, 16))
	pad := &fakePad{
		batches: [][]Event{{{Kind: KeyDown, Code: "Space"}}},
		err:     errors.New("device removed"),
	}
	opened := 0
	old := openPad
	openPad = func(string) (events, error) { opened++; return pad, nil }
	t.Cleanup(func() { openPad = old })

	now := time.Now()
	p := newPadSource("")
	p.now = func() time.Time { return now }

	if evs, err := p.poll(); err != nil || len(evs) != 1 || evs[0].Code != "Space" {
		t.Fatalf("first poll: %v %v", evs, err)
	}
	if p.Device() != "event0" {
		t.Errorf("attached to %q", p.Device())
	}
	// The pad is pulled out: the console keeps running and the pad is let go.
	if evs, err := p.poll(); err != nil || len(evs) != 0 {
		t.Fatalf("unplugged poll: %v %v", evs, err)
	}
	if p.Device() != "" || pad.closed != 1 {
		t.Fatalf("after unplugging: device %q, closed %d", p.Device(), pad.closed)
	}
	// It is not reopened again and again in between.
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

func TestPadSourceWithoutAPad(t *testing.T) {
	fakeInputs(t, "event0 AT Keyboard|"+bitmapWith(0x20, 16))
	p := newPadSource("")
	now := time.Now()
	p.now = func() time.Time { return now }
	// A console with no pad plugged in runs; it just receives nothing.
	for i := 0; i < 3; i++ {
		if evs, err := p.poll(); err != nil || len(evs) != 0 {
			t.Fatalf("poll %d: %v %v", i, evs, err)
		}
	}
	if err := p.close(); err != nil {
		t.Fatal(err)
	}
}
