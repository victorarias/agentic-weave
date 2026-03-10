---
name: hodor-review
description: Review guidance for Agentic Weave pull requests. Focus on correctness, public API changes, tests, and the repository's safety and architecture constraints.
---

## Review priorities
- Prioritize correctness, regressions, security issues, missing validation, and broken assumptions.
- Ignore stylistic nits unless they hide a functional bug or materially reduce maintainability.
- Treat suggestions as advisory; do not require perfection for early-stage code.

## Repository expectations
- This library is early-stage; breaking changes are allowed, but prefer additive and backward-compatible changes when practical.
- Update docs when public interfaces, examples, or workflows change.
- Keep the Getting Started path runnable and minimal.
- Keep the core library LLM-agnostic.
- Optional submodules should remain independently importable.
- Favor small, composable interfaces and pluggable designs.

## Go validation expectations
- For Go changes, expect `go test ./...` to stay green.
- For Go changes, expect `go vet ./...` to stay green.
- Watch for accidental API drift, unchecked errors, nil handling bugs, race-prone code, and mismatches between docs and implementation.

## wv and TUI-specific expectations
- Prefer deterministic virtual-terminal harness tests over brittle full-screen snapshots.
- Review renderer/editor changes for resize behavior, cursor placement, scroll behavior, and diff rendering invariants.
- Treat TUI regressions as first-class issues.

## Safety expectations
- File operations should remain workspace-scoped unless a change intentionally broadens that contract.
- Safer-by-default behavior should remain intact unless explicitly documented.
- New automation should be advisory unless the repository explicitly makes it blocking.
