package script

import (
	"fmt"
	"image"
	"math"
	"sort"
	"strings"

	"github.com/riftbane/veduta/v2"
	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/lua"
	"github.com/riftbane/veduta/v2/scene"
	"github.com/riftbane/veduta/v2/sim"
	"github.com/riftbane/veduta/v2/sprite"
)

// The engine's API for scripts: input, scene, entities, camera, world, hud, trace,
// invariant and require. docs/lua.md describes it; keep the two in step.

type fn = lua.GoFunction

// lib sets a global table of functions, in name order so pairs visits it the same way on
// every run.
func (g *Game) lib(name string, funcs map[string]fn) *lua.Table {
	t := lua.NewTable(0, len(funcs))
	keys := make([]string, 0, len(funcs))
	for k := range funcs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.SetString(k, lua.FunctionValue(lua.NewFunction(name+"."+k, funcs[k])))
	}
	g.vm.SetGlobal(name, lua.TableValue(t))
	return t
}

func (g *Game) global(name string, f fn) {
	g.vm.SetGlobal(name, lua.FunctionValue(lua.NewFunction(name, f)))
}

func (g *Game) install() {
	g.installEntity()
	g.installMesh()
	g.installSave()
	g.global("require", func(vm *lua.VM, args []lua.Value) []lua.Value {
		name := vm.CheckString(args, 0, "require")
		v, err := g.require(g.moduleFile(name))
		if err != nil {
			if le, ok := err.(*lua.Error); ok {
				panic(le)
			}
			vm.Errorf("%s", err)
		}
		return vm.Ret(v)
	})
	g.global("trace", func(vm *lua.VM, args []lua.Value) []lua.Value {
		name := vm.CheckString(args, 0, "trace")
		fields := map[string]any{}
		if t := lua.Arg(args, 1); !t.IsNil() {
			tb := vm.CheckTable(args, 1, "trace")
			if m, ok := toGo(lua.TableValue(tb), 0).(map[string]any); ok {
				fields = m
			} else {
				vm.ArgError(1, "trace", "table with string keys expected")
			}
		}
		g.ctx.Trace(name, fields)
		return nil
	})
	g.global("invariant", func(vm *lua.VM, args []lua.Value) []lua.Value {
		name := vm.CheckString(args, 0, "invariant")
		pred := vm.CheckFunction(args, 1, "invariant")
		if err := asset.ValidName(name); err != nil {
			vm.ArgError(0, "invariant", err.Error())
		}
		g.ctx.Invariant(name, func() bool {
			if g.err != nil {
				return true
			}
			res, err := g.vm.Call(pred)
			if err != nil {
				g.err = err
				return true
			}
			return len(res) > 0 && res[0].Truthy()
		})
		return nil
	})
	g.lib("input", map[string]fn{
		"down":     g.button(func(b sim.Button) bool { return g.in.Down(b) }),
		"pressed":  g.button(func(b sim.Button) bool { return g.in.JustPressed(b) }),
		"released": g.button(func(b sim.Button) bool { return g.in.JustReleased(b) }),
		"dpad": func(vm *lua.VM, args []lua.Value) []lua.Value {
			d := g.in.DPad()
			return vm.Ret(lua.Int(int64(d.X)), lua.Int(int64(d.Y)))
		},
	})
	g.lib("scene", map[string]fn{
		"name": func(vm *lua.VM, args []lua.Value) []lua.Value {
			return vm.Ret(lua.String(g.scene(vm, "scene.name").Name))
		},
		"find": func(vm *lua.VM, args []lua.Value) []lua.Value {
			return vm.Ret(g.entity(g.scene(vm, "scene.find").Find(vm.CheckString(args, 0, "find"))))
		},
		"tagged": func(vm *lua.VM, args []lua.Value) []lua.Value {
			return vm.Ret(g.list(g.scene(vm, "scene.tagged").Tagged(vm.CheckString(args, 0, "tagged"))))
		},
		"entities": func(vm *lua.VM, args []lua.Value) []lua.Value {
			var live []*scene.Entity
			for _, e := range g.scene(vm, "scene.entities").Entities() {
				if e.Alive() {
					live = append(live, e)
				}
			}
			return vm.Ret(g.list(live))
		},
		"spawn":        g.spawn,
		"spawn_prefab": g.spawnPrefab,
		"load": func(vm *lua.VM, args []lua.Value) []lua.Value {
			g.scene(vm, "scene.load")
			if err := g.ctx.LoadScene(vm.CheckString(args, 0, "load")); err != nil {
				vm.Errorf("%s", err)
			}
			return nil
		},
	})
	g.lib("camera", map[string]fn{
		"get": func(vm *lua.VM, args []lua.Value) []lua.Value {
			c := g.scene(vm, "camera.get").Camera
			t := lua.NewTable(0, 7)
			t.SetString("position", vec3(c.Position))
			t.SetString("target", vec3(c.Target))
			t.SetString("ortho", lua.Bool(c.Ortho))
			t.SetString("fov", lua.Float(float64(c.FovDeg)))
			t.SetString("size", lua.Float(float64(c.Size)))
			t.SetString("near", lua.Float(float64(c.Near)))
			t.SetString("far", lua.Float(float64(c.Far)))
			return vm.Ret(lua.TableValue(t))
		},
		"set": func(vm *lua.VM, args []lua.Value) []lua.Value {
			s := g.scene(vm, "camera.set")
			t := vm.CheckTable(args, 0, "set")
			c := s.Camera
			if v := t.GetString("position"); !v.IsNil() {
				c.Position = toVec3(vm, v, "position")
			}
			if v := t.GetString("target"); !v.IsNil() {
				c.Target = toVec3(vm, v, "target")
			}
			if v := t.GetString("ortho"); !v.IsNil() {
				c.Ortho = v.Truthy()
			}
			for _, field := range []struct {
				key string
				dst *float32
			}{{"fov", &c.FovDeg}, {"size", &c.Size}, {"near", &c.Near}, {"far", &c.Far}} {
				if v := t.GetString(field.key); !v.IsNil() {
					f, ok := v.Float()
					if !ok {
						vm.Errorf("camera.set: %s must be a number", field.key)
					}
					*field.dst = float32(f)
				}
			}
			s.Camera = c
			return nil
		},
		"follow2d": func(vm *lua.VM, args []lua.Value) []lua.Value {
			s := g.scene(vm, "camera.follow2d")
			x, y := vm.CheckFloat(args, 0, "follow2d"), vm.CheckFloat(args, 1, "follow2d")
			h := vm.CheckFloat(args, 2, "follow2d")
			s.Camera = scene.Camera2D(gmath.V2(float32(x), float32(y)), float32(h))
			return nil
		},
	})
	g.lib("world", map[string]fn{
		"load": func(vm *lua.VM, args []lua.Value) []lua.Value {
			name := vm.CheckString(args, 0, "load")
			at := [2]int32{int32(vm.OptInt(args, 1, "load", 0)), int32(vm.OptInt(args, 2, "load", 0))}
			if err := g.ctx.LoadWorld(name, at); err != nil {
				vm.Errorf("%s", err)
			}
			return nil
		},
		"name": func(vm *lua.VM, args []lua.Value) []lua.Value {
			if w := g.ctx.World(); w != nil {
				return vm.Ret(lua.String(w.Name))
			}
			return vm.Ret(lua.Nil)
		},
		"focus": func(vm *lua.VM, args []lua.Value) []lua.Value {
			g.world(vm, "world.focus").Focus(g.point(vm, args, 0, "focus"))
			return nil
		},
		"height": func(vm *lua.VM, args []lua.Value) []lua.Value {
			w := g.world(vm, "world.height")
			x, z := vm.CheckFloat(args, 0, "height"), vm.CheckFloat(args, 1, "height")
			return vm.Ret(lua.Float(float64(w.HeightAt(gmath.V3(float32(x), 0, float32(z))))))
		},
		"water": func(vm *lua.VM, args []lua.Value) []lua.Value {
			w := g.world(vm, "world.water")
			x, z := vm.CheckFloat(args, 0, "water"), vm.CheckFloat(args, 1, "water")
			level, ok := w.WaterAt(gmath.V3(float32(x), 0, float32(z)))
			if !ok {
				return vm.Ret(lua.Nil)
			}
			return vm.Ret(lua.Float(float64(level)))
		},
	})
	g.lib("hud", map[string]fn{
		"text": func(vm *lua.VM, args []lua.Value) []lua.Value {
			b := g.batch(vm, "hud.text")
			x, y := vm.CheckFloat(args, 0, "text"), vm.CheckFloat(args, 1, "text")
			s := vm.ToString(vm.CheckAny(args, 2, "text"))
			color := g.color(vm, args, 3, "text", 0xffffffff)
			scale := vm.OptInt(args, 4, "text", 1)
			w := g.ctx.Text(b, float32(x), float32(y), int(scale), s, color)
			return vm.Ret(lua.Float(float64(w)))
		},
		"text_width": func(vm *lua.VM, args []lua.Value) []lua.Value {
			s := vm.ToString(vm.CheckAny(args, 0, "hud.text_width"))
			w, _ := sprite.MeasureText(g.ctx.Font, s, int(vm.OptInt(args, 1, "hud.text_width", 1)))
			return vm.Ret(lua.Int(int64(w)))
		},
		"wrap": func(vm *lua.VM, args []lua.Value) []lua.Value {
			s := vm.ToString(vm.CheckAny(args, 0, "hud.wrap"))
			width := vm.CheckInt(args, 1, "hud.wrap")
			scale := vm.OptInt(args, 2, "hud.wrap", 1)
			wrapped := sprite.Wrap(g.ctx.Font, s, int(width), int(scale))
			return vm.Ret(lua.String(wrapped), lua.Int(int64(strings.Count(wrapped, "\n")+1)))
		},
		"image":      g.hudImage,
		"panel":      g.hudPanel,
		"image_size": g.hudImageSize,
		"rect": func(vm *lua.VM, args []lua.Value) []lua.Value {
			b := g.batch(vm, "hud.rect")
			x, y := vm.CheckFloat(args, 0, "rect"), vm.CheckFloat(args, 1, "rect")
			w, h := vm.CheckFloat(args, 2, "rect"), vm.CheckFloat(args, 3, "rect")
			b.Rect(gmath.R(float32(x), float32(y), float32(w), float32(h)), g.color(vm, args, 4, "rect", 0xffffffff))
			return nil
		},
	})
}

