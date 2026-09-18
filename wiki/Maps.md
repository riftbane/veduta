# Maps

A **map** is the ground of a 2D game seen from above: a grid of cells painted with
terrains (grass, water, soil, a stone floor), in layers, plus **objects** — named rectangles
the game reads, like doors and spawn points. The engine draws it under the entities, and
the game reads and paints its cells while it plays: till the soil, flood a field, open a
path. This page builds a small farm.

## Terrains are textures

A cell shows a texture. Any texture works; for a farm, 16 × 16 pixel-art tiles that repeat
without a seam (`"tiling": true`):

```json
{
  "veduta": "texture/1",
  "size": [16, 16],
  "tiling": true,
  "layers": [
    { "type": "solid", "color": "#4f9a3a" },
    { "type": "noise", "seed": 2, "scale": 4, "octaves": 2, "color": "#2f6a22", "opacity": 0.6 }
  ]
}
```

Save it as `assets/textures/grass.vtex`, and a PNG tile works as well (`"type": "image"`,
see [Graphics Assets](Graphics-Assets)).

### Terrains with borders

Water next to grass should not be a staircase of squares. Give the water texture an
`edge`: it then spills over its lower neighbours with a wandering border, and there are no
transition tiles to draw:

```json
{
  "veduta": "texture/1",
  "size": [16, 16],
  "tiling": true,
  "layers": [
    { "type": "solid", "color": "#2a6fdb" }
  ],
  "frames": [
    { "layers": [ { "type": "noise", "seed": 1, "scale": 4, "color": "#bfe0ff", "opacity": 0.45 } ] },
    { "layers": [ { "type": "noise", "seed": 2, "scale": 4, "color": "#bfe0ff", "opacity": 0.45 } ] },
    { "layers": [ { "type": "noise", "seed": 3, "scale": 4, "color": "#bfe0ff", "opacity": 0.45 } ] }
  ],
  "clips": { "flow": { "frames": [0, 1, 2], "fps": 3 } },
  "play": "flow",
  "edge": { "priority": 20, "width": 4, "roughness": 0.6, "seed": 1 }
}
```

`priority` decides who spills over whom: water (20) over sand (10) over grass (none). A
`width` of 4 reaches a quarter of a 16-pixel cell into the neighbour, `roughness` makes the
line wander. The frames and the `play` clip make the water ripple by itself, on every cell,
with no code.

### Tiles drawn by hand: autotiles

A cliff, a hedge or a fence wants borders you draw yourself. An **autotile** is a PNG of
6 × 3 tiles: on the left an **island** (the terrain in 3 × 3 cells with nothing around:
its sides and its outer corners), on the right a **lake** (the terrain around one empty
cell: its inner corners; the middle tile is not used):

```json
{
  "veduta": "texture/1",
  "size": [96, 48],
  "layers": [
    { "type": "image", "path": "textures/cliff.png" }
  ],
  "autotile": true
}
```

A map picks every cell's tile by its 8 neighbours: a cell that looks like a tile of the
drawing gets that tile whole, any other is put together from quarters of them, so the 17
tiles cover every shape — lines one cell wide, lone cells, corners that only touch. Put
cliffs on a layer of their own above the grass: the transparent pixels of the tiles show
the ground. Frames (`grid` and a `play` clip) animate an autotile like any texture.

## The map

`assets/maps/farm.vmap` (`veduta new map farm` writes a first one). Each layer is a list of
rows, one character per cell: a terrain's `key`, or a space for nothing.

```json
{
  "veduta": "map/1",
  "size": [16, 10],
  "terrains": [
    { "key": ".", "name": "grass", "texture": "grass", "tags": ["tillable"] },
    { "key": "~", "name": "water", "texture": "water", "tags": ["solid", "water"] },
    { "key": "s", "name": "sand",  "texture": "sand" },
    { "key": "=", "name": "soil",  "texture": "soil", "tags": ["soil"] },
    { "key": "d", "name": "path",  "texture": "dirt" },
    { "key": "T", "name": "treetop", "texture": "treetop" }
  ],
  "layers": [
    { "name": "ground", "rows": [
      "................",
      "..sss...........",
      ".ss~~s..........",
      ".s~~~s..........",
      ".ss~ss....====..",
      "..sss.....====..",
      "................",
      "................",
      "................",
      "................"
    ] },
    { "name": "paths", "rows": [
      "       d        ",
      "       d        ",
      "       d        ",
      "       dddd     ",
      "          d     ",
      "          d     ",
      "          d     ",
      "          d     ",
      "          d     ",
      "          d     "
    ] },
    { "name": "above", "z": 2, "rows": [
      "              TT",
      "              TT",
      "                ",
      "                ",
      "                ",
      "                ",
      "                ",
      "                ",
      "                ",
      "                "
    ] }
  ],
  "objects": [
    { "name": "house_door", "at": [7, 0], "tags": ["door"], "props": { "to": "house" } },
    { "name": "start", "at": [10, 8], "tags": ["spawn"] }
  ]
}
```

- The first layer is the ground, every cell painted. The `paths` layer is mostly empty; its
  dirt gets a border over the grass below, because an empty cell counts lower than any
  terrain.
- Layers are drawn at z 0, 0.1, 0.2… A layer that must cover the hero (tree tops, roofs)
  needs a z above the hero's: the hero walks at z 1, `above` is at 2.
- Tags are what the game asks about a cell: `solid`, `tillable`, `water`. They are yours.

