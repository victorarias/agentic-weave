# Overview

Agentic Weave is a small core for tool execution plus optional modules that add features when you need them.

## The Core Loop
1) You define tools.
2) You register tools.
3) Your agent decides when to call tools.
4) Tool results flow back to the agent and/or UI via events.

## Design Principles
- LLM-agnostic core
- Small interfaces for pluggability
- Optional modules for complexity
- Streaming-first UX

## Where to Start
- If you only need tools: read `docs/01-core.md`.
- If you need UI streaming: read `docs/02-streaming.md`.
- If you want advanced tool use: read `docs/05-advanced-tool-use.md`.