// texture looks up a texture for the hud by its asset name.
func (g *Game) texture(vm *lua.VM, args []lua.Value, i int, fname string) (gfx.TextureID, int, int) {
	name := vm.CheckString(args, i, fname)
	tex, w, h, ok := g.ctx.Texture(name)
	if !ok {
		vm.Errorf("%s: no texture %q (assets/textures/%s.tex.json)", fname, name, name)
	}
	return tex, w, h
}

// srcRect reads options.src, {x, y, w, h} in texels, defaulting to the whole texture.
func srcRect(vm *lua.VM, opts *lua.Table, w, h int, fname string) image.Rectangle {
	if opts == nil || opts.GetString("src").IsNil() {
		return image.Rect(0, 0, w, h)
	}
	t := opts.GetString("src").Table()
	var r [4]int
	for i := range r {
		n := lua.Nil
		if t != nil {
			n = t.GetInt(int64(i) + 1)
		}
		v, ok := n.Int()
		if !ok {
			vm.Errorf("%s: src must be {x, y, w, h} in whole texels", fname)
		}
		r[i] = int(v)
	}
	src := image.Rect(r[0], r[1], r[0]+r[2], r[1]+r[3])
	if r[2] <= 0 || r[3] <= 0 || !src.In(image.Rect(0, 0, w, h)) {
		vm.Errorf("%s: src {%d, %d, %d, %d} is not inside the %d×%d texture", fname, r[0], r[1], r[2], r[3], w, h)
	}
	return src
}

