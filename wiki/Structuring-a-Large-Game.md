# Structuring a Large Game

A small game fits in `main.lua`. An RPG with towns, dungeons, a party, an inventory and
dozens of enemies does not. This page is a layout that scales, and the rules that keep a
large Lua game testable.

## Folders

```
main.lua               wiring only: kinds, screens, game.init / update / draw
game/state.lua         what a play is: party, inventory, flags, and reset()
screens/title.lua      one screen per file: enter, update, draw
screens/field.lua
screens/battle.lua
screens/menu.lua       the Select menu: party, items, save
kinds/hero.lua         one kind per file, returning its table
kinds/npc.lua
kinds/slime.lua
data/items.lua         tables of numbers and text: items, enemies, shops
data/dialogue.lua
levels/town.lua        what stands on each map: its NPCs, doors, events
lib/grid.lua           pure helpers: tiles, paths, collisions
ui/menu.lua, ui/box.lua
assets/
  materials/characters/…, materials/tiles/…, materials/ui/…
  textures/characters/…, textures/ui/…
  scenes/town.vscene, scenes/dungeons/cave_1.vscene
tests/scenarios/       flat: name them by area (town_shop_buy, battle_flee)
```

`require("screens.battle")` loads `screens/battle.lua`. Asset sources can sit in folders
under their kind's directory; an asset's name is still its file name, so keep names unique
(prefixes help: `ui_heart`, `slime_idle_1`). Scenarios stay in one flat folder.

## main.lua only wires

```lua
kinds.hero  = require("kinds.hero")
kinds.npc   = require("kinds.npc")
kinds.slime = require("kinds.slime")

local screens = {
  title  = require("screens.title"),
  field  = require("screens.field"),
  battle = require("screens.battle"),
  menu   = require("screens.menu"),
}
local current

local function go(name, ...)
  current = screens[name]
  current.enter(go, ...)
end

function game.init()   go("title") end
function game.update() current.update() end
function game.draw()   current.draw() end
```

Registering every kind in `main.lua` shows at a glance what exists, and no module can
replace another's kind by accident. Passing `go` to each screen lets screens switch
screens without requiring each other.

## One shared state

`require` runs a module once, so every file that requires `game.state` gets the same table:
the natural home of what a play is.

```lua game/state.lua
local state = {}

function state.reset()
  state.party = {{name = "Mira", hp = 30, max_hp = 30}}
  state.items = {potion = 2}
  state.gold = 0
  state.flags = {}
end

state.reset()
return state
```

Put it in one place and reset it in one place: `scene.load` does not reset Lua variables.

## A screen

```lua screens/field.lua
local state = require("game.state")
local box = require("ui.box")

local field = {}
local go

function field.enter(switch, map)
  go = switch
  scene.load(map or "town")
end

function field.update()
  if input.pressed("select") then
    go("menu")
  end
end

function field.draw()
  box.status(state.party[1])
end

return field
```

A screen owns its part of the flow; kinds own the behaviour of entities. When a screen
grows, split it (`screens/battle/turns.lua`, `screens/battle/ui.lua`).

## Data in tables

Items, enemies, shops and dialogue are data. Keep them in modules that return plain tables,
and keep logic out of them:

```lua data/items.lua
return {
  potion = {name = "Potion", price = 20, heal = 15, icon = {0, 1}},
  ether  = {name = "Ether",  price = 50, mana = 10, icon = {1, 1}},
}
```

Plain tables are easy to check in a scenario (copy a value into an entity's `state`) and
easy to balance without touching code.

## Rules that keep it working

- **No globals besides `game` and `kinds`.** Every module uses `local` and returns a table.
- **No require cycles.** If `a` requires `b` and `b` requires `a`, one of them gets `true`
  instead of the module. Move what both need into a third module.
- **`draw` only reads.** Every change of state happens in `update` or in kinds; tests draw
  few frames or none.
- **Iterate in a known order.** `pairs` follows insertion order, which is deterministic but
  depends on how a table was built. For lists, use arrays and `ipairs`.
- **A scenario per feature**, named by area. When you split a file, `veduta test` tells you
  at once whether anything changed, down to the golden trace.
- **Invariants for the rules of the game**: gold never negative, hp between 0 and max, no
  item count below zero. Fuzzing then hunts for the inputs that break them.

## Sequences with coroutines

Cutscenes and dialogue that wait read best as a sequence. A coroutine runs until it
yields, and the next resume carries on where it stopped: resume it once per tick and
`wait(n)` is n ticks.

```lua cutscenes/bridge.lua
local say = require("ui.say")

local function wait(ticks)
  for _ = 1, ticks do coroutine.yield() end
end

local function wait_for(button)
  repeat coroutine.yield() until input.pressed(button)
end

return function()
  say("Mira", "The bridge is out.")
  wait_for("a")
  say("Old man", "Take the cave path. And mind the bats.")
  wait_for("a")
  say(nil)
  local hero = scene.find("hero")
  for _ = 1, 20 do          -- walk right for a second
    hero:move(0.1, 0, 0)
    coroutine.yield()
  end
  wait(10)
end
```

```lua
local cutscene   -- the running cutscene, or nil

local function play(fn)
  cutscene = coroutine.create(fn)
end

function game.update()
  if cutscene then
    local ok, err = coroutine.resume(cutscene)
    if not ok then error(err) end
    if coroutine.status(cutscene) == "dead" then cutscene = nil end
    return   -- the game waits while a cutscene plays
  end
  if input.pressed("b") then
    play(require("cutscenes.bridge"))
  end
end
```

Coroutines are deterministic like the rest of the game: scenarios replay a cutscene tick for
tick, and snapshots restore one where it was.
