// Package docs embeds the references and guides in this directory. The MCP docs tool returns
// them verbatim to AI agents that author Veduta sources, and internal/site makes them the
// website people read (https://riftbane.github.io/veduta/).
package docs

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed *.md
var files embed.FS

// Topics are the documented topics (spec §11): the source formats and the game API.
var Topics = []string{"model", "texture", "material", "scene", "prefab", "world", "map", "scenario", "api", "lua"}

// Extra topics that are also available.
var Extra = []string{"project", "vda", "inspect", "config", "2d", "start-windows", "first-game"}

// Get returns the reference text of a topic.
func Get(topic string) (string, error) {
	data, err := files.ReadFile(topic + ".md")
	if err != nil {
		return "", fmt.Errorf("unknown docs topic %q (want one of %s)", topic, strings.Join(All(), ", "))
	}
	return string(data), nil
}

// All lists every available topic, sorted.
func All() []string {
	entries, _ := files.ReadDir(".")
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(out)
	return out
}
