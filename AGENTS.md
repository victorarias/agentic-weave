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

## Coding-agent direction
- Do not add or revive an in-repo TUI coding agent under `cmd/`; the abandoned `cmd/wv` prototype has been removed.
- The coding-agent runtime target is **pi**. New orchestration work should focus on the remote-control stack around pi (`pi`/`pi-mono`, wrapper, bridge, relay, protocol, inspector), not on building a separate local CLI agent.
- When updating architecture docs, prefer walking-skeleton slices that prove the pi-based control loop end-to-end before adding UI, PTY takeover, or ACP shim layers.
- Be extra careful with PTY / raw-terminal input handling. Do not assume control keys arrive as single raw bytes; terminals, multiplexers, SSH, Bubble Tea, and keyboard enhancement modes may encode the same chord in different multi-byte forms (for example `Ctrl-]` may arrive as raw `0x1d`, CSI-u, or modifyOtherKeys). When changing takeover/input code, preserve buffering for split escape sequences, test against real PTYs, and add/update regression coverage before declaring the UX fixed.
