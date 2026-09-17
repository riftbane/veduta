# Scenes

A scene is the starting state of a level: a camera, a light, a background colour and the
entities that exist when it loads. It lives in `assets/scenes/<name>.scene.json`.

The game starts in the project's `default_scene` (`main` unless `veduta.json` says
otherwise). `scene.load(name)` switches to another one, and `scene.name()` tells which is
loaded.

```json
{
  "veduta": "scene/1",
  "camera": { "type": "perspective", "fov_deg": 60, "position": [0, 5, 10], "look_at": [0, 0, 0] },
  "light": { "direction": [-0.4, -1, -0.3], "color": "#ffffff", "ambient": "#404040" },
  "background": "#202830",
  "entities": [
    { "name": "ground", "kind": "static", "model": "ground", "material": "grass" },
    { "name": "player", "kind": "player", "model": "hero", "material": "hero",
      "rotation_deg": [0, 180, 0], "tags": ["player"] },
    { "name": "hat", "kind": "static", "model": "hat", "parent": "player",
      "position": [0, 1.8, 0] },
    { "name": "exit", "kind": "static", "position": [0, 0, -9],
      "hitbox": [[-1, 0, -0.5], [1, 2, 0.5]], "tags": ["exit"] }
  ]
}
```

## Units and axes

- Distances are metres (or simply *units*). Coordinates are right-handed: +X right, +Y up,
  −Z forward, into the screen.
- Angles in files are degrees; `rotation_deg: [x, y, z]` applies Z, then X, then Y.
- Colours are `"#RRGGBB"` or `"#RRGGBBAA"`.

## Camera

| Field | Default | Meaning |
|-------|---------|---------|
| `type` | `"perspective"` | `perspective` or `orthographic` |
| `fov_deg` | `60` | vertical field of view, perspective only |
| `size` | required for orthographic | the visible height in units; the width follows the 4:3 panel |
| `near`, `far` | `0.1`, `200` | clip distances |
| `position` | required | where the camera is |
| `look_at` | required | the point it looks at |

An orthographic camera looking down −Z is a 2D view, see [2D Games](2D-Games). Scripts
change the camera while the game runs, see [Camera](Camera).

## Light

One directional light plus an ambient colour. `direction` is where the light travels
(`[0, -1, 0]` shines straight down). Materials marked `unlit` ignore it, as every sprite
should. Leave `light` out for the defaults shown above.

## Entities

| Field | Default | Meaning |
|-------|---------|---------|
| `name` | required | unique in the scene; scenarios and `scene.find` use it |
| `kind` | required | `static` for scenery, or a kind defined in `kinds` |
| `model`, `material` | none | asset names; without a model the entity is invisible |
| `position` | `[0, 0, 0]` | |
| `rotation_deg` | `[0, 0, 0]` | |
| `scale` | `[1, 1, 1]` | no component may be 0; a negative one mirrors |
| `tags` | `[]` | labels for scripts, invariants and tests |
| `parent` | none | another entity's name: this one's transform is then relative to it |
| `visible` | `true` | |
| `hitbox` | none | `[[minx, miny, minz], [maxx, maxy, maxz]]` in the entity's own space: replaces the model's bounds for collisions |
| `layer` | `0` | draw order, −1000 to 1000 |
| `frame` | `0` | the frame of a sprite sheet material |

Ids are given in file order when the scene loads: the first entity is 1. Entities spawned
later take the following ids.

An entity with a `hitbox` and no `model` is an invisible trigger zone:

```lua
kinds.player = {
  update = function(e)
    if #e:overlapping("exit") > 0 then
      trace("level_done", {})
      scene.load("level2")
    end
  end,
}
```

## A scene per screen

Scenes are cheap. A title screen, each level, a game-over screen can each be a scene, with
`game.update` deciding when to switch and `game.draw` drawing the text of each:

```lua
function game.update()
  local s = scene.name()
  if s == "title" and input.pressed("a") then
    scene.load("level1")
  elseif s == "gameover" and input.pressed("a") then
    scene.load("title")
  end
end
```

Remember that `scene.load` resets entities but not Lua variables: reset the score and the
lives yourself when a new game starts.

## Editing a scene while it shows

Lay a level out with its scene file open beside the simulator. Every save reloads the game
**in place**: it restarts in the scene it was in (not the project's default), so a moved
platform or a new enemy shows a second later, without playing back to the level. The same
goes for a texture, a material or a hud written in `game.draw`. A file that does not
compile, or a script that fails, leaves the last frame on the screen with the error over
it until a save fixes it; F9 restarts from the start.

To open the simulator directly in a scene, in VS Code add a debug configuration with the
snippet **Veduta: Play a scene** (`"scene": "level3"` in `.vscode/launch.json`) and press
F5, or in a terminal:

```sh
veduta sim --scene level3
```

The run starts at tick 0 in that scene and `game.init` runs again, so a game whose `init`
loads its title scene comes back to the title: load the first scene from a scene file's
entities or from `game.update` instead, or make `init` respect `scene.name()`.

The complete format, with every error message, is in the
[scene reference](https://riftbane.github.io/veduta/scene.html).
