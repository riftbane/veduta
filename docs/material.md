# Material — `assets/materials/<name>.mat.json`

A material says how a surface is shaded: its base color, an optional texture, whether it
is lit, how its alpha is used, which faces are drawn and how the texture is sampled.
Model parts name their material; a scene entity's `material` is used by the parts of its
model that name none. A part that ends up with no material uses the default material
(white, opaque, lit, back faces culled, bilinear filtering).

## File and name

- Location: `assets/materials/<name>.mat.json`, or a folder under it
  (`assets/materials/enemies/bat.mat.json`). Hidden folders (`.name`) are skipped.
- The material's name is the file name without `.mat.json` (`crate_wood.mat.json` →
  `crate_wood`). Names are 1–64 characters of `a-z`, `0-9`, `_` and `-`, and must start
  with a letter or a digit. Any other file name is an error. The name ignores the folder,
  so two materials of the same name in different folders are an error.
- The file is one JSON object. Decoding is strict: unknown fields, duplicate keys, wrong
  types and trailing data are errors. Every error carries `file:line:col` and the JSON path
  of the offending value, and all problems in a file are reported together.

## Fields

| Field | Type | Default | Allowed values and meaning |
|-------|------|---------|----------------------------|
| `veduta` | string | required | Must be exactly `"material/1"`. |
| `albedo` | color string | `"#ffffff"` | Base color multiplier, `"#RRGGBB"` or `"#RRGGBBAA"` (hex digits, either case). The final surface color is texel × albedo; without a texture it is the albedo. The alpha byte only matters when `alpha` is `blend` or `cutout`; `#RRGGBB` means alpha `ff`. |
| `texture` | string | none | Name of a texture asset (`assets/textures/<name>.tex.json`), for example `"crate_wood"`. Must be a valid asset name. It is a reference by name: the material compiles even if the texture does not exist yet; `inspect scene` reports missing assets. Omit the field for an untextured material. |
| `unlit` | boolean | `false` | `true` ignores the scene light: color = texel × albedo. `false` applies the directional light and ambient term (Lambert on vertex normals). |
| `alpha` | string | `"opaque"` | `opaque`: alpha is ignored, depth is written. `blend`: alpha-blended over what is behind (depth tested, not written). `cutout`: fragments whose alpha (texel alpha × albedo alpha) is below `cutoff` are discarded; the rest are drawn opaque. |
| `cutoff` | number | `0.5` | Alpha threshold for `cutout`, in the range (0, 1] (greater than 0, at most 1). Only allowed when `alpha` is `"cutout"`; setting it with any other `alpha` is an error. |
| `cull` | string | `"back"` | `back`: faces seen from behind (clockwise on screen) are not drawn. `none`: both sides are drawn — use for thin, two-sided geometry such as planes, leaves and flags. |
| `filter` | string | `"bilinear"` | Texture sampling: `bilinear` (smooth) or `nearest` (sharp texels, for pixel art). The mip level is chosen per triangle from the UV derivatives in both cases. |
| `grid` | `[columns, rows]` | none | The texture is a sprite sheet of equal frames, 1 to 256 columns and rows; an entity's `frame` (from 0, left to right then top to bottom, wrapping around) picks the one drawn: the model's texture coordinates are scaled into that cell. Needs `texture`. Give sheets `"mipmaps": false` and `"filter": "nearest"`, so neighbouring frames never bleed in. |

Enumerated values are lowercase and case-sensitive. An absent field and an empty string
both mean "use the default".

## Compiled form

The compiler produces `asset.Material`: `Name`, `Albedo` (packed BGRA8, `0xAARRGGBB`),
`Texture` (name or `""`), `Unlit`, `Alpha` (`opaque|blend|cutout`), `Cutoff` (0.5 unless
set; used only by `cutout`), `Cull` and `Filter`. Its binary layout in `.vda` files is in
`docs/vda.md` (chunk `MATL`).

## Errors (examples)

```
crate_wood.mat.json:3:13: alpha: unknown value "transparent" (want one of [opaque blend cutout])
crate_wood.mat.json:4:14: cutoff: only allowed when alpha is "cutout"
crate_wood.mat.json:2:13: albedo: color "#fff": want #RRGGBB or #RRGGBBAA
```

## Examples

Full example (every field set):

```json
{
  "veduta": "material/1",
  "albedo": "#ffffffff",
  "texture": "leaves",
  "unlit": false,
  "alpha": "cutout",
  "cutoff": 0.4,
  "cull": "none",
  "filter": "nearest"
}
```

Typical textured material (everything else default):

```json
{ "veduta": "material/1", "texture": "crate_wood" }
```

Semi-transparent glass tint, unlit:

```json
{ "veduta": "material/1", "albedo": "#80c0ff60", "alpha": "blend", "unlit": true }
```
