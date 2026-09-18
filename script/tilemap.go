package script

import (
	"sort"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/lua"
	"github.com/riftbane/veduta/v2/tilemap"
)

// tileMap returns the scene's tile map or raises an error naming fname.
func (g *Game) tileMap(vm *lua.VM, fname string) *tilemap.Map {
	g.scene(vm, fname)
	m := g.ctx.Map()
	if m == nil {
		vm.Errorf("%s: the scene has no map (give its file \"map\", or call map.load)", fname)
	}
	return m
}

// mapLayer reads the optional layer name at args[i]: its index, or def when absent.
func mapLayer(vm *lua.VM, m *tilemap.Map, args []lua.Value, i int, fname string, def int) int {
	if lua.Arg(args, i).IsNil() {
		return def
	}
	name := vm.CheckString(args, i, fname)
	l := m.Layer(name)
	if l < 0 {
		var names []string
		for _, ly := range m.Source().Layers {
			names = append(names, ly.Name)
		}
		vm.Errorf("%s: map %s has no layer %q (it has %v)", fname, m.Name(), name, names)
	}
	return l
}

// mapCell reads the cell (x, y) at args[i] and args[i+1].
func mapCell(vm *lua.VM, args []lua.Value, i int, fname string) (int, int) {
	return int(vm.CheckInt(args, i, fname)), int(vm.CheckInt(args, i+1, fname))
}

func stringList(list []string) lua.Value {
	t := lua.NewTable(len(list), 0)
	for i, s := range list {
		t.SetInt(int64(i+1), lua.String(s))
	}
	return lua.TableValue(t)
}

// mapObject returns a map object as a table: name, x, y, w, h, tags and props.
func mapObject(o *asset.MapObject) lua.Value {
	t := lua.NewTable(0, 7)
	t.SetString("name", lua.String(o.Name))
	t.SetString("x", lua.Int(int64(o.X)))
	t.SetString("y", lua.Int(int64(o.Y)))
	t.SetString("w", lua.Int(int64(o.W)))
	t.SetString("h", lua.Int(int64(o.H)))
	t.SetString("tags", stringList(o.Tags))
	props := lua.NewTable(0, len(o.Props))
	for _, p := range o.Props {
		var v lua.Value
		switch x := p.Value.(type) {
		case string:
			v = lua.String(x)
		case bool:
			v = lua.Bool(x)
		case float64:
			if i := int64(x); float64(i) == x {
				v = lua.Int(i)
			} else {
				v = lua.Float(x)
			}
		}
		props.SetString(p.Key, v)
	}
	t.SetString("props", lua.TableValue(props))
	return lua.TableValue(t)
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

func (g *Game) installMap() {
	g.lib("map", map[string]fn{
		"name": func(vm *lua.VM, args []lua.Value) []lua.Value {
			g.scene(vm, "map.name")
			if m := g.ctx.Map(); m != nil {
				return vm.Ret(lua.String(m.Name()))
			}
			return vm.Ret(lua.Nil)
		},
		"load": func(vm *lua.VM, args []lua.Value) []lua.Value {
			g.scene(vm, "map.load")
			name := ""
			if !lua.Arg(args, 0).IsNil() {
				name = vm.CheckString(args, 0, "map.load")
			}
			if err := g.ctx.LoadMap(name); err != nil {
				vm.Errorf("map.load: %s", err)
			}
			return nil
		},
		"size": func(vm *lua.VM, args []lua.Value) []lua.Value {
			w, h := g.tileMap(vm, "map.size").Size()
			return vm.Ret(lua.Int(int64(w)), lua.Int(int64(h)))
		},
		"tile": func(vm *lua.VM, args []lua.Value) []lua.Value {
			return vm.Ret(lua.Float(float64(g.tileMap(vm, "map.tile").Source().Tile)))
		},
		"layers": func(vm *lua.VM, args []lua.Value) []lua.Value {
			var names []string
			for _, l := range g.tileMap(vm, "map.layers").Source().Layers {
				names = append(names, l.Name)
			}
			return vm.Ret(stringList(names))
		},
		"cell": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := g.tileMap(vm, "map.cell")
			x, y := vm.CheckFloat(args, 0, "map.cell"), vm.CheckFloat(args, 1, "map.cell")
			cx, cy := m.CellAt(float32(x), float32(y))
			return vm.Ret(lua.Int(int64(cx)), lua.Int(int64(cy)))
		},
		"center": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := g.tileMap(vm, "map.center")
			x, y := m.Center(mapCell(vm, args, 0, "map.center"))
			return vm.Ret(lua.Float(float64(x)), lua.Float(float64(y)))
		},
		"inside": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := g.tileMap(vm, "map.inside")
			return vm.Ret(lua.Bool(m.Inside(mapCell(vm, args, 0, "map.inside"))))
		},
		"get": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := g.tileMap(vm, "map.get")
			x, y := mapCell(vm, args, 0, "map.get")
			if t := m.Get(mapLayer(vm, m, args, 2, "map.get", 0), x, y); t != nil {
				return vm.Ret(lua.String(t.Name))
			}
			return vm.Ret(lua.Nil)
		},
		"set": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := g.tileMap(vm, "map.set")
			x, y := mapCell(vm, args, 0, "map.set")
			terrain := ""
			if !lua.Arg(args, 2).IsNil() {
				terrain = vm.CheckString(args, 2, "map.set")
			}
			if err := m.Set(mapLayer(vm, m, args, 3, "map.set", 0), x, y, terrain); err != nil {
				vm.Errorf("map.set: %s", err)
			}
			return nil
		},
		"has": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := g.tileMap(vm, "map.has")
			x, y := mapCell(vm, args, 0, "map.has")
			tag := vm.CheckString(args, 2, "map.has")
			return vm.Ret(lua.Bool(m.Has(mapLayer(vm, m, args, 3, "map.has", -1), x, y, tag)))
		},
		"tags": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := g.tileMap(vm, "map.tags")
			x, y := mapCell(vm, args, 0, "map.tags")
			l := mapLayer(vm, m, args, 2, "map.tags", -1)
			var tags []string
			for i := range m.Source().Layers {
				if l >= 0 && i != l {
					continue
				}
				if t := m.Get(i, x, y); t != nil {
					for _, tag := range t.Tags {
						if !hasTag(tags, tag) {
							tags = append(tags, tag)
						}
					}
				}
			}
			sort.Strings(tags)
			return vm.Ret(stringList(tags))
		},
		"objects": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := g.tileMap(vm, "map.objects")
			tag := vm.OptString(args, 0, "map.objects", "")
			t := lua.NewTable(0, 0)
			n := int64(0)
			for i := range m.Source().Objects {
				o := &m.Source().Objects[i]
				if tag == "" || hasTag(o.Tags, tag) {
					n++
					t.SetInt(n, mapObject(o))
				}
			}
			return vm.Ret(lua.TableValue(t))
		},
		"object": func(vm *lua.VM, args []lua.Value) []lua.Value {
			m := g.tileMap(vm, "map.object")
			name := vm.CheckString(args, 0, "map.object")
			for i := range m.Source().Objects {
				if o := &m.Source().Objects[i]; o.Name == name {
					return vm.Ret(mapObject(o))
				}
			}
			return vm.Ret(lua.Nil)
		},
	})
}
