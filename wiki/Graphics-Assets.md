# Graphics Assets

What an entity looks like is two assets: a **model** (its shape) and a **material** (its
surface), and the material may use a **texture** (an image). All three are JSON files
that describe how to build the asset: primitive shapes, layer programs, colours. The tool
compiles them when the game runs (`veduta cook` does it ahead of time), and VS Code
completes and checks them as you type.

## Models

`assets/models/<name>.vmodel`: a list of primitive parts joined into one mesh.

```json
{
  "veduta": "model/1",
  "pivot": "bottom-center",
  "parts": [
    { "shape": "box", "size": [0.6, 1.2, 0.4], "position": [0, 0.6, 0], "material": "cloth" },
    { "shape": "sphere", "radius": 0.3, "position": [0, 1.5, 0], "material": "skin" },
    { "shape": "cylinder", "radius": 0.05, "height": 0.8, "position": [0.4, 0.8, 0], "rotation_deg": [0, 0, 20] }
  ]
}
```

| Shape | Fields |
|-------|--------|
| `box` | `size` |
| `cylinder` | `radius`, `height`, `segments` |
| `sphere` | `radius`, `segments`, `rings` |
| `plane` | a flat rectangle facing +Y (a floor) |
| `extrude` | a 2D outline pushed along an axis |
| `lathe` | a profile turned around the Y axis (a vase, a column) |
| `mirror` | a mirrored copy of other parts |

Every part takes `position`, `rotation_deg`, `scale` and `material`. A part without a
material uses the entity's `material`. `pivot` (`origin`, `center`, `bottom-center`) sets
which point of the model sits at the entity's position.

Every new project has `quad`, a 1 × 1 × 0.02 box with its pivot at the center: the model
of every sprite. Models can also have levels of detail and a draw distance for large 3D
scenes. The full format is in the [model reference](https://riftbane.github.io/veduta/model.html).

For shapes computed while the game runs (a block world, a generated maze), build the mesh
from Lua instead: see [3D, Worlds and Blocks](3D-Worlds-and-Blocks#block-worlds-mesh-and-volume).

## Materials

`assets/materials/<name>.vmat`:

```json
{ "veduta": "material/1", "albedo": "#80c0ff60", "alpha": "blend", "unlit": true }
```

| Field | Default | Meaning |
|-------|---------|---------|
| `albedo` | `"#ffffff"` | colour, multiplied with the texture |
| `texture` | none | a texture's name |
| `unlit` | `false` | `true` ignores the scene's light |
| `alpha` | `"opaque"` | `opaque`, `cutout` (pixels below `cutoff` are not drawn) or `blend` (translucent) |
| `cutoff` | `0.5` | the threshold of `cutout` |
| `cull` | `"back"` | `none` draws both sides (leaves, flags) |
| `filter` | `"bilinear"` | `nearest` keeps pixel art sharp |
| `grid` | none | `[columns, rows]`: the texture is a sprite sheet, and an entity's `frame` picks the frame drawn |

A sprite's material is always:

```json
{ "veduta": "material/1", "texture": "coin", "unlit": true, "alpha": "cutout", "filter": "nearest" }
```

## Textures

`assets/textures/<name>.vtex`: an image described as layers painted one over the other,
bottom first.

```json
{
  "veduta": "texture/1",
  "size": [32, 32],
  "layers": [
    { "type": "solid", "color": "#8a5a2b" },
    { "type": "noise", "color": "#6b4420", "scale": 6, "octaves": 2, "seed": 4 },
    { "type": "stripes", "width": 4, "angle_deg": 90, "colors": ["#00000000", "#5a3a1a"], "opacity": 0.6 },
    { "type": "rect", "xy": [1, 1], "size": [30, 30], "color": "#3a2410", "outline": 2 }
  ]
}
```

| Layer | Paints |
|-------|--------|
| `solid` | the whole texture |
| `noise` | smooth value noise (`scale`, `octaves`, `seed`) |
| `stripes` | parallel bands cycling through `colors` (transparent ones allowed) |
| `rect` | a rectangle, optionally rounded (`corner`) or outlined (`outline`) |
| `circle` | a disc or a ring |
| `gradient` | a linear gradient |
| `checker` | a checkerboard |
| `image` | a PNG file from under `assets/` |

Every layer takes `opacity` and `blend` (`normal`, `multiply`, `screen`, `add`). Positions
are pixels from the top left. Pixels no layer paints stay transparent, which is how a
sprite gets its shape.

### Seeing it while you write it

In VS Code, the eye in the title bar of a `.vtex` file (or **Veduta: Preview the
Texture**) opens a panel beside it that draws the texture as you type, with nothing to save
and no game to start. A grid and a ruler measure the picture, the pointer reads the texel it
is on and that texel's colour — which is how you find the `src` rectangle of one sprite in a
sheet — and dragging across the picture measures a rectangle. Every layer has a switch, so
you can look under the one on top, and a texture with `tiling` can be shown repeated to
check its seams. A source the engine would refuse is not drawn: the panel names the field
that is wrong, the way `veduta build` does.

### Pixel art from PNG files

Draw sprites in any editor, save them as PNG under `assets/` and wrap each in a texture of
the same size:

```json
{
  "veduta": "texture/1",
  "size": [16, 16],
  "mipmaps": false,
  "layers": [
    { "type": "image", "path": "sprites/hero.png" }
  ]
}
```

An image exactly as large as the texture is copied pixel for pixel. VS Code's **tile
editor** draws such textures itself — tiles, animations frame by frame and autotiles —
and saves the PNG with its `.vtex`: see [Maps](Maps#drawing-tiles-with-the-editor).

### Textures that repeat

`"tiling": true` makes a texture wrap around seamlessly: ground, walls, water. Worlds require
it for their ground materials.

### One PNG, many textures

An image layer's `rect` takes a part of the PNG, `[x, y, width, height]` in its pixels, so
one sheet of sprites drawn in any editor gives a texture per sprite:

```json
{
  "veduta": "texture/1",
  "size": [16, 16],
  "layers": [
    { "type": "image", "path": "sprites/items.png", "rect": [32, 0, 16, 16] }
  ]
}
```

A texture exists only while the game is built: the console loads the compiled pixels,
never the PNG, so textures sharing a PNG cost nothing more.

### Animations

A texture can be a sheet of frames: `"grid": [columns, rows]` cuts its image into frames,
and `"frames"` paints each frame with layers of its own. `"clips"` names animations of
those frames, and `"play"` names one that runs by itself wherever the texture is drawn and
nothing picks a frame (water, torches, the cells of a map):

```json
{
  "veduta": "texture/1",
  "size": [64, 32],
  "layers": [
    { "type": "image", "path": "sprites/hero.png" }
  ],
  "grid": [4, 2],
  "clips": {
    "walk": { "frames": [0, 1, 2, 3], "fps": 8 },
    "idle": { "frames": [4], "fps": 1 },
    "hit":  { "frames": [5, 6], "fps": 12, "loop": false, "next": "idle" }
  }
}
```

An entity plays a clip by name: `e.anim = "walk"` (see [2D Games](2D-Games#animation)).

### Terrain borders

`"edge"` makes a texture a terrain that spills over lower terrains on a map, with a
wandering border instead of square cells: see [Maps](Maps#terrains-with-borders).

The full format, with the exact maths of every layer and blend mode, is in the
[texture reference](https://riftbane.github.io/veduta/texture.html).

## Checking assets

`veduta inspect model|texture|scene NAME` reports problems ranked by severity (a model over
its triangle budget, a texture that is not a power of two, a scene with overlapping
sprites) and writes contact sheets you can look at under `out/`.