The scene names its map, and the hero stands on it:

```json
{
  "veduta": "scene/1",
  "camera": { "type": "orthographic", "size": 12, "position": [8, -5, 100], "look_at": [8, -5, 0] },
  "map": "farm",
  "entities": [
    { "name": "hero", "kind": "hero", "model": "quad", "material": "hero", "position": [10.5, -8.5, 1],
      "scale": [0.8, 0.8, 1] }
  ]
}
```

Cell (x, y) counts columns right and rows **down** from the top-left cell (0, 0), and the
map's top-left corner is at the world's origin: cell (10, 8) has its center at
(10.5, −8.5). `map.center` and `map.cell` convert.

## Walking and blocking

```lua
local SPEED = 4

kinds.hero = {
  update = function(e)
    local dx, dy = input.dpad()
    local nx, ny = e.x + dx * SPEED * engine.dt, e.y + dy * SPEED * engine.dt
    local cx, cy = map.cell(nx, ny)
    if map.inside(cx, cy) and not map.has(cx, cy, "solid") then
      e.x, e.y = nx, ny
    end
    camera.follow2d(e.x, e.y, 12)
  end,
}
```

`map.has` looks at every layer unless you name one, so a fence on an upper layer blocks too.

## Tilling the soil

A on grass tills it; the change is drawn at once, borders included, and recorded in the
trace as a `map_set` event:

```lua
local tilled = {}   -- "x,y" = true: what the player changed, to save

local function till(e)
  local x, y = map.cell(e.x, e.y)
  if map.has(x, y, "tillable", "ground") then
    map.set(x, y, "soil")
    tilled[x .. "," .. y] = true
    save.write("farm", {tilled = tilled})
  end
end

function game.init()
  local data = save.read("farm")
  if data then
    tilled = data.tilled
    for key in pairs(tilled) do
      local x, y = key:match("(%-?%d+),(%-?%d+)")
      map.set(tonumber(x), tonumber(y), "soil")
    end
  end
end
```

Loading a scene or a map starts from its file, so what the player changed lives in a save
and is painted back, as here.

## Doors and spawn points

Objects are data: the engine draws nothing for them, the game decides what they mean.

```lua
local function at_door(x, y)
  for _, door in ipairs(map.objects("door")) do
    if x >= door.x and x < door.x + door.w and y >= door.y and y < door.y + door.h then
      return door
    end
  end
end

local function enter(e, door)
  map.load(door.props.to)                  -- the house's map; the hero stays
  local start = map.objects("spawn")[1]
  e.x, e.y = map.center(start.x, start.y)
end
```

`map.load` swaps the map and keeps the entities, so one scene can walk from the farm into
the house and back. A different scene per place works too: give each its `"map"`.

## Drawing it with the editor

In VS Code a `.vmap` opens in the **map editor**: the palette of terrains (each drawn with
its texture), a brush, rectangles, a bucket, an eraser and an eyedropper, the layers to
show, hide and reorder, and the objects, drawn as rectangles you drag out and name. It
draws the map exactly as the engine does, borders and ripples included, and every stroke
is an ordinary edit of the file: undo, redo and save work as for any file, and **Open as
JSON** shows the text. With the simulator open beside it, saving shows the change in the
running game at once.

## Drawing tiles with the editor

**New Tile…** (on the Textures section of the Veduta view, or its **+**) asks what to
draw — a **tile**, an **animated tile** or an **autotile** — and its size (16 × 16 pixels
by default), makes the PNG and its `.vtex` and opens them in the **tile editor**:

- pencil (B), eraser (E), line (L), rectangle (R, Shift fills it), bucket (G), eyedropper
  (I or Alt+click) and selection (M): drag it to move it, Ctrl+C / Ctrl+X / Ctrl+V (between
  tile editors too), F and Shift+F flip it, T turns it, Enter places it;
- left button draws with the first color, right button with the second (X swaps them);
  a palette, the colors of the frame, alpha;
- frames below the image: **Copy frame** (D) puts a copy of this frame after it to change,
  **Onion skin** (O) shows the frame before faintly, fps and **Play** in the preview;
- the preview repeats a tile 3 × 3 times to show its seams, and paints a small map with an
  autotile, as the engine draws it; **Start the lake from the island** copies the island's
  sides into the lake, for you to draw the inner corners;
- the wheel zooms, the middle button or Space pans, H hides the pixel grid.

Ctrl+S writes the PNG and the `.vtex` (frames side by side, the `play` clip at the fps you
set); undo and redo are VS Code's. A tile opens in the tile editor from the Veduta view;
**Open as JSON** shows the `.vtex`, and **Open in the Tile Editor** goes back.

## Checking

- `veduta inspect map farm`: cells per terrain and layer, terrains whose texture is
  missing, objects that overlap, and a picture of the whole map with its objects outlined.
- `veduta inspect scene main` draws the scene with its map.
- In a scenario, `{"tick": 12, "trace": "map_set", "count_min": 1}` checks that the soil
  was tilled.

## How it is drawn

A layer is drawn in chunks of 16 × 16 cells: chunks out of view cost nothing, and painting a
cell rebuilds only the chunk it is in. A cell is 2 triangles and a border up to 8 more,
so a camera 12 cells high draws a few thousand triangles at most. Maps go up to 1024 × 1024
cells, 16 layers and 92 terrains.

The format in full: the [map reference](https://riftbane.github.io/veduta/map.html).
