# Optional Modules

## Schema
- `schema.HashJSON` for stable schema hashing.
- `schema.SchemaFromStruct` for JSON schema from Go structs.

## Skills
- `skills.Source` to load skills from file or DB.
- File loader reads markdown with optional frontmatter.

## Context
- `context.Manager` compacts messages using a token counter + compaction hook.

## MCP
- `mcp.Registry` wraps an MCP client and gates by allowlist.

## Events
- `events.Event` and `events.Sink` for streaming agent updates.
