# pigo

A Go reimplementation of the [pi](https://github.com/earendil-works/pi) coding agent
(agent loop, streaming LLM, built-in tools, TUI, session persistence, extensions).

The executable matches pi's control flow on the path that can be expressed in Go:
agent loop (`internal/agent`), streaming providers (`internal/ai`), built-in tools
(`internal/tools`), interactive TUI (`internal/tui`), JSONL sessions (`internal/session`),
slash commands / skills / prompt templates / themes, compaction, and a `--mode rpc`
subset. Remaining gaps (OAuth, npm, extra providers, interactive tree navigator) are
listed in [`docs/parity-gaps.md`](docs/parity-gaps.md).

The full, source-grounded implementation plan is in
[`docs/migration-plan.md`](docs/migration-plan.md).
**Development is grounded in the real pi source** (cloned read-only to `~/deps/pi`):
read the corresponding pi files before porting each module, and treat the source as
the authority over any summary.

## Requirements

- Go 1.27+

## Toolchain

The Cloud Agent environment installs everything automatically via
[`.cursor/install.sh`](.cursor/install.sh): the Go 1.27 toolchain, `golangci-lint`,
and a read-only clone of the `pi` reference repo at `~/deps/pi`.

To set up locally instead:

```bash
# Go 1.27 (https://go.dev/dl/) and golangci-lint on your PATH, then:
go mod download
```

## Build, run, test, lint

```bash
go build ./...                 # build everything
go run ./cmd/pi --version      # print version
go run ./cmd/pi --help         # usage
go run ./cmd/pi -p "hello"     # single non-interactive prompt (print mode)
go test ./...                  # run tests
golangci-lint run              # lint
```

### Flags (aligned with pi `cli/args.ts`)

| Flag | Description |
| --- | --- |
| `-p`, `--print` | Non-interactive: process prompt and exit |
| `--mode` | `text` / `json` / `rpc` (default: TTY → interactive, else text) |
| `--continue`, `-c` / `--resume`, `-r` / `--session` | Session resume |
| `--fork <path\|id>` | Fork a session into a new file |
| `--no-session` | Do not persist |
| `--name`, `-n` | Session display name |
| `--no-tools`, `-nt` / `--tools`, `-t` / `--exclude-tools`, `-xt` | Tool filters |
| `--no-skills`, `-ns` / `--skill` | Skills |
| `--no-context-files`, `-nc` | Skip AGENTS.md / CLAUDE.md |
| `--model` | `provider/id` and optional `:<thinking>` |
| `--models` | Comma-separated patterns for Ctrl+P cycling |
| `--thinking` | `off\|minimal\|low\|medium\|high\|xhigh\|max` |
| `--export <session.jsonl> [out.html]` | Export session to HTML |
| `--list-models` | List known models |
| `auth login\|logout\|print-api-key\|check` | API-key credentials |

Positional `@file` arguments are inlined as `<file name="...">` blocks (text only).

Configuration is read from `~/.pi/agent/settings.json` (override with
`PI_CODING_AGENT_DIR`) and can also be overridden with `PIGO_`-prefixed environment
variables.

## Layout

```
cmd/pi/            # entrypoint (cobra), flags aligned with pi
internal/
├── ai/            # StreamFn + provider adapters
├── agent/         # agent loop, tool scheduling, cancellation
├── tools/         # read bash edit write grep find ls
├── session/       # JSONL + tree + HTML export
├── compaction/    # history compaction
├── tui/           # bubbletea TUI
├── slash/         # built-in slash commands
├── skills/        # SKILL.md discovery
├── prompt/        # system prompt + prompt templates
├── theme/         # TUI themes
├── models/        # catalog + cycling
├── auth/          # auth.json
├── ext/           # extension system (subprocess RPC)
├── protocol/      # cross-process wire format
├── runtime/       # shared engine (print/json/rpc/TUI)
└── config/        # settings.json (~/.pi/agent)
```

## Reference

The original TypeScript implementation is cloned read-only to `~/deps/pi` in the
Cloud Agent environment. It is a reference only and is never imported by `pigo`.
