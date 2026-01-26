# Agent Loop

## Flow
1) Receive input
2) Append message.created (user)
3) Call LLM
4) If tool call:
   - Append tool.call
   - Execute tool
   - Append tool.result (stream chunks if large)
5) Append assistant message.created + message.part

## Constraints
- One active loop per session.
- Interrupt cancels current model stream.
- Tool outputs streamed to EventBus + Remote.
