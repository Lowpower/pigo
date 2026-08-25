# pigo ↔ pi parity gaps

pigo aims to match pi's **user-visible behaviour and control flow**, not its TypeScript UI toolkit. This file is the issue backlog for work that is **not** in the current branch. Each section is written so it can be pasted into a GitHub issue.

`gh` is read-only in this environment, so these were not opened automatically.

## Done in this branch (vs previous main)

- Provider-facing **tool_use / tool_result pairing** (Anthropic + OpenAI Chat Completions)
- Agent loop replays assistant **tool-call blocks**; length-stop **fails** truncated tools
- Steering / follow-up injection; **QueueMode `one-at-a-time` (default) vs `all`**
- `--print` / `--mode text|json|rpc` through the agent loop + tools
- Config dir `~/.pi/agent/settings.json` with pi key names
- Session `--continue` / `--resume` / `--session` / `--no-session` / `--fork <path|id>` / `--session-id` / `--name`
- Session **parentId tree** (`GetTree`, `/tree` text dump, RPC `get_tree` / `get_entries` / `get_fork_messages`)
- Slash commands with working handlers (pi `BUILTIN_SLASH_COMMANDS` plus `/help`)
- Skills (`SKILL.md`) and **prompt templates** (`prompts/*.md`, `$1` / `$@` substitution)
- Themes, compaction, `--extension`, `auth login|logout|print-api-key|check`
- CLI aliases from `cli/args.ts` (`-nt`/`-ns`/`-nc`/`-ne`/`-xt`/`-nbt`/`-np`), `@file` inlining, `--model provider/id[:thinking]`, `--models` cycling
- TUI keybindings: Enter steer, Alt+Enter follow-up, Escape interrupt, Ctrl+D exit, Ctrl+P / Shift+Ctrl+P cycle model, Shift+Tab cycle thinking
- HTML export (`--export`, `/export`, RPC `export_html`) — self-contained dump, not pi's themed template
- RPC envelope `{type:response, command, success, data}` plus `cycle_model`, `cycle_thinking_level`, `fork` by `entryId`, `clone`, `export_html`

## Issues to file

### 1. Remaining pi `--mode rpc` commands that need extra runtime

**Problem:** still missing `images` on prompt/steer/follow_up, `abort_retry`, `abort_bash`, auto-retry, and extension `select`/`confirm`/`input` UI over RPC.

**Why not here:** needs in-flight bash process tracking, retry state, image content blocks, and a widget protocol.

**Proposal:** port remaining commands from `packages/coding-agent/src/modes/rpc/` with golden JSON fixtures.

**Source:** `packages/coding-agent/src/modes/rpc/rpc-types.ts`

---

### 2. OAuth login (`/login`, `pi auth` device/browser flows, `print-bearer-token`)

**Problem:** pigo stores API keys in `auth.json` and can print/check them. pi supports OAuth for Claude, ChatGPT, Google, GitHub, including refresh and bearer-token export.

**Why not here:** needs browser redirect, token refresh, and provider-specific apps we do not ship.

**Proposal:** implement one provider (Anthropic) as a template, then the rest.

**Source:** `packages/coding-agent/src/cli/auth-command.ts`, provider OAuth modules under `packages/ai`

---

### 3. npm package manager (`pi install|remove|update|list`)

**Problem:** pi installs extensions/skills/themes from npm/git. pigo `--extension` only takes a local command.

**Why not here:** depends on npm as a runtime and pi's package-manager-cli.

**Proposal:** either shell out to `npm` with pi's package layout, or document a Go-native alternative and treat npm as out of scope.

**Source:** `packages/coding-agent/src/cli/` install/remove/update/list commands

---

### 4. Provider adapters: `openai-responses`, Google, Bedrock, generic registry

**Problem:** pigo has Anthropic Messages, OpenAI Completions, and OpenCode routing. pi's model catalog also uses Responses API, Gemini, Bedrock Converse, Mistral, etc. SDKs are already pinned in `go.mod`.

**Why not here:** each adapter is a large, independently testable port with live-API fixtures.

**Proposal:** add a `Provider{id, auth, api}` registry (migration-plan §4.9) and port adapters incrementally.

**Source:** `packages/ai/src/api/*`, `packages/ai/src/providers/*`

---

### 5. Interactive session-tree navigator (labels, fold, branch summaries)

**Problem:** JSONL has `parentId` / `parentSession`; `/tree` prints a text dump and RPC `get_tree` returns the forest. There is still no interactive TUI navigator, labelled branches, or abandoned-path summaries.

**Why not here:** this is widget UX (`tree-selector.ts`), not the tree data model.

**Proposal:** TUI list selector over `GetTree()` plus `AppendCompaction` for branch summaries.

**Source:** `packages/coding-agent/src/modes/interactive/components/tree-selector.ts`

---

### 6. TUI chrome that is widget-not-behaviour (model picker UI, mermaid, images)

**Problem:** keybindings that map to existing actions are in. Still missing: model-selector overlay (`ctrl+l`), image paste, mermaid, external editor, alt-screen search.

**Note:** migration plan explicitly does **not** clone `pi-tui`. Parity here means *behaviours*, not widgets.

**Proposal:** implement remaining actions from `keybindings.ts` one at a time when the supporting feature exists.

**Source:** `packages/coding-agent/src/core/keybindings.ts`

---

### 7. sqlite session backend

**Problem:** `modernc.org/sqlite` is pinned but unused. pi can store sessions in sqlite.

**Proposal:** implement after JSONL resume/fork UX exists, behind a setting.

**Source:** `packages/session-backends/sqlite-node`

---

### 8. Themed HTML export / gist share / changelog viewer

**Problem:** `/export` and `--export` now write a self-contained HTML dump. pi's exporter uses `export-html/template.{html,css,js}` plus highlight.js/marked. `/share` (secret gist) and `/changelog` are stubs.

**Proposal:** port the template assets; gist upload; changelog can read GitHub releases.

**Source:** `packages/coding-agent/src/core/export-html/`

---

### 9. Project trust, sandbox, Windows bash, image tool results

**Problem:** pi has project-trust prompts, containerization docs, Windows shells, and `ImageContent` on tool results. pigo tools are Unix text-only. `@file` inlines text; image `@file` attachments are not sent to the model.

**Proposal:** separate issues per platform/capability.

**Source:** `packages/coding-agent/src/core/tools/*`, `cli/file-processor.ts`

---

### 10. `/scoped-models` interactive editor

**Problem:** `--models` already scopes Ctrl+P cycling. pi's `/scoped-models` opens a TUI to enable/disable/reorder that list and persist it to settings.

**Proposal:** settings.json `enabledModels` plus a simple slash editor, or keep CLI-only.

**Source:** `packages/coding-agent/src/modes/interactive/components/settings-selector.ts`
