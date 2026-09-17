# Publishing to the Console

The console runs **VedutaOS** from its SD card. Games live on the same card, in the
`games` folder, one folder per game. A Lua game is copied as it is: its scripts, its
assets and its card. The console's runtime runs the scripts, so the same folder plays on
every console.

## Straight to the card

1. Switch the console off and take out its SD card.
2. Put the card in the PC. It appears as a drive named `VEDUTAOS` (or `VEDUTA`).
3. In VS Code run **Veduta: Deploy to the Console's Card**, or in the project:

   ```sh
   veduta deploy
   ```

   The tool finds the card by its label; name a folder to use another place
   (`veduta deploy E:\`).
4. Eject the card, put it back and switch the console on. The dashboard lists the game by
   its title: A plays it, Home comes back.

`deploy` writes `games/<name>/` with the `.lua` files, `assets/` (without the compiled
cache), `veduta.json`, `README.md`, the icon and a `card.json` carrying the version
(from `git describe`). Deploying again replaces the folder, so it is also how to update a
game on the card.

## The dashboard's card

`card.json` in the project tells the dashboard what to show:

```json
{
  "veduta": "card/1",
  "title": "Gem Cave",
  "name": "gemcave"
}
```

For an icon beside the title, put a PNG in the project (outside `assets/`) and name it in
`veduta.json` with `"icon": "icon.png"`. Without one the dashboard draws a placeholder.

## Releases

A project created by `veduta init` has a GitHub Actions workflow
(`.github/workflows/release.yml`) that publishes the game whenever a version tag is pushed.
`veduta release` runs the checklist first and pushes the tag only when everything passes:

```sh
veduta release v1.0.0 --dry-run   # the checklist only
veduta release v1.0.0             # checklist, CHANGELOG entry, commit, tag, push
```

The checklist: a clean git tree, `veduta test` passing (scenarios and goldens), every asset
compiling, a frame rendering, every script compiling, a valid `card.json` and a workflow
that publishes the console's archive. The workflow then attaches
`<name>_v1.0.0.tar.gz` to a GitHub release. That archive unpacks to a folder ready for the
card's `games` folder.

Pre-release versions (`v1.0.0-rc.1`) are refused by `veduta release`: tag them by hand
(`git tag v1.0.0-rc.1 && git push origin v1.0.0-rc.1`); the workflow publishes them as
pre-releases.

Players with VedutaOS's tool can also put a released archive on a card themselves:
`vedutaos card E:\ --game gemcave_v1.0.0.tar.gz` (the card's drive or mount point first).

## API levels

Each runtime has a **Lua API level**: `engine.api` in a script, 1 in Veduta v2.0. When a
later engine adds functions, the level goes up. A game that uses them says so in
`veduta.json`:

```json
{ "veduta": "project/1", "name": "gemcave", "engine": "v2.1.0", "script": "main.lua", "api": 2 }
```

A console whose runtime has a lower level lists the game with **UPDATE VEDUTAOS** and does
not start it, instead of failing in the middle of play on a missing function. Leave `api`
out while the game uses only level 1.

## Before publishing

- `veduta doctor` checks the project against the console: manifest, card, workflow,
  versions.
- `veduta bench --scenario tests/scenarios/<a long one>.scenario.json` shows how much of
  the 50 ms tick budget the game uses. Keep a wide margin: the console is much slower than
  a PC. See [Debugging and Performance](Debugging-and-Performance#performance).
- Play it on the console itself when you can: the panel's colours are 16-bit, and a
  gradient that is smooth on a monitor may band on the panel (the simulator shows the
  panel's colours too).
