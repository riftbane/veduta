# 3D, Worlds and Blocks

The same engine draws 3D: a perspective camera, lit models, and worlds far larger than a
scene, streamed around the player. The console renders everything in software on its CPU,
so keep scenes modest: a few hundred to a thousand triangles on screen (check with
`veduta bench`, see [Debugging and Performance](Debugging-and-Performance)).

## A 3D scene

A perspective camera, a light, and models with lit materials:

```json
{
  "veduta": "scene/1",
  "camera": { "type": "perspective", "fov_deg": 60, "position": [0, 6, 8], "look_at": [0, 1, 0] },
  "light": { "direction": [-0.4, -1, -0.3], "color": "#fff4e0", "ambient": "#303848" },
  "background": "#87a8c8",
  "entities": [
    { "name": "floor", "kind": "static", "model": "floor", "material": "stone" },
    { "name": "hero", "kind": "walker", "model": "hero", "material": "hero", "tags": ["player"] },
    { "name": "crate", "kind": "static", "model": "crate", "material": "wood", "position": [3, 0, -2] }
  ]
}
```

Models are built from boxes, cylinders, spheres, extrusions and lathed profiles, see
[Graphics Assets](Graphics-Assets#models). A hero, feet at its origin:

```json
{
  "veduta": "model/1",
  "pivot": "bottom-center",
  "parts": [
    { "shape": "box", "size": [0.6, 1.2, 0.4], "position": [0, 0.6, 0] },
    { "shape": "sphere", "radius": 0.3, "position": [0, 1.5, 0] }
  ]
}
```

Walking on the XZ plane: up on the D-pad goes forward, toward −Z, with a camera behind. The
hero turns to face where it walks (for a model whose front faces −Z):

```lua
local SPEED = 4

kinds.walker = {
  update = function(e)
    local dx, dy = input.dpad()
    e.x = e.x + dx * SPEED * engine.dt
    e.z = e.z - dy * SPEED * engine.dt
    if dx ~= 0 or dy ~= 0 then
      -- face the direction of travel: yaw 0 looks toward -z, -90 toward +x
      e:set_rotation(0, math.deg(math.atan(-dx, dy)), 0)
    end
    camera.set{position = {e.x, e.y + 6, e.z + 8}, target = {e.x, e.y + 1, e.z}}
  end,
}
```

## Streamed worlds

A **world** (`assets/worlds/<name>.world.json`) is a map generated from a seed: biomes,
rolling ground, hills, lakes and seas, vegetation, villages and landmarks. Only the chunks
around a focus point exist at a time, so a world can span kilometres.

```json
{
  "veduta": "world/1",
  "seed": 7,
  "camera": { "type": "perspective", "fov_deg": 60, "position": [0, 6, 8], "look_at": [0, 0, 0] },
  "biomes": [ { "name": "meadow", "ground": "grass" } ],
  "terrain": { "relief": 2, "relief_scale": 32 },
  "entities": [
    { "name": "hero", "kind": "walker", "model": "hero", "material": "hero", "tags": ["player"] }
  ]
}
```

The ground material's texture must be `"tiling": true`. `entities` are the persistent ones
(the hero), placed relative to the start cell.

From Lua:

| Function | Does |
|----------|------|
| `world.load(name, cx, cz)` | replaces the scene with the world, starting at cell (cx, cz) |
| `world.name()` | the loaded world, or `nil` |
| `world.focus(x, y, z)` | where the chunks follow; call it every tick |
| `world.height(x, z)` | the ground's height there |
| `world.water(x, z)` | the water level there, or `nil` on dry land |

A hero that walks on the ground and stays out of the water:

```lua
local SPEED = 4

function game.init()
  world.load("meadow", 0, 0)
end

kinds.walker = {
  update = function(e)
    local dx, dy = input.dpad()
    local x = e.x + dx * SPEED * engine.dt
    local z = e.z - dy * SPEED * engine.dt   -- up on the D-pad walks north, toward -z
    if world.water(x, z) == nil then
      e:set_position(x, world.height(x, z), z)
    end
    world.focus(e.x, e.y, e.z)
    camera.set{position = {e.x, e.y + 6, e.z + 8}, target = {e.x, e.y + 1, e.z}}
  end,
}
```

A project can also start straight in a world with `"default_world"` in `veduta.json`.
Scenarios load one with `"world"` and `"at"` instead of `"scene"`, and the trace records a
`chunk_load` event per chunk loaded.

Authoring a world is a loop of editing rules and looking at the result:
`veduta world map <name>` prints its map, `veduta world query <name> --cell x,z` describes
a cell, and `veduta world terrain|vegetation|place` add hills, lakes, trees and landmarks
where the rules allow (`veduta help world`). The full format is in the
[world reference](https://riftbane.github.io/veduta/world.html).

## Block worlds: mesh and volume

For geometry that changes while the game runs (a block world that is dug and built, a maze
generated from the seed) scripts build models themselves. The heavy loops run inside the
engine, so a whole chunk of blocks is one call.

A **volume** is a 3D grid of block ids (0 is empty, 1 to 255 are block types).
`mesh.voxels` turns it into a mesh of the faces between a block and empty space, one part
per block id with the material you name, and `mesh.set` registers it as a model entities
can use:

```lua
local chunk = volume.new(16, 8, 16)

local function rebuild()
  mesh.set("game:chunk", mesh.voxels(chunk, {[1] = "stone", [2] = "grass", [3] = "wood"}))
end

function game.init()
  chunk:fill(0, 0, 0, 15, 2, 15, 1)    -- three layers of stone
  chunk:fill(0, 3, 0, 15, 3, 15, 2)    -- grass on top
  rebuild()
  scene.spawn{name = "chunk", model = "game:chunk"}
end

-- dig or place the block under a cursor
local function set_block(x, y, z, id)
  chunk:set(x, y, z, id)
  rebuild()
end
```

| Function | Does |
|----------|------|
| `volume.new(x, y, z)` | an empty grid, at most 4 194 304 blocks |
| `v:get(x, y, z)`, `v:set(x, y, z, id)` | one block; cells count from 0 |
| `v:fill(x1, y1, z1, x2, y2, z2, id)` | every block of a box |
| `v:size()` | x, y, z |
| `mesh.voxels(v, materials [, size])` | the visible faces as a mesh; `size` is a block's edge in units (default 1) |
| `mesh.new()` | an empty mesh, to build by hand |
| `m:part([material])` | what follows is drawn with that material |
| `m:quad(...)`, `m:triangle(...)` | a face from 4 or 3 corners, counter-clockwise seen from the front |
| `m:box(x, y, z, w, h, d [, faces])` | a box, optionally only some faces (`"+y-y"`) |
| `m:triangles()` | the triangle count |
| `mesh.set(name, m)` | makes `m` the model `name`; the name must contain `:` |
| `mesh.remove(name)` | forgets it |

Models set this way survive `scene.load`. Setting a model copies the mesh, so change the
mesh and call `mesh.set` again to update it. Split a large world into chunks and rebuild
only the chunk that changed: fewer triangles to rebuild, and chunks out of view are not
drawn.
