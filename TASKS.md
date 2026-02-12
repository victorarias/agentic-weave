# Agentic Weave

This file tracks current work items and progress.

## Implementation Plan: POC Families (first set)

### Family: tui-rendering (branch family: feat/poc-tui-*)
- [ ] PR 1: Bubble Tea renderer + minimal TUI shell
  - Description: Build a Bubble Tea-based TUI shell with split layout and a mock event stream.
  - Depends on: none
  - Definition of Done:
    - [ ] Tests: `internal/tui/tui_layout_test.go` verifies pane layout sizing and resize handling.
    - [ ] Tests: `internal/render/render_test.go` validates diff-based render buffer output for a few fixed frames.
    - [ ] Docs: update `docs/coding-agent/08-tui-spec.md` with renderer contract + layout rules.
    - [ ] Logging: on TUI init failure, log error and exit with non-zero status; on resize, emit debug log (guarded by config).
    - [ ] Backward-compat: N/A (new module); config keys optional and default to disabled debug logging.

- [ ] PR 2: TUI event stream + tool/output blocks (Bubble Tea)
  - Description: Wire a mock event stream into the TUI and render assistant/tool blocks with streaming updates.
  - Depends on: PR 1 (tui-rendering)
  - Definition of Done:
    - [ ] Tests: `internal/tui/stream_render_test.go` simulates streaming events and asserts stable render snapshots.
    - [ ] Docs: update `docs/coding-agent/04-agent-loop.md` to reference the event types consumed by TUI.
    - [ ] Logging: warn on unknown event types and skip rendering those blocks.
    - [ ] Backward-compat: unknown event fields ignored; missing optional fields render as empty content.

- [ ] PR 3: Side panel changed-files + diff preview
  - Description: Add a right-side panel that lists changed files and shows diff preview for selection.
  - Depends on: PR 1 (tui-rendering)
  - Definition of Done:
    - [ ] Tests: `internal/vcs/git_status_test.go` uses fixtures under `tests/fixtures/git-repo` to validate file status and diff generation.
    - [ ] Tests: `internal/tui/diff_panel_test.go` verifies selection and preview rendering for added/modified files.
    - [ ] Docs: update `docs/coding-agent/08-tui-spec.md` with side panel behavior + keybindings.
    - [ ] Logging: on git command failure, show in UI status line and log error; panel renders “Unavailable”.
    - [ ] Backward-compat: if repo not found, panel hidden; no impact to existing flows.

**Integration Gate (tui-rendering)**
- [ ] Manual: run `cmd/opencode-tui` with mock session; verify resize, scroll, and side panel toggle.
- [ ] Manual: simulate tool output streaming; confirm no flicker and stable cursor position.

---

### Family: remote-protocol (branch family: feat/poc-remote-*)
- [ ] PR 4: Remote protocol types + command/poll queue
  - Description: Define remote event envelope, add command + output queues with cursor-based polling, and document handshake.
  - Depends on: none
  - Definition of Done:
    - [ ] Tests: `internal/remote/codec_test.go` validates JSON encode/decode compatibility and error cases.
    - [ ] Tests: `internal/remote/queue_test.go` verifies command queue ordering and poll cursor behavior.
    - [ ] Docs: update `docs/coding-agent/06-remote-protocol.md` with event schema + handshake + command/poll semantics.
    - [ ] Logging: invalid frames log warn and drop; command timeouts log warn.
    - [ ] Backward-compat: N/A (new module); if config missing, remote stays disabled.

- [ ] PR 5: WS transport (client + local server stub)
  - Description: Add WS transport for remote protocol, including local server stub and client reconnect/backoff.
  - Depends on: PR 4 (remote-protocol)
  - Definition of Done:
    - [ ] Tests: `internal/remote/ws_test.go` spins up in-process WS server and verifies connect, send command, and poll output.
    - [ ] Docs: update `docs/coding-agent/06-remote-protocol.md` with transport details + retry/backoff rules.
    - [ ] Logging: connection errors log with remote URL and retry backoff; disconnect reasons log info.
    - [ ] Backward-compat: remote disabled by default; no impact to local-only flows.

- [ ] PR 6: Remote TUI for connect/send/poll
  - Description: Add a minimal Bubble Tea remote TUI to connect to agents, send commands, and poll output.
  - Depends on: PR 5 (remote-protocol)
  - Definition of Done:
    - [ ] Tests: `internal/remoteui/model_test.go` validates state transitions (disconnected → connected → polling).
    - [ ] Docs: update `docs/coding-agent/06-remote-protocol.md` with remote TUI usage notes.
    - [ ] Logging: command failures log warn and surface in UI status line.
    - [ ] Backward-compat: N/A (new module).

- [ ] PR 7: Remote input merge policy (local vs remote)
  - Description: Implement queue merge semantics and add conflict policy described in the spec (local wins ties).
  - Depends on: PR 5 (remote-protocol)
  - Definition of Done:
    - [ ] Tests: `internal/supervisor/queue_merge_test.go` covers ordering for local vs remote inputs.
    - [ ] Docs: update `docs/coding-agent/08-tui-spec.md` merge policy section.
    - [ ] Logging: log remote enqueue failures and continue; queue overflow logs warn and drops oldest.
    - [ ] Backward-compat: existing local-only behavior unchanged when remote disabled.

