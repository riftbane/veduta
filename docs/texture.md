# Texture format — `texture/1`

A texture is an image described as a **layer program**: a list of layers (solid fills,
value noise, stripes, rectangles, circles, gradients, checkerboards and PNG images) that
are painted one over the other, each with a blend mode and an opacity. The compiler
renders the program into a BGRA8 image with a mip chain. Materials use textures by name.

File: `assets/textures/<name>.vtex`. The texture name is the file name without
`.vtex` (`crate_wood.vtex` → `crate_wood`); it must be 1–64 characters of
`a-z`, `0-9`, `_`, `-`, starting with a letter or digit. A folder under
`assets/textures/` works too (`assets/textures/ui/icons.vtex`): the name is still the
file name, so it must be unique across the folders.

## Minimal example

```json
{
  "veduta": "texture/1",
  "size": [64, 64],
  "layers": [
    { "type": "solid", "color": "#8a5a2b" }
  ]
}
```

## Top-level fields

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `veduta` | string | required | Must be `"texture/1"`. |
| `size` | [width, height] | required | Integers in pixels, each 1–4096. Non-power-of-two sizes are allowed (`inspect texture` warns with `TEX_NOT_POWER_OF_TWO`). |
| `tiling` | boolean | `false` | `true`: the texture is meant to repeat. It is sampled with wrap-around (repeat) addressing, and `noise` layers wrap so the texture repeats without a seam (see [Tiling](#tiling)). `false`: texture coordinates outside [0, 1] clamp to the edge. |
| `mipmaps` | boolean | `true` | `true`: the compiler also produces the mip chain (each level half the size of the previous one, down to 1×1). `false`: only the full-size image. |
| `layers` | array | required | 1–64 layer objects, painted in list order: **the first layer is at the bottom**, each later layer is painted over the result of the previous ones. |

## Coordinates, angles and colors

- Positions and lengths are in **pixels** of the texture. The origin is the **top-left
  corner**; x grows to the right, y grows **downwards**. Pixel (x, y) is the square from
  (x, y) to (x + 1, y + 1); its center is (x + 0.5, y + 0.5). Coordinates need not be
  integers, and shapes may extend beyond the texture (they are cut off at its edges).
- Angles (`angle_deg`) are in degrees and describe a direction in pixel space: 0° points
  right (+x), 90° points down (+y), 180° left, 270° (or −90°) up. Positive angles turn
  clockwise on screen. Any finite value is allowed (450 is the same as 90).
- Colors are strings `"#RRGGBB"` (opaque) or `"#RRGGBBAA"` (hex digits, either case). The
  alpha byte `AA` is straight (not premultiplied) opacity: `00` transparent, `ff` opaque.

## Layers and compositing

Every layer is an object with a `type` field. Each type accepts only its own fields plus
the common fields below; **any other field is an error, even when it is `null`, `0`, `""`
or `false`**. Layer indices (in error messages and in `inspect` reports) start at 0.

### Common fields (every layer type)

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `type` | string | required | `solid`, `noise`, `stripes`, `rect`, `circle`, `gradient`, `checker` or `image`. |
| `opacity` | number | `1` | In [0, 1]. Multiplies the layer's alpha everywhere. |
| `blend` | string | `"normal"` | `normal`, `multiply`, `screen` or `add`: how the layer's color combines with what is below it. An empty string also means `normal`. |

### How a layer is painted

The canvas starts **transparent black** (every channel 0). For every pixel, a layer
produces a color `s` (red, green, blue, each in [0, 1]) and an alpha:

    a = (alpha of the layer's color at that pixel) × coverage × opacity

`coverage` is how much of the pixel the layer covers (for example 0.5 on the
anti-aliased edge of a circle; the noise value for `noise`). With `d` the canvas color
and `dA` the canvas alpha at that pixel before the layer, the blend function `B` is
applied per channel:

| `blend` | B(d, s) | Effect |
|---------|---------|--------|
| `normal` | s | paints the color |
| `multiply` | d · s | darkens (white changes nothing, black gives black) |
| `screen` | 1 − (1 − d) · (1 − s) | lightens (black changes nothing, white gives white) |
| `add` | min(1, d + s) | lightens by adding, clamped at 1 |

On an **opaque** canvas (dA = 1, the usual case once a `solid` layer is at the bottom):

    result color = d + (B(d, s) − d) · a
    result alpha = 1

In general (W3C source-over compositing with blending), which gives the formula above when
dA = 1:

    result alpha = a + dA · (1 − a)
    result color = (a · (1 − dA) · s + a · dA · B(d, s) + (1 − a) · dA · d) / result alpha

So over a transparent part of the canvas (dA = 0) a layer keeps its own color: a
half-transparent white circle on nothing is white at 50% alpha, not grey. A layer whose
alpha is 0 at a pixel changes nothing there.

The canvas is kept in 32-bit floating point while the layers are painted. At the end
every channel is clamped to [0, 1], multiplied by 255 and rounded (halves round up) to 8
bits. The result has straight alpha. Fully transparent pixels store color 0 (black).

Anti-aliasing: `stripes`, `rect` and `circle` take 4 × 4 samples per pixel, at offsets
0.125, 0.375, 0.625 and 0.875 in x and y. Coverage is the fraction of samples inside the
shape; a sample exactly on a shape's edge counts as inside. When the samples of one pixel
have different colors (a stripe edge), they are averaged with premultiplied alpha: the
pixel's alpha is the mean of the sample alphas and its color is the alpha-weighted mean of
the sample colors.

## Layer types

### `solid`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `color` | color | required | Fills the whole texture. |

### `noise`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `color` | color | required | The color painted; the noise value multiplies its alpha. |
| `seed` | integer | `0` | Any 64-bit signed integer. The same seed always gives the same pattern; different seeds give unrelated patterns. |
| `scale` | number | `8` | Size of the pattern: the number of noise cells across the texture **width**, in (0, 4096]. Larger values give smaller features. |
| `octaves` | integer | `1` | 1–8 layers of detail. Each octave has twice the cells of the previous one and half its weight. |

Value noise: octave o (0, 1, …) is a grid of cells whose corners hold random values in
[0, 1) from a hash of (`seed`, o, corner column, corner row). A pixel's value in one octave
is the smooth interpolation of the four corners around its center, with weights t²·(3 − 2t)
of its position t inside the cell in each direction. The octaves are summed with weights
1, 1/2, 1/4, … and divided by the sum of the weights, so the noise value n stays in [0, 1).
The layer paints `color` with alpha = color alpha × n × `opacity`.

Cells across the texture:

- **`tiling` false:** octave o has `scale` · 2^o cells across the width. Cells are square,
  so the height has `scale` · 2^o · height / width cells.
- **`tiling` true:** the noise must repeat exactly, so the cell counts must be whole
  numbers. `scale` is rounded to the nearest integer S (halves round up; at least 1),
  the height gets Sy = max(1, round(height · S / width)) cells, and octave o has
  S · 2^o × Sy · 2^o cells whose corner values wrap around (the last cell of a row
  interpolates towards the first corner of the row, and likewise for columns). Every
  octave, and therefore the sum, repeats with a period of exactly the texture size. Cells
  are square when height · S / width is a whole number.

Example: `"scale": 16, "octaves": 3` on a 256 × 256 texture gives cells of 16, 8 and 4
pixels. A dark `multiply` noise at low opacity is the usual way to add grain or dirt.

### `stripes`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `width` | number | required | Width of one band in pixels, > 0 (need not be an integer). |
| `angle_deg` | number | `0` | Direction along which the bands follow each other. 0: the color changes along x, so the bands are **vertical**; 90: **horizontal** bands. |
| `colors` | [color, …] | required | 2–64 colors, used in order and repeated. |

For a sample point (X, Y), t = X · cos(angle) + Y · sin(angle) is its distance along the
direction from the top-left corner. It lies in band k = floor(t / `width`), which has color
`colors[k mod n]` (n colors; the index is never negative). Band 0 starts exactly at the
origin. Colors may be transparent: `["#00000000", "#00000040"]` darkens every other band.
To draw thin lines between wide bands, repeat a color: `"width": 2` with 15 transparent
colors followed by one dark color gives a 2-pixel line every 32 pixels.

### `rect`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `xy` | [x, y] | required | Top-left corner in pixels (any finite numbers, may be negative). |
| `size` | [width, height] | required | Size in pixels, each > 0. The rectangle covers x to x + width and y to y + height. |
| `color` | color | required | Fill or outline color. |
| `corner` | number | `0` | Corner radius in pixels, ≥ 0. Clamped to half the smaller side, so a very large value makes a pill or, for a square, a circle. |
| `outline` | number | `0` | ≥ 0. `0` fills the rectangle. A positive value draws only an outline of that width, **inside** the rectangle: its outer edge is the rectangle's edge, its inner edge is the rectangle inset by `outline` on every side, with corner radius max(`corner` − `outline`, 0). An outline of at least half the smaller side fills the rectangle. |

### `circle`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `center` | [x, y] | required | Center in pixels (any finite numbers). |
| `radius` | number | required | Radius in pixels, > 0. |
| `color` | color | required | Fill or outline color. |
| `outline` | number | `0` | ≥ 0. `0` fills the disk. A positive value draws a ring **inside** the circle, from radius − `outline` to `radius`. An outline ≥ `radius` fills the disk. |

### `gradient`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `from` | color | required | Color at the start. |
| `to` | color | required | Color at the end. |
| `angle_deg` | number | `0` | Direction from `from` to `to`. 0: left → right; 90: top → bottom; 45: top-left → bottom-right; 180: right → left. |

A linear gradient across the whole texture. With direction D = (cos(angle), sin(angle))
and P a pixel center, the position is t = (P · D − t0) / (t1 − t0), where t0 and t1 are the
smallest and largest P · D over the centers of the four corner pixels. So the first
pixel(s) along the direction are exactly `from` and the last exactly `to` (a texture
one pixel long in that direction is `from`). Colors are mixed with premultiplied alpha:
alpha = (1 − t) · from.alpha + t · to.alpha and color = ((1 − t) · from.alpha · from.rgb +
t · to.alpha · to.rgb) / alpha, so fading from `"#ffffff"` to `"#ffffff00"` stays white
and fading `"#ff000000"` to `"#0000ff"` never shows red. The gradient does not repeat
when `tiling` is true.

### `checker`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `cells` | integer | required | Cells per side, 1–4096: `cells` columns across the width and `cells` rows across the height (cells are not square when the texture is not). |
| `colors` | [color, color] | required | Exactly 2 colors. The top-left cell has the first. |

A pixel takes the color of the cell containing its center: column = floor((x + 0.5) ·
cells / width), row = floor((y + 0.5) · cells / height), color = `colors[(column + row) mod
2]`. There is no anti-aliasing; use sizes that are multiples of `cells` for sharp, equal
cells. A tiling checker repeats seamlessly only when `cells` is even (with an odd count the
last and first cells of a row have the same color).

### `image`

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `path` | string | required | A PNG file relative to the assets directory, for example `"textures/src/logo.png"`. |
| `fit` | string | `"contain"` | How the image is placed: `contain`, `cover` or `stretch` (below). An empty string also means `contain`. |

`path` rules: forward slashes only; no leading `/`, no drive letter or `:`, no `..`
segment (the file must be inside the assets directory), no `.` or empty segments, no
trailing `/`; the name must end in `.png` (any case). The file must be a PNG image (any
color type; 16-bit channels are reduced to 8 bits) of at most 8192 × 8192 pixels. It is
read when the texture is compiled; `veduta cook` recompiles the texture when the PNG
changes.

Placement, for a W × H texture and an iw × ih image:

- `contain`: scaled by min(W / iw, H / ih), keeping its aspect ratio, and centered; the
  whole image is visible and the uncovered bands (letterbox) are transparent.
- `cover`: scaled by max(W / iw, H / ih), keeping its aspect ratio, and centered; the
  image fills the texture and the parts that overflow are cut off.
- `stretch`: scaled to exactly W × H; the aspect ratio is not kept.

Sampling: image pixels are interpolated bilinearly (image pixel centers at half-integer
positions, edges clamped, premultiplied alpha so transparent pixels do not darken their
neighbours). When the image is shrunk by a factor f > 1 along an axis, each texture pixel
averages ceil(f) evenly spaced bilinear samples along that axis, so large images do not
alias. The layer's coverage at a pixel is the fraction of the pixel's area covered by the
placed image (letterbox edges are anti-aliased); the image's own alpha multiplies it. The
image never repeats, even when `tiling` is true. An image exactly as large as the texture
is copied pixel for pixel (fully transparent pixels become transparent black).

## Tiling

With `"tiling": true` the compiled texture uses repeat addressing, and `noise` layers are
built to wrap (see [noise](#noise)). The other layers are painted once, as described; they
do not wrap around the edges. To keep a tiling texture seamless (`inspect texture`
reports `TEX_SEAM` otherwise):

- keep `rect` and `circle` layers inside the texture, or draw them identically at both
  edges (a `rect` outline around the whole texture meets itself across the seam);
- use `stripes` at 0° or 90° whose period (`width` × number of colors) divides the
  texture width (0°) or height (90°);
- use an even `cells` for `checker`;
- avoid `gradient` and `image` layers, or keep them faint.

## Mipmaps

When `mipmaps` is `true`, level 0 is the rendered image and every next level halves each
dimension (rounding down, never below 1) until 1 × 1. Each pixel of level n + 1 is the
rounded average of a 2 × 2 block of level n (an odd last row or column is averaged with
itself). The average uses straight colors, so the black of fully transparent pixels
darkens the edges of transparent areas in small levels; put an opaque layer at the
bottom when the texture has no intended transparency.

## What the compiler produces

`asset.Texture`: `Name`; `Data.Levels` (level 0 is `size`; pixels are 32-bit
`0xAARRGGBB`, straight alpha, rows top to bottom); `Data.Wrap` (repeat when `tiling`,
else clamp); `Tiling`; `Layers` (the number of source layers). Its binary layout in
`.vda` files is in `docs/vda.md` (chunk `TEXR`). Compilation is deterministic: the same
source and the same image files always produce the same bytes.

## Errors

Decoding is strict. Unknown fields (typos), wrong JSON types (for example `2.5` where an
integer is expected) and duplicate keys are errors. Validation then reports **every**
problem at once, each with the file, line, column and JSON path of the offending value.
For example, `wall.vtex` containing

```json
{
  "veduta": "texture/1",
  "size": [256, 0],
  "layers": [
    { "type": "solid", "color": "#8a5a2b", "radius": 4 },
    { "type": "noise", "color": "#000000", "octaves": 12, "opacity": 2 },
    { "type": "circle", "center": [128, 128], "color": "#ff0000" },
    { "type": "image", "path": "../logo.png" }
  ]
}
```

reports

```
wall.vtex:3:17: size[1]: 0 out of range [1, 4096]
wall.vtex:5:54: layers[0].radius: not used by layer type solid
wall.vtex:6:55: layers[1].octaves: 12 out of range [1, 8]
wall.vtex:6:70: layers[1].opacity: 2 out of range [0, 1]
wall.vtex:7:5: layers[2].radius: is required
wall.vtex:8:32: layers[3].path: "../logo.png": must not leave the assets directory ("..")
```

Checks: required fields present; `size` integers in range; 1–64 layers; `type` known;
fields the type does not use absent; colors valid; numbers finite and in range (an
explicit `0` for `octaves` or `cells` is out of range, not "default"); enum values from
the lists above; image paths valid, and the file present, a PNG and not too large.

## Limits

| Item | Range |
|------|-------|
| `size` | 1–4096 per side |
| layers | 1–64 |
| `opacity` | 0–1, default 1 |
| noise `scale` | (0, 4096], default 8 |
| noise `octaves` | 1–8, default 1 |
| stripes `colors` | 2–64 |
| checker `cells` | 1–4096 |
| image file | PNG, 1–8192 pixels per side |

## Full examples

The example of the specification. The checker is opaque, so it hides every layer below
it; only the checker and the logo are visible in the result:

```json
{
  "veduta": "texture/1",
  "size": [256, 256],
  "tiling": true,
  "mipmaps": true,
  "layers": [
    { "type": "solid",    "color": "#8a5a2b" },
    { "type": "noise",    "seed": 7, "scale": 16, "octaves": 3, "color": "#000000", "opacity": 0.25, "blend": "multiply" },
    { "type": "stripes",  "width": 8, "angle_deg": 90, "colors": ["#00000000", "#00000030"] },
    { "type": "rect",     "xy": [16, 16], "size": [224, 224], "color": "#ffffff20", "corner": 6, "outline": 2 },
    { "type": "circle",   "center": [128, 128], "radius": 40, "color": "#ff0000" },
    { "type": "gradient", "from": "#ffffff", "to": "#000000", "angle_deg": 45, "opacity": 0.3 },
    { "type": "checker",  "cells": 8, "colors": ["#c0c0c0", "#404040"] },
    { "type": "image",    "path": "textures/src/logo.png", "fit": "contain" }
  ]
}
```

A seamless wooden crate side: grain from two noises, horizontal planks separated by thin
dark lines, a dark frame and a slight vertical light falloff:

```json
{
  "veduta": "texture/1",
  "size": [128, 128],
  "tiling": true,
  "layers": [
    { "type": "solid", "color": "#9c6b3c" },
    { "type": "noise", "seed": 3, "scale": 2, "octaves": 5, "color": "#5a3a1a", "opacity": 0.7 },
    { "type": "noise", "seed": 9, "scale": 32, "octaves": 2, "color": "#000000", "opacity": 0.15, "blend": "multiply" },
    { "type": "stripes", "width": 2, "angle_deg": 90,
      "colors": ["#00000000", "#00000000", "#00000000", "#00000000", "#00000000", "#00000000",
                 "#00000000", "#00000000", "#00000000", "#00000000", "#00000000", "#00000000",
                 "#00000000", "#00000000", "#00000000", "#2a1a0acc"] },
    { "type": "rect", "xy": [0, 0], "size": [128, 128], "color": "#3a2410", "outline": 4 },
    { "type": "gradient", "from": "#ffffff", "to": "#000000", "angle_deg": 90, "opacity": 0.12, "blend": "screen" }
  ]
}
```

A HUD badge with transparency (not tiling): a translucent rounded panel, a ring and a
logo letterboxed inside:

```json
{
  "veduta": "texture/1",
  "size": [128, 64],
  "mipmaps": false,
  "layers": [
    { "type": "rect", "xy": [0, 0], "size": [128, 64], "color": "#10182080", "corner": 12 },
    { "type": "rect", "xy": [0, 0], "size": [128, 64], "color": "#e9c46a", "corner": 12, "outline": 2 },
    { "type": "circle", "center": [32, 32], "radius": 22, "color": "#ffffff", "outline": 3 },
    { "type": "image", "path": "textures/src/logo.png", "fit": "contain", "opacity": 0.9 }
  ]
}
```