// options reads an optional table argument.
func options(vm *lua.VM, args []lua.Value, i int, fname string) *lua.Table {
	if lua.Arg(args, i).IsNil() {
		return nil
	}
	return vm.CheckTable(args, i, fname)
}

// optNumber reads a number field of an options table.
func optNumber(vm *lua.VM, opts *lua.Table, key, fname string, def float64) float64 {
	if opts == nil {
		return def
	}
	v := opts.GetString(key)
	if v.IsNil() {
		return def
	}
	f, ok := v.Float()
	if !ok {
		vm.Errorf("%s: %s must be a number", fname, key)
	}
	return f
}

// optColor reads the color field of an options table.
func (g *Game) optColor(vm *lua.VM, opts *lua.Table, fname string) uint32 {
	if opts == nil {
		return 0xffffffff
	}
	return g.color(vm, []lua.Value{opts.GetString("color")}, 0, fname, 0xffffffff)
}

// hudImage is hud.image(texture, x, y [, {src = {x, y, w, h}, w =, h =, color =, flip_x =,
// flip_y =}]).
func (g *Game) hudImage(vm *lua.VM, args []lua.Value) []lua.Value {
	b := g.batch(vm, "hud.image")
	tex, tw, th := g.texture(vm, args, 0, "hud.image")
	x, y := vm.CheckFloat(args, 1, "hud.image"), vm.CheckFloat(args, 2, "hud.image")
	opts := options(vm, args, 3, "hud.image")
	src := srcRect(vm, opts, tw, th, "hud.image")
	w := optNumber(vm, opts, "w", "hud.image", float64(src.Dx()))
	h := optNumber(vm, opts, "h", "hud.image", float64(src.Dy()))
	dst := gmath.R(float32(x), float32(y), float32(w), float32(h))
	if opts != nil && opts.GetString("flip_x").Truthy() {
		dst.Min.X, dst.Max.X = dst.Max.X, dst.Min.X
	}
	if opts != nil && opts.GetString("flip_y").Truthy() {
		dst.Min.Y, dst.Max.Y = dst.Max.Y, dst.Min.Y
	}
	b.Image(tex, tw, th, src, dst, g.optColor(vm, opts, "hud.image"), gfx.FilterNearest)
	return nil
}

