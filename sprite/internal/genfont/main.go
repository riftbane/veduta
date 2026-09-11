// Command genfont renders the built-in bitmap font source (font8x8.txt) into the atlas
// PNG embedded by package sprite (font8x8.png). It is run by `go generate ./sprite`, whose
// working directory is the sprite package directory.
//
// Usage:
//
//	go run ./internal/genfont [-in font8x8.txt] [-out font8x8.png]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/riftbane/veduta/sprite/internal/fontsrc"
)

func main() {
	in := flag.String("in", "font8x8.txt", "font source")
	out := flag.String("out", "font8x8.png", "atlas PNG to write")
	flag.Parse()
	if err := run(*in, *out); err != nil {
		fmt.Fprintln(os.Stderr, "genfont:", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	src, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	data, err := fontsrc.Generate(src)
	if err != nil {
		return fmt.Errorf("%s: %w", in, err)
	}
	return os.WriteFile(out, data, 0o644)
}
