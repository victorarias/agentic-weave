# Provider Capabilities

Capabilities keep provider-specific feature flags out of the core.

## Capability Flags
Capability adapters report supported features:
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
- `capabilities/vertex` for Vertex Gemini capability flags.

## Responsibilities
- Provide a stable capability surface for feature gating.
- Optionally surface model limits and usage (see `limits` and `usage` packages).
- Provider packages own message conversion and request building.

## Helper Utilities
- `capabilities.StopReasonFromFinish` maps provider finish reasons to `usage.StopReason`.
- `capabilities.NormalizeUsage` fills missing usage totals.
