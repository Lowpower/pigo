# Extensions

An extension is a **subprocess**. The host spawns a binary (or script) and
talks over stdin/stdout using length-prefixed JSON frames (`internal/protocol`,
API version 1, max 16 MiB).

There is no in-process plugin loader. Write a small program that calls
`ext.Serve`.

## Load paths

- `-e` / `--extension` (repeatable): a local command, or `npm:<pkg>` / `git:<url>` which install for this process without writing settings
- `~/.pigo/agent/extensions/` (executable, `.js`/`.ts`, or `index.js`/`index.ts`)
- package manifests and `settings.extensions[]`

`--no-extensions` skips discovery. Explicit `-e` still loads, including `npm:` / `git:`. `/reload` restarts extension processes.

`npm:` / `git:` reuse an already-installed tree (`~/.pigo/agent/npm|git` or a trusted project copy) when present, then spawn using the same discovery as `pigo install` (manifest / `index.*` / `extensions/`, executable or `#!` shebang). Persist with `pigo install` if the next session should auto-load the package. pigo does not load `*.ts` in-process.

Minimal tool:

```bash
go build -o /tmp/hello-ext ./examples/extensions/hello
pigo -e /tmp/hello-ext -p "say hello to pigo"
pigo -e npm:some-ext -p "hi"
pigo -e git:github.com/org/repo -p "hi"
```

Copy the binary into `~/.pigo/agent/extensions/` for auto-discovery.

## Authoring (`ext.Serve`)

```go
err := ext.Serve(ext.Handler{
    Name: "hello",
    Tools: []ext.ToolDef{{
        Name: "hello", Description: "Greet", Schema: map[string]any{"type": "object"},
        Fn: func(ctx context.Context, args map[string]any) (string, bool) {
            return "Hello", false // (result, isError)
        },
    }},
    Commands:  []ext.CommandDef{{Name: "cmd", Description: "slash", Fn: func(string) {}}},
    Shortcuts: []ext.ShortcutDef{{Name: "ctrl+shift+g", Description: "…", Fn: func() {}}},
    Flags:     []ext.FlagDef{{Name: "plan", Description: "plan mode", Type: "boolean", Default: false}},
    Providers: []ext.ProviderDef{{ID: "demo", Args: map[string]any{"name": "demo", "stream": true}}},
    Events:    []string{"tool_call", "input"},
    OnEvent: func(event string, payload map[string]any) map[string]any {
        return nil // or {"block": true, "reason": "…"} / {"action": "transform", "text": "…"}
    },
    OnStream: func(req map[string]any, emit func(string, map[string]any), abort <-chan struct{}) {
        emit("start", map[string]any{})
        // text_start / text_delta / text_end / done
    },
})
```

`ext.Serve` refuses tools whose `Schema` is missing or not `"type": "object"`.
The host also ignores such `register_tool` frames and notifies an error.

`ext.Flag(name)` returns CLI values claimed during handshake. Unknown root
`--flags` are offered to `register_flag`; leftovers error.

`ext.SetActiveTools(names)` sends `set_active_tools` to the host. Unknown names
are ignored. Call it from a tool or a `session_start` handler.

## Dynamic tool loading

Register every tool, keep a small loader active, and activate more tools during
execution with `ext.SetActiveTools`. A purely additive change is recorded on
that tool result as `addedToolNames`. The next model request then sees the new
definitions.

On Fireworks Messages (and on any `anthropic-messages` model with
`compat.supportsToolReferences: true`), late tools are sent with
`defer_loading: true` and loaded at the tool result via `tool_reference`
blocks. Other providers still receive the full active list with no extra
fields.

Fireworks only drops deferred schemas from the cached prefix when the loader is
named `ToolSearch` or `tool_search`. Other loader names still work, but the
loaded schemas stay in the initial tool prefix.

Do not remove currently active tools in the same `SetActiveTools` call if you
want deferred loading; a shrink or replace falls back to sending the whole
active list.

GLM and Kimi K3 on Fireworks use Chat Completions, not Messages, so this
protocol does not apply there.

## Lifecycle events (subscribe)

`session_start`, `session_shutdown`, `session_info_changed`,
`session_before_switch`, `session_before_fork`, `session_before_tree`,
`session_tree`, `session_before_compact`, `session_compact`,
`session_compact_failed`, `agent_start`, `agent_end`, `turn_start`, `turn_end`,
`message_start`, `message_update`, `message_end`, `tool_call`,
`tool_execution_start`, `tool_execution_update`, `tool_execution_end`,
`tool_result`, `input`, `before_agent_start`, `context`, `resources_discover`,
`project_trust`, `user_bash`, `before_provider_headers`,
`before_provider_request`, `after_provider_response`, `model_select`,
`thinking_level_select`, `ui_prompt_start`, `ui_prompt_end`.

Useful return payloads:

- `tool_call`: `{ "block": true, "reason": "…", "terminate": true? }` or `{ "input": { … } }` to rewrite args
- `input`: `{ "action": "handled" }` or `{ "action": "transform", "text": "…" }`
- `before_agent_start`: `{ "systemPrompt": "…" }`
- `project_trust` / `user_bash`: `{ "block": true }`

## UI (TUI / RPC)

Extensions may send `notify`, `status_line_item`, and `ui_request` methods:
`select`, `confirm`, `input`, `setWidget`, `setTitle`, `set_editor_text`.
RPC clients answer via `extension_ui_response`. Full custom TUI widgets are not
embedded as host components.

## Wire types

`hello`, `ready`, `initialized`, `register_tool|command|shortcut|flag|provider`,
`unregister_provider`, `subscribe`, `tool_call` / `tool_result`, `command`,
`shortcut`, `event` / `event_result`, `get_flag` / `flag_value`,
`oauth_login|refresh|get_api_key` / `oauth_result`, `refresh_models` /
`refresh_models_result`, `stream_start|event|abort`, `notify`,
`status_line_item`, `ui_request` / `ui_result`, `set_active_tools`, `ping` /
`pong`, `shutdown`.

Provider stream events: `start`, `text_*`, `thinking_*`, `toolcall_*`, `done`,
`error`.

Samples: [`examples/extensions/`](../examples/extensions/).
