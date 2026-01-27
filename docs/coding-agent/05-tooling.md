# Tooling

## Built-in tools (MVP)

### read
- args: path, offset?, limit?
- returns raw text
- supports large files with paging

### write
- args: path, content, mode (overwrite/append)
- returns bytes written

## Tool result streaming
- If output > N bytes, emit tool.result.chunk events.

## Optional hooks (POC)
- Git checkpoint hook: stash at turn end and offer restore on branch/fork.
- Hooks are opt-in and should not change core behavior when disabled.
