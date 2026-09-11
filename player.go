package veduta

import (
	"fmt"
	"time"

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/gfx"
	"github.com/riftbane/veduta/platform"
	"github.com/riftbane/veduta/sim"
)

// runPlayer opens the game window and runs the game: one tick per 1/tick_rate seconds,
// one rendered frame per tick (no interpolation), input from the window. It returns when
// the window is closed.
func runPlayer(g Game, p *asset.Project, a *Assets) error {
	win, err := platform.Open(platform.Options{Title: p.Name, Width: p.Resolution[0], Height: p.Resolution[1]})
	if err != nil {
		return fmt.Errorf("%w (use -headless on machines without a display)", err)
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
			// A window that cannot lock the pointer still plays: the game only loses the
			// mouse look once the cursor reaches the edge of the screen.
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
