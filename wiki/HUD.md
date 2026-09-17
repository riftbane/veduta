# HUD

The HUD is what is drawn over the scene: score, lives, menus, dialogue. It is drawn in
`game.draw`, which runs once per frame after the scene, and only there.

Coordinates are **pixels** from the top-left corner of the frame, which is 320 × 240 on the
console. `engine.width` and `engine.height` give the frame's size inside `game.draw`.

| Function | Draws |
|----------|-------|
| `hud.text(x, y, text [, color [, scale]])` | text in the built-in 8 × 8 font; returns the width drawn in pixels |
| `hud.rect(x, y, w, h [, color])` | a filled rectangle |
| `hud.image(texture, x, y [, options])` | a texture, or a part of it, with sharp texels |
| `hud.panel(texture, x, y, w, h, border [, options])` | a frame that stretches to any size |
| `hud.image_size(texture)` | a texture's width and height in texels |

A colour is `"#rrggbb"`, `"#rrggbbaa"` (with alpha, for translucent panels) or an integer
`0xrrggbb`. The default is white. `scale` is a whole number: 2 draws 16 × 16 characters.

```lua
local score, lives = 0, 3

function game.draw()
  -- a translucent bar across the top
  hud.rect(0, 0, engine.width, 14, "#000000b0")
  hud.text(4, 3, "SCORE " .. score, "#f4f0e0")

  -- lives as small squares on the right
  for i = 1, lives do
    hud.rect(engine.width - 12 * i, 3, 8, 8, "#ff6060")
  end
end
```

## Centering text

Every character is 8 pixels wide times the scale:

```lua
local function centered(y, text, color, scale)
  scale = scale or 1
  local w = 8 * scale * #text
  hud.text((engine.width - w) // 2, y, text, color, scale)
end
```

`#text` counts bytes: keep HUD text to ASCII. The font has every printable ASCII character
(space to `~`): upper and lower case letters, digits and punctuation.

## Bars

```lua
local function bar(x, y, w, h, value, max, color)
  hud.rect(x, y, w, h, "#303040")
  hud.rect(x + 1, y + 1, math.floor((w - 2) * value / max), h - 2, color)
end
```

## What to keep out of draw

`game.draw` runs once per frame shown: never change the game there. Everything that
changes state belongs in `game.update` or a kind, so tests (which may draw fewer frames, or
none) play the same game as the player. Read variables in `draw`, write them in `update`.

## Images and icons

`hud.image` draws a texture asset (see [Graphics Assets](Graphics-Assets#textures)), whole or
a part of it. Keep the icons of a game in one sheet and pick each with `src`, the part's
`{x, y, w, h}` in texels:

```lua
local ICON = 16   -- the sheet is a grid of 16 × 16 icons

local function icon(col, row, x, y)
  hud.image("icons", x, y, {src = {col * ICON, row * ICON, ICON, ICON}})
end

function game.draw()
  icon(0, 0, 4, 4)                                  -- a heart
  hud.image("portrait_mira", 8, 160, {w = 48, h = 48}) -- a 24 × 24 portrait, twice as large
  hud.image("arrow", 300, 220, {flip_x = true, color = "#ffffff80"})
end
```

| Option | Meaning |
|--------|---------|
| `src` | `{x, y, w, h}`: the part of the texture, in texels (default: all of it) |
| `w`, `h` | the size drawn, in pixels (default: the part's size) |
| `color` | multiplies the texels: `"#ff8080"` tints, `"#ffffff80"` is half transparent |
| `flip_x`, `flip_y` | mirror the image |

Texels stay sharp at any size. For pixel art, draw at whole multiples of the texture's size.

## Panels and dialogue boxes

`hud.panel` draws a nine-slice panel: a small frame texture whose corners keep their size
while the edges and the middle stretch, so one 12 × 12 texture makes boxes of any size.
`border` is the corners' width in texels, one number or `{left, top, right, bottom}`:

```lua
local function dialogue(speaker, text)
  hud.panel("frame", 8, 172, 304, 60, 4)
  hud.image("portrait_" .. speaker, 16, 180, {w = 44, h = 44})
  hud.text(68, 184, text, "#f4f0e0")
  if engine.tick % 20 < 10 then
    hud.text(292, 216, ">", "#f4c542")     -- a blinking "more" arrow
  end
end
```
