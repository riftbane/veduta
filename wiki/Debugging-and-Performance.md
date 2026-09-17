# Debugging and Performance

## The debugger

In VS Code, **F5** plays the game in the simulator under the debugger; **Ctrl+F5** plays it
without. No configuration is needed.

- **Breakpoints**: click left of a line number in any `.lua` file. The game stops when it
  reaches the line.
- **Stepping**: F10 steps over, F11 into a call, Shift+F11 out, F5 continues.
- **Variables**: the Run and Debug panel shows the locals, upvalues and globals of each
  frame of the stack, and entities opened field by field (position, tags, state).
- **Debug Console**: `print(...)` output and error messages appear here.

While the game is stopped at a breakpoint, the simulator window is not redrawn; it catches
up when you continue.

To debug a scenario instead of live play, **Run → Add Configuration → Veduta: Scenario**
writes:

```json fragment
{
  "type": "veduta",
  "request": "launch",
  "name": "Scenario",
  "project": "${workspaceFolder}",
  "mode": "scenario",
  "scenario": "start"
}
```

Set `scenario` to the scenario's name and press F5: the run is the same every time, so a
bug that shows in a scenario can be stepped through as often as needed.

## Errors

A Lua error stops the run and names the file, the line and the call stack:

```
main.lua:42: attempt to index a nil value (local 'hero')
stack traceback:
	main.lua:42: in function 'game.update'
```

In the simulator the window stays open with the error written over the last frame (in
the terminal and the Debug Console too); fix the script and save it, and the game
reloads. The same happens to a scene file or a texture that does not compile.

The most common ones:

| Message | Usually means |
|---------|---------------|
| `attempt to index a nil value (local 'hero')` | `scene.find` returned `nil`: a misspelled name, or the entity was despawned |
| `entity has no field 'speed' (…keep your own values in state)` | a value set on an entity: keep it in `e.state.speed` |
| `entity "x": kind "enemy" is not registered` | a scene names a kind no `kinds.enemy` defines |
| `the script ran for more than 20000000 steps without returning (an endless loop?)` | a loop that never ends in one callback |
| `bad argument #1 to 'input' (unknown button "Up" …)` | button names are lowercase: `"up"` |
| `attempt to call a nil value (field 'deg')` | a standard function Veduta's Lua lacks, see [FAQ](FAQ-and-Troubleshooting#missing-standard-functions) |

A syntax error shows in the Problems panel as soon as the file is saved (**Veduta: Build**
checks every script and asset).

## The simulator's own keys

| Key | Does |
|-----|------|
| F1 | shows the time each tick takes against the console's budget, and the triangles drawn |
| F5 | starts and stops recording the session as a scenario |
| F9 | reloads scripts and assets and restarts the game from the start |
| Ctrl+Q | the console's Home: leaves the game |

Saving any script or asset also reloads, **in place**: the game restarts in the scene it
was in (a world, around the cell the player stands in), with the same seed. See
[Editing a scene while it shows](Scenes#editing-a-scene-while-it-shows).

## Looking at frames without playing

`veduta render --scene NAME --tick T --out frame.png` draws a frame headless;
`--mode wireframe`, `depth`, `ids`, `overdraw` or `collision` show what the renderer sees.
A scenario's `screenshots` give a contact sheet of several ticks at once
([Testing](Testing)).

## Performance

The console runs 20 ticks a second: each tick has **50 ms** for the game's update and the
drawing of the frame, on a small ARM processor, in software. The PC is many times faster, so
measure rather than guess:

```sh
veduta bench --scene level --ticks 400
```

```
400 ticks at 320x240 on 4 cpus (amd64), budget 50 ms per tick
  update_ms  mean    0.32  p50    0.09  p95    0.83  max    9.72 ms
  render_ms  mean    1.43  p50    0.81  p95    6.69  max   11.86 ms
  frame_ms   mean    1.75  p50    0.93  p95    8.46  max   14.91 ms
  over budget 0 ticks, slowest tick 5; triangles mean 328.32 max 336, drawn mean 77.82 max 80
```

`bench --scenario` measures a scenario's play, which is more realistic than an idle scene.
Numbers measured on a PC are far below the console's: keep a wide margin, and run the same
bench on a Linux ARM machine when you have one.

What costs time, roughly in order:

1. **Triangles drawn.** Every sprite is 12 triangles (a thin box), culled when off screen.
   Big tile maps: merge static tiles into one mesh with `mesh` / `volume` instead of an
   entity per tile.
2. **Per-tick loops over every entity.** `e:overlapping(tag)` and `scene.tagged(tag)` are
   cheaper than scanning `scene.entities()` and comparing by hand.
3. **Lua work per entity.** Lua runs about twice as slow as the same logic in Go. Keep
   `update` functions short, skip idle entities early, and do heavy geometry with `mesh`
   and `volume`, which run inside the engine.
4. **Spawning and despawning many entities per tick.** Reuse entities (hide them with
   `visible = false`) for bullets and particles.
