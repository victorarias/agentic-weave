# Agentic Weave

This file tracks current work items and progress.

## Current Initiative: vertex-provider
- [x] Add Vertex Gemini provider (ADC-only) under agentic/providers/vertex.
- [x] Add docs + example usage.

## Current Initiative: loop-truncation-fixes
- [x] Return partial output when head truncation hits first-line byte limit.
- [x] Preserve byte-based truncation metadata for tail truncation.
- [x] Avoid appending compaction summaries for history stores without rewrite support.

## Current Initiative: loop-history-rewriter
- [x] Require history.Rewriter when budget compaction is configured.

## Current Initiative: compat-compactor-guard
- [x] Preserve no-compaction behavior when legacy CompactFunc is nil.

## Current Initiative: docs-accuracy
- [x] Align context budget docs with loop API and history requirements.
- [x] Add ADC setup note for Vertex provider.

## Current Initiative: test-harness
- [x] Add integration harness covering loop, truncation, and compaction flows.
- [x] Expand harness coverage across loop behavior, tools, and policies.
- [x] Add guard, event ordering, byte truncation, and usage passthrough tests.
- [x] Add MCP integration and Vertex config tests.

## Current Initiative: ci-harness
- [x] Add GitHub Actions workflow to run all tests (including harness).
- [x] Add formatter and linter checks to CI workflow.
- [x] Switch CI linter to staticcheck (latest) for reliable module coverage.

## Current Initiative: mono-parity-context
- [x] Design optional, pluggable context budgeting + compaction + truncation modules.
- [x] Define minimal interfaces for model limits + usage reporting.
- [x] Provide seamless integration example for agent loop usage.
- [x] Implement budget + truncation packages with tests.
- [x] Add loop helper, adapter utilities, history hook, and context compatibility.

## Progress Log
- 2026-01-16 22:39: Switched CI linter to staticcheck to avoid golangci-lint module detection issues.
- 2026-01-16 22:33: Added gofmt and golangci-lint checks to CI workflow.
- 2026-01-16 22:30: Added MCP integration tests and Vertex provider config checks.
- 2026-01-16 22:26: Added harness tests for budget guards, event ordering, byte truncation, and usage passthrough.
- 2026-01-16 22:16: Added CI workflow to run harness tests on push/PR.
- 2026-01-16 22:10: Fixed harness truncation test output to use raw lines.
- 2026-01-16 22:09: Expanded harness to cover loop behavior, tool policies, and truncation modes.
- 2026-01-16 22:01: Stabilized harness tool truncation scenario.
- 2026-01-16 22:00: Added integration test harness for loop scenarios.
- 2026-01-16 21:53: Updated docs for loop API, history rewriter requirement, and Vertex ADC setup.
- 2026-01-16 21:47: Guarded ToBudget so nil legacy compactor stays disabled.
- 2026-01-16 21:45: Enforced history.Rewriter for configured compaction; added guard test.
- 2026-01-16 21:36: Fixed truncation edge cases and history compaction persistence behavior.
- 2026-01-16 21:12: Added Vertex Gemini provider, adapter stub, and docs.
- 2026-01-16 21:05: Started Vertex Gemini provider implementation.
- 2026-01-14 10:18: Created repo scaffolding and task tracking.
- 2026-01-14 11:11: Core module + docs + runnable example complete.
- 2026-01-14 11:46: Optional modules, adapters, and tests complete.
- 2026-01-14 14:05: Real SDK examples added as nested modules.
- 2026-01-14 14:24: MIT LICENSE and CONTRIBUTING added.
- 2026-01-14 14:36: Docs updated for open-source release.
- 2026-01-14 15:24: Added .gitignore + Anthropic real example fix verified.
- 2026-01-14 15:31: Removed PLAN/IMPLEMENTATION/GETTING_STARTED in favor of docs index.
- 2026-01-14 22:50: Started design for mono-like context budgeting + compaction (optional modules).
- 2026-01-14 23:02: Added limits/usage/truncate/budget packages with docs and tests.
- 2026-01-14 23:28: Added loop helper, adapter helpers, history hook, compat layer, and mono-like example.
