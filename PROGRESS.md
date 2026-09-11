# Progress log

Factual log for the human reviewer. One section per phase of `SPEC-v0.1.0.md` §16.

## Phase 0 — repository skeleton (2026-09-11)

- Built: `go.mod` (`github.com/riftbane/veduta`, `go 1.25`), `LICENSE` (MIT), README stub,
  `CHANGELOG.md` with an Unreleased section, `.gitignore`, `.gitattributes`,
  `.github/workflows/ci.yml`, `.github/workflows/release.yml`.
- Environment: Go 1.27.1 installed from go.dev (sha256 verified) into `/usr/local/go`.
- Verified: `go vet ./...`, `gofmt -l .` on the empty module; CI run on push.
