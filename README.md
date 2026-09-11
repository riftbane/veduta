# Veduta

> **veduta** (it.) — a highly detailed, faithful painting of a view.

Veduta is a headless, deterministic game engine and asset toolchain written in pure Go,
designed to be *looked at by machines*. Its primary user is an AI agent working on a
server with no display: every feature exists to let that agent see, measure, and fix what
it is building. Humans download the resulting game builds and play them.

**Status:** under construction towards v0.1.0. The specification is
[`SPEC-v0.1.0.md`](SPEC-v0.1.0.md); progress is logged in [`PROGRESS.md`](PROGRESS.md).

## Principles

- Numbers before pixels: every visual check has a numeric counterpart.
- Declarative JSON sources for models, textures, materials, scenes and scenarios.
- Headless everywhere except the player build.
- Deterministic: same seed + same inputs → same trace, same frames.
- Standard library only, `CGO_ENABLED=0`.

## License

MIT — see [`LICENSE`](LICENSE).
