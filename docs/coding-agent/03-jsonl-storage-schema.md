# JSONL Storage Schema

## File format
- One JSON object per line, append-only.
- Filename: `.opencode/sessions/<sessionID>.jsonl`

## Event types
- session.created
- session.updated
- message.created
- message.part
- tool.call
- tool.result

## Minimal schemas
```json
{"type":"session.created","id":"s1","title":"New","created_at":1700000000}
{"type":"message.created","id":"m1","session_id":"s1","role":"user","created_at":1700000001}
{"type":"message.part","message_id":"m1","part":{"type":"text","text":"hello"}}
{"type":"tool.call","id":"t1","message_id":"m2","tool":"read","args":{"path":"README.md"}}
{"type":"tool.result","id":"t1","output":"...","truncated":false}
```

## Replay algorithm
- Read JSONL sequentially.
- Rebuild session + message index.
- Rebuild message parts and tool results.
- Return in-memory session view to TUI.
