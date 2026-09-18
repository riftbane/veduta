# Installation

A Lua game needs two things: the `veduta` tool and, to write it comfortably, VS Code with
the Veduta extension. Nothing else: no Go, no compiler, no SDK.

## Windows

Install [VS Code](https://code.visualstudio.com) first, then run in PowerShell:

```powershell
$env:VEDUTA_CHANNEL = "beta"
irm https://raw.githubusercontent.com/riftbane/veduta/main/install.ps1 | iex
```

This puts `veduta.exe` in `%LOCALAPPDATA%\Programs\veduta`, adds that folder to your PATH
without administrator rights, and installs the Veduta extension into VS Code. Open a **new**
terminal afterwards so it sees the PATH, then check:

```powershell
veduta version
```

It must print a `v2` version (`v2.0.0-rc.10` or later). Running the same two lines again
updates both the tool and the extension.

> **Why the first line matters.** Until v2.0.0 is out, the *stable* channel still installs
> Veduta v1, whose games are written in Go: without `VEDUTA_CHANNEL = "beta"` you get a
> tool that creates Go projects. See [FAQ](FAQ-and-Troubleshooting#veduta-creates-go-projects).

Settings of the installer, all optional:

| Variable | Meaning |
|----------|---------|
| `$env:VEDUTA_CHANNEL` | `stable` (default) or `beta` |
| `$env:VEDUTA_VERSION` | an exact version, such as `v2.0.0-rc.10` |
| `$env:VEDUTA_HOME` | where to install instead of `%LOCALAPPDATA%\Programs\veduta` |
| `$env:VEDUTA_VSCODE` | `"0"` leaves VS Code alone |

## Linux

```sh
curl -fsSL https://raw.githubusercontent.com/riftbane/veduta/main/install.sh | sh -s -- --channel beta --no-go
veduta update --channel beta   # keep following the beta channel from now on
```

The tool goes to `~/.local/bin` (`--prefix DIR` to change it). `--no-go` skips installing
Go, which only Go games need. Releases carry the tool for linux/amd64, linux/arm64 and
windows/amd64. On macOS, build it with Go 1.25 or newer, naming the version (such a build
cannot update itself):

```sh
go install -ldflags "-X main.version=v2.0.0-rc.10" github.com/riftbane/veduta/v2/cmd/veduta@v2.0.0-rc.10
```

The simulator window exists only on Windows. On Linux, `veduta run` plays the game on a
framebuffer (a text console or a panel); every headless command (test, simulate, render,
bench) works everywhere.

## The VS Code extension

The installer adds it when VS Code is present. To install it by hand, download
`veduta-vscode.vsix` from the [latest release](https://github.com/riftbane/veduta/releases)
and run:

```sh
code --install-extension veduta-vscode.vsix
```

When you open a game, the extension recommends the **Lua** extension (`sumneko.lua`):
accept it. Scripts then get completion, hover help and checks against the Veduta API, and
the JSON files of the game get completion from their schemas.

What the extension adds: the **Project view** (the Veduta icon in the activity bar), the
game's files by what they are, where a right-click makes a new scene, prefab, script and so
on (see [Project Layout](Project-Layout#the-project-view)), and these commands:

| Command (Ctrl+Shift+P) | Does |
|------------------------|------|
| Veduta: New Game | creates a project and opens it |
| Veduta: Play in the Simulator | same as Ctrl+F5 |
| Veduta: Test | runs every scenario |
| Veduta: Build | checks scripts and assets; errors go to the Problems panel (also on save) |
| Veduta: Deploy to the Console's Card | copies the game to the SD card |

**F5** plays the game under the debugger, **Ctrl+F5** without it. See
[Debugging and Performance](Debugging-and-Performance).

## Keeping up to date

| Command | Updates |
|---------|---------|
| `veduta update` | the tool, from GitHub Releases (checksum verified) |
| `veduta update --channel beta` | switches to release candidates and remembers it |
| `veduta extension --install` | the VS Code extension, to the one released with the tool |
| `veduta upgrade` | a project, to the tool's engine version (run inside the project) |

On Windows `veduta update` cannot replace the running program: run the two lines of
`install.ps1` again instead, which update the tool and the extension. The tool and the
extension are released together but VS Code keeps its extensions itself, so they can drift
apart: when the extension is older than the one that came with your `veduta`, it says so
and **Update the Extension** installs the right one (then reload the window).

Updating the tool never changes a project; `veduta upgrade` does, explicitly, and also
refreshes the editor files (`.veduta/`, `.vscode/`, `.luarc.json`).
