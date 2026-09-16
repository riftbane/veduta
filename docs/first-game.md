# Your first game (`first-game`)

Star catcher: stars fall, a paddle at the bottom catches them, three missed and the game is
over. About a hundred lines of Lua and three small files, and every step can be played.
It assumes the tool and VS Code are installed ([Getting started on Windows](start-windows.md)).

## 1. Make the game

In VS Code, **Veduta: New Game** (Ctrl+Shift+P), Lua, a folder, the name `catcher`. Or in
a terminal:

```sh
veduta init catcher
code catcher
```

Press **F5**: the simulator opens with the game's name in the middle of a dark screen. That
is `main.lua` drawing it; everything else comes from `assets/`. Close the window (or
Ctrl+Q) to come back.

## 2. The paddle

A thing on screen is an entity: a model (its shape), a material (its look), a position.
The new game already has the model `quad`, a 1 × 1 square. The paddle needs a material,
a flat yellow:

```json assets/materials/hero.mat.json
{ "veduta": "material/1", "albedo": "#f4c542", "unlit": true }
```

and the stars a blue one:

```json assets/materials/star.mat.json
{ "veduta": "material/1", "albedo": "#8fd3ff", "unlit": true }
```

The scene lists what is there when the game starts. Add the paddle to its `entities`: the
camera shows 16 × 12 units around the centre, so `y` −5 is near the bottom, and `scale`
stretches the square to 2 × 0.5.

```json assets/scenes/main.scene.json
{
  "veduta": "scene/1",
  "camera": { "type": "orthographic", "size": 12, "position": [0, 0, 100], "look_at": [0, 0, 0] },
  "background": "#101018",
  "entities": [
    { "name": "hero", "kind": "hero", "model": "quad", "material": "hero", "position": [0, -5, 1], "scale": [2, 0.5, 1] }
  ]
}
```

VS Code completes these files as you type and marks a field that does not exist. The
paddle's `kind` is `hero`: its behaviour is Lua's to give. Replace `main.lua` with the
start of the game:

```lua
-- Star catcher: move the paddle with the D-pad, catch the falling stars.

local SPEED = 8   -- the paddle, in units per second
local FALL = 4    -- the stars
local EDGE = 7    -- how far left and right things go

local score, missed = 0, 0
```

## 3. Move it

`kinds.hero` is what every entity of kind `hero` does, once per tick (20 a second).
`input.dpad()` is −1, 0 or 1; multiplied by the speed and by `engine.dt`, the length of a
tick in seconds, it moves the paddle at the same speed whatever the tick rate. Add to
`main.lua`:

```lua
kinds.hero = {
  update = function(e)
    if missed >= 3 then
      return
    end
    local dx = input.dpad()
    e.x = math.max(-EDGE, math.min(EDGE, e.x + dx * SPEED * engine.dt))
    for _, star in ipairs(e:overlapping("star")) do
      score = score + 1
      trace("star_caught", {score = score})
      star:despawn()
    end
    e.state.score = score
  end,
}
```

The loop over `e:overlapping("star")` is the catch: every star touching the paddle scores
and goes. `trace` records that it happened, which tests count, and `e.state.score` is kept
where tests can read it. F5: the arrow keys (or A and D) move the paddle.

## 4. Falling stars

`game.update` runs once per tick before the entities. Every twentieth tick (once a second)
it adds a star at a random place along the top. `math.random` is the game's own generator:
the same run always falls the same way, which is what makes the game testable. Add:

```lua
function game.update()
  if missed >= 3 then
    if input.pressed("a") then
      score, missed = 0, 0
      scene.load("main")
    end
    return
  end
  if engine.tick % 20 == 0 then
    scene.spawn{
      kind = "star", model = "quad", material = "star", tags = {"star"},
      position = {math.random(-EDGE, EDGE), 7, 1}, scale = {0.6, 0.6, 1},
    }
  end
end
```

A star falls until it passes the bottom, where it counts as missed. Add:

```lua
kinds.star = {
  update = function(e)
    if missed >= 3 then
      return
    end
    e.y = e.y - FALL * engine.dt
    if e.y < -7 then
      missed = missed + 1
      trace("star_missed", {missed = missed})
      if missed == 3 then
        trace("game_over", {score = score})
      end
      e:despawn()
    end
  end,
}
```

After three, both kinds stop and `game.update` waits for A, which loads the scene again
from the start.

## 5. Score and game over

`game.draw` runs for every frame shown, after the scene: the place for text over it.
`hud.text` writes with the console's 8 × 8 font, in pixels from the top left. Add:

```lua
function game.draw()
  hud.text(4, 4, "SCORE " .. score, "#f4f0e0")
  if missed >= 3 then
    local msg = "GAME OVER - A TO PLAY"
    hud.text((engine.width - 8 * #msg) // 2, engine.height // 2 - 4, msg, "#ff8080")
  end
end
```

And last, a check that holds in every test run: the score never goes below zero. Add:

```lua
function game.init()
  invariant("score_not_negative", function() return score >= 0 end)
end
```

Press F5 and play. F1 in the simulator shows how long each tick takes against the budget
of the console.

## 6. Test it

A scenario is a run with scripted buttons and what must be true at the end. Tests run the
game without a window, exactly as it plays, and give the same result on every machine.

Holding right for ten ticks moves the paddle right:

```json tests/scenarios/move.scenario.json
{
  "veduta": "scenario/1",
  "scene": "main",
  "seed": 1,
  "ticks": 10,
  "inputs": [
    { "tick": 1, "press": ["right"] }
  ],
  "expect": [
    { "tick": 10, "entity": "hero", "path": "position.x", "op": ">", "value": 0 }
  ]
}
```

Doing nothing loses, once:

```json tests/scenarios/game_over.scenario.json
{
  "veduta": "scenario/1",
  "scene": "main",
  "seed": 1,
  "ticks": 200,
  "expect": [
    { "tick": 200, "trace": "game_over", "count_min": 1, "count_max": 1 },
    { "tick": 200, "trace": "star_caught", "count_max": 0 }
  ],
  "screenshots": [60, 200]
}
```

and A starts again with a score of zero:

```json tests/scenarios/restart.scenario.json
{
  "veduta": "scenario/1",
  "scene": "main",
  "seed": 1,
  "ticks": 220,
  "inputs": [
    { "tick": 200, "press": ["a"] },
    { "tick": 201, "release": ["a"] }
  ],
  "expect": [
    { "tick": 220, "trace": "scene_load", "count_min": 2 },
    { "tick": 220, "entity": "hero", "path": "state.score", "op": "==", "value": 0 }
  ]
}
```

**Veduta: Test** (or `veduta test`) runs them all:

```
scenario game_over            pass  golden new
scenario move                 pass  golden new
scenario restart              pass  golden new
scenario start                pass  golden new
PASS
```

The first run records each scenario's result as its golden; from then on a change that
alters what happens fails the test until you accept it. Instead of writing inputs by hand,
press F5 in the simulator, play, and F5 again: the run is saved as a scenario to add
expectations to.

## 7. On the console

Take the console's card out, put it in the PC, and **Veduta: Deploy to the Console's
Card** (or `veduta deploy`). Put the card back: the dashboard lists `catcher`, and A plays
it.

## Where next

- [Lua API](lua.md): everything a script can do.
- [2D games](2d.md): sprites with textures, depth, collisions.
- [Scenarios](scenario.md): every kind of expectation.
