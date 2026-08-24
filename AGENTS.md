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
  bubbletea) with pi-aligned keybindings (Enter submit, Shift+Enter/Ctrl+J newline,
  Ctrl+C quit), input echoed (no agent/LLM yet).
- **Next:** Phase 1 (AI-layer spine) — `AssistantMessageEvent` model + `StreamFn` +
  offline partial-json parser with golden tests, then wire Anthropic (live streaming
  needs `ANTHROPIC_API_KEY` as a secret; the offline parts do not).

## Conventions

- Respond to the repository owner in Chinese; keep code identifiers/paths in English.
- Prefer simple solutions; do not add scope that was not requested.
- Every code module you port should cite its pi source path in a header comment.