// hudPanel is hud.panel(texture, x, y, w, h, border [, {src =, color =}]): a nine-slice
// panel whose border is one width for every side or {left, top, right, bottom}.
func (g *Game) hudPanel(vm *lua.VM, args []lua.Value) []lua.Value {
	b := g.batch(vm, "hud.panel")
	tex, tw, th := g.texture(vm, args, 0, "hud.panel")
	x, y := vm.CheckFloat(args, 1, "hud.panel"), vm.CheckFloat(args, 2, "hud.panel")
	w, h := vm.CheckFloat(args, 3, "hud.panel"), vm.CheckFloat(args, 4, "hud.panel")
	var inset [4]int
	if t := lua.Arg(args, 5).Table(); t != nil {
		for i := range inset {
			n, ok := t.GetInt(int64(i) + 1).Int()
			if !ok {
				vm.ArgError(5, "hud.panel", "border must be a whole number or {left, top, right, bottom}")
			}
			inset[i] = int(n)
		}
	} else {
		n := vm.CheckInt(args, 5, "hud.panel")
		inset = [4]int{int(n), int(n), int(n), int(n)}
	}
	opts := options(vm, args, 6, "hud.panel")
	src := srcRect(vm, opts, tw, th, "hud.panel")
	b.NineSlice(tex, tw, th, src, inset, gmath.R(float32(x), float32(y), float32(w), float32(h)), g.optColor(vm, opts, "hud.panel"), gfx.FilterNearest)
	return nil
}

// hudImageSize is hud.image_size(texture): its width and height in texels.
func (g *Game) hudImageSize(vm *lua.VM, args []lua.Value) []lua.Value {
	g.batch(vm, "hud.image_size")
	_, w, h := g.texture(vm, args, 0, "hud.image_size")
	return vm.Ret(lua.Int(int64(w)), lua.Int(int64(h)))
}

// scene returns the scene, which a script cannot reach before the first one is loaded.
func (g *Game) scene(vm *lua.VM, fname string) *scene.Scene {
	if g.ctx.Scene == nil {
		vm.Errorf("%s: no scene yet (the main script runs before the scene is loaded; use game.init)", fname)
	}
	return g.ctx.Scene
}

func (g *Game) world(vm *lua.VM, fname string) *veduta.World {
	w := g.ctx.World()
	if w == nil {
		vm.Errorf("%s: no world is loaded", fname)
	}
	return w
}

func (g *Game) batch(vm *lua.VM, fname string) *sprite.Batch {
	if g.hud == nil {
		vm.Errorf("%s: the hud can be drawn only in game.draw", fname)
	}
	return g.hud
}

// button returns an input function over a button named by its first argument.
func (g *Game) button(test func(sim.Button) bool) fn {
	return func(vm *lua.VM, args []lua.Value) []lua.Value {
		name := vm.CheckString(args, 0, "button")
		b, err := sim.ParseButton(name)
		if err != nil {
			vm.ArgError(0, "input", err.Error())
		}
		return vm.Ret(lua.Bool(test(b)))
	}
}

// color reads argument i as "#rrggbb", "#rrggbbaa" or an integer 0xrrggbb.
func (g *Game) color(vm *lua.VM, args []lua.Value, i int, fname string, def uint32) uint32 {
	v := lua.Arg(args, i)
	if v.IsNil() {
		return def
	}
	if s, ok := v.Str(); ok {
		c, err := gfx.ParseColor(s)
		if err != nil {
			vm.ArgError(i, fname, err.Error())
		}
		return c
	}
	n := vm.CheckInt(args, i, fname)
	if n < 0 || n > 0xffffff {
		vm.ArgError(i, fname, "an integer color is 0xrrggbb")
	}
	return 0xff000000 | uint32(n)
}

