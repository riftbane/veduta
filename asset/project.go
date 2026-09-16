package asset

import (
	"io/fs"
	"strconv"
	"strings"
	"unicode"

	"github.com/riftbane/veduta/gmath"
)

// ProjectFile is the file name of the project manifest at the project root.
const ProjectFile = "veduta.json"

// MaxResolution is the largest width or height accepted for resolution and
// inspect_resolution.
const MaxResolution = 8192

// Project is a compiled project manifest (veduta.json) with every default filled in.
type Project struct {
	Name              string     // project name, a valid asset name
	Title             string     // name shown to a player, defaults to Name
	Icon              string     // optional PNG at the project root, shown beside the title
	Engine            string     // engine version the project targets, "vX.Y.Z[-pre][+build]"
	Entry             string     // Go package of the game binary, "./cmd/game"
	Script            string     // the game's main Lua file, "main.lua"; set, the game is a script game and Entry is not used
	Resolution        [2]int     // the frame the game is designed for: default player and render size (width, height)
	InspectResolution [2]int     // default inspection image size
	TickRate          int        // simulation ticks per second
	DefaultScene      string     // scene used when a command names none
	DefaultWorld      string     // world used instead of DefaultScene when set (the player starts in it)
	DefaultSeed       uint64     // seed used when a command names none
	Assets            string     // asset sources directory, relative to the project root
	Cooked            string     // cooked .vda output directory, relative to the project root
	Invariants        []string   // canonical invariant specs checked in every run (ParseInvariant)
	Bounds            gmath.AABB // world bounds for within_bounds and inspection
}

// DefaultProject holds the defaults of every optional manifest field (spec §5.2).
var DefaultProject = Project{
	Entry:             "./cmd/game",
	Resolution:        [2]int{320, 240},
	InspectResolution: [2]int{320, 240},
	TickRate:          20,
	DefaultScene:      "main",
	DefaultSeed:       1,
	Assets:            "assets",
	Cooked:            "assets/.cooked",
	Bounds:            gmath.AABB{Min: gmath.V3(-100, -50, -100), Max: gmath.V3(100, 100, 100)},
}

// ParseProject decodes and validates the project manifest file (normally
// "veduta.json"). Absent fields — and fields holding their zero value — take the
// defaults of DefaultProject.
func ParseProject(file string, data []byte) (*Project, error) {
	var src ProjectSource
	loc, err := Decode(file, data, TypeProject, &src)
	if err != nil {
		return nil, err
	}
	return CompileProject(&src, loc)
}

