# Input

The console has a D-pad, **A**, **B**, **Select**, **Cancel** and **Home**. A game sees the
first eight; Home always returns to the console's menu and never reaches the game.

| Button | Name in Lua and scenarios | Meant for |
|--------|---------------------------|-----------|
| D-pad | `"up"`, `"down"`, `"left"`, `"right"` | moving, choosing |
| A | `"a"` | the main action, confirm |
| B | `"b"` | the second action (jump, run, shoot) |
| Select | `"select"` | the game's menu, pause |
| Cancel | `"cancel"` | back, close a menu |

## Reading buttons

| Function | True when |
|----------|-----------|
| `input.down(b)` | the button is held this tick |
| `input.pressed(b)` | it went down this tick |
| `input.released(b)` | it went up this tick |
| `input.dpad()` | returns x, y: x is −1 (left), 0 or 1 (right), y is −1 (down), 0 or 1 (up) |

```lua
kinds.ship = {
  update = function(e)
    local dx, dy = input.dpad()
    e:move(dx * 6 * engine.dt, dy * 6 * engine.dt, 0)
    if input.pressed("a") then
      scene.spawn{kind = "shot", model = "quad", material = "shot",
        position = {e.x, e.y + 0.5, 1}, scale = {0.2, 0.4, 1}, tags = {"shot"}}
    end
    e.state.charging = input.down("b")
  end,
}
```

Use `pressed` for actions that happen once per press (jump, shoot, confirm) and `down` for
actions that last while the button is held (run, charge). Opposite D-pad directions held
together cancel out; a diagonal is (±1, ±1), so normalise it if diagonal movement must not
be faster.

Input is read once per tick. A button pressed and released within one tick still shows in
`pressed` and `released`, but not in `down`.

## On the PC

The simulator maps the console's buttons to the keyboard, and any gamepad works too:

| Console | Keyboard | Gamepad |
|---------|----------|---------|
| D-pad | arrows, or W A S D | D-pad (or the stick, on a pad whose D-pad is not four buttons) |
| A | Space or Z | A |
| B | X or Shift | B |
| Select | Enter or Tab | Start |
| Cancel | Escape or Backspace | none |
| Home | Ctrl+Q | Home, or Select |

A gamepad's Select leaves the game, as Home does, and its Start is the game's menu. A
gamepad has no Cancel, so a menu should close with Select (Start on the pad) or B as well.

## Menus

A menu is a variable holding the selected entry, moved by the D-pad and confirmed by A:

```lua
local entries = {"RESUME", "RESTART", "QUIT"}
local menu_open, choice = false, 1

function game.update()
  if input.pressed("select") then
    menu_open = not menu_open
    choice = 1
    return
  end
  if not menu_open then
    return
  end
  if input.pressed("down") then
    choice = choice % #entries + 1
  elseif input.pressed("up") then
    choice = (choice - 2) % #entries + 1
  elseif input.pressed("cancel") or input.pressed("b") then
    menu_open = false
  elseif input.pressed("a") then
    menu_open = false
    if entries[choice] == "RESTART" then
      scene.load(scene.name())
    elseif entries[choice] == "QUIT" then
      scene.load("title")
    end
  end
end

function game.draw()
  if not menu_open then
    return
  end
  hud.rect(100, 70, 120, 100, "#101018e0")
  for i, label in ipairs(entries) do
    local color = (i == choice) and "#f4c542" or "#a0a0b0"
    hud.text(124, 70 + 20 * i, (i == choice and "> " or "  ") .. label, color)
  end
end
```

Every kind should skip its `update` while `menu_open` is true, so the game pauses behind the
menu.

## In tests

Scenarios press and release the same button names at given ticks (see [Testing](Testing)).
Home cannot be pressed by a scenario, since no game ever sees it.
