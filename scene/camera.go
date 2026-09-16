package scene

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// Camera is a look-at camera, perspective or orthographic.
type Camera struct {
	Ortho    bool
	FovDeg   float32 // vertical field of view (perspective)
	Size     float32 // visible height in meters (orthographic)
	Near     float32
	Far      float32
	Position gmath.Vec3
	Target   gmath.Vec3
}

// DefaultCamera looks at the origin from above-front.
func DefaultCamera() Camera {
	return Camera{FovDeg: 60, Near: 0.1, Far: 200, Position: gmath.V3(0, 5, 10)}
}

// Camera2DDistance is how far in front of the z = 0 plane a Camera2D sits.
const Camera2DDistance = 100

// Camera2D returns the camera of a 2D game played in the XY plane: orthographic, looking
// down -Z at (center.X, center.Y, 0) from z = Camera2DDistance, +X to the right and +Y up
// on screen, showing height world units vertically (the width follows from the aspect
// ratio). Near and far are the scene defaults, 0.1 and 200, so everything with z in
// [-100, 99.9] is visible and a larger z is nearer the camera. A game can follow its hero
// with it:
//
//	p := hero.WorldPosition()
//	ctx.Scene.Camera = scene.Camera2D(gmath.V2(p.X, p.Y), 12)
func Camera2D(center gmath.Vec2, height float32) Camera {
	return Camera{Ortho: true, Size: height, Near: 0.1, Far: 200,
		Position: gmath.V3(center.X, center.Y, Camera2DDistance), Target: gmath.V3(center.X, center.Y, 0)}
}

// CameraFromAsset converts a compiled scene camera.
func CameraFromAsset(c asset.Camera) Camera {
	return Camera{Ortho: c.Ortho, FovDeg: c.FovDeg, Size: c.Size, Near: c.Near, Far: c.Far, Position: c.Position, Target: c.LookAt}
}

// View returns the world→eye matrix.
func (c Camera) View() gmath.Mat4 { return gmath.LookAt(c.Position, c.Target, gmath.Up) }

// Proj returns the eye→clip matrix for the given aspect ratio (width / height).
func (c Camera) Proj(aspect float32) gmath.Mat4 {
	if c.Ortho {
		// Rounded explicitly so arm64 cannot fuse them into a multiply-add (see gmath.m32).
		h := float32(c.Size / 2)
		w := float32(h * aspect)
		return gmath.Orthographic(-w, w, -h, h, c.Near, c.Far)
	}
	return gmath.Perspective(gmath.Radians(c.FovDeg), aspect, c.Near, c.Far)
}

// GfxView returns the gfx view of the camera for a w×h target.
func (c Camera) GfxView(w, h int) gfx.View {
	return gfx.View{View: c.View(), Proj: c.Proj(float32(w) / float32(h)), Eye: c.Position, Near: c.Near, Far: c.Far}
}

// Presets lists the built-in camera presets accepted by CameraPreset besides the names
// of camera entities and "orbit:<yaw degrees>".
var Presets = []string{"scene", "top", "front", "back", "left", "right", "iso"}

// CameraPreset resolves a camera by name:
//
//   - "" or "scene": the scene camera;
//   - "top", "front", "back", "left", "right": orthographic views that frame the scene
//     bounds (front looks along -Z, right looks along -X, top looks down with -Z up);
//   - "iso": a perspective view from the (+1, +1, +1) direction framing the bounds;
//   - "orbit:<deg>": a perspective view circling the bounds at yaw deg, 25° above;
//   - the name of an entity of kind "camera": its world position looking along its -Z,
//     with the scene camera's projection.
func (s *Scene) CameraPreset(name string, aspect float32) (Camera, error) {
	b := s.Bounds()
	if b.IsEmpty() {
		b = gmath.AABB{Min: gmath.V3(-1, -1, -1), Max: gmath.V3(1, 1, 1)}
	}
	switch name {
	case "", "scene":
		return s.Camera, nil
	case "top":
		return FrameOrtho(b, gmath.V3(0, -1, 0), aspect), nil
	case "front":
		return FrameOrtho(b, gmath.V3(0, 0, -1), aspect), nil
	case "back":
		return FrameOrtho(b, gmath.V3(0, 0, 1), aspect), nil
	case "left":
		return FrameOrtho(b, gmath.V3(1, 0, 0), aspect), nil
	case "right":
		return FrameOrtho(b, gmath.V3(-1, 0, 0), aspect), nil
	case "iso":
		return FramePerspective(b, gmath.V3(-1, -1, -1), 40, aspect), nil
	}
	if deg, ok := strings.CutPrefix(name, "orbit:"); ok {
		d, err := strconv.ParseFloat(deg, 32)
		if err != nil {
			return Camera{}, fmt.Errorf("camera preset %q: bad angle", name)
		}
		return FramePerspective(b, OrbitDir(float32(d), 25), 40, aspect), nil
	}
	if e := s.Find(name); e != nil && e.Kind == KindCamera {
		c := s.Camera
		c.Position = e.WorldPosition()
		c.Target = c.Position.Add(e.World().MulDir(gmath.Forward).Normalize())
		return c, nil
	}
	return Camera{}, fmt.Errorf("unknown camera preset %q (want one of %v, orbit:<deg>, or a camera entity)", name, Presets)
}

