# Map format — `map/1`

A map is a grid of cells painted with **terrains**, in **layers**, plus **objects**: named
rectangles of cells the game reads (doors, spawn points, fields). It is the ground of a 2D
game seen from above, like a farm, a town or a dungeon floor. A scene names the map it
stands on; the engine draws it under the entities and the game reads and paints its cells.

File: `assets/maps/<name>.vmap`. The map name is the file name without `.vmap`
(`farm.vmap` → `farm`), 1–64 characters of `a-z`, `0-9`, `_`, `-`, starting with a letter or
digit; a folder under `assets/maps/` works too, the name staying unique.

## Example

```json
{
  "veduta": "map/1",
  "size": [12, 8],
  "terrains": [
    { "key": ".", "name": "grass", "texture": "grass", "tags": ["tillable"] },
    { "key": "~", "name": "water", "texture": "water", "tags": ["water", "solid"] },
    { "key": "s", "name": "sand",  "texture": "sand" },
    { "key": "d", "name": "path",  "texture": "dirt" },
    { "key": "=", "name": "soil",  "texture": "soil", "tags": ["soil"] }
  ],
  "layers": [
    { "name": "ground", "rows": [
      "............",
      "..ssss......",
      ".ss~~ss.....",
      ".s~~~~s.....",
      ".ss~~ss.....",
      "..ssss......",
      "............",
      "............"
    ] },
    { "name": "paths", "rows": [
      "        d   ",
      "        d   ",
      "        d   ",
      "        dddd",
      "            ",
      "            ",
      "            ",
      "            "
    ] }
  ],
  "objects": [
    { "name": "house_door", "at": [8, 0], "tags": ["door"], "props": { "to": "house", "x": 3, "y": 7 } },
    { "name": "field", "at": [8, 5], "size": [4, 3], "tags": ["field"] }
  ]
}
```

and the scene that stands on it:

```json
{
  "veduta": "scene/1",
  "camera": { "type": "orthographic", "size": 8, "position": [6, -4, 100], "look_at": [6, -4, 0] },
  "map": "farm",
  "entities": []
}
```

Every character of a row is one cell: the terrain whose `key` it is, or a space for an
empty cell. The rows read like the map looks.

## Coordinates

A map lies in the XY plane of a 2D game, seen by a camera looking down −Z
([docs/2d.md](2d.md)). Cell (x, y) is x columns right and y rows **down** from the
top-left cell (0, 0). With `origin` (ox, oy, oz) and `tile` t, cell (x, y) covers world x
from ox + x·t to ox + (x + 1)·t and world y from oy − y·t down to oy − (y + 1)·t: the origin
is the map's top-left corner. Its center is (ox + (x + 0.5)·t, oy − (y + 0.5)·t). A layer
is drawn at z = oz + its `z`.

## Top-level fields

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `veduta` | string | required | Must be `"map/1"`. |
| `size` | [columns, rows] | required | 1–1024 each. |
| `tile` | number | `1` | Meters per cell, above 0. |
| `origin` | [x, y, z] | `[0, 0, 0]` | The world position of the map's top-left corner. |
| `terrains` | array | required | 1–92 terrains (below): what cells are painted with. |
| `layers` | array | required | 1–16 layers (below), bottom first. |
| `objects` | array | none | Up to 4096 objects (below). |

