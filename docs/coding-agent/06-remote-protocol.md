# Remote Protocol

## Transport
- Dial-out WebSocket
- No queueing: SaaS rejects commands if agent offline

## Agent -> SaaS
- hello { agent_id, name, version, capabilities }
- event { session_id, type, payload }
- stream { request_id, chunk }
- result { request_id, success, output, truncated? }
- status { session_id, state }
- heartbeat {}

## SaaS -> Agent
- run { request_id, session_id, input, model?, tools?, agent? }
- interrupt { session_id }
- config.update { ... }
- ping {}
