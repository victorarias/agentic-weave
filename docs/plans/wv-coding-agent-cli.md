# Plan: wv — A Coding Agent CLI

## Context

Build a terminal coding agent (like pi-mono's `pi`) that uses agentic-weave as its agentic core and gopher-lua for extensibility. The agent should have a full TUI with differential rendering, streaming LLM output, tool result display, and a Lua extension system with live reload.

Pi-mono is the reference implementation (TypeScript/Node.js). We're building the Go equivalent, optimized for speed and low memory.

## Module Strategy

**Separate Go module** inside the same repo: `cmd/wv/` has its own `go.mod`. This means:
- `go get github.com/victorarias/agentic-weave` pulls only the core library
- `go install github.com/victorarias/agentic-weave/cmd/wv@latest` installs the CLI
- gopher-lua, chroma, x/term stay out of the core module's dependency tree

This follows the nested module pattern already used in `examples/anthropic-real/`.

## Architecture Overview

```
cmd/wv/
  go.mod                ← module github.com/victorarias/agentic-weave/cmd/wv
  main.go               ← CLI entrypoint
  tui/                  ← Custom differential renderer (like pi-tui)
  tui/components/       ← UI components (editor, markdown, tool output, etc.)
  session/              ← Agent session management (wraps agentic-weave loop)
  tools/                ← Built-in coding tools (bash, read, write, edit, grep, glob, ls)
  extensions/           ← Lua extension loader, API, and reload
  config/               ← Configuration and settings
```

### Key Decision: Custom TUI vs Bubbletea

**Custom line-based renderer** (like pi-mono). Rationale:
- Pi-mono proved this approach works well for coding agents
- Simpler mental model: components → lines → diff → output
- Lower overhead than bubbletea's MVU loop
- Direct control over rendering for streaming text
- No framework abstractions between us and the terminal
- Dependencies: only `golang.org/x/term` for raw mode

We use `golang.org/x/term` for raw terminal mode and write ANSI escape codes directly. The renderer compares line arrays frame-to-frame and only outputs diffs. Synchronized output (CSI 2026) prevents tearing.

---

## Phase 1: Skeleton + Minimal TUI + Agent Loop

**Goal**: Type a message, get a streamed LLM response displayed in the terminal.

### 1.1 Project structure

```
cmd/wv/
  main.go                          ← CLI entrypoint
  tui/
    tui.go                         ← Renderer: line diff, raw mode, input loop
    terminal.go                    ← Terminal abstraction (write, cursor, size)
    component.go                   ← Component interface
    ansi.go                        ← ANSI utilities (visible width, truncate, wrap)
  tui/components/
    container.go                   ← Groups child components
    text.go                        ← Static text with word wrap
    markdown.go                    ← Markdown → styled terminal lines
    editor.go                      ← Single/multi-line input with cursor
    loader.go                      ← Animated spinner
  session/
    session.go                     ← Wraps loop.Runner, manages conversation state
  config/
    config.go                      ← Load settings from ~/.wv/ and .wv/
```

### 1.2 Component interface

```go
// cmd/wv/tui/component.go
type Component interface {
    Render(width int) []string  // Returns styled lines
}

type InputHandler interface {
    HandleInput(data []byte) bool  // Returns true if consumed
}

type Container struct {
    Children []Component
}
```

### 1.3 Differential renderer

```go
// cmd/wv/tui/tui.go
type TUI struct {
    term          *Terminal
    root          *Container
    previousLines []string
    previousWidth int
}

func (t *TUI) Render() {
    lines := t.root.Render(t.term.Width())
    // Strategy 1: first render → output all
    // Strategy 2: width changed → clear + full render
    // Strategy 3: normal → find first diff line, output from there
    // Wrap in synchronized output (CSI 2026)
}
```

### 1.4 Session wrapping agentic-weave

```go
// cmd/wv/session/session.go
type Session struct {
    runner  *loop.Runner
    history []message.AgentMessage
    // events channel bridges loop events → TUI updates
    events  chan events.Event
}

func (s *Session) Send(userMessage string) {
    // Run loop.Runner.Run in a goroutine
    // Emit events to s.events channel
    // TUI reads from channel and updates components
}
```

### 1.5 Layout

```
┌─────────────────────────────┐
│ header (model name, status) │
├─────────────────────────────┤
│                             │
│ chat messages               │
│   user message              │
│   assistant response        │
│   (streaming...)            │
│                             │
├─────────────────────────────┤
│ status (spinner/info)       │
├─────────────────────────────┤
│ editor (input)              │
└─────────────────────────────┘
```

### 1.6 Files to modify/create

- `cmd/wv/go.mod` — new
- `cmd/wv/main.go` — new
- `cmd/wv/tui/tui.go` — new
- `cmd/wv/tui/terminal.go` — new
- `cmd/wv/tui/component.go` — new
- `cmd/wv/tui/ansi.go` — new
- `cmd/wv/tui/components/container.go` — new
- `cmd/wv/tui/components/text.go` — new
- `cmd/wv/tui/components/markdown.go` — new
- `cmd/wv/tui/components/editor.go` — new
- `cmd/wv/tui/components/loader.go` — new
- `cmd/wv/session/session.go` — new
- `cmd/wv/config/config.go` — new

### 1.7 Dependencies to add

- `golang.org/x/term` — raw terminal mode
- `github.com/alecthomas/chroma/v2` — syntax highlighting for code blocks

### Verification

- Run `cd cmd/wv && go build -o wv . && ./wv`
- Type a message, see streamed response from Anthropic
- Ctrl+C to exit
- No flicker during streaming

---

## Phase 2: Built-in Coding Tools

**Goal**: The agent can read, write, edit files and run commands.

### 2.1 Tool implementations

```
cmd/wv/tools/
  bash.go       ← Execute shell commands (with timeout, working dir)
  read.go       ← Read file contents (with line range support)
  write.go      ← Write file (create or overwrite)
  edit.go       ← String replacement in files (old_string → new_string)
  grep.go       ← Search file contents (regex, glob filter)
  glob.go       ← Find files by pattern
  ls.go         ← List directory contents
```

Each tool implements `agentic.Tool`. All registered in a `agentic.Registry`.

### 2.2 Tool output rendering

- `cmd/wv/tui/components/tool_output.go` — Collapsible tool result display
  - Pending state: gray background, spinner
  - Success: green accent, truncated preview
  - Error: red accent, error message
  - Expand/collapse with Ctrl+O

### 2.3 System prompt

- `cmd/wv/session/system_prompt.go` — Builds the system prompt with:
  - Agent identity and capabilities
  - Available tools and their descriptions
  - Working directory context
  - Git status if in a repo
  - CLAUDE.md / project instructions if present

### Verification

- Ask the agent to read a file → see file contents in tool output
- Ask the agent to create a file → file appears on disk
- Ask the agent to run `ls` → see directory listing
- Tool outputs are collapsible

---

## Phase 3: Lua Extension System

**Goal**: Load `.lua` extensions that register tools and subscribe to events. Support `/reload`.

### 3.1 Extension loader

```go
// cmd/wv/extensions/loader.go
type Loader struct {
    state      *lua.LState
    extensions []Extension
}

// Discovery paths:
// 1. ~/.wv/extensions/     (global)
// 2. .wv/extensions/       (project-local)

func (l *Loader) Load() error {
    l.state = lua.NewState()
    l.registerAPI(l.state)  // Expose wv.* module
    // Scan directories for *.lua files
    // DoFile each one
}

func (l *Loader) Reload() error {
    l.state.Close()
    return l.Load()  // Fresh VM, re-execute all
}
```

### 3.2 Lua API surface (`wv` module)

```lua
local wv = require("wv")

-- Register a tool
wv.register_tool({
    name = "my_tool",
    description = "Does something useful",
    input_schema = {
        type = "object",
        properties = {
            query = { type = "string", description = "Search query" }
        },
        required = { "query" }
    },
    execute = function(params)
        -- params is a Lua table decoded from JSON input
        return { text = "result: " .. params.query }
    end
})

-- Subscribe to events
wv.on("tool_start", function(event)
    -- event.tool_call.name, event.tool_call.input, etc.
end)

wv.on("message_end", function(event)
    -- event.content, event.role, etc.
end)

-- Register a slash command
wv.register_command("hello", {
    description = "Say hello",
    handler = function(args)
        wv.send_message("Hello from Lua!")
    end
})

-- Access config
local model = wv.config.model
local cwd = wv.config.working_dir
```

### 3.3 Go-side API registration

```go
// cmd/wv/extensions/api.go
func (l *Loader) registerAPI(L *lua.LState) {
    mod := L.NewTable()

    L.SetField(mod, "register_tool", L.NewFunction(l.luaRegisterTool))
    L.SetField(mod, "on", L.NewFunction(l.luaOn))
    L.SetField(mod, "register_command", L.NewFunction(l.luaRegisterCommand))
    L.SetField(mod, "send_message", L.NewFunction(l.luaSendMessage))
    L.SetField(mod, "config", l.buildConfigTable(L))

    L.PreloadModule("wv", func(L *lua.LState) int {
        L.Push(mod)
        return 1
    })
}
```

### 3.4 Lua tools as agentic.Tool

```go
// cmd/wv/extensions/lua_tool.go
// Wraps a Lua function as an agentic.Tool
type LuaTool struct {
    def     agentic.ToolDefinition
    state   *lua.LState
    handler *lua.LFunction
}

func (t *LuaTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    // Decode call.Input to Lua table
    // Call t.handler
    // Encode return value to JSON
}
```

### 3.5 Event dispatch

```go
// cmd/wv/extensions/events.go
// Bridges agentic events → Lua callbacks
type EventBridge struct {
    handlers map[string][]*lua.LFunction
    state    *lua.LState
}

func (b *EventBridge) Emit(e events.Event) {
    // Convert event to Lua table
    // Call each registered handler
}
```

### 3.6 Reload command

Built-in `/reload` slash command:
1. Unregister all Lua-provided tools from the registry
2. Call `loader.Reload()` (close VM, create new, re-execute)
3. Re-register all newly declared tools
4. Display confirmation in TUI

### 3.7 Files

- `cmd/wv/extensions/loader.go` — new
- `cmd/wv/extensions/api.go` — new
- `cmd/wv/extensions/lua_tool.go` — new
- `cmd/wv/extensions/events.go` — new

### 3.8 Dependencies to add

- `github.com/yuin/gopher-lua` — Lua 5.1 VM

### Verification

- Create `~/.wv/extensions/hello.lua` with a simple tool
- Start wv, verify tool is available to the LLM
- Modify the lua file, run `/reload`, verify new behavior
- Test event subscriptions fire on tool calls

---

## Phase 4: TUI Polish

**Goal**: Full-featured TUI matching pi-mono's UX.

### 4.1 Editor improvements

- Multi-line editing (Shift+Enter for newline)
- Horizontal scrolling for long lines
- Ctrl+A/E for line start/end
- Ctrl+W for word delete
- Ctrl+U/K for line kill
- History navigation (up/down through previous messages)

### 4.2 Slash commands + autocomplete

- `/model` — switch model
- `/reload` — reload extensions
- `/compact` — force context compaction
- `/clear` — clear conversation
- `/help` — show available commands
- Tab completion in editor when `/` is typed

### 4.3 Markdown rendering

- Headings (colored, bold)
- Code blocks with syntax highlighting (chroma)
- Inline code, bold, italic
- Links (OSC 8 hyperlinks)
- Lists, blockquotes
- Streaming-aware: re-render accumulated buffer on each chunk

### 4.4 Tool output polish

- Per-tool renderers:
  - `bash`: truncated output preview, expandable
  - `read`: file content with line numbers
  - `write`: file path confirmation
  - `edit`: diff display (old → new)
  - `grep`: matched lines with context
- Thinking blocks: collapsible, dimmed italic

### 4.5 Overlay system

- `cmd/wv/tui/overlay.go` — Modal rendering on top of main content
- Anchor-based positioning (center, top, bottom)
- Used for: model selector, settings, confirmation dialogs

### 4.6 Theming

- `cmd/wv/tui/theme.go` — Color palette and style definitions
- Auto-detect dark/light terminal background
- Configurable via `~/.wv/theme.lua` (Lua table defining colors)

### Verification

- Full editing experience (multi-line, keybindings)
- Slash commands work with autocomplete
- Markdown renders correctly during streaming
- Tool outputs show diffs, code highlighting
- Overlay dialogs work for model selection

---

## Phase 5: Session Management + Advanced Features

**Goal**: Persistent sessions, conversation forking, and advanced agent capabilities.

### 5.1 Session persistence

- `cmd/wv/session/store.go` — Save/load conversations to `~/.wv/sessions/`
- JSON-lines format for append-only history
- `/resume` command to list and resume sessions

### 5.2 Configuration

```
~/.wv/
  config.lua          ← Global settings (model, keybindings, theme)
  extensions/         ← Global extensions
  sessions/           ← Persisted conversations
  theme.lua           ← Color customization

.wv/
  config.lua          ← Project-local overrides
  extensions/         ← Project-local extensions
  instructions.md     ← Project context (like CLAUDE.md)
```

### 5.3 Non-interactive mode

- `wv -c "fix the bug in main.go"` — Run a single prompt, output to stdout, exit
- `echo "explain this" | wv` — Pipe mode
- Useful for scripting and CI

### 5.4 Git awareness

- Auto-detect git repo, show branch in header
- Include git status in system prompt
- Tools respect `.gitignore` for file search

### Verification

- Close and resume a conversation
- Project-local config overrides global
- Non-interactive mode works in pipelines
- Git info appears in header and system prompt

---

## Summary of New Dependencies

| Package | Purpose |
|---------|---------|
| `golang.org/x/term` | Raw terminal mode, terminal size |
| `github.com/yuin/gopher-lua` | Lua 5.1 VM for extensions |
| `github.com/alecthomas/chroma/v2` | Syntax highlighting in code blocks |

## Testing Strategy

- **TUI components**: Unit test `Render(width)` output (pure string arrays, no terminal needed)
- **Tools**: Unit test with temp directories and fixtures
- **Lua extensions**: Integration test loading a `.lua` file and verifying tool registration
- **Session**: Unit test conversation state management
- **E2E**: Script that runs `wv -c "..."` and checks output (needs API key)
