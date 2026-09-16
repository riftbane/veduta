package script

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riftbane/veduta"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
)

// The same game logic in Lua and in Go: movers that walk, bounce off the bounds, count their
// bounces in their state, and every tick look for the entities they overlap. bench runs
// both through the engine the way the player does; TestLuaAgainstGo reports the update
// milliseconds of each.

const benchMovers = 200

const benchLua = `
function game.init()
  for i = 1, 200 do
    scene.spawn{kind = "mover", model = "quad", position = {(i % 20) - 10, (i // 20) - 5, 0},
      scale = {0.3, 0.3, 1}, tags = {"mover"}, state = {vx = 1 + i % 3, vy = 1 + i % 5, bounces = 0}}
  end
end

kinds.mover = {
  update = function(e)
    local s = e.state
    local x, y = e:position()
    x, y = x + s.vx * engine.dt, y + s.vy * engine.dt
    if x > 10 or x < -10 then s.vx = -s.vx; s.bounces = s.bounces + 1 end
    if y > 8 or y < -8 then s.vy = -s.vy; s.bounces = s.bounces + 1 end
    e:set_position(x, y, 0)
    local near = e:overlapping("mover")
    s.near = #near
  end,
}
`

// goMover is the Go mover's state.
type goMover struct {
	VX, VY  float32
	Bounces int
	Near    int
}

type goBenchGame struct{}

func (goBenchGame) Init(ctx *veduta.Context) error {
	for i := 1; i <= benchMovers; i++ {
		e := ctx.Spawn(scene.Entity{Kind: "gomover", Model: "quad", Visible: true, Tags: []string{"mover"},
			Transform: scene.Transform{Position: gmath.V3(float32(i%20-10), float32(i/20-5), 0), Rotation: gmath.QuatIdent(), Scale: gmath.V3(0.3, 0.3, 1)}})
		e.State = &goMover{VX: float32(1 + i%3), VY: float32(1 + i%5)}
	}
	return nil
}
func (goBenchGame) Update(*veduta.Context, veduta.Input) {}
func (goBenchGame) Draw(*veduta.Context, *gfx.DrawList)  {}

func init() {
	veduta.RegisterKind("gomover", func(e *scene.Entity) veduta.Behaviour {
		return veduta.BehaviourFunc(func(ctx *veduta.Context, e *scene.Entity, in veduta.Input) {
			s, ok := e.State.(*goMover)
			if !ok {
				return
			}
			p := e.Transform.Position
			p.X += float32(s.VX * ctx.DT)
			p.Y += float32(s.VY * ctx.DT)
			if p.X > 10 || p.X < -10 {
				s.VX, s.Bounces = -s.VX, s.Bounces+1
			}
			if p.Y > 8 || p.Y < -8 {
				s.VY, s.Bounces = -s.VY, s.Bounces+1
			}
			e.Transform.Position = gmath.V3(p.X, p.Y, 0)
			near := 0
			for _, o := range ctx.Overlapping(e) {
				if o.HasTag("mover") {
					near++
				}
			}
			s.Near = near
		})
	})
}

type benchReport struct {
	UpdateMS struct {
		Mean float64 `json:"mean"`
		P95  float64 `json:"p95"`
	} `json:"update_ms"`
	Error string `json:"error"`
}

func benchUpdate(t *testing.T, run func(args []string, stdout *bytes.Buffer) int, dir string) benchReport {
	t.Helper()
	var out bytes.Buffer
	code := run([]string{"-project", dir, "-headless", "bench", "--scene", "empty", "--ticks", "200", "--cpus", "1"}, &out)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var r benchReport
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &r); err != nil || code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	return r
}

// TestLuaAgainstGo logs how much slower the Lua movers update than the Go ones. It runs
// with -run TestLuaAgainstGo -v; a short run skips it.
func TestLuaAgainstGo(t *testing.T) {
	if testing.Short() || os.Getenv("VEDUTA_BENCH") == "" {
		t.Skip("set VEDUTA_BENCH=1 to compare Lua with Go")
	}
	dir := copyGame(t, map[string]string{"main.lua": benchLua,
		"veduta.json": `{"veduta": "project/1", "name": "bench", "engine": "v1.4.1", "script": "main.lua", "default_scene": "empty"}`})
	os.WriteFile(filepath.Join(dir, "assets", "scenes", "empty.scene.json"),
		[]byte(`{"veduta": "scene/1", "camera": {"type": "orthographic", "size": 20, "position": [0, 0, 100], "look_at": [0, 0, 0]}, "entities": []}`), 0o644)
	lua := benchUpdate(t, func(args []string, out *bytes.Buffer) int { return Run(args, out, os.Stderr) }, dir)
	goRes := benchUpdate(t, func(args []string, out *bytes.Buffer) int {
		return veduta.RunArgs(goBenchGame{}, args, out, os.Stderr)
	}, dir)
	ratio := lua.UpdateMS.Mean / goRes.UpdateMS.Mean
	t.Logf("%d movers, update per tick: Lua %.3f ms (p95 %.3f), Go %.3f ms (p95 %.3f): Lua is %.1f× Go",
		benchMovers, lua.UpdateMS.Mean, lua.UpdateMS.P95, goRes.UpdateMS.Mean, goRes.UpdateMS.P95, ratio)
}
