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
