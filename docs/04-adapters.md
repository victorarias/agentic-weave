# Provider Adapters

Adapters keep provider-specific behavior out of the core.

## Capability Flags
Adapters report supported features:
- Tool use
- Tool choice none
- Tool search, examples, defer-load
- Allowed callers
- Prompt caching, token counting
- Batching, models API
- Vision, code execution, computer use

## Examples
- `examples/anthropic` and `examples/anthropic-real`
- `examples/gemini` and `examples/gemini-real`

## Responsibilities
- Convert tool definitions to provider formats
- Enforce tool-use ordering rules
- Decide tool choice mode
- Decide which results enter model context
