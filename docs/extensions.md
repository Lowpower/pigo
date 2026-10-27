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
blocks.

OpenAI Responses models with `compat.supportsAdditionalTools: true` keep that
stable prefix in the top-level `tools` array and attach each new batch with an
`additional_tools` item immediately after the tool result that added it. Models
with only `compat.supportsToolSearch: true` use a client-executed
`tool_search_call` / `tool_search_output` pair at that same point. When both
flags are set, `additional_tools` is used. Responses models with neither flag
still receive the full active list and no extra input items.

Chat Completions models with `compat.deferredToolsMode` set to `kimi` use the
same split. Each new batch is a `system` message whose `tools` field carries
the definitions, placed after the tool result that added them. Other Chat
Completions models still receive the full active list.

Fireworks only drops deferred schemas from the cached prefix when the loader is
named `ToolSearch` or `tool_search`. Other loader names still work, but the
loaded schemas stay in the initial tool prefix.

Do not remove currently active tools in the same `SetActiveTools` call if you
want deferred loading; a shrink or replace falls back to sending the whole
active list.

GLM and Kimi K3 on Fireworks use Chat Completions, not Messages, so the
Fireworks `tool_reference` protocol does not apply there. Kimi Chat Completions
can opt in with `compat.deferredToolsMode: "kimi"` instead.

## Lifecycle events (subscribe)

`session_start`, `session_shutdown`, `session_info_changed`,
`session_before_switch`, `session_before_fork`, `session_before_tree`,
`session_tree`, `session_before_compact`, `session_compact`,
`session_compact_failed`, `agent_start`, `agent_end`, `agent_before_settle`,
`turn_start`, `turn_end`,
`message_start`, `message_update`, `message_end`, `tool_call`,
`tool_execution_start`, `tool_execution_update`, `tool_execution_end`,
`tool_result`, `input`, `before_agent_start`, `context`, `context_with_system`,
`resources_discover`,
`project_trust`, `user_bash`, `before_provider_headers`,
`before_provider_request`, `after_provider_response`, `provider_stream_event`, `model_select`,
`thinking_level_select`, `ui_prompt_start`, `ui_prompt_end`,
`telemetry_span`, `agent_settled`, `cache_warming_decision`.

`telemetry_span` is fire-and-forget (`EmitEvent`, no `event_result`). The
payload is a `SpanRecord`: `name`, `trace_id`, `span_id`, `parent_span_id`,
unix-nano timestamps, `status`, `attrs`. Prompt text and tool arguments are
not included. Official sample: [`examples/extensions/telemetry`](../examples/extensions/telemetry).

`provider_stream_event` fires for each parsed provider stream event before pigo
normalizes it into `message_update`. The payload is `provider`, `api`, `model`,
and `data` (the earliest structured value the adapter has: raw JSON for HTTP/SSE,
or the SDK chunk encoded as JSON for Google and Bedrock). It does not add
request headers or API keys. The event is notification-only, is not written to
the session, and a handler error does not change the provider response. With no
subscriber the adapters skip this path. `data` can contain vendor fields that
`message_update`'s `assistantMessageEvent` (`text_delta` and the rest) drops.

`cache_warming_decision` is emitted before each cache refresh. The payload is
`warmCost`, `missCost`, `continuationProbability`, and `action` (`warm` or
`stop`). Return `{ "action": "warm" }` or `{ "action": "stop" }` to override
that decision. A handler error keeps pigo's decision. There is no event at
`session_start`.

Useful return payloads:

- `tool_call`: `{ "block": true, "reason": "…", "terminate": true? }` or `{ "input": { … } }` to rewrite args
- `input`: `{ "action": "handled" }` or `{ "action": "transform", "text": "…" }`
- `before_agent_start`: `{ "systemPrompt": "…" }` replaces the system prompt for this turn only and is not written to the session. `{ "forceSystemPrompt": "…" }` persists as the `preamble` section: on the first request it is part of the full declaration; once a system message exists it is a preamble-only patch, leaving other sections and tools in place.
- `context`: `{ "messages": [ … ] }` replaces the conversation for this request. System messages are not in the list. If the returned list differs, pigo puts the current prompt and tool declaration back at the front. An unchanged list leaves the request as it was.
- `context_with_system`: runs after `context`. `messages` includes the leading system message. The returned list is sent as-is, and index 0 must stay a system message.
- `turn_end` and `agent_before_settle`: `{ "entries": [ … ], "continue": true }`. Allowed entry types are `custom`, `custom_message`, `context_edit`, and `compaction`. Omitted fields keep the previous proposal. `continue: true` asks for one more provider request when this turn would otherwise stop; tool results, steering, and follow-ups already satisfy that request. Error and aborted turns ignore `continue`. `agent_settled` is notification-only and runs once the run will not continue.
- `project_trust`: `{ "block": true }`
- `user_bash`: omit the payload to run the local shell. Replace execution with `{ "result": { "output": "…", "cancelled": false, "truncated": false, "exitCode": 0 } }`. Any other payload, including `{ "block": true }` or `{ "operations": … }`, fails the command and does not run the local shell. A handler timeout or crash does the same.

## Virtual models

An extension can register a selectable model that picks a physical model
before each request. pigo does not embed JavaScript. Registration is one
`register_virtual_model` frame. Each request is one `route` / `route_result`
round trip.

```go
VirtualModels: []ext.VirtualModelDef{{
    Provider: "router", ID: "auto", Name: "Auto",
    ThinkingLevels: []string{"low", "high"},
    ContextWindow: 200000, MaxTokens: 8192,
    Route: func(ctx context.Context, req ext.RouteRequest) (ext.RouteResult, error) {
        id := "claude-haiku-4-5"
        if req.Reason == "direct" {
            id = "claude-sonnet-4-5"
        }
        return ext.RouteResult{
            Model: ext.RouteModel{Provider: "anthropic", ID: id},
            ThinkingLevel: "medium",
        }, nil
    },
}},
```

`route` runs only while that virtual model is selected. The request carries
`reason` (`user`, `continuation`, `retry`, `direct`), the selected thinking
level, `previous` (last successful physical reply), `failed` (retry only),
`state` (omitted for `direct`), and `messages` including the system message.
Return a physical model and thinking level. Returning `state` replaces the
in-memory state for the current session branch. `KeepState`, or a nil `State`,
leaves it. `direct` requests neither read nor store state.

The selected model stays virtual. Only that one call uses the physical
provider, model, and thinking level. The thinking level is clamped to the
physical model. A route error, a returned virtual model, or a model whose
provider has no credentials fails that request.

The same `provider` and `id` replaces a previous registration.
`unregister_virtual_model` removes it. `unregister_provider` does not.
Registering an id that already belongs to a physical model of that provider
is rejected. If a later catalog refresh adds that physical id, the virtual
model stays and hides it in `/model`, scoped models, and settings.

A virtual model listed under a real provider is selectable only when that
provider has credentials. A provider id that no provider uses is always
selectable. Session recording of the routed model is separate from this
registration.

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
`unregister_provider`, `register_virtual_model`, `unregister_virtual_model`,
`route` / `route_result` / `route_abort`, `subscribe`, `tool_call` / `tool_result`, `command`,
`shortcut`, `event` / `event_result`, `get_flag` / `flag_value`,
`oauth_login|refresh|get_api_key` / `oauth_result`, `refresh_models` /
`refresh_models_result`, `stream_start|event|abort`, `notify`,
`status_line_item`, `ui_request` / `ui_result`, `set_active_tools`,
`host_request` / `host_result`, `host_event` / `host_event_result`, `ping` /
`pong`, `shutdown` (host→ext), `shutdown_request` (ext→host).

Provider stream events: `start`, `text_*`, `thinking_*`, `toolcall_*`, `done`,
`error`.

Samples: [`examples/extensions/`](../examples/extensions/).
