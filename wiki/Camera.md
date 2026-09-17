# Camera

Every scene has one camera, set in the [scene file](Scenes#camera). Scripts read and change
it while the game runs; a change takes effect from the frame drawn at the end of the tick.

## 2D: following the player

```lua
kinds.hero = {
  update = function(e)
    -- ... move the hero ...
    camera.follow2d(e.x, e.y, 12)
  end,
}
```

`camera.follow2d(x, y, height)` makes the orthographic camera of a 2D game: it looks down
−Z at (x, y) and shows `height` units vertically (the width follows from the 4:3 panel, so
12 units high is 16 wide). It sits at z = 100, so everything from z = −100 to 99.9 is
visible, and a larger z is nearer.

Pixel art stays sharp when a unit is a whole number of pixels: on the 240-pixel panel a
height of 12 is 20 pixels per unit, 24 is 10, 15 is 16.

### Keeping the camera inside the level

```lua
local LEVEL_W, LEVEL_H = 24, 14   -- the level's size in units
local VIEW_H = 12
local VIEW_W = VIEW_H * 4 / 3

local function clamp(v, lo, hi)
  return math.max(lo, math.min(hi, v))
end

local function follow(e)
  local x = clamp(e.x, VIEW_W / 2 - 0.5, LEVEL_W - VIEW_W / 2 - 0.5)
  local y = clamp(e.y, -(LEVEL_H - VIEW_H / 2 - 0.5), -(VIEW_H / 2 - 0.5))
  camera.follow2d(x, y, VIEW_H)
end
```

(For a level of tiles whose centers run from x = 0 to LEVEL_W − 1 and from y = 0 down to
−(LEVEL_H − 1), as in [Your First Game](Your-First-Game).)

## Any camera

`camera.get()` returns a table; `camera.set{...}` changes the fields it is given and keeps
the others:

| Field | Meaning |
|-------|---------|
| `position` | `{x, y, z}` where the camera is |
| `target` | `{x, y, z}` the point it looks at |
| `ortho` | `true` for orthographic, `false` for perspective |
| `fov` | vertical field of view in degrees (perspective) |
| `size` | visible height in units (orthographic) |
| `near`, `far` | clip distances |

A third-person camera behind a 3D hero:

```lua
kinds.walker = {
  update = function(e)
    -- ... move the hero ...
    camera.set{position = {e.x, e.y + 6, e.z + 8}, target = {e.x, e.y + 1, e.z}}
  end,
}
```

A screen shake, a few ticks of random offset:

```lua
local shake = 0

local function shake_camera(x, y)
  if shake > 0 then
    shake = shake - 1
    x = x + (math.random() - 0.5) * 0.3
    y = y + (math.random() - 0.5) * 0.3
  end
  camera.follow2d(x, y, 12)
end
```

`math.random` is the game's seeded generator, so even the shake is the same in every replay
of a scenario.
