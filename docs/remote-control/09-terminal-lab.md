# Terminal Lab

**Status (2026-03-15):** initial implementation landed. The repository now has a reusable PTY test harness (`internal/ptytest`), named terminal regression tests under `terminaltests/`, data-driven key fixtures in `testdata/terminal/`, and a small runner script at `scripts/terminal-lab.sh`.

## Goal

Turn every real PTY / takeover / TUI bug into a named regression that can be replayed and re-run later.

The core principle is:

> if a user can hit a weird terminal bug once, agents should be able to capture it, encode it as a fixture, and make it part of the suite going forward.

## What exists now

### Reusable PTY harness

`internal/ptytest` provides:

- spawn a process in a real PTY
- send raw bytes or split bytes with delays
- resize the PTY
- wait for visible text on screen
- capture raw stdin/stdout
- strip ANSI for assertions
- write artifact bundles (`stdin.bin`, `stdout.bin`, `stdout.txt`, `metadata.json`)

### Named regression suite

`terminaltests/` currently covers the first high-value bug family:

- `TestTakeoverDisconnectEncodings`
  - direct takeover path
  - plain PTY mode
  - tmux-wrapped mode when `tmux` is installed
- `TestTUITakeoverDisconnectEncodings`
  - Bubble Tea TUI path
  - spawn -> takeover -> disconnect -> return to TUI

### Fixture data

`testdata/terminal/encodings/ctrl-right-bracket.json` records the known `Ctrl-]` disconnect encodings:

- raw `0x1d`
- CSI-u `ESC [ 93 ; 5 u`
- modifyOtherKeys `ESC [ 27 ; 5 ; 93 ~`

The suite sends these encodings **split byte-by-byte**, because split-read behavior is exactly what caused the recent regression.

### Runner script

```bash
scripts/terminal-lab.sh quick
scripts/terminal-lab.sh scenario TestTUITakeoverDisconnectEncodings/csi-u
```

This enables a normal human or agent workflow without remembering the environment variable gate.

## Artifact layout

By default, artifacts are written to:

```text
.artifacts/terminaltests/
```

Override with:

```bash
WEAVE_TERMINAL_ARTIFACTS_DIR=/some/path scripts/terminal-lab.sh quick
```

Each scenario run gets its own folder with:

- relay logs
- cluster metadata
- PTY stdin capture
- PTY stdout capture
- ANSI-stripped stdout snapshot
- per-scenario metadata

## How to add a new regression

1. Capture the real bytes / behavior from a user or manual repro.
2. Add a fixture under `testdata/terminal/encodings/` or a new scenario manifest under `testdata/terminal/scenarios/`.
3. Add a scenario test in `terminaltests/` using `internal/ptytest`.
4. Ensure the scenario writes artifacts and uses semantic assertions.
5. Add a short note to `TASKS.md` so future agents know the regression exists.

## Near-term expansion

The current lab is intentionally focused on the exact PTY bugs we just hit. The next expansion points are:

- zellij-wrapped scenarios
- SSH-wrapped scenarios
- resize torture scenarios
- replay tooling on top of saved artifact bundles
- environment-profile capture for user terminals

## Commands

Run the suite:

```bash
scripts/terminal-lab.sh quick
```

Run a specific scenario:

```bash
scripts/terminal-lab.sh scenario TestTakeoverDisconnectEncodings/tmux/csi-u
```

Run directly with Go:

```bash
WEAVE_TERMINAL_TESTS=1 go test -v ./terminaltests
```
