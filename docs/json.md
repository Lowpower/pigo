# JSON event stream

```bash
pigo --mode json -p "hello"
```

Stdout is JSONL. The session header is written first (same object as the
session file header), then agent events.

| `type` | When |
| --- | --- |
| `agent_start` / `agent_end` | Loop begin / end (`agent_end` includes `messages`, `willRetry`) |
| `turn_start` / `turn_end` | One model+tools round (`turn_end` has `message`, `toolResults`) |
| `message_start` / `message_update` / `message_end` | Streaming assistant (or user/toolResult on start/end) |
| `tool_execution_start` / `update` / `end` | Tool call (`toolCallId`, `toolName`, `args`, `result`, `isError`) |

`message_update` carries usage plus the provider assistant-message event; it
does not repeat the full cumulative text.

This mode is for logs and pipelines. For a bidirectional control channel use
[RPC mode](rpc.md).
