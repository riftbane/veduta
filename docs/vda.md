# Compiled container — `.vda`

`veduta cook` compiles each asset source (model, texture, material, scene) into one
`.vda` file under the project's `cooked` directory (default `assets/.cooked`). A `.vda`
file is a small chunked binary container inspired by PNG. Never edit cooked files: change
the source and cook again. Cooking is incremental: a source is recompiled only when the
hash stored in its `META` chunk no longer matches, or the compiler version changed.

Everything is **little-endian**.

## Container

```
file  = magic chunk*
magic = "VDA1"                       4 bytes, ASCII
chunk = type length payload crc
  type    4 bytes   ASCII letters and digits, case-sensitive (e.g. "MESH")
  length  u32       payload size in bytes (0 to 4294967295)
  payload length bytes
  crc     u32       CRC-32 (IEEE 802.3 polynomial, as in PNG and zip) of type + payload
```

- A file with no chunks after the magic is well formed (but not a usable asset).
- Readers verify the magic, that every chunk fits in the file, and every CRC; a truncated
  file or a CRC mismatch is an error.
- Readers skip chunk types they do not know, so later versions can add chunks.
- A cooked asset file has exactly one `META` chunk and exactly one body chunk whose type
  matches the asset kind. Writers put `META` first, then the body; readers accept any
  order.

| Chunk | Kind | Payload |
|-------|------|---------|
| `META` | all | asset metadata, canonical JSON (below) |
| `MESH` | model | compiled model |
| `TEXR` | texture | compiled texture with its mip chain |
| `MATL` | material | compiled material |
| `SCEN` | scene | compiled scene |

## `META`

UTF-8 JSON object, canonical: keys in this (sorted) order, no whitespace, no HTML
escaping, `deps` sorted without duplicates and `[]` when empty. Readers reject META that
is not byte-for-byte canonical, so equal metadata always has equal bytes.

```json
{"compiler":"veduta-asset/0.2.0","deps":[],"kind":"model","name":"crate","source":"models/crate.model.json","source_hash":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
```

| Key | Meaning |
|-----|---------|
| `compiler` | Compiler version (`asset.CompilerVersion`: `veduta-asset/0.2.0` since scene entities carry a hitbox and a layer, `veduta-asset/0.1.0` before). A different version forces a recompile. |
| `deps` | Other input files the compiled output depends on besides the source (for example the PNG of a texture `image` layer), as paths relative to the assets directory. |
| `kind` | `model`, `texture`, `material` or `scene`. |
| `name` | Asset name (the source file name without its suffix). |
| `source` | Source path relative to the assets directory, forward slashes (`models/crate.model.json`). |
| `source_hash` | Lowercase hex SHA-256 (64 characters) of the compiler inputs, computed by `cook`: SHA-256 over `veduta-cook/1\n`, the compiler version and `\n`, then `source <path> <length>\n` followed by the source bytes, then for each dependency in `deps` order `dep <path> <length>\n` followed by its bytes (`missing <path>\n` when it cannot be read). It changes whenever the source or any dependency changes. |

## Primitive encodings

| Name | Size | Encoding |
|------|------|----------|
| `u8` | 1 | unsigned byte |
| `bool` | 1 | `0` = false, `1` = true; any other value is an error |
| `u32` | 4 | unsigned 32-bit |
| `i64` | 8 | signed 64-bit, two's complement (every Go `int` field) |
| `f32` | 4 | IEEE-754 binary32 bit pattern, stored as a `u32` (exact, including −0 and NaN payloads) |
| `str` | 4 + n | `u32` byte length n, then n bytes of UTF-8, no terminator |
| `vec2` | 8 | `f32` x, `f32` y |
| `vec3` | 12 | `f32` x, `f32` y, `f32` z |
| `aabb` | 24 | `vec3` min, `vec3` max |
| `color` | 4 | `u32` 0xAARRGGBB, i.e. bytes B, G, R, A in the file |
| `list<T>` | 4 + … | `u32` count, then count elements of T |

Empty lists are written with count 0 and read back as absent (nil) lists. A payload
must be consumed exactly: missing bytes and trailing bytes are errors.

## `MESH` — compiled model

