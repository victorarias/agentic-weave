# AGENTS.md

This file provides guidance to AI agents when working in this repository.

## Work tracking
- Use TASKS.md at the repo root to track all initiatives.
- Each initiative should have a short name and (if applicable) a branch family tag.
- Keep a brief progress log with date + time for each meaningful change.
- Update status in TASKS.md as you work.

## Changes and compatibility
- This library is early-stage; breaking changes are allowed.
- Prefer additive, backward-compatible changes when possible.

## Documentation expectations
- Update docs when adding or changing public interfaces.
- Keep the Getting Started guide runnable and minimal.

## Build & Test
- Use standard Go tooling:
  - `go test ./...`
  - `go vet ./...`

## Design principles
- Keep core LLM-agnostic.
- Optional submodules should be importable independently.
- Favor small interfaces to maximize pluggability.

## wv testing philosophy (pi-mono inspired)
- Use a deterministic virtual terminal harness for TUI tests instead of relying on a real terminal.
- Test behavior and invariants (diff rendering, cursor placement, width/resize handling, input dispatch), not brittle full-screen snapshots.
- Keep tests layered:
  - Component-level pure render/input tests.
  - Renderer-level tests with virtual terminal writes.
  - Session/agent integration tests that validate event flow and streaming behavior.
- Treat TUI regressions as first-class: new renderer/editor features should include harness-backed tests.
- Keep the harness reusable so the same approach can expand from `cmd/wv/tui` to whole coding-agent flows.

## wv basic architecture
- `cmd/wv` is a nested Go module to keep CLI dependencies out of core library consumers.
- `cmd/wv/main.go` wires config, session, and TUI runtime.
- `cmd/wv/config` loads runtime settings from env (`ANTHROPIC_*`, `WV_*`).
- `cmd/wv/session` is provider-agnostic: it wraps `agentic/loop.Runner` and bridges loop events to UI updates.
- `cmd/wv/tui` owns terminal mode, differential rendering, input loop, and ANSI utilities.
- `cmd/wv/tui/components` contains composable UI primitives (container, markdown, editor, loader, text).
- `cmd/wv/tools` provides built-in coding tools (`bash`, `read`, `write`, `edit`, `grep`, `glob`, `ls`).
- `cmd/wv/extensions` provides a minimal Lua extension loader used by `/reload`.
- Current provider scope: Anthropic-only.

## wv safety defaults
- Default runtime is safer-by-default:
  - Lua extensions are disabled unless `WV_ENABLE_EXTENSIONS=1`.
  - Project-local extensions (`.wv/extensions`) are disabled unless `WV_ENABLE_PROJECT_EXTENSIONS=1` (global extensions can still be enabled independently).
  - `bash` tool registration is disabled unless `WV_ENABLE_BASH=1`.
- File tools are workspace-scoped and reject paths outside the current workspace root.
- Event delivery preserves structural state transitions (`message_end`, `tool_end`, etc.); only high-frequency message deltas are drop-eligible under pressure.
- Runs are cancellable via `/cancel` and time-bounded by `WV_RUN_TIMEOUT_SECONDS` (default: 180s).
