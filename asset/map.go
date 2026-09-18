package asset

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/riftbane/veduta/v2/gmath"
)

// Map is a compiled tile map (docs/map.md): layers of cells painted with terrains, and
// objects the game reads. Cell (x, y) counts columns to the right and rows down from the
// top-left cell (0, 0).
type Map struct {
	Name     string
	W, H     int     // columns and rows
	Tile     float32 // meters per cell
	Origin   gmath.Vec3
	Terrains []MapTerrain // cells hold index+1
	Layers   []MapLayer
	Objects  []MapObject // file order
}

// MapTerrain is what a map paints cells with.
type MapTerrain struct {
	Key      byte   // the character that stands for it in a layer's rows
	Name     string // what the game reads
	Texture  string // drawn with a material the map makes, or
	Material string // drawn with this material
	Tags     []string
}

// MapLayer is one layer of a map's cells.
type MapLayer struct {
	Name  string
	Z     float32 // added to the origin's z
	Layer int     // draw order, as an entity's
	Cells []uint8 // W×H, row by row from the top: 0 empty, else a terrain index + 1
}

// MapObject is a named rectangle of cells with tags and properties: a door, a spawn point,
// a zone.
type MapObject struct {
	Name  string
	X, Y  int // top-left cell
	W, H  int // cells
	Tags  []string
	Props []MapProp // by key
}

// MapProp is one property of a map object: a string, a number or a boolean.
type MapProp struct {
	Key   string
	Value any // string, float64 or bool
}

// Map limits (docs/map.md).
const (
	MaxMapSize     = 1024 // columns or rows
	MaxMapLayers   = 16
	MaxMapObjects  = 4096
	MaxMapTerrains = 92 // printable ASCII but space, '"' and '\'
)

// TerrainAt returns the terrain of cell (x, y) of layer l, or nil for an empty cell or a
// cell outside the map.
func (m *Map) TerrainAt(l, x, y int) *MapTerrain {
	if l < 0 || l >= len(m.Layers) || x < 0 || y < 0 || x >= m.W || y >= m.H {
		return nil
	}
	if v := m.Layers[l].Cells[y*m.W+x]; v > 0 {
		return &m.Terrains[v-1]
	}
	return nil
}

// Terrain returns the index of the terrain called name, or -1.
func (m *Map) Terrain(name string) int {
	for i := range m.Terrains {
		if m.Terrains[i].Name == name {
			return i
		}
	}
	return -1
}

// Layer returns the index of the layer called name, or -1.
func (m *Map) Layer(name string) int {
	for i := range m.Layers {
		if m.Layers[i].Name == name {
			return i
		}
	}
	return -1
}

// ParseMap decodes and compiles the map source file (for example
// "assets/maps/farm.vmap"). The map name is the file name without its ".vmap" suffix.
func ParseMap(file string, data []byte) (*Map, error) {
	var src MapSource
	loc, err := Decode(file, data, TypeMap, &src)
	if err != nil {
		return nil, err
	}
	name, err := nameFromFile(KindMap, file, loc)
	if err != nil {
		return nil, err
	}
	return CompileMap(name, &src, loc)
}

// validKey reports whether k may stand for a terrain in a layer's rows.
func validKey(k byte) bool { return k > ' ' && k < 0x7f && k != '"' && k != '\\' }