// point reads x, y, z from arguments i…i+2.
func (g *Game) point(vm *lua.VM, args []lua.Value, i int, fname string) gmath.Vec3 {
	return gmath.V3(float32(vm.CheckFloat(args, i, fname)), float32(vm.CheckFloat(args, i+1, fname)), float32(vm.CheckFloat(args, i+2, fname)))
}

func (g *Game) list(ents []*scene.Entity) lua.Value {
	t := lua.NewTable(len(ents), 0)
	for _, e := range ents {
		t.Append(g.entity(e))
	}
	return lua.TableValue(t)
}

func vec3(v gmath.Vec3) lua.Value {
	t := lua.NewTable(3, 0)
	t.Append(lua.Float(float64(v.X)))
	t.Append(lua.Float(float64(v.Y)))
	t.Append(lua.Float(float64(v.Z)))
	return lua.TableValue(t)
}

func toVec3(vm *lua.VM, v lua.Value, what string) gmath.Vec3 {
	t := v.Table()
	var out [3]float32
	for i := range out {
		var f float64
		ok := t != nil
		if ok {
			f, ok = t.GetInt(int64(i) + 1).Float()
		}
		if !ok {
			vm.Errorf("%s must be {x, y, z}", what)
		}
		out[i] = float32(f)
	}
	return gmath.V3(out[0], out[1], out[2])
}

// toEntity reads an entity value, or the name of an entity of the scene.
func (g *Game) toEntity(vm *lua.VM, v lua.Value, what string) *scene.Entity {
	if u := v.Userdata(); u != nil {
		if e, ok := u.Data.(*scene.Entity); ok {
			return e
		}
	}
	if name, ok := v.Str(); ok {
		if e := g.scene(vm, what).Find(name); e != nil {
			return e
		}
		vm.Errorf("%s: no entity named %q", what, name)
	}
	vm.Errorf("%s must be an entity or an entity's name", what)
	return nil
}

// toBox reads {{min x, y, z}, {max x, y, z}}.
func toBox(vm *lua.VM, v lua.Value, what string) *gmath.AABB {
	t := v.Table()
	if t == nil || t.Len() != 2 {
		vm.Errorf("%s must be {{min x, y, z}, {max x, y, z}}", what)
	}
	b := gmath.AABB{Min: toVec3(vm, t.GetInt(1), what+" min"), Max: toVec3(vm, t.GetInt(2), what+" max")}
	if b.Min.X > b.Max.X || b.Min.Y > b.Max.Y || b.Min.Z > b.Max.Z {
		vm.Errorf("%s: min must not exceed max on any axis", what)
	}
	return &b
}

func boxValue(b *gmath.AABB) lua.Value {
	if b == nil {
		return lua.Nil
	}
	t := lua.NewTable(2, 0)
	t.Append(vec3(b.Min))
	t.Append(vec3(b.Max))
	return lua.TableValue(t)
}

// spawnPrefab is scene.spawn_prefab(name, x, y, z [, rotation [, prefix]]): the prefab's
// entities in a table that lists them in prefab order and also holds each under its name in
// the prefab.
func (g *Game) spawnPrefab(vm *lua.VM, args []lua.Value) []lua.Value {
	g.scene(vm, "scene.spawn_prefab")
	name := vm.CheckString(args, 0, "spawn_prefab")
	origin := g.point(vm, args, 1, "spawn_prefab")
	rotation := vm.OptInt(args, 4, "spawn_prefab", 0)
	prefix := vm.OptString(args, 5, "spawn_prefab", "")
	ents, err := g.ctx.SpawnPrefab(name, origin, int(rotation), prefix)
	if err != nil {
		vm.Errorf("%s", err)
	}
	if prefix == "" {
		prefix = name
	}
	t := lua.NewTable(len(ents), len(ents))
	for _, e := range ents {
		v := g.entity(e)
		t.Append(v)
		// The entity's name in the prefab: its scene name without the prefix, and without
		// the "#<id>" a name already taken gets.
		key := strings.TrimPrefix(e.Name, prefix+"_")
		if i := strings.LastIndexByte(key, '#'); i >= 0 {
			key = key[:i]
		}
		t.SetString(key, v)
	}
	return vm.Ret(lua.TableValue(t))
}

