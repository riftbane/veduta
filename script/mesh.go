package script

import (
	"sort"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/lua"
)

// Models a game builds at runtime (Context.SetModel), from Lua: a mesh builder that adds
// geometry in Go, and a volume of blocks whose visible faces become a mesh in one call.
// Building a chunk of blocks vertex by vertex in Lua would take most of a tick; these do the
// loops natively.

// maxVolumeCells bounds a volume: 128 × 128 × 256 blocks.
const maxVolumeCells = 1 << 22

// meshBuilder is the mesh a script is building.
type meshBuilder struct {
	verts     []gfx.Vertex
	indices   []uint32
	parts     []gfx.MeshPart
	materials []string
}

// part makes the next geometry part of the part drawn with material ("": the entity's).
func (m *meshBuilder) part(material string) {
	mi := -1
	for i, name := range m.materials {
		if name == material {
			mi = i
		}
	}
	if mi < 0 {
		mi = len(m.materials)
		m.materials = append(m.materials, material)
	}
	if n := len(m.parts); n > 0 && m.parts[n-1].Count == 0 {
		m.parts[n-1].Material = mi // a part left empty is replaced, not kept
		return
	}
	m.parts = append(m.parts, gfx.MeshPart{First: len(m.indices), Material: mi})
}

// quad adds the quad a, b, c, d (counter-clockwise seen from its front), with its normal
// and the UVs (0, 0), (1, 0), (1, 1), (0, 1).
func (m *meshBuilder) quad(a, b, c, d gmath.Vec3) {
	if len(m.parts) == 0 {
		m.part("")
	}
	n := b.Sub(a).Cross(c.Sub(a)).Normalize()
	base := uint32(len(m.verts))
	m.verts = append(m.verts,
		gfx.Vertex{Pos: a, Normal: n, UV: gmath.V2(0, 0)},
		gfx.Vertex{Pos: b, Normal: n, UV: gmath.V2(1, 0)},
		gfx.Vertex{Pos: c, Normal: n, UV: gmath.V2(1, 1)},
		gfx.Vertex{Pos: d, Normal: n, UV: gmath.V2(0, 1)})
	m.indices = append(m.indices, base, base+1, base+2, base, base+2, base+3)
	m.parts[len(m.parts)-1].Count += 6
}

func (m *meshBuilder) triangle(a, b, c gmath.Vec3) {
	if len(m.parts) == 0 {
		m.part("")
	}
	n := b.Sub(a).Cross(c.Sub(a)).Normalize()
	base := uint32(len(m.verts))
	m.verts = append(m.verts,
		gfx.Vertex{Pos: a, Normal: n, UV: gmath.V2(0, 0)},
		gfx.Vertex{Pos: b, Normal: n, UV: gmath.V2(1, 0)},
		gfx.Vertex{Pos: c, Normal: n, UV: gmath.V2(0, 1)})
	m.indices = append(m.indices, base, base+1, base+2)
	m.parts[len(m.parts)-1].Count += 3
}

// Faces of a box, as the face strings name them: +x, -x, +y, -y, +z, -z.
const (
	faceXPos = 1 << iota
	faceXNeg
	faceYPos
	faceYNeg
	faceZPos
	faceZNeg
	allFaces = 1<<6 - 1
)

// box adds the faces of the box from lo to hi.
func (m *meshBuilder) box(lo, hi gmath.Vec3, faces int) {
	x0, y0, z0, x1, y1, z1 := lo.X, lo.Y, lo.Z, hi.X, hi.Y, hi.Z
	v := gmath.V3
	if faces&faceXPos != 0 {
		m.quad(v(x1, y0, z1), v(x1, y0, z0), v(x1, y1, z0), v(x1, y1, z1))
	}
	if faces&faceXNeg != 0 {
		m.quad(v(x0, y0, z0), v(x0, y0, z1), v(x0, y1, z1), v(x0, y1, z0))
	}
	if faces&faceYPos != 0 {
		m.quad(v(x0, y1, z1), v(x1, y1, z1), v(x1, y1, z0), v(x0, y1, z0))
	}
	if faces&faceYNeg != 0 {
		m.quad(v(x0, y0, z0), v(x1, y0, z0), v(x1, y0, z1), v(x0, y0, z1))
	}
	if faces&faceZPos != 0 {
		m.quad(v(x0, y0, z1), v(x1, y0, z1), v(x1, y1, z1), v(x0, y1, z1))
	}
	if faces&faceZNeg != 0 {
		m.quad(v(x1, y0, z0), v(x0, y0, z0), v(x0, y1, z0), v(x1, y1, z0))
	}
}

