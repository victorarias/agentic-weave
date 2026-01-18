# Agentic Weave

![Go](https://img.shields.io/badge/go-1.22%2B-blue)
![Status](https://img.shields.io/badge/status-early--stage-orange)

Pluggable, LLM-agnostic tooling framework for agentic systems.

## Quickstart (2 minutes)
```bash
go run ./examples/basic
```
Expected output:
```
The sum is 42.
```

## Start Here (progressive docs)
- `docs/00-overview.md` — orientation and principles
- `docs/01-core.md` — tools, registry, calls
- `docs/02-streaming.md` — events and turn boundaries
- `docs/03-optional-modules.md` — schema, skills, context, MCP
- `docs/04-adapters.md` — provider adapters and capabilities
- `docs/05-advanced-tool-use.md` — search, examples, defer-load, allowed callers
- `docs/06-context-budgets.md` — design for optional context budgets + compaction

## Examples
- `examples/basic` — streaming agent loop
- `examples/anthropic` — mocked Anthropic-style tool loop
- `examples/gemini` — mocked Gemini-style tool loop
- `examples/anthropic-real` — real Anthropic SDK (nested module)
- `examples/gemini-real` — real Gemini SDK (nested module)
- `examples/mono-like` — mono-style loop with compaction + truncation

## License
MIT
