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

## Start Here
- `GETTING_STARTED.md` — minimal setup and first tool
- `docs/01-core.md` — core concepts (tools, registry, calls)
- `docs/02-streaming.md` — event streaming and turn boundaries
- `docs/03-optional-modules.md` — skills, context, MCP
- `docs/04-adapters.md` — provider adapters + capability flags
- `docs/05-advanced-tool-use.md` — tool search, examples, defer-load, allowed callers

## Examples
- `examples/basic` — streaming agent loop
- `examples/anthropic` — mocked Anthropic-style tool loop
- `examples/gemini` — mocked Gemini-style tool loop
- `examples/anthropic-real` — real Anthropic SDK (nested module)
- `examples/gemini-real` — real Gemini SDK (nested module)

## Docs Map (progressive)
1) **Core only**: tool definitions, registry, execute
2) **Streaming**: events, turn boundaries, deltas
3) **Optional modules**: skills, context, MCP
4) **Adapters**: Anthropic/Gemini capability flags
5) **Advanced features**: tool search, examples, defer-load, allowed callers

## License
TBD