// LookFrom returns c placed at eye and looking along yaw and pitch degrees: yaw 0 looks
// along -Z and grows counter-clockwise seen from above (90 looks along -X), positive
// pitch looks up. Projection, near and far are kept, so a game can make a first-person
// camera from the scene's own camera:
//
//	ctx.Scene.Camera = ctx.Scene.Camera.LookFrom(eye, yaw, pitch)
func (c Camera) LookFrom(eye gmath.Vec3, yawDeg, pitchDeg float32) Camera {
	c.Position = eye
	c.Target = eye.Add(OrbitDir(yawDeg, -pitchDeg))
	return c
}

// OrbitDir returns the viewing direction of a camera at yaw degrees around +Y (0 looks
// along -Z) and pitch degrees above the horizon.
func OrbitDir(yawDeg, pitchDeg float32) gmath.Vec3 {
	sy, cy := gmath.SinCos(gmath.Radians(yawDeg))
	sp, cp := gmath.SinCos(gmath.Radians(pitchDeg))
	// Callers add the direction to a position: round each product explicitly so arm64
	// cannot fuse it into a multiply-add (see gmath.m32).
	return gmath.V3(float32(-sy*cp), -sp, float32(-cy*cp))
}

// FrameOrtho returns an orthographic camera looking along dir that fits box b with a
// 10% margin.
func FrameOrtho(b gmath.AABB, dir gmath.Vec3, aspect float32) Camera {
	dir = dir.Normalize()
	c := b.Center()
	radius := b.Size().Len() / 2
	if radius == 0 {
		radius = 1
	}
	view := gmath.LookAt(c.Sub(dir.Scale(radius*2)), c, gmath.Up)
	// Extent of the box projected on the view's x and y axes.
	var w, h float32
	for _, p := range b.Corners() {
		q := view.MulPoint(p)
		w = max(w, gmath.Abs(q.X))
		h = max(h, gmath.Abs(q.Y))
	}
	size := max(2*h, 2*w/aspect) * 1.1
	if size == 0 {
		size = 1
	}
	return Camera{Ortho: true, Size: size, Near: 0.01, Far: float32(radius * 4), Position: c.Sub(dir.Scale(radius * 2)), Target: c}
}

// FramePerspective returns a perspective camera looking along dir that fits the bounding
// sphere of b with a small margin.
func FramePerspective(b gmath.AABB, dir gmath.Vec3, fovDeg, aspect float32) Camera {
	dir = dir.Normalize()
	c := b.Center()
	radius := b.Size().Len() / 2
	if radius == 0 {
		radius = 1
	}
	half := gmath.Radians(fovDeg) / 2
	if aspect < 1 {
		half = gmath.Atan(gmath.Tan(half) * aspect)
	}
	// Each product is rounded explicitly so arm64 cannot fuse it into a multiply-add (see
	// gmath.m32).
	dist := float32(radius / gmath.Sin(half) * 1.05)
	return Camera{FovDeg: fovDeg, Near: max(dist-float32(radius*1.5), float32(dist*0.01)), Far: dist + float32(radius*1.5), Position: c.Sub(dir.Scale(dist)), Target: c}
}
