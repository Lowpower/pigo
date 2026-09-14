# JSON event stream

```bash
pigo --mode json -p "hello"
```

Stdout is JSONL. The session header is written first (same object as the
session file header), then agent events. RPC mode (`--mode rpc`) mixes the same
agent/session event shapes onto stdout while a turn runs.

| `type` | When |
| --- | --- |
| `agent_start` / `agent_end` | Loop begin / end (`agent_end` includes `messages`, `willRetry`) |
| `turn_start` / `turn_end` | One model+tools round (`turn_end` has `message`, `toolResults`) |
| `message_start` / `message_update` / `message_end` | Streaming assistant (or user/toolResult on start/end) |
| `tool_execution_start` / `update` / `end` | Tool call (`toolCallId`, `toolName`, `args`, `result`, `isError`) |
| `agent_settled` | JSON print loop finished (retries/overflow included) |
| `queue_update` | Steering / follow-up queues changed (`steering` and `followUp` string arrays) |
| `compaction_start` | Compaction began (`reason`: `manual`, `threshold`, `overflow`, …) |
| `compaction_end` | Compaction finished (`reason`, `result`, `aborted`, `willRetry`; failure may add `errorMessage`) |
| `entry_appended` | A session JSONL entry was written (`entry`) |
| `auto_retry_start` | Agent auto-retry backoff started (`attempt`, `maxAttempts`, `delayMs`, `errorMessage`) |
| `auto_retry_end` | Auto-retry finished (`success`, `attempt`, `finalError`) |
| `summarization_retry_scheduled` | Compaction/summary LLM call will retry (`attempt`, `maxAttempts`, `delayMs`, `errorMessage`) |
| `summarization_retry_attempt_start` | That retry attempt begins (`source`, `reason`, plus the scheduled fields) |
| `summarization_retry_finished` | Summarization retry loop ended (success or give-up) |

`message_update` carries usage plus the provider assistant-message event; it
does not repeat the full cumulative text.

`compaction_end.result` (when present) has `summary`, `firstKeptEntryId`,
`tokensBefore`, `estimatedTokensAfter`.

This mode is for logs and pipelines. For a bidirectional control channel use
[RPC mode](rpc.md).