// spawn is scene.spawn{kind=, name=, model=, material=, position=, rotation=, scale=, tags=,
// visible=, layer=, parent=, hitbox=, state=}.
func (g *Game) spawn(vm *lua.VM, args []lua.Value) []lua.Value {
	s := g.scene(vm, "scene.spawn")
	t := vm.CheckTable(args, 0, "spawn")
	e := scene.Entity{Visible: true, Transform: scene.Identity()}
	str := func(key string) string {
		v := t.GetString(key)
		if v.IsNil() {
			return ""
		}
		str, ok := v.Str()
		if !ok {
			vm.Errorf("scene.spawn: %s must be a string", key)
		}
		return str
	}
	e.Kind, e.Name, e.Model, e.Material = str("kind"), str("name"), str("model"), str("material")
	if e.Kind == "" {
		e.Kind = scene.KindStatic
	}
	if v := t.GetString("position"); !v.IsNil() {
		e.Transform.Position = toVec3(vm, v, "position")
	}
	if v := t.GetString("rotation"); !v.IsNil() {
		e.Transform.Rotation = gmath.QuatEulerDeg(toVec3(vm, v, "rotation"))
	}
	if v := t.GetString("scale"); !v.IsNil() {
		e.Transform.Scale = toVec3(vm, v, "scale")
	}
	if v := t.GetString("visible"); !v.IsNil() {
		e.Visible = v.Truthy()
	}
	if v := t.GetString("layer"); !v.IsNil() {
		n, ok := v.Int()
		if !ok || n < math.MinInt32 || n > math.MaxInt32 {
			vm.Errorf("scene.spawn: layer must be an integer")
		}
		e.Layer = int(n)
	}
	if v := t.GetString("tags"); !v.IsNil() {
		tags := v.Table()
		if tags == nil {
			vm.Errorf("scene.spawn: tags must be a list of strings")
		}
		for i := int64(1); i <= int64(tags.Len()); i++ {
			tag, ok := tags.GetInt(i).Str()
			if !ok {
				vm.Errorf("scene.spawn: tags must be a list of strings")
			}
			e.Tags = append(e.Tags, tag)
		}
	}
	if v := t.GetString("frame"); !v.IsNil() {
		n, ok := v.Int()
		if !ok || n < math.MinInt32 || n > math.MaxInt32 {
			vm.Errorf("scene.spawn: frame must be an integer")
		}
		e.Frame = int(n)
	}
	if v := t.GetString("parent"); !v.IsNil() {
		e.Parent = g.toEntity(vm, v, "scene.spawn: parent").ID
	}
	if v := t.GetString("hitbox"); !v.IsNil() {
		e.Hitbox = toBox(vm, v, "scene.spawn: hitbox")
	}
	_ = s
	ent := g.ctx.Spawn(e)
	if st := t.GetString("state"); st.Table() != nil {
		if old, ok := ent.State.(*State); ok {
			// The kind's init already ran on its own table: it keeps what init stored,
			// under what the spawn gives.
			st.Table().ForEach(func(k, v lua.Value) bool {
				old.Table.Set(k, v)
				return true
			})
		} else {
			ent.State = &State{Table: st.Table()}
		}
	}
	return vm.Ret(g.entity(ent))
}

// installEntity builds the metatable of entity values.
// EntityFields are the fields of an entity, in the order a debugger shows them.
var EntityFields = []string{"id", "name", "kind", "alive", "x", "y", "z", "visible", "model", "material", "layer", "frame", "parent", "hitbox", "state"}

