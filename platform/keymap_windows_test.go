//go:build windows

package platform

import (
	"testing"

	"github.com/riftbane/veduta/asset"
)

// keyLP builds a key message lParam: repeat count 1, the scan code, the extended flag,
// the previous-state bit (auto-repeat, always set on release) and the transition bit.
func keyLP(scan uint32, extended, repeat, release bool) uintptr {
	lp := uintptr(1) | uintptr(scan&0xFF)<<16
	if extended {
		lp |= 1 << 24
	}
	if repeat || release {
		lp |= 1 << 30
	}
	if release {
		lp |= 1 << 31
	}
	return lp
}

func TestScanCodesProduceEveryKeyCode(t *testing.T) {
	produced := map[string]int{}
	for i, code := range scanCodes {
		if code == "" {
			continue
		}
		if !asset.IsKeyCode(code) {
			t.Errorf("scanCodes[%#x] = %q is not in asset.KeyCodes", i, code)
		}
		if j, dup := produced[code]; dup {
			t.Errorf("%q is produced by both %#x and %#x", code, j, i)
		}
		produced[code] = i
	}
	for _, code := range asset.KeyCodes {
		if _, ok := produced[code]; !ok {
			t.Errorf("no scan code produces %q", code)
		}
	}
}

func TestScanCodeExtendedPairs(t *testing.T) {
	// The same make code is two different keys depending on the 0xE0 prefix.
	for _, c := range []struct {
		scan          uint32
		plain, withE0 string
	}{
		{0x1C, "Enter", "NumpadEnter"},
		{0x1D, "ControlLeft", "ControlRight"},
		{0x38, "AltLeft", "AltRight"},
		{0x35, "Slash", "NumpadDivide"},
		{0x37, "NumpadMultiply", ""}, // E0 37 is PrintScreen
		{0x47, "Numpad7", "Home"},
		{0x48, "Numpad8", "ArrowUp"},
		{0x49, "Numpad9", "PageUp"},
		{0x4B, "Numpad4", "ArrowLeft"},
		{0x4C, "Numpad5", ""},
		{0x4D, "Numpad6", "ArrowRight"},
		{0x4F, "Numpad1", "End"},
		{0x50, "Numpad2", "ArrowDown"},
		{0x51, "Numpad3", "PageDown"},
		{0x52, "Numpad0", "Insert"},
		{0x53, "NumpadDecimal", "Delete"},
		{0x5B, "", "MetaLeft"},
		{0x5C, "", "MetaRight"},
		{0x5D, "", ""}, // ContextMenu
		{0x45, "", ""}, // Pause / NumLock
		{0x46, "", ""}, // ScrollLock / Ctrl+Break
		{0x2A, "ShiftLeft", ""},
		{0x36, "ShiftRight", "ShiftRight"},
		{0x1E, "KeyA", ""},
		{0x11, "KeyW", ""},
		{0x0B, "Digit0", ""},
		{0x29, "Backquote", ""},
		{0x44, "F10", ""},
		{0x57, "F11", ""},
		{0x58, "F12", ""},
	} {
		for _, ext := range []bool{false, true} {
			want := c.plain
			if ext {
				want = c.withE0
			}
			got := ""
			if i, ok := keyIndex(c.scan, ext); ok {
				got = scanCodes[i]
			}
			if got != want {
				t.Errorf("scan %#x extended=%v: got %q, want %q", c.scan, ext, got, want)
			}
		}
	}
}

func TestKeyIndexSynthetic(t *testing.T) {
	if _, ok := keyIndex(0x2A, true); ok {
		t.Error("fake Shift (E0 2A) is not dropped")
	}
	if i, ok := keyIndex(0x36, true); !ok || i != 0x36 {
		t.Errorf("E0 36 (CJK IME Right Shift) = %#x, %v; want 0x36", i, ok)
	}
	if i, ok := keyIndex(0x1D, true); !ok || i != extendedKey|0x1D {
		t.Errorf("E0 1D = %#x, %v", i, ok)
	}
}

func TestDecodeKeyLParam(t *testing.T) {
	for _, c := range []struct {
		lp   uintptr
		want keyLParam
	}{
		{0x001E0001, keyLParam{scan: 0x1E}},                                              // A down
		{0x401E0001, keyLParam{scan: 0x1E, repeat: true}},                                // A auto-repeat
		{0xC01E0001, keyLParam{scan: 0x1E, repeat: true, release: true}},                 // A up
		{0x011D0001, keyLParam{scan: 0x1D, extended: true}},                              // Right Ctrl down
		{0xC1480001, keyLParam{scan: 0x48, extended: true, repeat: true, release: true}}, // ArrowUp up
		{0x20380001, keyLParam{scan: 0x38}},                                              // Alt down, context code set
		{0x0000FFFF, keyLParam{}},                                                        // repeat count only
	} {
		if got := decodeKeyLParam(c.lp); got != c.want {
			t.Errorf("decodeKeyLParam(%#x) = %+v, want %+v", c.lp, got, c.want)
		}
	}
	if got, want := keyLP(0x48, true, false, true), uintptr(0xC1480001); got != want {
		t.Errorf("keyLP = %#x, want %#x", got, want)
	}
}