// CompileMap validates src and returns the compiled map called name. Every problem is
// reported, located through loc (nil reports positions as unknown), in one Errors value.
// Textures and materials are checked for syntax only.
func CompileMap(name string, src *MapSource, loc *Locator) (*Map, error) {
	c := NewChecker(ensureLoc(loc))
	if err := ValidName(name); err != nil {
		c.Errorf("", "map %v", err)
	}
	m := &Map{Name: name, W: 1, H: 1, Tile: c.Positive("tile", src.Tile, 1), Origin: c.Vec3("origin", src.Origin, gmath.Zero3)}
	switch {
	case src.Size == nil:
		c.Errorf("size", "is required ([columns, rows])")
	case len(src.Size) != 2:
		c.Errorf("size", "want [columns, rows], got %d numbers", len(src.Size))
	default:
		ok := true
		for i, v := range src.Size {
			if v < 1 || v > MaxMapSize {
				c.Errorf(Path("size", i), "%d out of range [1, %d]", v, MaxMapSize)
				ok = false
			}
		}
		if ok {
			m.W, m.H = src.Size[0], src.Size[1]
		}
	}
	keys := map[byte]int{} // key → terrain index + 1; lookup only
	names := map[string]int{}
	switch n := len(src.Terrains); {
	case n == 0:
		c.Errorf("terrains", "at least one terrain is required")
	case n > MaxMapTerrains:
		c.Errorf("terrains", "%d terrains, want at most %d", n, MaxMapTerrains)
	}
	for i, t := range src.Terrains {
		p := Path("terrains", i)
		mt := MapTerrain{Name: t.Name, Texture: t.Texture, Material: t.Material}
		switch {
		case len(t.Key) != 1 || !validKey(t.Key[0]):
			c.Errorf(Path(p, "key"), "must be one printable ASCII character but space, '\"' and '\\', got %q", t.Key)
		case keys[t.Key[0]] > 0:
			c.Errorf(Path(p, "key"), "duplicate key %q (first used by terrains[%d])", t.Key, keys[t.Key[0]]-1)
		default:
			mt.Key = t.Key[0]
			keys[mt.Key] = i + 1
		}
		if t.Name == "" {
			c.Errorf(Path(p, "name"), "is required")
		} else if c.Name(Path(p, "name"), t.Name) {
			if j, dup := names[t.Name]; dup {
				c.Errorf(Path(p, "name"), "duplicate terrain name %q (first used by terrains[%d])", t.Name, j)
			} else {
				names[t.Name] = i
			}
		}
		switch {
		case t.Texture == "" && t.Material == "":
			c.Errorf(p, "needs a texture or a material")
		case t.Texture != "" && t.Material != "":
			c.Errorf(Path(p, "material"), "not allowed with texture: a terrain is drawn with one or the other")
		case t.Texture != "":
			c.Name(Path(p, "texture"), t.Texture)
		default:
			c.Name(Path(p, "material"), t.Material)
		}
		mt.Tags = compileTags(c, Path(p, "tags"), t.Tags)
		m.Terrains = append(m.Terrains, mt)
	}
	switch n := len(src.Layers); {
	case n == 0:
		c.Errorf("layers", "at least one layer is required")
	case n > MaxMapLayers:
		c.Errorf("layers", "%d layers, want at most %d", n, MaxMapLayers)
	}
	layerNames := map[string]int{}
	for i, l := range src.Layers {
		p := Path("layers", i)
		ml := MapLayer{Name: l.Name, Z: float32(i) / 10, Layer: c.Int(Path(p, "layer"), l.Layer, MinLayer, MaxLayer, 0)}
		if l.Name == "" {
			c.Errorf(Path(p, "name"), "is required")
		} else if c.Name(Path(p, "name"), l.Name) {
			if j, dup := layerNames[l.Name]; dup {
				c.Errorf(Path(p, "name"), "duplicate layer name %q (first used by layers[%d])", l.Name, j)
			} else {
				layerNames[l.Name] = i
			}
		}
		if l.Z != nil {
			ml.Z = c.Float(Path(p, "z"), l.Z, -1e6, 1e6, ml.Z)
		}
		ml.Cells = make([]uint8, m.W*m.H)
		if len(l.Rows) != m.H {
			c.Errorf(Path(p, "rows"), "%d rows, want %d (the map's size)", len(l.Rows), m.H)
		}
		for y, row := range l.Rows {
			rp := Path(Path(p, "rows"), y)
			if y >= m.H {
				break
			}
			if len(row) != m.W {
				c.Errorf(rp, "%d characters, want %d (the map's size; a space is an empty cell)", len(row), m.W)
				continue
			}
			for x := 0; x < m.W; x++ {
				k := row[x]
				if k == ' ' {
					continue
				}
				if keys[k] == 0 {
					c.Errorf(rp, "character %q at column %d is not a terrain's key (a space is an empty cell)", string(rune(k)), x)
					break
				}
				ml.Cells[y*m.W+x] = uint8(keys[k])
			}
		}
		m.Layers = append(m.Layers, ml)
	}
	if len(src.Objects) > MaxMapObjects {
		c.Errorf("objects", "%d objects, want at most %d", len(src.Objects), MaxMapObjects)
	}
	objNames := map[string]int{}
	for i, o := range src.Objects {
		p := Path("objects", i)
		mo := MapObject{Name: o.Name, W: 1, H: 1}
		if o.Name == "" {
			c.Errorf(Path(p, "name"), "is required")
		} else if c.Name(Path(p, "name"), o.Name) {
			if j, dup := objNames[o.Name]; dup {
				c.Errorf(Path(p, "name"), "duplicate object name %q (first used by objects[%d])", o.Name, j)
			} else {
				objNames[o.Name] = i
			}
		}
		switch {
		case o.At == nil:
			c.Errorf(Path(p, "at"), "is required ([column, row] of its top-left cell)")
		case len(o.At) != 2:
			c.Errorf(Path(p, "at"), "want [column, row], got %d numbers", len(o.At))
		case o.At[0] < 0 || o.At[1] < 0 || o.At[0] >= m.W || o.At[1] >= m.H:
			c.Errorf(Path(p, "at"), "cell %v is outside the %d × %d map", o.At, m.W, m.H)
		default:
			mo.X, mo.Y = o.At[0], o.At[1]
		}
		if o.Size != nil {
			switch {
			case len(o.Size) != 2:
				c.Errorf(Path(p, "size"), "want [columns, rows], got %d numbers", len(o.Size))
			case o.Size[0] < 1 || o.Size[1] < 1 || mo.X+o.Size[0] > m.W || mo.Y+o.Size[1] > m.H:
				c.Errorf(Path(p, "size"), "%v cells from %v leave the %d × %d map (each at least 1)", o.Size, o.At, m.W, m.H)
			default:
				mo.W, mo.H = o.Size[0], o.Size[1]
			}
		}
		mo.Tags = compileTags(c, Path(p, "tags"), o.Tags)
		keys := make([]string, 0, len(o.Props))
		for k := range o.Props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			pp := Path(Path(p, "props"), k)
			if k == "" || len(k) > 64 || strings.ContainsAny(k, ". \t\n") {
				c.Errorf(pp, "a property name is 1-64 characters without spaces or '.', got %q", k)
				continue
			}
			switch v := o.Props[k].(type) {
			case string, bool:
				mo.Props = append(mo.Props, MapProp{Key: k, Value: v})
			case float64:
				if math.IsInf(v, 0) || math.IsNaN(v) {
					c.Errorf(pp, "not a finite number")
					continue
				}
				mo.Props = append(mo.Props, MapProp{Key: k, Value: v})
			default:
				c.Errorf(pp, "must be a string, a number or a boolean")
			}
		}
		m.Objects = append(m.Objects, mo)
	}
	if err := c.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// EncodeMap returns the TMAP chunk of m (docs/vda.md).
