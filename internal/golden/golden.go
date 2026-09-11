// Package golden compares images and text against committed golden files under
// testdata/golden at the module root.
//
// Golden files are rewritten only when the environment variable VEDUTA_UPDATE_GOLDEN is
// set to 1 (what `veduta test --update-golden` does) or when the test binary is run with
// -update-golden. A mismatch writes the actual output and a heat diff under
// out/golden-fail/ so the difference can be looked at.
package golden

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/riftbane/veduta/gfx"
)

var updateFlag = flag.Bool("update-golden", false, "rewrite golden files instead of comparing")

// Updating reports whether golden files should be rewritten.
func Updating() bool { return *updateFlag || os.Getenv("VEDUTA_UPDATE_GOLDEN") == "1" }

// Root returns the module root (the directory containing go.mod), searching upwards from
// the working directory.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("golden: go.mod not found above working directory")
		}
		dir = parent
	}
}

// Path returns the path of golden file name under testdata/golden.
func Path(t testing.TB, name string) string {
	t.Helper()
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "testdata", "golden", name)
}

// Image compares img with testdata/golden/<name>.png pixel by pixel.
func Image(t testing.TB, name string, img *gfx.Image) {
	t.Helper()
	path := Path(t, name+".png")
	if Updating() {
		var buf bytes.Buffer
		if err := img.EncodePNG(&buf); err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, buf.Bytes())
		t.Logf("golden: updated %s", path)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("golden: %v (run with VEDUTA_UPDATE_GOLDEN=1 to create it)", err)
	}
	defer f.Close()
	want, err := gfx.DecodePNG(f)
	if err != nil {
		t.Fatalf("golden: decode %s: %v", path, err)
	}
	if want.W != img.W || want.H != img.H {
		fail(t, name, img, nil)
		t.Fatalf("golden: %s is %dx%d, got %dx%d", name, want.W, want.H, img.W, img.H)
	}
	changed := 0
	for i := range want.Pix {
		if want.Pix[i] != img.Pix[i] {
			changed++
		}
	}
	if changed > 0 {
		fail(t, name, img, want)
		t.Fatalf("golden: %s differs in %d of %d pixels; actual and diff written under out/golden-fail/", name, changed, len(want.Pix))
	}
}

// Text compares data with testdata/golden/<name> byte for byte.
func Text(t testing.TB, name string, data []byte) {
	t.Helper()
	path := Path(t, name)
	if Updating() {
		writeFile(t, path, data)
		t.Logf("golden: updated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden: %v (run with VEDUTA_UPDATE_GOLDEN=1 to create it)", err)
	}
	if !bytes.Equal(want, data) {
		root, _ := Root()
		out := filepath.Join(root, "out", "golden-fail", name)
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		_ = os.WriteFile(out, data, 0o644)
		t.Fatalf("golden: %s differs (actual written to %s)", name, out)
	}
}

func writeFile(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// fail writes the actual image and, when want is known, a side-by-side heat diff.
func fail(t testing.TB, name string, got, want *gfx.Image) {
	root, err := Root()
	if err != nil {
		return
	}
	dir := filepath.Join(root, "out", "golden-fail")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	save := func(p string, m *gfx.Image) {
		var buf bytes.Buffer
		if m.EncodePNG(&buf) == nil {
			_ = os.WriteFile(p, buf.Bytes(), 0o644)
		}
	}
	save(filepath.Join(dir, name+".actual.png"), got)
	if want == nil || want.W != got.W || want.H != got.H {
		return
	}
	heat := gfx.NewImage(got.W, got.H)
	for i := range heat.Pix {
		d := maxDelta(got.Pix[i], want.Pix[i])
		if d == 0 {
			heat.Pix[i] = 0xff000000 | (got.Pix[i]>>2)&0x3f3f3f
		} else {
			heat.Pix[i] = 0xffff0000 | uint32(255-min(255, d*4))<<8
		}
	}
	save(filepath.Join(dir, name+".diff.png"), heat)
	t.Logf("golden: wrote %s", filepath.Join(dir, fmt.Sprintf("%s.{actual,diff}.png", name)))
}

func maxDelta(a, b uint32) uint32 {
	var d uint32
	for s := 0; s < 32; s += 8 {
		x, y := (a>>s)&0xff, (b>>s)&0xff
		if x > y {
			d = max(d, x-y)
		} else {
			d = max(d, y-x)
		}
	}
	return d
}
