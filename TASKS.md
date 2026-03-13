# Agentic Weave

This file tracks current work items and progress.

## Archived Plan: POC Families (first set, archived 2026-02-20)

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

### Family: pi-mono-eval (branch family: chore/pi-mono-*)
- [x] Investigation: Remote control + PTY feasibility in pi-mono extensions
  - Description: Assess whether pi can be remotely controlled and whether PTY-style interaction can be enabled on demand via extensions.
  - Findings:
    - RPC mode already provides headless remote control over stdin/stdout JSON protocol.
    - Extensions can inject/queue user messages and intercept user bash commands.
    - Extensions can delegate tool execution (including bash/read/write/edit/etc.) to remote backends via pluggable operations.
    - Interactive-mode raw terminal input interception is available via `ctx.ui.onTerminalInput`; this is not available in RPC mode.
    - Full interactive PTY takeover is possible in interactive mode by suspending TUI and inheriting stdio (existing example).

---

## Active Initiative: Remote Agent Control Requirements (branch family: feat/requirements-*)

### Family: requirements-discovery
- [ ] Interview: Operational and control-plane requirements
  - Description: Collect exact requirements for autonomous agent-to-agent control, attach/detach semantics, live inspection, and session lifecycle.
  - Output:
    - [ ] `docs/coding-agent/requirements/01-control-plane.md`
    - [ ] `docs/coding-agent/requirements/02-session-lifecycle.md`
    - [ ] `docs/coding-agent/requirements/03-attach-and-observability.md`

- [ ] Spec Draft: State machine + command protocol
  - Description: Produce a concrete session state model and remote command protocol draft from interview answers.
  - Depends on: Interview
  - Output:
    - [ ] `docs/coding-agent/requirements/04-state-machine.md`
    - [ ] `docs/coding-agent/requirements/05-control-protocol.md`

---

## Active Initiative: Remote Agent Control Walking Skeleton (branch family: feat/remote-control-*)

### Family: tier0-local-skeleton
- [x] Cleanup: remove abandoned `cmd/wv` CLI prototype and stale plan doc
  - Description: Delete the in-repo TUI coding-agent experiment so the repo clearly targets pi-based orchestration only.
  - Output:
    - [x] `cmd/wv/` removed
    - [x] `docs/plans/wv-coding-agent-cli.md` removed
    - [x] `AGENTS.md` updated to point new coding-agent work at pi-based orchestration

- [x] PR 1: Tier 0 local pi wrapper + protocol + dev client
  - Description: Build the walking-skeleton local control loop for pi: local transport, `initialize`, `session.prompt`, `session.cancel`, `session.agent_ready`, and normalized `session.update`.
  - Depends on: cleanup
  - Output:
    - [x] local wrapper process (`cmd/weave-wrapper`, `remotecontrol/local`)
    - [x] shared protocol types (`remotecontrol/protocol`)
    - [x] tiny local dev client / inspector command (`cmd/weave-inspect` local mode)
    - [x] smoke demo for prompt/stream/cancel/clean shutdown against a real pi process

---

## Active Initiative: PR Review Automation (branch family: chore/hodor-review-*)

### Family: hodor-pr-review
- [x] Workflow: Hodor PR review via Vertex AI Gemini 3 Flash Preview
  - Description: Replace the always-on Claude review workflow with Hodor, authenticated through Google Cloud, and keep the review advisory.
  - Output:
    - [x] `.github/workflows/hodor-review.yml`
    - [x] `.github/hodor/v0.3.4-google-vertex.patch`
    - [x] `.hodor/skills/hodor-review/SKILL.md`
    - [x] `docs/09-hodor-pr-review.md`

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
- 2026-03-13 21:02: Ran the Tier 0 flow against a real local pi process: `weave-wrapper` bootstrapped pi RPC successfully, `weave-inspect` initialized and prompted, and a socket-level manual test confirmed cancel followed by completion.
- 2026-03-13 20:52: Added the Tier 0 pi orchestration scaffold: `remotecontrol/protocol` envelope/update types, `remotecontrol/local` wrapper around `pi --mode rpc`, `cmd/weave-wrapper`, `cmd/weave-inspect`, fake-pi-backed tests, and doc updates that rename the stack to `weave-*` and make RPC the walking-skeleton starting point.
- 2026-03-13 20:35: Removed the abandoned `cmd/wv` CLI prototype and deleted `docs/plans/wv-coding-agent-cli.md` so the repository clearly targets pi-based orchestration instead of a homegrown TUI agent.
- 2026-03-10 21:57: Added `docs/remote-control/08-walking-skeleton-implementation-handoff.md` with a concrete handoff plan for another agent: three real early tiers, smoke tests, stop-and-evaluate gates, explicit non-goals, and PR slicing guidance.
- 2026-03-10 21:47: Folded Gastown learnings into the remote-control docs: added core design principles (`push, not scrape`, progressive integration tiers, adapter registry, explicit delivery semantics), a runtime preset/adapter registry section, and reworked the main architecture plan into walking-skeleton tiers with demoable end-to-end slices.
- 2026-03-10 21:35: Added concrete `session.update` examples for every V1 kind (`lifecycle`, message, tool, permission, status, error, complete) so the protocol is easier to implement consistently.
- 2026-03-10 21:31: Tightened the remote-control protocol docs: added identity model, capability-negotiation authority, closed `session.update` taxonomy, permission lifecycle invariants, error categories, reconnect/order rules, and explicit session-vs-runtime semantics.
- 2026-03-10 21:22: Added `docs/remote-control/07-acp-shim.md` describing the minimum viable ACP-compatible shim: purpose, non-goals, ACP↔weave mapping, adapter-service recommendation, and acceptance criteria.
- 2026-03-10 21:15: Refined the remote-control docs based on Agent Client Protocol (ACP): added ACP alignment stance, initialize/load/prompt/cancel/update semantics, permission-response + PTY resize protocol gaps, and ACP-focused test coverage notes.
- 2026-03-10 19:23: Mirrored `attn` secret naming in the Hodor workflow (`VERTEX_AI_SA`, `GOOGLE_CLOUD_PROJECT`) and defaulted `GOOGLE_CLOUD_LOCATION` to `global`.
- 2026-03-10 19:19: Hardened Hodor workflow to skip fork PRs, moved review guidance into tracked `.hodor/skills`, and removed the always-on Claude review workflow.
- 2026-03-10 19:15: Added advisory Hodor PR review workflow on GitHub Actions using Vertex AI `google-vertex/gemini-3-flash-preview`, plus a local upstream patch for Google/Vertex model parsing.
- 2026-03-10 19:14: Added repository-specific Hodor review skill and documentation for Google Cloud auth/setup.
- 2026-02-20 21:36: Archived `docs/coding-agent` to `docs/archived/coding-agent` before requirements interviews.
- 2026-02-20 21:35: Archived prior POC implementation plan and opened active requirements-discovery initiative for remote agent control design.
- 2026-02-20 21:34: Investigated `badlogic/pi-mono` extension + RPC architecture for remote-control/PTTY feasibility; cloned to `/tmp/pi-mono-investigate` and documented results.
- 2026-01-27 21:40: Updated POC plan + docs to reflect branch-only tree and optional git checkpoint file sync.
- 2026-01-27 21:02: Marked tui-design-plan complete; docs added under docs/coding-agent.
- 2026-01-27 21:00: Pruned completed initiatives and history entries per cleanup request.
