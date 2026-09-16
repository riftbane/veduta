package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/riftbane/veduta/v2/sprite/internal/fontsrc"
)

func TestRunWritesGeneratedAtlas(t *testing.T) {
	in := filepath.Join("..", "..", "font8x8.txt")
	out := filepath.Join(t.TempDir(), "atlas.png")
	if err := run(in, out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	want, err := fontsrc.Generate(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("run output differs from fontsrc.Generate")
	}
}

func TestRunReportsSourceErrors(t *testing.T) {
	in := filepath.Join(t.TempDir(), "bad.txt")
	if err := os.WriteFile(in, []byte("0x20\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(in, filepath.Join(t.TempDir(), "x.png")); err == nil {
		t.Fatal("run accepted a truncated source")
	}
}