func EncodeMap(m *Map) Chunk {
	w := wbuf{b: make([]byte, 0, 64+len(m.Layers)*m.W*m.H)}
	w.str(m.Name)
	w.u32(uint32(m.W))
	w.u32(uint32(m.H))
	w.f32(m.Tile)
	w.vec3(m.Origin)
	w.count(len(m.Terrains))
	for _, t := range m.Terrains {
		w.u8(t.Key)
		w.str(t.Name)
		w.str(t.Texture)
		w.str(t.Material)
		w.strs(t.Tags)
	}
	w.count(len(m.Layers))
	for _, l := range m.Layers {
		w.str(l.Name)
		w.f32(l.Z)
		w.i64(l.Layer)
		w.b = append(w.b, l.Cells...)
	}
	w.count(len(m.Objects))
	for _, o := range m.Objects {
		w.str(o.Name)
		for _, v := range [4]int{o.X, o.Y, o.W, o.H} {
			w.u32(uint32(v))
		}
		w.strs(o.Tags)
		w.count(len(o.Props))
		for _, p := range o.Props {
			w.str(p.Key)
			switch v := p.Value.(type) {
			case string:
				w.u8(0)
				w.str(v)
			case float64:
				w.u8(1)
				w.b = binary64(w.b, v)
			case bool:
				w.u8(2)
				w.bool(v)
			default:
				panic(fmt.Sprintf("asset.EncodeMap: property %s is a %T", p.Key, p.Value))
			}
		}
	}
	return Chunk{Type: ChunkMap, Data: w.b}
}

