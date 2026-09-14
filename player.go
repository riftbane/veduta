package veduta

import (
	"fmt"
	"time"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/platform"
	"github.com/riftbane/veduta/sim"
)

// runPlayer puts the game on the framebuffer and runs it: one tick per 1/tick_rate
// seconds, one rendered frame per tick (no interpolation), input from the pad and the
// keyboard. It returns when the player quits (Home, Select+Start, or Ctrl+Q).
func runPlayer(g Game, p *asset.Project, a *Assets) error {
	win, err := platform.Open(platform.Options{Title: p.Name, Width: p.Resolution[0], Height: p.Resolution[1]})
	if err != nil {
		return fmt.Errorf("%w (the player draws on a framebuffer: it runs on a console or at a Linux text console; use -headless to render and simulate)", err)
	}
	defer win.Close()
	e := newEngine(g, p, a)
	defer e.close()
	if err := e.start(runOptions{Scene: p.DefaultScene, Seed: p.DefaultSeed}); err != nil {
		return err
	}
	var input sim.InputState
	pointerLocked := false
	period := time.Second / time.Duration(p.TickRate)
	next := time.Now()
	for {
		events, err := win.Poll()
		if err != nil {
			return err
		}
		for _, ev := range events {
			switch ev.Kind {
			case platform.KeyDown:
				input.KeyDown(ev.Code)
			case platform.KeyUp:
				input.KeyUp(ev.Code)
			case platform.MouseMove:
				input.MouseMove(ev.X, ev.Y)
			case platform.ButtonDown:
				input.MouseMove(ev.X, ev.Y)
				input.ButtonDown(ev.Button)
			case platform.ButtonUp:
				input.MouseMove(ev.X, ev.Y)
				input.ButtonUp(ev.Button)
			case platform.Text:
				input.TypeText(ev.Text)
			case platform.Stick:
				input.SetStick(ev.X, ev.Y)
			case platform.FocusLost:
				input.ReleaseAll()
			case platform.Close:
				return nil
			}
		}
		if err := e.step(input.Next()); err != nil {
			return err
		}
		if want := e.ctx.PointerLocked(); want != pointerLocked {
			pointerLocked = want
			// No backend implements pointer lock (the framebuffer accepts the request and
			// does nothing), so its answer does not matter: the game plays either way.
			_ = win.SetPointerLock(want)
		}
		w, h := win.Size()
		if w > 0 && h > 0 {
			cam, err := e.ctx.Scene.CameraPreset("scene", float32(w)/float32(h))
			if err != nil {
				return err
			}
			f, err := e.render(cam, w, h, gfx.ModeColor, false)
			if err != nil {
				return err
			}
			if err := win.Present(f.FB.Image()); err != nil {
				return err
			}
		}
		// Hold the tick rate; when far behind (a stall), skip ahead instead of racing.
		next = next.Add(period)
		now := time.Now()
		if d := next.Sub(now); d > 0 {
			time.Sleep(d)
		} else if -d > 5*period {
			next = now
		}
	}
}