// CompileProject validates src and returns the project with defaults filled in. Every
// problem is reported, located through loc (nil reports positions as unknown), in one
// Errors value.
func CompileProject(src *ProjectSource, loc *Locator) (*Project, error) {
	c := NewChecker(ensureLoc(loc))
	d := DefaultProject
	p := &Project{
		Name:         src.Name,
		Engine:       src.Engine,
		Entry:        orDefault(src.Entry, d.Entry),
		Script:       src.Script,
		TickRate:     c.Int("tick_rate", src.TickRate, 1, 1000, d.TickRate),
		DefaultScene: orDefault(src.DefaultScene, d.DefaultScene),
		DefaultWorld: src.DefaultWorld,
		DefaultSeed:  src.DefaultSeed,
		Assets:       orDefault(src.Assets, d.Assets),
		Cooked:       orDefault(src.Cooked, d.Cooked),
	}
	if p.DefaultSeed == 0 {
		p.DefaultSeed = d.DefaultSeed
	}
	if src.Name == "" {
		c.Errorf("name", "is required (the game's name, for example \"mygame\")")
	} else {
		c.Name("name", src.Name)
	}
	// A console shows the title and the icon; name is an identifier and cannot carry a
	// space or an accent, so a game that wants to be called something readable says so.
	p.Title = orDefault(src.Title, src.Name)
	if src.Title != "" {
		checkTitle(c, "title", src.Title)
	}
	if src.Icon != "" {
		p.Icon = src.Icon
		checkRelPath(c, "icon", src.Icon)
		if !strings.HasSuffix(src.Icon, ".png") {
			c.Errorf("icon", "%q must be a .png file", src.Icon)
		}
	}
	if src.Engine == "" {
		c.Errorf("engine", "is required (engine version, for example \"v0.1.0\")")
	} else if !validVersion(src.Engine) {
		c.Errorf("engine", "%q is not a version like v0.1.0 (vMAJOR.MINOR.PATCH, optional -prerelease and +build)", src.Engine)
	}
	if e := p.Entry; e != "." && !strings.HasPrefix(e, "./") {
		c.Errorf("entry", "%q must be a Go package path relative to the project root starting with \"./\", for example \"./cmd/game\"", e)
	} else if e != "." {
		checkRelPath(c, "entry", strings.TrimPrefix(e, "./"))
	}
	if src.Script != "" {
		checkRelPath(c, "script", src.Script)
		if !strings.HasSuffix(src.Script, ".lua") {
			c.Errorf("script", "%q must be a .lua file", src.Script)
		}
		if src.Entry != "" {
			c.Errorf("entry", "a script game (script %q) has no Go entry package; remove entry", src.Script)
		}
	}
	p.Resolution = resolution(c, "resolution", src.Resolution, d.Resolution)
	p.InspectResolution = resolution(c, "inspect_resolution", src.InspectResolution, d.InspectResolution)
	if src.DefaultScene != "" {
		c.Name("default_scene", src.DefaultScene)
	}
	if src.DefaultWorld != "" {
		c.Name("default_world", src.DefaultWorld)
	}
	checkRelPath(c, "assets", p.Assets)
	checkRelPath(c, "cooked", p.Cooked)
	if p.Assets == p.Cooked {
		c.Errorf("cooked", "must differ from assets (%q)", p.Assets)
	}
	p.Invariants = checkInvariants(c, "invariants", src.Invariants)
	p.Bounds = bounds(c, src.Bounds, d.Bounds)
	if err := c.Err(); err != nil {
		return nil, err
	}
	return p, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func resolution(c *Checker, path string, v []int, def [2]int) [2]int {
	if v == nil {
		return def
	}
	if len(v) != 2 {
		c.Errorf(path, "want [width, height], got %d numbers", len(v))
		return def
	}
	r := def
	for i, x := range v {
		if x < 1 || x > MaxResolution {
			c.Errorf(Path(path, i), "%d out of range [1, %d]", x, MaxResolution)
			continue
		}
		r[i] = x
	}
	return r
}

func bounds(c *Checker, v [][]float32, def gmath.AABB) gmath.AABB {
	if v == nil {
		return def
	}
	if len(v) != 2 {
		c.Errorf("bounds", "want [[min_x, min_y, min_z], [max_x, max_y, max_z]], got %d vectors", len(v))
		return def
	}
	before := len(c.Errs)
	b := gmath.AABB{Min: c.RequireVec3("bounds[0]", v[0]), Max: c.RequireVec3("bounds[1]", v[1])}
	if len(c.Errs) != before {
		return def
	}
	lo, hi := [3]float32{b.Min.X, b.Min.Y, b.Min.Z}, [3]float32{b.Max.X, b.Max.Y, b.Max.Z}
	for k := range 3 {
		if !(hi[k] > lo[k]) {
			c.Errorf(Path("bounds[1]", k), "max %s (%v) must be greater than min %s (%v)", "xyz"[k:k+1], hi[k], "xyz"[k:k+1], lo[k])
		}
	}
	if len(c.Errs) != before {
		return def
	}
	return b
}

// checkTitle reports a display title that is empty, too long, or holds a character that
// cannot be printed: a dashboard shows it as it stands, so a newline or a tab would break
// the tile it sits in.
func checkTitle(c *Checker, path, s string) {
	n := 0
	for _, r := range s {
		if !unicode.IsPrint(r) {
			c.Errorf(path, "%q contains a character that cannot be printed", s)
			return
		}
		n++
	}
	if n > 64 {
		c.Errorf(path, "%q is %d characters, want 1 to 64", s, n)
	}
}

// checkRelPath reports a directory path that is not a clean, slash-separated path
// inside the project root.
func checkRelPath(c *Checker, path, s string) {
	switch {
	case strings.Contains(s, `\`):
		c.Errorf(path, "%q: use forward slashes", s)
	case strings.HasPrefix(s, "/") || len(s) >= 2 && s[1] == ':':
		c.Errorf(path, "%q must be relative to the project root", s)
	case !fs.ValidPath(s) || s == ".":
		c.Errorf(path, "%q must be a clean relative path inside the project (no \"..\", \".\" or empty segments, no trailing slash)", s)
	}
}

// validVersion reports whether s is a semantic version "vMAJOR.MINOR.PATCH" with an
// optional "-prerelease" and "+build" suffix.
func validVersion(s string) bool {
	if !strings.HasPrefix(s, "v") {
		return false
	}
	s = s[1:]
	if i := strings.IndexByte(s, '+'); i >= 0 {
		if !validIdents(s[i+1:]) {
			return false
		}
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		if !validIdents(s[i+1:]) {
			return false
		}
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 1 && p[0] == '0' {
			return false
		}
		if _, err := strconv.ParseUint(p, 10, 32); err != nil {
			return false
		}
	}
	return true
}

// validIdents checks dot-separated non-empty identifiers of [0-9A-Za-z-].
func validIdents(s string) bool {
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		for _, r := range id {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
				return false
			}
		}
	}
	return true
}
