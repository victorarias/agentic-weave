# Package Layout & Interfaces

## Package layout
```
/cmd/opencode-tui
/internal/supervisor
/internal/agent
/internal/eventbus
/internal/tui
/internal/remote
/internal/storage/jsonl
/internal/config
/internal/tools
```

## Key interfaces (Go)

### supervisor
```go
type Supervisor interface {
  Enqueue(input Input) error
  Interrupt(sessionID string) error
  NewSession(title string) (string, error)
  ListSessions() []SessionInfo
  LoadSession(sessionID string) error
}
```

### agent
```go
type Agent interface {
  Run(ctx context.Context, sessionID string, input Input) error
}
```

### eventbus
```go
type EventBus interface {
  Publish(evt Event)
  Subscribe() <-chan Event
}
```

### remote
```go
type RemoteClient interface {
  Start(cfg RemoteConfig) error
  Stop() error
  Send(evt Event) error
}
```

### storage/jsonl
```go
type Store interface {
  Append(evt StorageEvent) error
  Replay(sessionID string) ([]StorageEvent, error)
  ListSessions() []SessionInfo
}
```

### tools
```go
type Tool interface {
  ID() string
  Execute(ctx context.Context, args map[string]any) (ToolResult, error)
}
```