| # | Field | Type | Meaning |
|---|-------|------|---------|
| 1 | name | `str` | model name |
| 2 | pivot | `str` | `origin`, `center` or `bottom-center` |
| 3 | symmetry | `str` | `""`, `x`, `y` or `z` (symmetry check requested) |
| 4 | smooth_angle_deg | `f32` | normal smoothing angle in degrees |
| 5 | triangle_budget | `i64` | triangle budget for inspection |
| 6 | pivot_offset | `vec3` | translation applied to every vertex to honour the pivot |
| 7 | bounds | `aabb` | mesh bounding box (after the pivot offset) |
| 8 | vertices | `list<vertex>` | vertex = `vec3` position, `vec3` normal, `vec2` uv (32 bytes) |
| 9 | indices | `list<u32>` | triangle vertex indices, 3 per triangle, counter-clockwise front faces |
| 10 | mesh_parts | `list<mesh_part>` | mesh_part = `i64` first index, `i64` index count, `i64` material (index into materials) |
| 11 | materials | `list<str>` | material names in first-use order; `""` = the entity's material or the default |
| 12 | parts | `list<part>` | one per source part, see below |

part = `i64` index (source part index), `str` shape, `i64` first index, `i64` index
count, `str` material (`""` for none), `str` uv mapping, `bool` flip_normals, `i64` of
(mirror: mirrored source part index; −1 otherwise).

Readers check: the index count is a multiple of 3; every index is below the vertex count;
every mesh part and part range lies within the indices and its count is a multiple of 3;
every mesh part's material is a valid index into materials (or 0 when materials is
empty); every `of` is ≥ −1.

## `TEXR` — compiled texture

| # | Field | Type | Meaning |
|---|-------|------|---------|
| 1 | name | `str` | texture name |
| 2 | tiling | `bool` | the source asked for a tileable texture |
| 3 | layers | `i64` | number of source layers |
| 4 | width | `u32` | width of level 0 in pixels |
| 5 | height | `u32` | height of level 0 in pixels |
| 6 | levels | `u32` | number of mip levels (1 without mipmaps) |
| 7 | wrap | `u8` | `0` = repeat (tiling), `1` = clamp |
| 8 | level × levels | | for level i = 0 … levels−1: `u32` w, `u32` h, then w × h `color` pixels, row-major, top row first |

Readers check: level i measures exactly max(1, width >> i) × max(1, height >> i); levels
is at most 1 + ⌊log₂ max(width, height)⌋ (the chain stops at 1 × 1); width and height
are 1 to 16777216 when levels > 0 and both 0 when levels = 0; wrap is 0 or 1.

## `MATL` — compiled material

| # | Field | Type | Meaning |
|---|-------|------|---------|
| 1 | name | `str` | material name |
| 2 | albedo | `color` | base color |
| 3 | texture | `str` | texture name, `""` for none |
| 4 | unlit | `bool` | ignore the light |
| 5 | alpha | `str` | `opaque`, `blend` or `cutout` (anything else is an error) |
| 6 | cutoff | `f32` | alpha threshold used by `cutout` |
| 7 | cull | `u8` | `0` = back, `1` = none |
| 8 | filter | `u8` | `0` = bilinear, `1` = nearest |

## `SCEN` — compiled scene

| # | Field | Type | Meaning |
|---|-------|------|---------|
| 1 | name | `str` | scene name |
| 2 | camera.ortho | `bool` | orthographic camera |
| 3 | camera.fov_deg | `f32` | vertical field of view in degrees (0 for orthographic) |
| 4 | camera.size | `f32` | orthographic visible height in meters (0 for perspective) |
| 5 | camera.near | `f32` | near plane distance |
| 6 | camera.far | `f32` | far plane distance |
| 7 | camera.position | `vec3` | eye position |
| 8 | camera.look_at | `vec3` | target point |
| 9 | light.direction | `vec3` | direction the light travels, as written in the source |
| 10 | light.color | `vec3` | linear RGB in [0, 1] |
| 11 | light.ambient | `vec3` | linear RGB in [0, 1] |
| 12 | background | `color` | clear color |
| 13 | entities | `list<entity>` | in scene-file order |

entity = `str` name, `str` kind, `str` model (`""` for none), `str` material (`""` for
none), `vec3` position, `vec3` rotation_deg, `vec3` scale, `list<str>` tags, `str` parent
(`""` for none), `bool` visible, `bool` has_hitbox, then only when has_hitbox is true
`aabb` hitbox (local space, min <= max on every axis; readers reject any other box), then
`i64` layer (in [-1000, 1000]).

## Determinism

Encoding the same compiled asset twice yields identical bytes, and decoding then
re-encoding a file reproduces it byte for byte: every field has one encoding, floats are
stored as raw bits and META is canonical.