func binary64(b []byte, v float64) []byte {
	u := math.Float64bits(v)
	for i := 0; i < 8; i++ {
		b = append(b, byte(u>>(8*i)))
	}
	return b
}

// DecodeMap parses a TMAP chunk.
func DecodeMap(c Chunk) (*Map, error) {
	r, err := newReader(c, ChunkMap)
	if err != nil {
		return nil, err
	}
	m := &Map{}
	r.field = "header"
	m.Name = r.str()
	m.W, m.H = int(r.u32()), int(r.u32())
	m.Tile = r.f32()
	m.Origin = r.vec3()
	if r.err == nil && (m.W < 1 || m.H < 1 || m.W > MaxMapSize || m.H > MaxMapSize || !(m.Tile > 0)) {
		r.failf("size %d × %d or tile %v out of range", m.W, m.H, m.Tile)
	}
	r.field = "terrains"
	n := r.count(1 + 4*4)
	for i := 0; i < n && r.err == nil; i++ {
		t := MapTerrain{Key: r.u8(), Name: r.str(), Texture: r.str(), Material: r.str(), Tags: r.strs()}
		if r.err == nil && (!validKey(t.Key) || (t.Texture == "") == (t.Material == "")) {
			r.failf("terrain %d: key %q or drawing out of range", i, t.Key)
		}
		m.Terrains = append(m.Terrains, t)
	}
	r.field = "layers"
	n = r.count(4 + 4 + 8 + m.W*m.H)
	for i := 0; i < n && r.err == nil; i++ {
		l := MapLayer{Name: r.str(), Z: r.f32(), Layer: r.i64()}
		if p := r.take(m.W * m.H); p != nil {
			l.Cells = append([]uint8(nil), p...)
		}
		for _, v := range l.Cells {
			if int(v) > len(m.Terrains) {
				r.failf("layer %q: cell %d names no terrain", l.Name, v)
				break
			}
		}
		m.Layers = append(m.Layers, l)
	}
	r.field = "objects"
	n = r.count(4 + 16 + 4 + 4)
	for i := 0; i < n && r.err == nil; i++ {
		o := MapObject{Name: r.str(), X: int(r.u32()), Y: int(r.u32()), W: int(r.u32()), H: int(r.u32()), Tags: r.strs()}
		if r.err == nil && (o.W < 1 || o.H < 1 || o.X+o.W > m.W || o.Y+o.H > m.H) {
			r.failf("object %q outside the map", o.Name)
		}
		np := r.count(4 + 1)
		for k := 0; k < np && r.err == nil; k++ {
			p := MapProp{Key: r.str()}
			switch t := r.u8(); t {
			case 0:
				p.Value = r.str()
			case 1:
				if b := r.take(8); b != nil {
					var u uint64
					for j := 7; j >= 0; j-- {
						u = u<<8 | uint64(b[j])
					}
					p.Value = math.Float64frombits(u)
				}
			case 2:
				p.Value = r.bool()
			default:
				r.failf("object %q: property %q has type %d", o.Name, p.Key, t)
			}
			o.Props = append(o.Props, p)
		}
		m.Objects = append(m.Objects, o)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return m, nil
}
