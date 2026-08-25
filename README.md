# pigo

A Go reimplementation of the [pi](https://github.com/earendil-works/pi) coding agent
(agent loop, streaming LLM, built-in tools, TUI, session persistence, extensions).

**pi’s source is the behaviour reference.** pi keeps evolving; when adding or
changing a feature, read the corresponding files under `~/deps/pi` (or a fresh
clone of earendil-works/pi) and match that behaviour. Remaining work is tracked
as GitHub issues, not a phased migration plan.

## Requirements

- Go 1.27+

## Toolchain

The Cloud Agent environment installs everything via
[`.cursor/install.sh`](.cursor/install.sh): the Go 1.27 toolchain, `golangci-lint`,
and a read-only clone of pi at `~/deps/pi`.

To set up locally:

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
