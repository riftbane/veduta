package asset

import (
	"strings"

	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
)

// Scene defaults (docs/scene.md).
var (
	cameraTypes = []string{"perspective", "orthographic"}

	defaultFovDeg     = float32(60)
	defaultNear       = float32(0.1)
	defaultFar        = float32(200)
	defaultLightDir   = gmath.V3(-0.4, -1, -0.3)
	defaultLightColor = uint32(0xffffffff) // #ffffff
	defaultAmbient    = uint32(0xff404040) // #404040
	defaultBackground = uint32(0xff202830) // #202830
)

// BuiltinKinds are the entity kinds the engine provides without registration.
var BuiltinKinds = []string{"camera", "light", "static"}

// ParseScene decodes and compiles the scene source file (for example
// "assets/scenes/main.scene.json"). The scene name is the file name without its
// ".scene.json" suffix and must be a valid asset name.
func ParseScene(file string, data []byte) (*Scene, error) {
	var src SceneSource
	loc, err := Decode(file, data, TypeScene, &src)
	if err != nil {
		return nil, err
	}
	name, err := nameFromFile(KindScene, file, loc)
	if err != nil {
		return nil, err
	}
	return CompileScene(name, &src, loc)
}

// CompileScene validates src and returns the compiled scene called name. Every problem
// is reported, located through loc (nil reports positions as unknown), in one Errors
// value. Asset references (models, materials) are checked for syntax only; entities keep
// their scene-file order.
func CompileScene(name string, src *SceneSource, loc *Locator) (*Scene, error) {
	c := NewChecker(ensureLoc(loc))
	if err := ValidName(name); err != nil {
		c.Errorf("", "scene %v", err)
	}
	s := &Scene{
		Name:       name,
		Camera:     compileCamera(c, &src.Camera),
		Light:      compileLight(c, src.Light),
		Background: c.Color("background", src.Background, defaultBackground),
	}
	s.Entities = compileEntities(c, src.Entities)
	if err := c.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func compileCamera(c *Checker, src *CameraSource) Camera {
	cam := Camera{Ortho: c.Enum("camera.type", src.Type, cameraTypes, "perspective") == "orthographic"}
	if cam.Ortho {
		if src.FovDeg != nil {
			c.Errorf("camera.fov_deg", "only allowed for perspective cameras (orthographic cameras use size)")
		}
		if src.Size == nil {
			c.Errorf("camera.size", "is required for orthographic cameras (visible height in meters)")
			cam.Size = 1
		} else {
			cam.Size = c.Positive("camera.size", src.Size, 1)
		}
	} else {
		if src.Size != nil {
			c.Errorf("camera.size", "only allowed for orthographic cameras (perspective cameras use fov_deg)")
		}
		cam.FovDeg = defaultFovDeg
		if v := src.FovDeg; v != nil {
			if gmath.IsFinite(*v) && *v > 1 && *v < 179 {
				cam.FovDeg = *v
			} else {
				c.Errorf("camera.fov_deg", "%v out of range (1, 179)", *v)
			}
		}
	}
	before := len(c.Errs)
	cam.Near = c.Positive("camera.near", src.Near, defaultNear)
	cam.Far = c.Positive("camera.far", src.Far, defaultFar)
	if len(c.Errs) == before && cam.Near >= cam.Far {
		at := "camera.far"
		if src.Far == nil {
			at = "camera.near"
		}
		c.Errorf(at, "far (%v) must be greater than near (%v)", cam.Far, cam.Near)
	}
	before = len(c.Errs)
	cam.Position = c.RequireVec3("camera.position", src.Position)
	cam.LookAt = c.RequireVec3("camera.look_at", src.LookAt)
	if len(c.Errs) == before && cam.Position == cam.LookAt {
		c.Errorf("camera.look_at", "must differ from camera.position %v", cam.Position)
	}
	return cam
}

func compileLight(c *Checker, src *LightSource) gfx.Light {
	var l LightSource
	if src != nil {
		l = *src
	}
	dir := c.Vec3("light.direction", l.Direction, defaultLightDir)
	if l.Direction != nil && dir == gmath.Zero3 {
		c.Errorf("light.direction", "must not be the zero vector")
		dir = defaultLightDir
	}
	return gfx.Light{
		Dir:     dir,
		Color:   gfx.ColorVec3(lightColor(c, "light.color", l.Color, defaultLightColor)),
		Ambient: gfx.ColorVec3(lightColor(c, "light.ambient", l.Ambient, defaultAmbient)),
	}
}

// lightColor is Checker.Color restricted to opaque "#RRGGBB" colors.
func lightColor(c *Checker, path, s string, def uint32) uint32 {
	if len(s) == 9 && s[0] == '#' {
		c.Errorf(path, "light colors have no alpha: use #RRGGBB, got %q", s)
		return def
	}
	return c.Color(path, s, def)
}

func compileEntities(c *Checker, src []EntitySource) []Entity {
	if len(src) == 0 {
		return nil
	}
	ents := make([]Entity, len(src))
	byName := make(map[string]int, len(src)) // lookup only, never iterated
	for i := range src {
		e := &src[i]
		p := Path("entities", i)
		ent := &ents[i]
		ent.Name = e.Name
		if e.Name == "" {
			c.Errorf(Path(p, "name"), "is required")
		} else if c.Name(Path(p, "name"), e.Name) {
			if j, dup := byName[e.Name]; dup {
				c.Errorf(Path(p, "name"), "duplicate entity name %q (first used by entities[%d])", e.Name, j)
			} else {
				byName[e.Name] = i
			}
		}
		ent.Kind = e.Kind
		if e.Kind == "" {
			c.Errorf(Path(p, "kind"), "is required (static, camera, light, or a kind registered by the game)")
		} else {
			c.Name(Path(p, "kind"), e.Kind)
		}
		if e.Model != "" && c.Name(Path(p, "model"), e.Model) {
			ent.Model = e.Model
		}
		if e.Material != "" && c.Name(Path(p, "material"), e.Material) {
			ent.Material = e.Material
		}
		ent.Position = c.Vec3(Path(p, "position"), e.Position, gmath.Zero3)
		ent.RotationDeg = c.Vec3(Path(p, "rotation_deg"), e.RotationDeg, gmath.Zero3)
		ent.Scale = c.Vec3(Path(p, "scale"), e.Scale, gmath.One3)
		for k, v := range [3]float32{ent.Scale.X, ent.Scale.Y, ent.Scale.Z} {
			if v == 0 {
				c.Errorf(Path(Path(p, "scale"), k), "must be non-zero")
			}
		}
		if len(e.Tags) > 0 {
			ent.Tags = make([]string, 0, len(e.Tags))
			for k, tag := range e.Tags {
				tp := Path(Path(p, "tags"), k)
				if !c.Name(tp, tag) {
					continue
				}
				if dup := indexOf(ent.Tags, tag); dup >= 0 {
					c.Errorf(tp, "duplicate tag %q", tag)
					continue
				}
				ent.Tags = append(ent.Tags, tag)
			}
		}
		ent.Visible = e.Visible == nil || *e.Visible
	}
	checkParents(c, src, ents, byName)
	return ents
}

// checkParents resolves parent names and reports unknown parents and cycles. Each cycle
// is reported once, at the parent field of its entity with the lowest index.
func checkParents(c *Checker, src []EntitySource, ents []Entity, byName map[string]int) {
	parent := make([]int, len(src))
	for i := range src {
		parent[i] = -1
		name := src[i].Parent
		if name == "" {
			continue
		}
		pp := Path(Path("entities", i), "parent")
		if !c.Name(pp, name) {
			continue
		}
		if name == src[i].Name {
			c.Errorf(pp, "an entity cannot be its own parent")
			continue
		}
		j, ok := byName[name]
		if !ok {
			c.Errorf(pp, "no entity named %q in this scene", name)
			continue
		}
		parent[i] = j
		ents[i].Parent = name
	}
	const done = -1
	state := make([]int, len(src)) // 0 unvisited, i+1 on walk i, done
	for i := range src {
		if state[i] != 0 {
			continue
		}
		var walk []int
		j := i
		for j >= 0 && state[j] == 0 {
			state[j] = i + 1
			walk = append(walk, j)
			j = parent[j]
		}
		if j >= 0 && state[j] == i+1 {
			cyc := walk[indexOfInt(walk, j):]
			low := cyc[0]
			for _, k := range cyc {
				low = min(low, k)
			}
			names := []string{src[low].Name}
			for k := parent[low]; ; k = parent[k] {
				names = append(names, src[k].Name)
				if k == low {
					break
				}
			}
			c.Errorf(Path(Path("entities", low), "parent"), "parent cycle: %s", strings.Join(names, " -> "))
		}
		for _, k := range walk {
			state[k] = done
		}
	}
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func indexOfInt(list []int, v int) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}
