# Saves

A save keeps what must outlive a play: progress, settings, high scores. It is a Lua table of
numbers, strings, booleans and tables of them, stored under a name.

```lua
local state = {gold = 0, level = 1, party = {"mira"}, flags = {}}

function game.init()
  local saved = save.read("slot1")
  if saved then
    state = saved
  end
end

local function checkpoint()
  local ok, err = save.write("slot1", state)
  if not ok then
    message = "COULD NOT SAVE"   -- a full or missing card: the game goes on
    print(err)
  end
end
```

| Function | Does |
|----------|------|
| `save.write(name, table)` | stores the table, replacing the save of that name; `true`, or `nil` and a message |
| `save.read(name)` | a new table with what was saved, or `nil` when there is no such save |
| `save.remove(name)` | deletes the save; `true`, or `nil` and a message |
| `save.list()` | the names of the saves, sorted |

Names follow the asset rule: 1 to 64 characters of `a-z`, `0-9`, `_` and `-` (`slot1`,
`settings`, `best_times`).

## What a save can hold

- Numbers, strings, booleans, and tables of them, nested up to 64 deep.
- A table is either a list (`{"mira", "tobi"}`) or has string keys (`{gold = 3}`).
- Integers come back as integers and floats as floats: `3` stays `3`, `3.0` stays `3.0`.
- A save is at most 1 MiB once written.

Functions, entities, coroutines, a table that contains itself, keys that are neither strings
nor part of a list, `nan` and infinities are errors when writing: keep entity names or
positions in the save, not entities.

A table read back is a new table, with its keys in sorted order.

## Where saves live

| Where the game runs | Saves |
|---------------------|-------|
| The console | on its SD card, `saves/<game>/<name>.json` |
| The simulator (F5) | `out/saves/<name>.json` in the project |
| `VEDUTA_SAVE_DIR` set | in that folder |
| Tests, `simulate`, `fuzz`, `render` | in memory only: see below |

A save is a small JSON file you can read and edit. Writing goes to a temporary file first,
synced to the card, and the previous save is kept until the new one is in place, so a console
switched off in the middle of a save still has one of the two.

To start the simulator with no saves, delete `out/saves`. To back up the progress of a game
on the console, copy the card's `saves` folder.

## Saves in tests

A test run never reads or writes the saves of the simulator or the console, so it plays the
same everywhere. A scenario lists the saves the run starts with; what the game writes stays
in memory for that run, and each write or removal is a trace event a scenario can count:

```json
{
  "veduta": "scenario/1",
  "scene": "title",
  "ticks": 40,
  "saves": {
    "slot1": { "gold": 120, "level": 3, "party": ["mira", "tobi"], "flags": { "bridge": true } }
  },
  "inputs": [
    { "tick": 1, "press": ["a"] },
    { "tick": 2, "release": ["a"] }
  ],
  "expect": [
    { "tick": 40, "entity": "hero", "path": "state.gold", "op": "==", "value": 120 },
    { "tick": 40, "trace": "save_write", "count_max": 0 }
  ]
}
```

Write one scenario that starts from an empty game and one per save that matters: a new
game, a save in the middle of the story, a save from an older version of the game.

## Changing what a save holds

A save written by an older version of your game may lack fields the new one expects. Keep a
version number in it and fill in what is missing when reading:

```lua
local VERSION = 2

local function load_slot(name)
  local s = save.read(name)
  if not s then
    return nil
  end
  if (s.version or 1) < 2 then
    s.flags = s.flags or {}   -- added in version 2
  end
  s.version = VERSION
  return s
end
```
