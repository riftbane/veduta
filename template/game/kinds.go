package game

import (
	"github.com/riftbane/veduta"
	"github.com/riftbane/veduta/gmath"
	"github.com/riftbane/veduta/scene"
)

// Tunables of the demo. PlayerSpeed × 100 is the "deliberately broken build" of the
// spec's fuzzing check: the hero then leaves the world bounds within a second.
const (
	PlayerSpeed = 4.0  // meters per second
	JumpSpeed   = 5.0  // initial vertical speed, m/s
	Gravity     = 14.0 // m/s²
	GemSpin     = 90.0 // degrees per second
)

// PlayerState is the hero's per-entity state (trace: state.score, state.vel_y, …).
type PlayerState struct {
	Score    int     `json:"score"`
	VelY     float32 `json:"vel_y"`
	OnGround bool    `json:"on_ground"`
	Heading  float32 `json:"heading"` // yaw in degrees
}

// GemState is a gem's per-entity state.
type GemState struct {
	Value int     `json:"value"`
	Spin  float32 `json:"spin"` // current yaw in degrees
	BaseY float32 `json:"base_y"`
}

func init() {
	veduta.RegisterKind("player", newPlayer)
	veduta.RegisterKind("collectible", newCollectible)
}

func newPlayer(e *scene.Entity) veduta.Behaviour {
	e.State = &PlayerState{OnGround: true, Heading: e.Transform.Rotation.EulerDeg().Y}
	return veduta.BehaviourFunc(updatePlayer)
}

// updatePlayer moves the hero with WASD relative to the world (W = -Z), turns it to face
// the direction of travel, and handles jumping.
func updatePlayer(ctx *veduta.Context, e *scene.Entity, in veduta.Input) {
	st := e.State.(*PlayerState)
	dir := gmath.V3(in.Axis("KeyA", "KeyD"), 0, in.Axis("KeyW", "KeyS"))
	if l := dir.Len(); l > 0 {
		dir = dir.Scale(1 / l)
		e.Transform.Position = e.Transform.Position.Add(dir.Scale(PlayerSpeed * ctx.DT))
		// Facing -Z is yaw 0; atan2(-x, -z) gives the yaw of the travel direction.
		st.Heading = gmath.Degrees(gmath.Atan2(-dir.X, -dir.Z))
		e.Transform.Rotation = gmath.QuatEulerDeg(gmath.V3(0, st.Heading, 0))
	}
	if st.OnGround && in.JustPressed("Space") {
		st.VelY = JumpSpeed
		st.OnGround = false
		ctx.Trace("jump", map[string]any{"at": e.Transform.Position})
	}
	if !st.OnGround {
		st.VelY -= float32(Gravity * ctx.DT)
		e.Transform.Position.Y += float32(st.VelY * ctx.DT)
		if e.Transform.Position.Y <= 0 {
			e.Transform.Position.Y = 0
			st.VelY = 0
			st.OnGround = true
			ctx.Trace("land", map[string]any{"at": e.Transform.Position})
		}
	}
	if current != nil {
		st.Score = current.Score
	}
}

func newCollectible(e *scene.Entity) veduta.Behaviour {
	e.State = &GemState{Value: 1, BaseY: e.Transform.Position.Y}
	return veduta.BehaviourFunc(updateCollectible)
}

// updateCollectible spins and bobs the gem, and collects it when the hero touches it.
func updateCollectible(ctx *veduta.Context, e *scene.Entity, in veduta.Input) {
	st := e.State.(*GemState)
	st.Spin = gmath.Wrap(st.Spin+float32(GemSpin*ctx.DT), 360)
	e.Transform.Rotation = gmath.QuatEulerDeg(gmath.V3(0, st.Spin, 0))
	e.Transform.Position.Y = st.BaseY + float32(0.1*gmath.Sin(float32(float32(ctx.Tick)*ctx.DT)*3))
	for _, o := range ctx.Overlapping(e) {
		if o.HasTag("player") && current != nil {
			score := current.collect()
			ctx.Trace("gem_collected", map[string]any{"gem": e.Name, "score": score})
			ctx.Despawn(e)
			return
		}
	}
}
