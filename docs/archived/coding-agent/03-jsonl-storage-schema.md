# JSONL Storage Schema

## File format
- One JSON object per line, append-only.
- Filename: `.opencode/sessions/<sessionID>.jsonl`

## Tree model
- Every entry has `id` and `parent_id` (null for root).
- Branching is implicit: create a new entry whose `parent_id` points at an earlier entry.
- The active position is tracked by `session.updated.leaf_id`.

## Event types (POC)
- session.created
- session.updated (leaf_id changes)
- message.created
- message.part
- branch.summary
- compaction.summary
- tool.call
- tool.result

## Minimal schemas
```json
{"type":"session.created","id":"s1","title":"New","created_at":1700000000}
{"type":"message.created","id":"m1","session_id":"s1","parent_id":null,"role":"user","created_at":1700000001}
{"type":"message.part","message_id":"m1","part":{"type":"text","text":"hello"}}
{"type":"tool.call","id":"t1","message_id":"m2","tool":"read","args":{"path":"README.md"}}
{"type":"tool.result","id":"t1","output":"...","truncated":false}
{"type":"branch.summary","id":"b1","session_id":"s1","parent_id":"m2","from_id":"m2","summary":"...","details":{"read_files":["README.md"],"modified_files":[]}}
{"type":"session.updated","id":"s1","leaf_id":"m2","updated_at":1700000009}
```

## Replay algorithm
- Read JSONL sequentially.
- Rebuild entry index and parent pointers.
- Track latest `session.updated.leaf_id` (fallback: last entry).
- Build context by walking from leaf to root.
- Branch summaries and compaction summaries are included in context.

## File sync note
File state is not restored in core. Optional hooks/extensions can implement git checkpoints and restore on branch/fork.
