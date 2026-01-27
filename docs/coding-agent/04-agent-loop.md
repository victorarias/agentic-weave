# Agent Loop

## Flow
1) Receive input (local or remote).
2) Resolve active branch leaf (default: latest leaf).
3) Append message.created (user) with `parent_id=leaf`.
4) Call LLM on branch path (optionally tail-N for controller agents).
5) If tool call:
   - Append tool.call
   - Execute tool
   - Append tool.result (stream chunks if large)
6) Append assistant message.created + message.part.
7) Update session leaf.

## Branching rules
- If input is sent while not at the current leaf, create a new branch (child entry from selected node).
- If input is sent at the leaf, advance the current branch (no new branch).
- Branch summaries are optional: when switching branches, a summary entry can be added to preserve context.

## History loading
- Default: load full branch path.
- Controller mode: load last N messages only (tail-N).

## File state
- Core does not restore files on branch/fork.
- Optional hooks/extensions (e.g., git checkpoint) can stash and restore on branch/fork.

## Constraints
- One active loop per session.
- Interrupt cancels current model stream.
- Tool outputs streamed to EventBus + Remote.