// model returns a copy of the mesh as a model, with its bounds: the engine keeps the model
// it is given, and the script may go on changing the builder.
func (m *meshBuilder) model(name string) *asset.Model {
	md := gfx.MeshData{
		Vertices: append([]gfx.Vertex(nil), m.verts...),
		Indices:  append([]uint32(nil), m.indices...),
	}
	for _, p := range m.parts {
		if p.Count > 0 {
			md.Parts = append(md.Parts, p)
		}
	}
	if len(md.Vertices) > 0 {
		lo, hi := md.Vertices[0].Pos, md.Vertices[0].Pos
		for _, v := range md.Vertices[1:] {
			lo, hi = lo.Min(v.Pos), hi.Max(v.Pos)
		}
		md.Bounds = gmath.AABB{Min: lo, Max: hi}
	}
	return &asset.Model{Name: name, Mesh: md, Materials: append([]string(nil), m.materials...)}
}

// volume is a grid of block ids, 0 for empty.
type volume struct {
	sx, sy, sz int
	cells      []uint8
}

func (v *volume) at(x, y, z int) int { return (z*v.sy+y)*v.sx + x }

func (v *volume) inside(x, y, z int) bool {
	return x >= 0 && y >= 0 && z >= 0 && x < v.sx && y < v.sy && z < v.sz
}

// solid reports whether a cell holds a block; outside the volume is empty.
func (v *volume) solid(x, y, z int) bool {
	return v.inside(x, y, z) && v.cells[v.at(x, y, z)] != 0
}

// mesh adds to m the faces of every block that face an empty cell, one part per block id in
// increasing order drawn with materials[id], each block size units wide from the origin.
func (v *volume) mesh(m *meshBuilder, materials map[int]string, size float32) {
	var ids []int
	seen := [256]bool{}
	for _, c := range v.cells {
		if c != 0 && !seen[c] {
			seen[c] = true
			ids = append(ids, int(c))
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		m.part(materials[id])
		for z := 0; z < v.sz; z++ {
			for y := 0; y < v.sy; y++ {
				for x := 0; x < v.sx; x++ {
					if int(v.cells[v.at(x, y, z)]) != id {
						continue
					}
					faces := 0
					for bit, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
						if !v.solid(x+d[0], y+d[1], z+d[2]) {
							faces |= 1 << bit
						}
					}
					if faces == 0 {
						continue
					}
					// Products wrapped: a product feeding an addition would be fused on arm64.
					lo := gmath.V3(float32(float32(x)*size), float32(float32(y)*size), float32(float32(z)*size))
					m.box(lo, gmath.V3(lo.X+size, lo.Y+size, lo.Z+size), faces)
				}
			}
		}
	}
}

// parseFaces reads a face string such as "+y-y" (default all six).
func parseFaces(vm *lua.VM, s, fname string) int {
	if s == "" {
		return allFaces
	}
	faces := 0
	names := map[string]int{"+x": faceXPos, "-x": faceXNeg, "+y": faceYPos, "-y": faceYNeg, "+z": faceZPos, "-z": faceZNeg}
	for i := 0; i < len(s); i += 2 {
		f, ok := 0, false
		if i+2 <= len(s) {
			f, ok = names[s[i:i+2]]
		}
		if !ok {
			vm.Errorf("bad faces %q to '%s' (want +x, -x, +y, -y, +z, -z run together, as \"+y-y\")", s, fname)
		}
		faces |= f
	}
	return faces
}

