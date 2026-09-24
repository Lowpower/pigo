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

`ext.Notify`, `ext.Status`, `ext.UI`, and `ext.HostCall` send frames to the
host. `UI` and `HostCall` wait for a result. Call them from a tool, command,
shortcut, or event handler (not during handshake).

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
`thinking_level_select`, `ui_prompt_start`, `ui_prompt_end`,
`telemetry_span`, `agent_settled`, `cache_warming_decision`.

`telemetry_span` is fire-and-forget (`EmitEvent`, no `event_result`). The
payload is a `SpanRecord`: `name`, `trace_id`, `span_id`, `parent_span_id`,
unix-nano timestamps, `status`, `attrs`. Prompt text and tool arguments are
not included. Official sample: [`examples/extensions/telemetry`](../examples/extensions/telemetry).

`cache_warming_decision` is emitted before each cache refresh. The payload is
`warmCost`, `missCost`, `continuationProbability`, and `action` (`warm` or
`stop`). Return `{ "action": "warm" }` or `{ "action": "stop" }` to override
that decision. A handler error keeps pigo's decision. There is no event at
`session_start`.

Useful return payloads:

- `tool_call`: `{ "block": true, "reason": "…", "terminate": true? }` or `{ "input": { … } }` to rewrite args
- `input`: `{ "action": "handled" }` or `{ "action": "transform", "text": "…" }`
- `before_agent_start`: `{ "systemPrompt": "…" }`
- `project_trust`: `{ "block": true }`
- `user_bash`: omit the payload to run the local shell. Replace execution with `{ "result": { "output": "…", "cancelled": false, "truncated": false, "exitCode": 0 } }`. Any other payload, including `{ "block": true }` or `{ "operations": … }`, fails the command and does not run the local shell. A handler timeout or crash does the same.

## Host calls (`ext.HostCall` / `host_request`)

From a tool, command, shortcut, or event handler:

```go
info, err := ext.HostCall("session.info", nil)
_ = ext.Notify("cwd="+fmt.Sprint(info["cwd"]), "info")
_ = ext.Status("demo", "ok")
res, err := ext.UI("confirm", map[string]any{"title": "Continue?"})
_ = ext.RequestShutdown() // host exits when idle (not os.Exit)
```

Session / runtime: `mode`, `getContextUsage`, `isIdle`, `hasPendingMessages`,
`abort`, `shutdown`, `waitForIdle`, `getSystemPrompt`,
`getSystemPromptOptions`, `compact`, `reload`, `getActiveTools`, `getAllTools`,
`setActiveTools`, `getCommands`, `setModel`, `getThinkingLevel`,
`setThinkingLevel`, `session.info`, `session.entries`, `session.branch`,
`session.leaf`, `appendEntry`, `setLabel`, `setSessionName`, `getSessionName`,
`sendMessage`, `sendUserMessage`, `newSession`, `fork`, `navigateTree`,
`switchSession`, `exec`, `events.on`, `events.emit`.

`sendMessage` `deliverAs`: `steer` / `followUp` / `nextTurn`. `triggerTurn:false`
must not insert between a tool call and its result (steering is drained after
tools). Idle `steer` with `triggerTurn:true` starts a turn.

Model registry: `model.list` / `model.getAll`, `model.getAvailable`,
`model.find`, `model.hasConfiguredAuth`, `model.stream` / `model.streamSimple`
(events via `host_event` `model.stream`), `model.complete`, `model.refresh`,
`model.getApiKeyAndHeaders`, `model.getProviderAuth`,
`model.getApiKeyForProvider` (these three return an error; credentials stay
on the host), `model.isUsingOAuth`, `model.getProvider`,
`model.getProviderDisplayName`, `model.getProviderAuthStatus`.

Render / complete: `registerMessageRenderer`, `registerEntryRenderer`,
`registerMarkdownTransformer`, `registerToolRenderer`,
`addAutocompleteProvider`. The host asks back with `host_event`
(`render.message` / `render.entry` / `render.tool` / `markdown.transform` /
`autocomplete.query` / `command.complete`). Return `{text}` / `{markdown}` /
`{items}`.

`ext.EventsOn` / `ext.EventsEmit` is a host-forwarded bus (extensions do not
talk to each other).

## UI (TUI / RPC)

`notify`, `status_line_item`, and `ui_request` / `host_request` UI methods:

- dialogs: `select`, `confirm`, `input`, `editor` (title + prefill)
- `setWidget`, `setTitle`, `set_editor_text`, `getEditorText`, `pasteToEditor`
- `setFooter` / `setHeader`: whole-page `[]string`; empty restores the default.
  `status_line_item` only applies to the default footer. `getFooterData` is a
  snapshot (`gitBranch`, `statuses`, `tokens`, `cost`, `modelId`, `cwd`)
- working: `setWorkingMessage`, `setWorkingVisible`, `setWorkingIndicator`
  (`{frames, intervalMs}`; `[]` hides; omit frames to restore)
- `setHiddenThinkingLabel`, `setToolsExpanded` / `getToolsExpanded`
- themes: `getAllThemes`, `getTheme`, `getCurrentTheme`, `setTheme`
- `custom.open` / `update` / `close` / `focus` / `unfocus` / `hide` /
  `setHidden`: declarative widgets (`text`, `markdown`, `select`, `input`,
  `list`, `buttons`). Overlay is `{overlay: true}` plus static
  `overlayOptions` (`anchor`, `width`, `margin`). Interaction is `host_event`
  `custom.change` / `custom.submit` / `custom.cancel`. Unknown widget `kind`
  is skipped
- `terminal_input.subscribe`: host pushes `host_event` `terminal.input`;
  return `{consume, data?}`

The editor component itself is not swappable. Unknown UI methods still return
`{cancelled: true}`. RPC clients answer dialogs via `extension_ui_response`.

## Wire types

`hello`, `ready`, `initialized`, `register_tool|command|shortcut|flag|provider`,
`unregister_provider`, `subscribe`, `tool_call` / `tool_result`, `command`,
`shortcut`, `event` / `event_result`, `get_flag` / `flag_value`,
`oauth_login|refresh|get_api_key` / `oauth_result`, `refresh_models` /
`refresh_models_result`, `stream_start|event|abort`, `notify`,
`status_line_item`, `ui_request` / `ui_result`, `set_active_tools`,
`host_request` / `host_result`, `host_event` / `host_event_result`, `ping` /
`pong`, `shutdown` (host→ext), `shutdown_request` (ext→host).

Provider stream events: `start`, `text_*`, `thinking_*`, `toolcall_*`, `done`,
`error`.

Samples: [`examples/extensions/`](../examples/extensions/).
