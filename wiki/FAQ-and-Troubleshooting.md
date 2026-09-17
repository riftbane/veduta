# FAQ and Troubleshooting

## Installing

### Veduta creates Go projects

`veduta version` prints `v1.x`: the tool came from the **stable** channel, which still
serves Veduta v1 (games in Go) until v2.0.0 is released. Reinstall from the beta channel,
in PowerShell:

```powershell
$env:VEDUTA_CHANNEL = "beta"
irm https://raw.githubusercontent.com/riftbane/veduta/main/install.ps1 | iex
```

or, with the tool already installed, `veduta update --channel beta`. Open a new terminal
and check `veduta version` again. A project created by v1 stays a Go project: create a new
one.

### `veduta` is not recognised

The installer added its folder to your user PATH, which only new terminals see. Close the
terminal (and VS Code, whose terminals inherit its environment) and open it again.

### The Lua extension shows no completion

Accept the recommendation to install **Lua** (`sumneko.lua`) when the project opens, or
install it from the Extensions panel. The project's `.luarc.json` points it at the API
definitions in `.veduta/lua/`. After updating the tool, run `veduta upgrade` in the
project to refresh them.

## Writing the game

### Missing standard functions

Veduta's Lua is Lua 5.4 without the parts that would break determinism or reach outside the
game (`io`, `os`, `debug`, `load`, `goto`), and without `string.pack`, `string.unpack` and
`string.dump`, which have no use without file access. An engine before v2.0.0-rc.4 also
lacked `math.deg` and `math.rad`, and one before rc.5 the `coroutine` library: update it
(`veduta update`), then `veduta upgrade` in the project so the editor stops marking
`coroutine` as missing.

### Accented letters show as `?`

The font covers ASCII, Latin-1 and Windows-1252's extra characters from v2.0.0-rc.5. Other
scripts (Greek, Cyrillic, Japanese, emoji) are not in it. Save scripts as UTF-8.

### An asset in a folder is not found

Folders under an asset kind's directory work from v2.0.0-rc.4. The asset's name is its file
name, whatever the folder: two files of the same name in different folders are an error
that names both.

### "entity has no field …"

Entities accept only their own fields (`x`, `y`, `z`, `visible`, `model`, `material`,
`layer`, `frame`, `parent`, `hitbox`, `state`). Keep your values in `e.state`:
`e.state.speed = 3`.

### My variables survive `scene.load`

`scene.load` resets the entities, not the Lua variables. Reset them where a level starts:
in the kind `init` of an entity the level's scene contains, or next to the `scene.load`
call.

### Two sprites never collide

They are at different z. A `quad` is 0.02 units deep, so sprites overlap only when their z
are close. Put them at the same z, or give them hitboxes with depth in the scene file. See
[2D Games](2D-Games#collisions).

### A sprite is invisible

- It faces away: a `plane` part faces +Y and is edge-on to a 2D camera. Use `quad`.
- It is behind the camera's range: a 2D camera at z = 100 sees z from −100 to 99.9.
- It is behind an opaque sprite at a larger z, or a translucent sprite on a lower layer.
- `visible` is `false`, or its material names a texture that does not exist
  (`veduta inspect scene NAME` lists missing assets).

### The game behaves differently in a test

Something depends on what is not part of the run: `game.draw` changing state (it runs once
per frame, and tests may draw no frames), or a table iterated with an order that changed
because keys were inserted in a different order. Move every change of state into
`game.update` or kinds.

## Testing

### `golden mismatch` after a change

The change altered something in the run. Open the scenario's contact sheet under
`out/runs/` and the trace; if the new behaviour is right, record it with
`veduta test --update-golden` and commit `tests/golden/`.

### A scenario's expectation on `position.x == 5` fails at 4.9999995

Positions are float32. Compare with a margin: `>` 4.99 and `<` 5.01.

## Current limits (v2.0 release candidates)

These are known and planned, not bugs in your game:

- **No sound** yet.
- While the debugger is stopped at a breakpoint, the simulator window is not redrawn.
- The simulator window exists on Windows only; Linux and macOS have every headless command.

## Getting help

Open an issue at <https://github.com/riftbane/veduta/issues> with `veduta --json doctor`
output, and a scenario that shows the problem when you can: it replays the same way on any
machine.
