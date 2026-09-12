# Configuration — `~/.config/veduta/config.json`

The tool's own settings: whether it looks for newer releases of itself, how often, and
which release channel it follows. It is about the `veduta` binary only — a game project is
configured by its `veduta.json` (see `docs/project.md`), and updating the tool never
changes a project (`veduta upgrade` does, explicitly).

## File and name

- Location: `os.UserConfigDir()/veduta/config.json`, which is `~/.config/veduta/config.json`
  on Linux (`$XDG_CONFIG_HOME` is honoured), `%AppData%\veduta\config.json` on Windows and
  `~/Library/Application Support/veduta/config.json` on macOS.
- The file is optional: without it every default below applies.
- The file is one JSON object. Decoding is strict: an unknown key is an error, so a
  configuration written by a newer tool is refused rather than half-read. New keys appear
  only in minor releases, and unlike the source formats this file carries no version tag
  and has no migration path.
- A field set to its zero value (`0`, `""`, missing) takes its default.

## Fields

| Field | Type | Default | Allowed values and meaning |
|-------|------|---------|----------------------------|
| `auto_update` | string | `"check"` | `check`: `veduta mcp` and `veduta doctor` look for a newer release at most once per interval and report it. `auto`: `veduta mcp` also installs it before serving (never mid-session) and restarts itself. `off`: no lookups at all. |
| `check_interval_hours` | number | `24` | How long an answer is reused before asking GitHub again. A value of 0 or less means the default. |
| `channel` | string | `"stable"` | Which releases this tool offers itself: `stable` or `beta`. See below. |

## Channels

- **`stable`** takes the release GitHub marks as the latest one, which is never a
  pre-release.
- **`beta`** takes the newest of every published release, release candidates
  (`v0.2.0-rc.1`) included.

Beta is a superset of stable, not a separate stream: the archives, their checksums and the
verification are the same. Because it takes the newest of everything, it answers with a
stable release whenever that is the newer one — when a candidate is released, or when a
later line goes stable first — so following beta never strands you on an abandoned
candidate, and switching to it can never hand you an older binary than stable would.

Switching channels:

```
veduta update --check --channel beta    # what beta would install; changes nothing
veduta update --channel beta            # install it and follow beta from now on
veduta update --channel stable --force  # go back; --force because it is a downgrade
```

Naming a channel saves it, even when there is nothing to install; `--check` never writes,
so it stays a preview. `install.sh --channel beta` only installs a beta build — the
configuration is the tool's own, so run `veduta update --channel beta` once to keep
receiving them. When the file cannot be read, naming a channel is refused rather than
overwriting it with defaults; without `--channel` the command still works and says why. `veduta update` never downgrades on its own: when this build is newer than its
channel's newest release — after leaving beta, say — it says so and installs nothing until
`--force` is given. Automatic updates (`auto_update: auto`) follow the configured channel
and never downgrade.

The answer of the last lookup is cached in `os.UserCacheDir()/veduta/update.json`
(`~/.cache/veduta/update.json` on Linux) together with the channel it came from: an answer
from the other channel is never reused. The file can be deleted at any time.

`veduta doctor` prints the channel it is following and what it found.

## Errors (examples)

```
/root/.config/veduta/config.json: auto_update "sometimes" (want check, auto or off)
/root/.config/veduta/config.json: channel "nightly" (want stable or beta)
/root/.config/veduta/config.json: json: unknown field "chanel"
```

## Example

```json
{
  "auto_update": "check",
  "check_interval_hours": 24,
  "channel": "stable"
}
```
