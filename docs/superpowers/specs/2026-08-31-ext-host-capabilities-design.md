# Extension host capabilities

Issue: [#15](https://github.com/Lowpower/pigo/issues/15)
Date: 2026-08-31

Fill in the host side of the existing subprocess RPC so an extension can
register slash commands, shortcuts, CLI flags, intercept a fixed set of
lifecycle events, register providers (including OAuth, catalog refresh, and
a custom stream), and drive shallow TUI surfaces. The loader stays a
subprocess. Session JSONL and `~/.pigo/agent` layouts do not change.

## Non-goals

- Do not load `.ts` / `.js` modules in-process.
- Do not mention TypeScript loading (or the lack of it) in the README.
- Do not add source-path citations or “align with …” comments anywhere in
  this repo.
- Do not add command-context RPC (`newSession`, `fork`, `navigateTree`,
  `switchSession`, `waitForIdle`, `reload` from the extension). `/reload`
  remains a host slash command.
- Do not implement `custom()` TUI components, Markdown transformers, or
  custom message/entry renderers.
- Do not pass a live bash `operations` object for `user_bash`; replacement
  is a finished `result` only.
- Do not accept an in-process provider object. Registration is JSON only.
- Do not bump `APIVersion`. Host and `ext.Serve` ship together.

## Architecture

One envelope (`protocol.Message`). `Host` collects registrations, writes
frames, and waits on IDs. Business logic lives in small capability pieces
that runtime, agent, cobra, and TUI call.

```
cobra (unknown flags)
  │
  ▼
runtime.Engine ── spawn ──► ext.Host ── stdin/stdout ──► extension process
  │                            │
  │                            ├─ commands / shortcuts (fire-and-forget)
  │                            ├─ events (wait event_result)
  │                            ├─ flags (get_flag / flag_value)
  │                            ├─ provider / oauth / refresh / stream
  │                            └─ ui_request (existing)
  ▼
slash / keys / models / auth / TUI overlays
```

| Piece | Role |
| --- | --- |
| `internal/protocol` | New type tags; reuse existing fields; `event` gains `id`. |
| `internal/ext.Host` | Registration tables, send, pending wait, thin `readLoop`. |
| `internal/ext.Serve` | SDK hooks for commands, shortcuts, flags, event results, OAuth, refresh, stream. |
| `internal/runtime` | Fan-out events; `/reload` already respawns hosts. |
| `internal/agent` / tools | `tool_call` before `Execute`; `tool_result` after. |
| `cmd/pigo` | Keep unknown flags; fail if still unclaimed after spawn. |
| `internal/models` / `internal/auth` / `internal/ai` | Dynamic provider, OAuth handlers, `StreamFn` wrap. |
| `internal/tui` / `internal/slash` / `internal/keys` | Commands, shortcuts, footer/widget/title, `SetUI`. |

`APIVersion` stays 1. An extension that only registers tools still
handshakes. A subscriber that never sends `event_result` hits the 60s
`CallTimeout` and is treated as empty continue.

`/reload` closes hosts and spawns again. Registrations, flags, shortcuts,
and providers come from the new processes. Providers this extension
registered last time are unregistered first. Old widgets and status keys
are cleared.

## Protocol

New types: `event_result`, `register_shortcut`, `shortcut`,
`register_flag`, `get_flag`, `flag_value`, `register_provider`,
`unregister_provider`, `oauth_login`, `oauth_refresh`,
`oauth_get_api_key`, `oauth_result`, `refresh_models`,
`refresh_models_result`, `stream_start`, `stream_event`, `stream_abort`.

Existing types reused: `register_command`, `subscribe`, `command`,
`event`, `notify`, `status_line_item`, `ui_request`, `ui_result`.

Unknown types and frames missing a required `id` are dropped. They must
not crash the host.

### Registration (ext → host, before `initialized`)

| type | fields |
| --- | --- |
| `register_command` | `name`, `description` |
| `register_shortcut` | `name` (key id, e.g. `ctrl+shift+p`), `description` |
| `register_flag` | `name`, `description`, `args`: `{type: "boolean"\|"string", default?}` |
| `subscribe` | `events` (union if sent more than once) |
| `register_provider` | `name` = provider id; `args` as in Providers |
| `unregister_provider` | `name` = provider id |

### Invoke (host → ext, no business reply)

- `command`: `name` + `text` (argument string after `/name`)
- `shortcut`: `name` (key id)

UI from those handlers still uses `ui_request`.

### Flags

1. Root cobra parse keeps unknown flags. The leftover list is taken from
   the original argv (not from a second invented parser): `--foo`,
   `--foo=bar`, and `--foo <token>` when `<token>` does not start with
   `-`.
2. After spawn, each `register_flag` claims a leftover. Boolean `--foo` →
   `true`. String uses `--foo=value` or the following token.
3. Any leftover after all extensions initialize → process exits non-zero.
4. Ext sends `get_flag` (`id`, `name`). Host replies `flag_value`
   (`id`, `payload.value` when set). Missing `value` means unset
   (extension uses its default).

`get_flag` is allowed after `ready`, including before `initialized`.

### Events

Host sends only to subscribers, in load order (discovery then CLI `-e`):

`event`: `id`, `event`, `payload`

Ext replies `event_result`: `id`, `payload`

Timeout (60s) or a dead process = empty continue, one error log, later
extensions still run. Each handler sees the payload after earlier
modifications.

| event | host payload | ext payload |
| --- | --- | --- |
| `tool_call` | `toolCallId`, `toolName`, `input` | `block`, `reason`, `terminate`, `input` (full replace) |
| `tool_result` | `toolCallId`, `toolName`, `input`, `content`, `isError` | `content`, `isError` |
| `before_agent_start` | `prompt`, `images?`, `systemPrompt` | `systemPrompt` |
| `session_before_compact` | `reason`, `willRetry`, `customInstructions?` | `cancel`, or `compaction` (replacement summary) |
| `user_bash` | `command`, `excludeFromContext`, `cwd` | `result` `{stdout, stderr, exitCode}` (extension already ran it) |
| `input` | `text`, `images?`, `source` | `action`: `continue` / `transform` / `handled`; transform may include `text` / `images` |
| `project_trust` | `cwd` | `trusted`: `yes` / `no` / `undecided`, `remember?` |

`tool_call` with `block` skips `Execute` and returns `reason` as the tool
error. `terminate` is honoured only together with `block`: after this
tool batch the agent loop stops.

Hooks:

- `tool_call` immediately before `Registry.Execute`
- `tool_result` immediately after
- extension `/name` before the `input` event
- `input` before skill / template expansion
- `before_agent_start` after the user turn is accepted, before the first
  provider call
- `session_before_compact` on the existing compact path (`BeforeTree` /
  compact hooks already reserved on `Engine`)
- `user_bash` on `!` / `!!` before the host shell runs
- `project_trust` on the trust prompt; `yes` / `no` skips the dialog

## Commands and shortcuts

Slash parse stays in `internal/slash`. Built-ins win on the same name.
A colliding extension command is invoked as `/name:2` (load order, first
duplicate is `:2`). `/help` and editor completion include extension
commands.

TUI `/name` and RPC `prompt` that starts with `/` both dispatch
`command` when the name is registered.

Shortcuts register into `internal/keys` after spawn. Built-in bindings
marked `restrictOverride` are skipped (warning). Two extensions on the
same key: later load wins (warning). `/hotkeys` lists them. Press sends
`shortcut`; no command-context.

## Providers

`register_provider` `args`:

- `name` (display), `baseUrl`, `api`, `apiKey` (literal or `$ENV` /
  `${ENV}`), `headers`, `authHeader`
- `models[]`: `id`, `name?`, `api?`, `baseUrl?`, `reasoning?`,
  `contextWindow?`, `maxTokens?`
- `oauth?`: `{name, isSubscription?}` — extension implements login /
  refresh / getApiKey
- `refreshModels?`: bool
- `stream?`: bool — LLM calls go to this extension, not `LookupAPI`

`api` must be an already registered API id
(`anthropic-messages`, `openai-completions`, `openai-responses`,
`google-generative-ai`, `bedrock-converse-stream`, `opencode`) unless
`stream` is true. Unknown `api` without `stream` → provider is not
installed; surface an error (notify or startup log).

Re-register merges defined fields over the previous registration.
`unregister_provider` removes the extension overlay; a built-in that was
hidden comes back.

Without `stream`, host calls `models.RegisterProvider` and existing
`ai.StreamFor` with resolved key and `baseUrl`.

### OAuth

If `oauth` is present, the provider is added to the auth registry and
`/login` list. Credentials stay in `auth.json` as `access` / `refresh` /
`expires`.

| dir | type | body |
| --- | --- | --- |
| host→ext | `oauth_login` | `id` |
| ext→host | `oauth_result` | `id` + `{access, refresh, expires}` |
| host→ext | `oauth_refresh` | `id` + current credential |
| ext→host | `oauth_result` | same shape |
| host→ext | `oauth_get_api_key` | `id` + current credential |
| ext→host | `oauth_result` | `{apiKey}` |

During login the extension uses `ui_request` / `notify` (URL, device
code, input, select). Cancel or timeout fails login. Before a request, if
the stored token is expired, `oauth_refresh` then `oauth_get_api_key`.

### Catalog refresh

When `refreshModels` is true, the provider’s `RefreshModels` hook sends
`refresh_models` (`id`) and waits for `refresh_models_result`
(`id`, `models[]`). That list replaces the extension-provided models.
Timeout keeps the last catalog. `--list-models` and the existing refresh
paths call this hook.

### Custom stream

When `stream` is true, that provider’s `StreamFn` does not use
`LookupAPI`:

1. host→`stream_start`: `id` + `{provider, model, api, system, messages,
   tools, options}` (`ai.Context` + `ai.Options`)
2. ext→many `stream_event`: `id` + `event`
   (`start` / `text_start` / `text_delta` / `text_end` /
   `thinking_start` / `thinking_delta` / `thinking_end` /
   `toolcall_start` / `toolcall_delta` / `toolcall_end` /
   `done` / `error`) and the matching `ai.Event` fields (`delta`,
   `contentIndex`, `content`, terminal `message`)
3. abort: host→`stream_abort` (`id`); extension should emit
   `error` / `aborted` and stop

Delta frames omit a full `partial` snapshot. The host rebuilds the
in-progress message through the existing `EventStream`. `done` / `error`
carry the final `message`. If the child dies or `ctx` cancels, the host
sends `stream_abort` and surfaces `error` / `aborted`.

## TUI and other modes

After spawn (and after `/reload`), every `Host` gets `SetUI`. Method
names match RPC: `select`, `confirm`, `input`, plus one-way `setWidget`
and `setTitle`. Dialogs reuse current overlays (picker / login), including
timeouts.

| method | TUI |
| --- | --- |
| `notify` | transcript meta (`note()`) |
| `status_line_item` | `name` is the key, `text` is the value; same key overwrites; empty `text` deletes |
| `setWidget` | `args`: `{key, lines[], placement?}` with `aboveEditor` (default) or `belowEditor`; empty `lines` deletes; strings only |
| `setTitle` | OSC window title |

print / json: dialogs return `cancelled`; `notify` goes to stderr;
widget / title are no-ops. RPC keeps `extension_ui_request` /
`extension_ui_response`.

## Errors

| case | behaviour |
| --- | --- |
| handshake timeout | spawn fails (existing 10s) |
| `event_result` / OAuth / refresh timeout or dead child | empty continue / login fail / keep last catalog; log once |
| stream hang or cancel | `stream_abort`, then `error` / `aborted` |
| unclaimed CLI flag | non-zero exit |
| command / shortcut name missing at press time | ignore |
| malformed or unknown frame | drop |

## SDK (`ext.Serve`)

`Handler` grows:

- `Commands []` `{Name, Description, Fn(args string)}`
- `Shortcuts []` `{Key, Description, Fn()}`
- `Flags []` `{Name, Description, Type, Default}` and `Flag(name) (value, ok)`
- `OnEvent(event, payload) payload` (return value becomes `event_result`)
- `OnOAuth(kind, cred) result` with `kind` one of `login`, `refresh`, `get_api_key`
- `OnRefreshModels() []model`
- `OnStream(req, emit, abort <-chan)` 

The hello example stays a single tool. A second executable under
`examples/extensions/` covers `/cmd`, one intercept, and a scripted
stream. No TypeScript examples.

## Tests

No live LLM and no live OAuth HTTP.

- protocol encode/decode for every new type
- `Host` helper process: register command + subscribe + `event_result`,
  flag claim, fake provider, scripted `stream_event`
- runtime: `tool_call` block, `input` transform, unclaimed flag fails,
  claimed flag `get_flag`
- cobra: unknown flags are kept until claim
- TUI: footer status, widget lines, slash list includes an extension
  command (existing model-test style)
- example binary: build and spawn through `Host`

## Files (expected)

- `internal/protocol/protocol.go` — type constants; `event` uses `id`
- `internal/ext/host.go` — tables, pending, dispatch
- `internal/ext/serve.go` — SDK hooks
- `internal/ext/*_test.go` — helper-process tests
- `internal/runtime` — event fan-out, provider apply, UI attach, reload
  cleanup
- `internal/agent` / `internal/tools` — tool intercept
- `cmd/pigo` — unknown-flag parse and post-spawn claim
- `internal/models`, `internal/auth`, `internal/ai` — register / OAuth /
  stream wrap
- `internal/tui`, `internal/slash`, `internal/keys` — commands, shortcuts,
  footer, widgets, title
- `examples/extensions/` — second executable

## Issue 15 acceptance

| Criterion | How |
| --- | --- |
| `register_command` → `/name`; host sends `command` | Commands and shortcuts |
| `subscribe` + intercept with block/modify | Events table |
| `registerShortcut` / `registerFlag` / `getFlag`; unknown CLI flags | Flags + shortcuts |
| `registerProvider` (id + endpoint) into the registry | Providers; plus OAuth, `refreshModels`, `stream` |
| TUI `notify`, keyed status, string widget, `setTitle`; dialogs share RPC UI | TUI section; `custom()` out |
| `/reload` restarts extension processes | Already on `Engine.Reload`; re-attach UI and drop old providers |
| Go / executable examples; no `.ts` requirement | SDK + examples |
