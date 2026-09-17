# HUD

The HUD is what is drawn over the scene: score, lives, menus, dialogue. It is drawn in
`game.draw`, which runs once per frame after the scene, and only there.

Coordinates are **pixels** from the top-left corner of the frame, which is 320 × 240 on the
console. `engine.width` and `engine.height` give the frame's size inside `game.draw`.

| Function | Draws |
|----------|-------|
| `hud.text(x, y, text [, color [, scale]])` | text in the built-in 8 × 8 font; returns the width drawn in pixels |
| `hud.rect(x, y, w, h [, color])` | a filled rectangle |

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

## Current limits

The HUD has text and rectangles only: no images yet. For an icon or a portrait on screen,
use a sprite entity close to the camera (see [2D Games](2D-Games)), positioned relative to
the camera each tick.