func (g *Game) installMesh() {
	meshMeta := lua.NewTable(0, 4)
	volumeMeta := lua.NewTable(0, 4)
	meshMeta.SetString("__name", lua.String("mesh"))
	volumeMeta.SetString("__name", lua.String("volume"))
	builder := func(vm *lua.VM, args []lua.Value, fname string) *meshBuilder {
		if u := lua.Arg(args, 0).Userdata(); u != nil {
			if m, ok := u.Data.(*meshBuilder); ok {
				return m
			}
		}
		vm.ArgError(0, fname, "mesh expected")
		return nil
	}
	vol := func(vm *lua.VM, args []lua.Value, i int, fname string) *volume {
		if u := lua.Arg(args, i).Userdata(); u != nil {
			if v, ok := u.Data.(*volume); ok {
				return v
			}
		}
		vm.ArgError(i, fname, "volume expected")
		return nil
	}
	vec := func(vm *lua.VM, args []lua.Value, i int, fname string) gmath.Vec3 {
		return gmath.V3(float32(vm.CheckFloat(args, i, fname)), float32(vm.CheckFloat(args, i+1, fname)), float32(vm.CheckFloat(args, i+2, fname)))
	}
	cell := func(vm *lua.VM, v *volume, args []lua.Value, i int, fname string) (int, int, int) {
		x, y, z := int(vm.CheckInt(args, i, fname)), int(vm.CheckInt(args, i+1, fname)), int(vm.CheckInt(args, i+2, fname))
		if !v.inside(x, y, z) {
			vm.Errorf("%s: cell (%d, %d, %d) is outside the volume (0 to %d, %d, %d)", fname, x, y, z, v.sx-1, v.sy-1, v.sz-1)
		}
		return x, y, z
	}
	blockID := func(vm *lua.VM, args []lua.Value, i int, fname string) uint8 {
		id := vm.CheckInt(args, i, fname)
		if id < 0 || id > 255 {
			vm.ArgError(i, fname, "a block id is 0 (empty) to 255")
		}
		return uint8(id)
	}
	methods := func(meta *lua.Table, name string, funcs map[string]fn) {
		index := lua.NewTable(0, len(funcs))
		keys := make([]string, 0, len(funcs))
		for k := range funcs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			index.SetString(k, lua.FunctionValue(lua.NewFunction(name+":"+k, funcs[k])))
		}
		meta.SetString("__index", lua.TableValue(index))
	}
	methods(meshMeta, "mesh", map[string]fn{
		"part": func(vm *lua.VM, args []lua.Value) []lua.Value {
			builder(vm, args, "part").part(vm.OptString(args, 1, "part", ""))
			return nil
		},
		"quad": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := builder(vm, args, "quad")
			m.quad(vec(vm, args, 1, "quad"), vec(vm, args, 4, "quad"), vec(vm, args, 7, "quad"), vec(vm, args, 10, "quad"))
			return nil
		},
		"triangle": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := builder(vm, args, "triangle")
			m.triangle(vec(vm, args, 1, "triangle"), vec(vm, args, 4, "triangle"), vec(vm, args, 7, "triangle"))
			return nil
		},
		"box": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := builder(vm, args, "box")
			lo, size := vec(vm, args, 1, "box"), vec(vm, args, 4, "box")
			m.box(lo, lo.Add(size), parseFaces(vm, vm.OptString(args, 7, "box", ""), "box"))
			return nil
		},
		"triangles": func(vm *lua.VM, args []lua.Value) []lua.Value {
			return vm.Ret(lua.Int(int64(len(builder(vm, args, "triangles").indices) / 3)))
		},
	})
	methods(volumeMeta, "volume", map[string]fn{
		"get": func(vm *lua.VM, args []lua.Value) []lua.Value {
			v := vol(vm, args, 0, "get")
			x, y, z := cell(vm, v, args, 1, "get")
			return vm.Ret(lua.Int(int64(v.cells[v.at(x, y, z)])))
		},
		"set": func(vm *lua.VM, args []lua.Value) []lua.Value {
			v := vol(vm, args, 0, "set")
			x, y, z := cell(vm, v, args, 1, "set")
			v.cells[v.at(x, y, z)] = blockID(vm, args, 4, "set")
			return nil
		},
		"fill": func(vm *lua.VM, args []lua.Value) []lua.Value {
			v := vol(vm, args, 0, "fill")
			x0, y0, z0 := cell(vm, v, args, 1, "fill")
			x1, y1, z1 := cell(vm, v, args, 4, "fill")
			id := blockID(vm, args, 7, "fill")
			for z := min(z0, z1); z <= max(z0, z1); z++ {
				for y := min(y0, y1); y <= max(y0, y1); y++ {
					for x := min(x0, x1); x <= max(x0, x1); x++ {
						v.cells[v.at(x, y, z)] = id
					}
				}
			}
			return nil
		},
		"size": func(vm *lua.VM, args []lua.Value) []lua.Value {
			v := vol(vm, args, 0, "size")
			return vm.Ret(lua.Int(int64(v.sx)), lua.Int(int64(v.sy)), lua.Int(int64(v.sz)))
		},
	})
	g.lib("mesh", map[string]fn{
		"new": func(vm *lua.VM, args []lua.Value) []lua.Value {
			return vm.Ret(lua.UserdataValue(&lua.Userdata{Data: &meshBuilder{}, Meta: meshMeta}))
		},
		"set": func(vm *lua.VM, args []lua.Value) []lua.Value {
			name := vm.CheckString(args, 0, "set")
			var m *meshBuilder
			if u := lua.Arg(args, 1).Userdata(); u != nil {
				m, _ = u.Data.(*meshBuilder)
			}
			if m == nil {
				vm.ArgError(1, "set", "mesh expected")
			}
			if err := g.ctx.SetModel(name, m.model(name)); err != nil {
				vm.Errorf("%s", strings.TrimPrefix(err.Error(), "veduta: SetModel "))
			}
			return nil
		},
		"remove": func(vm *lua.VM, args []lua.Value) []lua.Value {
			g.ctx.RemoveModel(vm.CheckString(args, 0, "remove"))
			return nil
		},
		"voxels": func(vm *lua.VM, args []lua.Value) []lua.Value {
			v := vol(vm, args, 0, "voxels")
			materials := map[int]string{}
			if t := lua.Arg(args, 1); !t.IsNil() {
				vm.CheckTable(args, 1, "voxels").ForEach(func(k, val lua.Value) bool {
					id, ok := k.Int()
					name, isStr := val.Str()
					if !ok || !isStr || id < 1 || id > 255 {
						vm.ArgError(1, "voxels", "table of block id (1 to 255) → material name expected")
					}
					materials[int(id)] = name
					return true
				})
			}
			size := float32(1)
			if !lua.Arg(args, 2).IsNil() {
				size = float32(vm.CheckFloat(args, 2, "voxels"))
				if size <= 0 {
					vm.ArgError(2, "voxels", "a block's size must be above 0")
				}
			}
			m := &meshBuilder{}
			v.mesh(m, materials, size)
			return vm.Ret(lua.UserdataValue(&lua.Userdata{Data: m, Meta: meshMeta}))
		},
	})
	g.lib("volume", map[string]fn{
		"new": func(vm *lua.VM, args []lua.Value) []lua.Value {
			sx, sy, sz := vm.CheckInt(args, 0, "new"), vm.CheckInt(args, 1, "new"), vm.CheckInt(args, 2, "new")
			if sx < 1 || sy < 1 || sz < 1 || sx > maxVolumeCells || sy > maxVolumeCells || sz > maxVolumeCells || sx*sy*sz > maxVolumeCells {
				vm.Errorf("volume.new: %d × %d × %d blocks (each side at least 1, at most %d blocks in all)", sx, sy, sz, maxVolumeCells)
			}
			v := &volume{sx: int(sx), sy: int(sy), sz: int(sz), cells: make([]uint8, sx*sy*sz)}
			return vm.Ret(lua.UserdataValue(&lua.Userdata{Data: v, Meta: volumeMeta}))
		},
	})
}
