# Command Line

Everything the VS Code extension does goes through the `veduta` tool, which works just as
well in a terminal. Run commands inside a project folder (or pass `--project DIR`); add
`--json` for machine-readable output. `veduta help <command>` lists a command's flags.

## Making and playing

| Command | Does |
|---------|------|
| `veduta init NAME` | creates a Lua game project in a new folder |
| `veduta sim` | plays the game in the simulator window (Windows) |
| `veduta run` | plays the game on a Linux framebuffer (the console, a text console) |
| `veduta build` | checks every script and compiles the assets; errors with file, line and column |
| `veduta cook` | compiles changed assets ahead of time (`--force` for all) |

## Testing

| Command | Does |
|---------|------|
| `veduta test` | runs every scenario and compares goldens (`--update-golden` records them) |
| `veduta simulate --scenario FILE` | runs one scenario: verdict, events, contact sheet |
| `veduta simulate --scene S --ticks N [--seed N] [--input FILE] [--screenshots 0,60]` | runs a scene with optional scripted input |
| `veduta fuzz --scene S --games N --ticks T` | random play against the invariants; writes a minimal failing scenario |
| `veduta render --scene S [--tick T] [--mode M] [--out f.png]` | draws one frame |
| `veduta bench --scene S --ticks N` (or `--scenario FILE`) | times update and drawing against the 50 ms budget |
| `veduta diff A.png B.png` | compares two images |

## Assets and worlds

| Command | Does |
|---------|------|
| `veduta inspect model\|texture\|scene\|prefab\|world NAME` | reports problems and writes contact sheets |
| `veduta world map\|query\|place\|terrain\|vegetation\|remove NAME` | describes and edits a world |
| `veduta query` | asks which entity is at a pixel of a rendered frame bundle |

## Shipping

| Command | Does |
|---------|------|
| `veduta deploy [CARD]` | copies the game to the console's SD card |
| `veduta release vX.Y.Z [--dry-run]` | checklist, changelog, tag and push; CI publishes the archive |
| `veduta doctor` | checks the tool, the project and its readiness for the console |

## The tool itself

| Command | Does |
|---------|------|
| `veduta version` | the tool's version |
| `veduta update [--channel stable\|beta] [--check]` | updates the tool from GitHub Releases |
| `veduta upgrade` | moves the project to the tool's engine version and refreshes editor files |
| `veduta dap` | the debug adapter VS Code starts (not run by hand) |
| `veduta mcp` | serves the tool to an AI agent (Claude Code) over MCP |

## Working with an AI agent

Every project has a `CLAUDE.md` and a `.mcp.json`. Opened in Claude Code, the agent reaches
the same commands as tools (`simulate`, `render`, `inspect`, `test`, the docs of every
format) and can build, look at frames and test the game it is changing.
