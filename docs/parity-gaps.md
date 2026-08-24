# pigo ↔ pi parity gaps

pigo aims to match pi's **user-visible behaviour and control flow**, not its TypeScript UI toolkit. This file is the issue backlog for work that is **not** in the current branch. Each section is written so it can be pasted into a GitHub issue.

`gh` is read-only in this environment, so these were not opened automatically.

## Done in this branch (vs previous main)

- Provider-facing **tool_use / tool_result pairing** (Anthropic + OpenAI Chat Completions)
- Agent loop replays assistant **tool-call blocks**, not flattened text
- `--print` / `--mode text|json|rpc` go through the **agent loop + tools**
- Config dir `~/.pi/agent/settings.json` with pi key names (`defaultProvider` / `defaultModel`)
- Session `--continue` / `--resume` / `--session` / `--no-session`
- Slash commands (subset with working handlers; rest listed in `/help`)
- Skills discovery (`SKILL.md`) + system-prompt injection
- Themes (built-in dark/light/default + `~/.pi/agent/themes`)
- Compaction wired before turns
- `--extension` subprocess loading
- `auth login|logout`, `config`, `--list-models`
- Tool allow/deny, `--no-tools`, `--no-skills`, `--system-prompt`, `--append-system-prompt`, `--no-context-files`
- Agent loop **replays assistant+toolResult pairing** across `Run` calls (not only inside one Run)
- Length-stop **fails** truncated tool calls instead of executing them (pi `failToolCallsFromTruncatedMessage`)
- Steering / follow-up injection inside the agent loop (`Config.Steering` / `Config.FollowUp`)
- Session resume rebuilds provider `ai.Message`s; persist skips already-written prefix
- `--continue`/`--resume` history is passed into print/json/TUI, not a blank transcript
- Anthropic `thinking` budget forwarded from `settings.thinking` / `--thinking`
- RPC subset: `prompt` (with steer/followUp while running), `steer`, `follow_up`, `abort`, `quit`, `new_session`, `get_state`, `set_model`, `get_available_models`, `set_thinking_level`, `compact`, `bash`, `get_messages`, `switch_session`, `clone`, `set_session_name`, `get_commands`
- Slash: `/fork`, `/name`, `/logout`, `/reload`, `/copy` (OSC-52), `/skill` + unknown `/name` skill expand
- CLI: `--fork`, `--session-id`, `--name`, `--api-key`, `--no-builtin-tools`

## Issues to file

### 1. Remaining pi `--mode rpc` commands

**Problem:** pigo now covers the headless prompt/session/model/thinking/compact/bash/clone subset. Still missing pi commands that need UI or extra transports: `images` on prompt, `cycle_model`, `cycle_thinking_level`, `abort_retry`, `abort_bash`, `export_html`, `get_tree`, `get_fork_messages`, `get_entries`, extension `select`/`confirm`/`input`.

**Why not here:** those need widget protocol + HTML renderer + retry/bash-session process tracking.

**Proposal:** port remaining commands from `packages/coding-agent/src/modes/rpc/` with golden JSON fixtures.

### 2. OAuth login (`/login`, `pi auth` device/browser flows)

**Problem:** pigo stores API keys in `auth.json`. pi supports OAuth for several providers (Claude, ChatGPT, Google, GitHub).

**Why not here:** needs browser redirect, token refresh, and provider-specific apps we do not ship.

**Proposal:** implement one provider (Anthropic) as a template, then the rest.

### 3. npm package manager (`pi install|remove|update|list`)

**Problem:** pi installs extensions/skills/themes from npm/git. pigo `--extension` only takes a local command.

**Why not here:** depends on npm as a runtime and pi's package-manager-cli.

**Proposal:** either shell out to `npm` with pi's package layout, or document a Go-native alternative and treat npm as out of scope.

### 4. Provider adapters: `openai-responses`, Google, Bedrock, generic registry

**Problem:** pigo has Anthropic Messages, OpenAI Completions, and OpenCode routing. pi's model catalog also uses Responses API, Gemini, Bedrock Converse, Mistral, etc.

**Proposal:** add a `Provider{id, auth, api}` registry (migration-plan §4.9) and port adapters incrementally.

### 5. Session tree UX (`/tree`, `/fork` UI, branch summaries, labels)

**Problem:** JSONL has `parentId` / `parentSession` and `/fork` plus `--fork` now create a child session. There is still no interactive tree navigator, labelled branches, or abandoned-path summaries.

**Proposal:** TUI list selector over `parentId` plus `AppendCompaction` for branch summaries.

### 6. TUI chrome parity (model picker, theme picker, mermaid, images, keybindings)

**Problem:** pigo's TUI is bubbletea+textarea. pi's interactive-mode is 6k+ lines of widgets, keybindings (`ctrl+p` model cycle, `alt+enter` follow-up, image paste, mermaid, etc.).

**Note:** migration plan explicitly does **not** clone `pi-tui`. Parity here means *behaviours*, not widgets.

**Proposal:** implement keybinding map from `keybindings.ts` one action at a time. Enter-while-running now steers; leftover queued lines run as follow-up when the turn ends. Still missing `alt+enter` follow-up-while-streaming, `ctrl+p` model cycle, image paste, mermaid.

### 7. sqlite session backend

**Problem:** `modernc.org/sqlite` is pinned but unused. pi can store sessions in sqlite.

**Proposal:** implement after JSONL resume/fork UX exists, behind a setting.

### 8. HTML export / gist share / changelog viewer

**Problem:** `/export` currently prints the JSONL path; `/share` and `/changelog` are stubs.

**Proposal:** port `export-html` and gist upload; changelog can read GitHub releases.

### 9. Project trust, sandbox, Windows bash, image tool results

**Problem:** pi has project-trust prompts, containerization docs, Windows shells, and `ImageContent` on tool results. pigo tools are Unix text-only.

**Proposal:** separate issues per platform/capability.

### 10. QueueMode `all` vs `one-at-a-time`

**Problem:** the loop now polls `Steering` / `FollowUp` after each turn (pi `getSteeringMessages` / `getFollowUpMessages`). Drain currently returns the whole queue (pi `all`). `one-at-a-time` and in-stream steering (inject before tool results finish) are not modelled.

**Proposal:** honor `steeringMode` / `followUpMode` from settings when draining the engine queues.
