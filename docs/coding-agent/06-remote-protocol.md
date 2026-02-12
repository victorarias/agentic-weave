# Remote Protocol

## Transport
- WebSocket (local server stub + client).
- Remote is optional; local-only flows work unchanged.

## Handshake
Client -> server:
```json
{"type":"hello","client_id":"c1","protocol_version":1,"capabilities":["poll-v1"]}
```

Server -> client:
```json
{"type":"ready","server_id":"s1","protocol_version":1}
```

## Commands (client -> server)
```json
{"type":"command.send","command_id":"cmd1","session_id":"s1","input":"ls -la","meta":{"mode":"user"}}
```

## Output polling (combined stream)
Single output stream per session. Clients poll using a cursor.

Client -> server:
```json
{"type":"output.poll","session_id":"s1","since":42,"limit":200,"timeout_ms":5000}
```

Server -> client:
```json
{"type":"output.batch","session_id":"s1","events":[{"seq":43,"kind":"stdout","data":"..."}],"next":43,"reset":false}
```

## Notes
- `since` is the last seen cursor; `next` is the newest cursor in the batch.
- If the server cannot honor `since` (evicted history), it replies with `reset:true` and the latest cursor.
- Command timeouts are warnings; commands may still complete later and appear in the output stream.
