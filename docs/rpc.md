# RPC mode

```bash
pigo --mode rpc
```

JSONL on stdin/stdout. The first stdout line is:

```json
{"type":"ready","provider":"anthropic","model":"claude-sonnet-4"}
```

Each request has a `type` (the command). Optional `id` is echoed on the
response:

```json
{"type":"response","command":"get_state","success":true,"id":1,"data":{…}}
```

Failures set `success` false and `error`. Unknown commands emit
`{"type":"error","message":"unsupported rpc command: …"}`.

## Commands

`quit` / `shutdown`, `abort`, `clear_queue`,
`prompt` (`message`, optional `streamingBehavior` = `steer`|`followUp`,
`images[]` with `data` + `mimeType`),
`steer`, `follow_up`, `new_session`, `get_state`, `set_model`, `cycle_model`,
`get_available_models`, `set_thinking_level`, `cycle_thinking_level`,
`get_available_thinking_levels`, `set_steering_mode`, `set_follow_up_mode`,
`compact` (`customInstructions?`), `set_auto_compaction`,
`bash` (`command`, `excludeFromContext?`), `abort_bash`,
`get_messages`, `get_last_assistant_text`, `set_session_name`,
`switch_session`, `clone`, `fork` (`entryId`), `get_fork_messages`,
`get_entries` (`since?`), `get_tree`, `navigate_tree`,
`export_html`, `get_commands`, `get_session_stats`,
`set_auto_retry`, `abort_retry`.

While a prompt is running, another `prompt` must set `streamingBehavior` or it
fails. Compaction in progress rejects `prompt`.

Agent JSON events (same shapes as [json.md](json.md)) are mixed onto stdout
during a run. Extension UI uses `extension_ui_request` /
`extension_ui_response`.

For a long-lived Unix socket instead of stdin/stdout, see [server.md](server.md).