**Integration Gate (remote-protocol)**
- [ ] Manual: run local WS server + remote client, send input, confirm it appears in TUI and respects ordering.
- [ ] Manual: run remote TUI, connect to agent, send a command, and poll output to confirm round-trip.

---

### Family: history-tree (branch family: feat/poc-history-*)
- [ ] PR 8: History tree data model + JSONL persistence
  - Description: Implement a branch-only history tree (no merges) with branch pointers and JSONL storage for replay.
  - Depends on: none
  - Definition of Done:
    - [ ] Tests: `internal/historytree/tree_test.go` covers branch creation and traversal order.
    - [ ] Tests: `internal/storage/jsonl_tree_test.go` verifies append + replay for tree events.
    - [ ] Docs: update `docs/coding-agent/03-jsonl-storage-schema.md` with tree event entries.
    - [ ] Logging: on replay corruption, log error and skip invalid entries with count.
    - [ ] Backward-compat: if old linear session log exists, treat as single-branch root; no crash.

- [ ] PR 9: Agent loop integration (branching + resume)
  - Description: Wire the agent loop to create a new branch only when input is not on the current head; otherwise advance head.
  - Depends on: PR 8 (history-tree)
  - Definition of Done:
    - [ ] Tests: `internal/agent/loop_tree_test.go` ensures new input creates branch and resume uses selected branch.
    - [ ] Docs: update `docs/coding-agent/04-agent-loop.md` with tree semantics and resume behavior.
    - [ ] Logging: on missing branch ID, log warn and fall back to latest branch.
    - [ ] Backward-compat: if branch ID absent, default to linear continuation.

- [ ] PR 10: History query limits (tail N)
  - Description: Add a query API to load only the last N messages on the active branch, with an option to load full history.
  - Depends on: PR 8 (history-tree)
  - Definition of Done:
    - [ ] Tests: `internal/historytree/query_test.go` covers tail-N selection and full-history selection.
    - [ ] Tests: `internal/storage/jsonl_tail_test.go` validates tail-N replay on JSONL sessions.
    - [ ] Docs: update `docs/coding-agent/04-agent-loop.md` with history load limits for controller vs human modes.
    - [ ] Logging: if limit is set and truncation occurs, log debug once per session.
    - [ ] Backward-compat: default (limit unset or 0) loads full history.

- [ ] PR 11: Branch summaries + file tracking metadata
  - Description: Add optional branch-summary entries when switching branches and capture read/modified files for context (no file restore in core).
  - Depends on: PR 8 (history-tree)
  - Definition of Done:
    - [ ] Tests: `internal/historytree/summary_test.go` verifies summary entry creation and placement.
    - [ ] Tests: `internal/historytree/file_tracking_test.go` verifies read/modified file aggregation from tool calls and prior summaries.
    - [ ] Docs: update `docs/coding-agent/03-jsonl-storage-schema.md` with branch_summary + details schema.
    - [ ] Docs: update `docs/coding-agent/04-agent-loop.md` to document summary injection on branch switch.
    - [ ] Logging: summary generation failure logs warn and falls back to no-summary.
    - [ ] Backward-compat: if summary data missing, branch switch still succeeds with no extra context.

- [ ] PR 12: Optional git checkpoint hook (file sync POC)
  - Description: Add a minimal hook/extension that stashes git state on turn end and offers restore on branch/fork (interactive only).
  - Depends on: PR 9 (history-tree)
  - Definition of Done:
    - [ ] Tests: `internal/checkpoint/git_checkpoint_test.go` covers stash creation, lookup by entry, and restore selection.
    - [ ] Docs: update `docs/coding-agent/05-tooling.md` with checkpoint hook behavior and limitations.
    - [ ] Logging: when git is unavailable or stash fails, log info and skip.
    - [ ] Backward-compat: hook is opt-in and disabled by default; no effect on existing flows.

**Integration Gate (history-tree)**
- [ ] Manual: create two branches from same session; confirm tree view selects and replays correct branch.

---

## Modules (new/updated)
- New: `cmd/opencode-tui` (POC app)
- New: `internal/render` (renderer abstraction + diff buffer)
- New: `internal/tui` (layout, panes, event rendering)
- New: `internal/vcs` (git status + diff adapter)
- New: `cmd/opencode-remote` (minimal remote TUI for connections/commands)
- New: `internal/remote` (protocol types, ws client/server)
- New: `internal/historytree` (DAG model)
- New: `internal/checkpoint` (optional git checkpoint hook)
- Updated: `internal/storage/jsonl` (tree events + replay)
- Updated: `internal/supervisor` (queue merge policy)
- Updated: `docs/coding-agent/03-jsonl-storage-schema.md`
- Updated: `docs/coding-agent/04-agent-loop.md`
- Updated: `docs/coding-agent/05-tooling.md`
- Updated: `docs/coding-agent/06-remote-protocol.md`
- Updated: `docs/coding-agent/08-tui-spec.md`

## Progress Log
- 2026-01-27 21:40: Updated POC plan + docs to reflect branch-only tree and optional git checkpoint file sync.
- 2026-01-27 21:02: Marked tui-design-plan complete; docs added under docs/coding-agent.
- 2026-01-27 21:00: Pruned completed initiatives and history entries per cleanup request.
