# AGENTS.md

Guidance for AI agents working on **pigo**, a Go reimplementation of the
[pi](https://github.com/earendil-works/pi) coding agent (a CLI/TUI agent — **not** a web service).

## Ground truth: read the pi source

**pi's real source is the single source of truth.** It is cloned read-only to
`~/deps/pi` (the Cloud Agent environment sets this up via `.cursor/install.sh`).

- Before porting any module, **read the corresponding pi file(s)** and match their real
  behavior. Do not port from memory or from summaries.
- `docs/migration-plan.md` is the grounded plan/index (module mapping, fidelity
  contracts, phases). It is a summary verified against source — when it disagrees with
  the actual pi source, **the source wins**, then fix the doc.
- `~/deps/pi` is a reference only; it is **never imported** by pigo.

If `~/deps/pi` is missing, re-run `.cursor/install.sh` (it clones pi).

## How to work

- **Follow `docs/migration-plan.md`. Implement phases in order (P0 → P7); do not skip.**
- Each phase is a testable end-to-end vertical slice.
- Keep pigo's on-disk formats (session JSONL, config dir) read/write-compatible with pi
  where the plan says so — see the fidelity contracts in `docs/migration-plan.md` §4.
- Known scaffold-vs-source corrections to apply in later phases are listed in
  `docs/migration-plan.md` §8 (e.g. config root is `~/.pi/agent/`, keys
  `defaultProvider`/`defaultModel`; real `--mode` values are `text|json|rpc`).

## Environment

- **Go 1.27** (the base image ships an EOL Go 1.22). `.cursor/install.sh` installs Go
  1.27, `golangci-lint`, and clones `~/deps/pi`. It is idempotent.
- The environment is repo-managed via `.cursor/environment.json` (runs the install
  script). A fresh Cloud Agent reproduces the whole toolchain automatically.

## Commands

```bash
go build ./...            # build (fast; the pinned-but-unused stack is excluded)
go vet ./...
go test ./...
golangci-lint run ./...   # must be 0 issues
go build -tags tools ./...  # compile the full pinned dependency stack (Go 1.27 compat check)

go run ./cmd/pi           # interactive TUI (needs a TTY)
go run ./cmd/pi -p "hi"   # single non-interactive prompt (print mode)
```

## Dependencies

The full planned stack is **version-pinned** in `go.mod`/`go.sum`. Not-yet-used modules
are held by blank imports in `tools/pin.go` (`//go:build tools`, never compiled into the
binary). **When you start using a module for real, remove its blank import from
`tools/pin.go`** — the real import keeps it required. Do not bump versions casually.
`goreleaser` is a build-time CLI tool, not a Go import; it is not in `go.mod`.

## Progress

- **Done:** Go 1.27 Cloud Agent environment; dependency stack pinned; source-grounded
  migration plan; **Phase 0** — minimal interactive TUI editor (`internal/tui`,
  bubbletea) with pi-aligned keybindings; **Phase 1** — AI-layer spine in
  `internal/ai`: `AssistantMessageEvent` model (`event.go`/`message.go`), `StreamFn` +
  `EventStream` (`stream.go`), partial-json parser (`jsonparse.go`, ports
  `utils/json-parse.ts`), Anthropic Messages **SSE→event adapter** (`anthropic.go`,
  ports `api/anthropic-messages.ts`, raw HTTP/SSE like pi, base-URL/key configurable),
  and a mock provider (`mock.go`). `cmd/pi -p` streams send→screen (mock when no key).
- **Anthropic auth:** `ANTHROPIC_API_KEY` + optional `ANTHROPIC_BASE_URL` (so an
  OpenAI/Anthropic-compatible gateway such as an opencode plan endpoint can be used).
  Live streaming needs the key/gateway; all Phase 1 tests are offline (SSE fixture +
  httptest), no key required.
  **Phase 2** — agent loop (`internal/agent`, ports `agent-loop.ts`): turn cycle,
  tool scheduling (sequential/parallel, source-ordered results), ctx cancellation,
  and the full `AgentEvent` stream (agent/turn/message/tool_execution). The loop is
  tool-agnostic (takes a `ToolExecutor`); mid-turn steering/follow-up queueing
  integrates with the TUI later.
- **Providers:** `anthropic-messages` (x-api-key) + `openai-completions` (Bearer,
  `internal/ai/openai.go`) + an **opencode** provider (`internal/ai/opencode.go`,
  reads `OPENCODE_API_KEY`/`OPENCODE_BASE_URL`, routes `claude-*` → `/v1/messages`,
  else → `/v1/chat/completions`). `DefaultStreamFn`: opencode → anthropic → mock. To
  use a real model, add `OPENCODE_API_KEY` (and optionally `OPENCODE_BASE_URL`) as a
  secret. Backlog (`docs/migration-plan.md` §4.9): `openai-responses` (opencode `gpt-*`),
  google/bedrock adapters, a generic provider registry.
  **Phase 3** — built-in tools (`internal/tools`, ports `core/tools/*`): read,
  write, edit (exact unique-match, go-diff), bash (process group + timeout), grep,
  find, ls. JSON-Schema params via `invopop/jsonschema`; globstar via `glob.go`
  (gobwas per segment). A `Registry` exposes them as `ai.Tool` and dispatches calls;
  wire it to the loop with `agent.ToolFunc(func(ctx, c){ return reg.Execute(ctx, c.Name, c.Args) })`.
  Deferred: `.gitignore` awareness in grep/find; Windows bash.
  **Phase 4** — session persistence (`internal/session`, ports `session-manager.ts`):
  JSONL append log; dir `~/.pi/agent/sessions/--<cwd with /\: → ->--/`, filename
  `<ISO with :. → ->_<uuid>.jsonl`, header `version:3`, entries `type/id/parentId/
  timestamp/message` (parentId tree). Buffer-until-first-assistant then flush-all,
  then append (matches pi). `Manager.AppendMessage(role, payload)` + `Load(path)`.
  Deferred: full pi AgentMessage field-parity for round-tripping pi's own files, and
  buildSessionContext/compaction/branching.
- **Next:** Phase 5 — TUI completion (`internal/tui`, ports `modes/interactive`):
  wire the agent loop into the TUI, streaming render (glamour after each turn), token
  batching (~30fps), autocomplete, themes. Also outstanding: multi-provider adapters
  (§4.9) to connect a real model.

## Conventions

- Respond to the repository owner in Chinese; keep code identifiers/paths in English.
- Prefer simple solutions; do not add scope that was not requested.
- Every code module you port should cite its pi source path in a header comment.