func (g *Game) installEntity() {
	ent := func(vm *lua.VM, args []lua.Value, fname string) *scene.Entity {
		if u := lua.Arg(args, 0).Userdata(); u != nil {
			if e, ok := u.Data.(*scene.Entity); ok {
				return e
			}
		}
		vm.ArgError(0, fname, "entity expected")
		return nil
	}
	methods := map[string]fn{
		"position": func(vm *lua.VM, args []lua.Value) []lua.Value {
			p := ent(vm, args, "position").Transform.Position
			return vm.Ret(lua.Float(float64(p.X)), lua.Float(float64(p.Y)), lua.Float(float64(p.Z)))
		},
		"set_position": func(vm *lua.VM, args []lua.Value) []lua.Value {
			e := ent(vm, args, "set_position")
			e.Transform.Position = g.point(vm, args, 1, "set_position")
			return nil
		},
		"move": func(vm *lua.VM, args []lua.Value) []lua.Value {
			e := ent(vm, args, "move")
			e.Transform.Position = e.Transform.Position.Add(g.point(vm, args, 1, "move"))
			return nil
		},
		"world_position": func(vm *lua.VM, args []lua.Value) []lua.Value {
			p := ent(vm, args, "world_position").WorldPosition()
			return vm.Ret(lua.Float(float64(p.X)), lua.Float(float64(p.Y)), lua.Float(float64(p.Z)))
		},
		"rotation": func(vm *lua.VM, args []lua.Value) []lua.Value {
			r := ent(vm, args, "rotation").Transform.Rotation.EulerDeg()
			return vm.Ret(lua.Float(float64(r.X)), lua.Float(float64(r.Y)), lua.Float(float64(r.Z)))
		},
		"set_rotation": func(vm *lua.VM, args []lua.Value) []lua.Value {
			e := ent(vm, args, "set_rotation")
			e.Transform.Rotation = gmath.QuatEulerDeg(g.point(vm, args, 1, "set_rotation"))
			return nil
		},
		"scale": func(vm *lua.VM, args []lua.Value) []lua.Value {
			s := ent(vm, args, "scale").Transform.Scale
			return vm.Ret(lua.Float(float64(s.X)), lua.Float(float64(s.Y)), lua.Float(float64(s.Z)))
		},
		"set_scale": func(vm *lua.VM, args []lua.Value) []lua.Value {
			e := ent(vm, args, "set_scale")
			e.Transform.Scale = g.point(vm, args, 1, "set_scale")
			return nil
		},
		"has_tag": func(vm *lua.VM, args []lua.Value) []lua.Value {
			return vm.Ret(lua.Bool(ent(vm, args, "has_tag").HasTag(vm.CheckString(args, 1, "has_tag"))))
		},
		"add_tag": func(vm *lua.VM, args []lua.Value) []lua.Value {
			e := ent(vm, args, "add_tag")
			if tag := vm.CheckString(args, 1, "add_tag"); !e.HasTag(tag) {
				e.Tags = append(e.Tags, tag)
			}
			return nil
		},
		"remove_tag": func(vm *lua.VM, args []lua.Value) []lua.Value {
			e := ent(vm, args, "remove_tag")
			tag := vm.CheckString(args, 1, "remove_tag")
			kept := e.Tags[:0]
			for _, t := range e.Tags {
				if t != tag {
					kept = append(kept, t)
				}
			}
			e.Tags = kept
			return nil
		},
		"tags": func(vm *lua.VM, args []lua.Value) []lua.Value {
			e := ent(vm, args, "tags")
			t := lua.NewTable(len(e.Tags), 0)
			for _, tag := range e.Tags {
				t.Append(lua.String(tag))
			}
			return vm.Ret(lua.TableValue(t))
		},
		"overlapping": func(vm *lua.VM, args []lua.Value) []lua.Value {
			e := ent(vm, args, "overlapping")
			tag := vm.OptString(args, 1, "overlapping", "")
			var out []*scene.Entity
			for _, o := range g.ctx.Overlapping(e) {
				if tag == "" || o.HasTag(tag) {
					out = append(out, o)
				}
			}
			return vm.Ret(g.list(out))
		},
		"bounds": func(vm *lua.VM, args []lua.Value) []lua.Value {
			b := ent(vm, args, "bounds").AABB
			if b.IsEmpty() {
				return vm.Ret(lua.Nil)
			}
			f := func(x float32) lua.Value { return lua.Float(float64(x)) }
			return vm.Ret(f(b.Min.X), f(b.Min.Y), f(b.Min.Z), f(b.Max.X), f(b.Max.Y), f(b.Max.Z))
		},
		"children": func(vm *lua.VM, args []lua.Value) []lua.Value {
			e := ent(vm, args, "children")
			return vm.Ret(g.list(g.scene(vm, "entity:children").Children(e)))
		},
		"despawn": func(vm *lua.VM, args []lua.Value) []lua.Value {
			g.ctx.Despawn(ent(vm, args, "despawn"))
			return nil
		},
	}
	methodValues := map[string]lua.Value{}
	for name, f := range methods {
		methodValues[name] = lua.FunctionValue(lua.NewFunction("entity:"+name, f))
	}
	meta := lua.NewTable(0, 4)
	meta.SetString("__name", lua.String("entity"))
	meta.SetString("__index", lua.FunctionValue(lua.NewFunction("entity.__index", func(vm *lua.VM, args []lua.Value) []lua.Value {
		e := ent(vm, args, "index")
		key, _ := lua.Arg(args, 1).Str()
		if m, ok := methodValues[key]; ok {
			return vm.Ret(m)
		}
		p := e.Transform.Position
		switch key {
		case "id":
			return vm.Ret(lua.Int(int64(e.ID)))
		case "name":
			return vm.Ret(lua.String(e.Name))
		case "kind":
			return vm.Ret(lua.String(e.Kind))
		case "alive":
			return vm.Ret(lua.Bool(e.Alive()))
		case "x":
			return vm.Ret(lua.Float(float64(p.X)))
		case "y":
			return vm.Ret(lua.Float(float64(p.Y)))
		case "z":
			return vm.Ret(lua.Float(float64(p.Z)))
		case "visible":
			return vm.Ret(lua.Bool(e.Visible))
		case "model":
			return vm.Ret(optString(e.Model))
		case "material":
			return vm.Ret(optString(e.Material))
		case "layer":
			return vm.Ret(lua.Int(int64(e.Layer)))
		case "frame":
			return vm.Ret(lua.Int(int64(e.Frame)))
		case "parent":
			if e.Parent == 0 {
				return vm.Ret(lua.Nil)
			}
			return vm.Ret(g.entity(g.scene(vm, "entity.parent").Get(e.Parent)))
		case "hitbox":
			return vm.Ret(boxValue(e.Hitbox))
		case "state":
			if st, ok := e.State.(*State); ok {
				return vm.Ret(lua.TableValue(st.Table))
			}
			return vm.Ret(lua.Nil)
		}
		vm.Errorf("entity has no field '%s'", key)
		return nil
	})))
	meta.SetString("__newindex", lua.FunctionValue(lua.NewFunction("entity.__newindex", func(vm *lua.VM, args []lua.Value) []lua.Value {
		e := ent(vm, args, "newindex")
		key, _ := lua.Arg(args, 1).Str()
		v := lua.Arg(args, 2)
		num := func() float32 {
			f, ok := v.Float()
			if !ok {
				vm.Errorf("entity.%s must be a number, got %s", key, v.Type())
			}
			return float32(f)
		}
		name := func() string {
			if v.IsNil() {
				return ""
			}
			s, ok := v.Str()
			if !ok {
				vm.Errorf("entity.%s must be a string or nil, got %s", key, v.Type())
			}
			return s
		}
		switch key {
		case "x":
			e.Transform.Position.X = num()
		case "y":
			e.Transform.Position.Y = num()
		case "z":
			e.Transform.Position.Z = num()
		case "visible":
			e.Visible = v.Truthy()
		case "model":
			e.Model = name()
		case "material":
			e.Material = name()
		case "layer":
			n, ok := v.Int()
			if !ok {
				vm.Errorf("entity.layer must be an integer")
			}
			e.Layer = int(n)
		case "frame":
			n, ok := v.Int()
			if !ok || n < math.MinInt32 || n > math.MaxInt32 {
				vm.Errorf("entity.frame must be an integer")
			}
			e.Frame = int(n)
		case "parent":
			var p *scene.Entity
			if !v.IsNil() {
				p = g.toEntity(vm, v, "entity.parent")
			}
			if err := g.scene(vm, "entity.parent").SetParent(e, p); err != nil {
				vm.Errorf("%s", err)
			}
		case "hitbox":
			if v.IsNil() {
				e.Hitbox = nil
			} else {
				e.Hitbox = toBox(vm, v, "entity.hitbox")
			}
		case "state":
			switch {
			case v.IsNil():
				e.State = nil
			case v.Table() != nil:
				e.State = &State{Table: v.Table()}
			default:
				vm.Errorf("entity.state must be a table or nil")
			}
		case "id", "name", "kind", "alive":
			vm.Errorf("entity.%s cannot be changed", key)
		default:
			vm.Errorf("entity has no field '%s' (entities have x, y, z, visible, model, material, layer, frame, parent, hitbox and state; keep your own values in state)", key)
		}
		return nil
	})))
	meta.SetString("__tostring", lua.FunctionValue(lua.NewFunction("entity.__tostring", func(vm *lua.VM, args []lua.Value) []lua.Value {
		e := ent(vm, args, "tostring")
		return vm.Ret(lua.String(fmt.Sprintf("entity %d %q (%s)", e.ID, e.Name, e.Kind)))
	})))
	g.entityMeta = meta
}

func optString(s string) lua.Value {
	if s == "" {
		return lua.Nil
	}
	return lua.String(s)
}