### Terrains

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `key` | string | required | The one character standing for the terrain in rows: printable ASCII but space, `"` and `\`. Unique. |
| `name` | string | required | What the game reads and paints with (`map.get`, `map.set`); a name, unique. |
| `texture` | string | one of the two | A texture: its cells are drawn with it, unlit, cut out where it is transparent, with `nearest` filtering (pixel art). |
| `material` | string | one of the two | A material instead, for another look (lit, blended water, bilinear). |
| `tags` | [string, …] | none | What the game asks about a cell (`map.has(x, y, "solid")`): names, no repeats. |

A cell shows the whole texture, or one frame of it: a sheet (`grid` or `frames`) shows
frame 0, or its `play` clip runs by itself on every cell (water, lava). See
[docs/texture.md](texture.md#frames-and-clips).

### Layers

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `name` | string | required | A name, unique: the game names layers in `map.get(x, y, "paths")`. |
| `rows` | [string, …] | required | Exactly `size[1]` strings of exactly `size[0]` characters: a terrain's key, or a space for an empty cell. |
| `z` | number | 0.1 × its index | Added to the origin's z: which layer covers which. |
| `layer` | integer | `0` | Draw order among entities and layers, as an entity's `layer` (−1000 to 1000). |

The first layer is usually filled (the ground); layers above it are mostly empty (paths,
flowers, a floor). A layer that must cover the entities (tree tops, roofs) needs a z above
theirs: actors at z 1 walk under a layer at `"z": 2`.

### Objects

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `name` | string | required | A name, unique among the objects. |
| `at` | [column, row] | required | The top-left cell, on the map. |
| `size` | [columns, rows] | `[1, 1]` | Cells covered, staying on the map. |
| `tags` | [string, …] | none | For `map.objects(tag)`. |
| `props` | object | none | Anything the game wants: property name → string, number or boolean (names without spaces or `.`). |

Objects are data: the engine draws nothing for them. The game places what they stand for
(an NPC at a spawn point, a door that loads another map).

## Edges

A terrain whose texture has an `edge` ([docs/texture.md](texture.md#edges)) draws a
border over its lower neighbours: water spills over the sand around it, sand over grass,
with a wandering line instead of square cells, and no transition tiles to draw. For every
cell, each neighbour (sides and diagonals) of higher `priority` draws its border in the
quarters of the cell it touches: a band along a side, both bands at an inner corner, a
rounded corner when it touches only the diagonal. Borders of higher priority are drawn
over lower ones.

In a layer above the first, an empty cell counts as lower than any terrain: a path on the
`paths` layer gets a border over the ground below, and paths drawn one cell wide look
like paths, not rows of squares. A terrain without an edge has square cells.

## In the game

In Lua ([docs/lua.md](lua.md#map)):

```lua
kinds.hero = {
  update = function(e)
    local x, y = map.cell(e.x, e.y)           -- the cell under the hero
    local _, dy = input.dpad()
    if dy > 0 and not map.has(x, y - 1, "solid") then
      e.y = e.y + 4 * engine.dt               -- water or a fence above stops it
    end
    if input.pressed("a") and map.get(x, y) == "grass" then
      map.set(x, y, "soil")                   -- tilled: drawn at once, borders and all
    end
    for _, door in ipairs(map.objects("door")) do
      if door.x == x and door.y == y then map.load(door.props.to) end
    end
  end,
}
```

`map.get`, `map.set` and `map.tags` name a layer as their last argument (default: the
first for `get` and `set`, every layer for `has` and `tags`). `map.load(name)` replaces
the scene's map, as its file describes it, and keeps the entities: one scene can walk
from the farm to the town. In Go: `ctx.Map()` (package `tilemap`) and `ctx.LoadMap(name)`.

Changes last until the scene or the map is loaded again, which starts from the file. Keep
what the player changed (tilled soil, planted seeds) in a save and paint it back after
loading. Every change is a `map_set` event in the trace (layer, x, y, terrain), and
`map.load` a `map_load` event; snapshots keep the cells.

## Drawing

A layer is drawn in chunks of 16 × 16 cells: a chunk out of view is not drawn, and
painting a cell rebuilds only its chunk (and the chunk next to it when its border reaches
there). A cell is 2 triangles; each quarter with a border 2 more. At a camera 12 cells
high, about 4 chunks are in view.

## Checks

Decoding is strict (unknown fields, wrong types and duplicate keys are errors), and every
problem is reported at once with its file, line and column: `size` in range; keys one
allowed character and unique; terrain, layer and object names valid and unique; a
terrain with exactly one of `texture` and `material`; rows of the right count and length,
each character a key or a space; objects on the map; props strings, numbers or booleans.
`veduta build` also warns when a terrain's texture or material, or a scene's map, is not
found.

## Limits

| Item | Range |
|------|-------|
| `size` | 1–1024 columns and rows |
| terrains | 1–92 |
| layers | 1–16 |
| objects | up to 4096 |
