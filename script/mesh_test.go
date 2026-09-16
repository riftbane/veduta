package script

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta/v2/gmath"
	"github.com/riftbane/veduta/v2/internal/golden"
)

// meshGame appends to the test game a game.init that builds runtime models from Lua and a
// kind that traces what the engine made of them.
const meshGame = `+
function game.init()
  invariant("score_non_negative", function() return true end)
  local v = volume.new(3, 2, 1)
  v:fill(0, 0, 0, 2, 1, 0, 1)
  v:set(2, 1, 0, 2)
  assert(v:get(2, 1, 0) == 2 and v:get(0, 0, 0) == 1)
  local blocks = mesh.voxels(v, {[1] = "hero", [2] = "coin"}, 0.5)
  mesh.set("game:blocks", blocks)
  local m = mesh.new()
  m:part("coin")
  m:box(0, 0, 0, 1, 2, 3, "+y-y")
  m:quad(0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0)
  m:triangle(0, 0, 0, 1, 0, 0, 0, 1, 0)
  mesh.set("game:shape", m)
  scene.spawn{kind = "probe", name = "blocks", model = "game:blocks", position = {-4, 0, 0},
    state = {triangles = blocks:triangles()}}
  scene.spawn{kind = "probe", name = "shape", model = "game:shape", position = {4, 0, 0},
    state = {triangles = m:triangles()}}
end

kinds.probe = {
  update = function(e)
    local x0, y0, z0, x1, y1, z1 = e:bounds()
    e.state.size = string.format("%.2f %.2f %.2f", x1 - x0, y1 - y0, z1 - z0)
  end,
}
`

// TestMeshAndVolume: a volume's visible faces and a built mesh become models entities draw,
// with the triangle counts and bounds they should have.
func TestMeshAndVolume(t *testing.T) {
	dir := copyGame(t, map[string]string{"main.lua": meshGame})
	scenario := filepath.Join(t.TempDir(), "mesh.scenario.json")
	writeFile(t, scenario, `{
  "veduta": "scenario/1", "scene": "main", "seed": 1, "ticks": 2,
  "expect": [
    { "tick": 2, "entity": "blocks", "path": "state.triangles", "op": "==", "value": 44 },
    { "tick": 2, "entity": "blocks", "path": "state.size", "op": "==", "value": "1.50 1.00 0.50" },
    { "tick": 2, "entity": "shape", "path": "state.triangles", "op": "==", "value": 7 },
    { "tick": 2, "entity": "shape", "path": "state.size", "op": "==", "value": "1.00 2.00 3.00" }
  ],
  "screenshots": [2]
}`)
	r, stderr, code := run(t, dir, "simulate", "--scenario", scenario, "--out", t.TempDir())
	if code != 0 || r.Verdict != "pass" {
		t.Fatalf("exit %d, verdict %q: %s %s\n%+v\nstderr: %s", code, r.Verdict, r.FirstFailure, r.Error, r.Expect, stderr)
	}
}

func TestMeshErrors(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"name", `+function game.init() mesh.set("blocks", mesh.new()) end`, "must contain ':'"},
		{"faces", `+function game.init() mesh.new():box(0, 0, 0, 1, 1, 1, "+y+w") end`, `bad faces "+y+w"`},
		{"outside", `+function game.init() volume.new(2, 2, 2):set(2, 0, 0, 1) end`, "outside the volume"},
		{"too big", `+function game.init() volume.new(1024, 1024, 1024) end`, "at most"},
		{"id", `+function game.init() volume.new(1, 1, 1):set(0, 0, 0, 256) end`, "0 (empty) to 255"},
		{"materials", `+function game.init() mesh.voxels(volume.new(1, 1, 1), {grass = 1}) end`, "block id (1 to 255)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := copyGame(t, map[string]string{"main.lua": c.src})
			r, _, code := run(t, dir, "simulate", "--scene", "main", "--ticks", "1", "--seed", "1", "--out", t.TempDir())
			if code == 0 || !strings.Contains(r.Error, c.want) {
				t.Fatalf("exit %d, error %q, want %q", code, r.Error, c.want)
			}
		})
	}
}

// TestVoxelMeshGolden pins the geometry a volume makes, bit for bit: CI checks it on amd64,
// arm64 and Windows, where a fused multiply-add would change it.
func TestVoxelMeshGolden(t *testing.T) {
	v := &volume{sx: 5, sy: 4, sz: 3, cells: make([]uint8, 60)}
	for i := range v.cells {
		v.cells[i] = uint8((i * 7) % 4) // some empty, three ids
	}
	m := &meshBuilder{}
	v.mesh(m, map[int]string{1: "a", 2: "b"}, 0.37)
	m.quad(v3(0.1, 0.2, 0.3), v3(1.7, 0.2, 0.9), v3(1.3, 2.9, 0.4), v3(0.2, 1.1, 1.3))
	h := sha256.New()
	for _, vert := range m.verts {
		for _, f := range []float32{vert.Pos.X, vert.Pos.Y, vert.Pos.Z, vert.Normal.X, vert.Normal.Y, vert.Normal.Z, vert.UV.X, vert.UV.Y} {
			binary.Write(h, binary.LittleEndian, math.Float32bits(f))
		}
	}
	binary.Write(h, binary.LittleEndian, m.indices)
	golden.Text(t, "script_voxels.hash", []byte(fmt.Sprintf("%x %d vertices %d parts\n", h.Sum(nil), len(m.verts), len(m.parts))))
}

func v3(x, y, z float32) gmath.Vec3 { return gmath.V3(x, y, z) }

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
