// Command sitegen writes the documentation website: go run ./internal/site/sitegen -out _site.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/riftbane/veduta/v2/internal/site"
)

func main() {
	out := flag.String("out", "_site", "directory to write the site into")
	flag.Parse()
	if err := site.Build(*out); err != nil {
		fmt.Fprintln(os.Stderr, "sitegen:", err)
		os.Exit(1)
	}
}
