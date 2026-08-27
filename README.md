# pigo

A CLI/TUI coding agent written in Go (agent loop, streaming LLM, built-in tools,
TUI, session persistence, extensions). Remaining work is tracked as GitHub
issues, not a phased migration plan.

## Install

Download the archive for your OS from
[GitHub Releases](https://github.com/Lowpower/pigo/releases), extract it, and
put `pigo` on your `PATH`.

| OS | Arch | Asset |
| --- | --- | --- |
| Linux | amd64 | `pigo_*_linux_amd64.tar.gz` |
| Linux | arm64 | `pigo_*_linux_arm64.tar.gz` |
| macOS | amd64 | `pigo_*_darwin_amd64.tar.gz` |
| macOS | arm64 | `pigo_*_darwin_arm64.tar.gz` |
| Windows | amd64 | `pigo_*_windows_amd64.zip` |
| Windows | arm64 | `pigo_*_windows_arm64.zip` |

Pushing a `vX.Y.Z` tag runs GitHub Actions: pack all six binaries, smoke-test
each on a matching hosted runner (`--version` / `--help`), then publish the
Release.

With a Go 1.27+ toolchain you can also install from source:

```bash
go install github.com/Lowpower/pigo/cmd/pigo@latest
```

## Requirements

- Go 1.27+ (only needed to build from source)

## Toolchain

The Cloud Agent environment installs everything via
[`.cursor/install.sh`](.cursor/install.sh): the Go 1.27 toolchain,
`golangci-lint`, and a read-only behaviour reference at `~/deps/pi` (for
working GitHub issues; never imported).

To set up locally:

```bash
# Go 1.27 (https://go.dev/dl/) and golangci-lint on your PATH, then:
go mod download
```

## Build, run, test, lint

```bash
go build ./...                 # build everything
go run ./cmd/pigo --version      # print version
go run ./cmd/pigo --help         # usage
go run ./cmd/pigo -p "hello"     # single non-interactive prompt (print mode)
go test ./...                  # run tests
golangci-lint run              # lint
```

### Flags

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

Configuration is read from `~/.pigo/agent/settings.json` (override with
`PIGO_CODING_AGENT_DIR`) and can also be overridden with `PIGO_`-prefixed environment
variables.

## Layout

```
cmd/pigo/            # entrypoint (cobra)
examples/extensions/ # sample extension (hello tool)
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
└── config/        # settings.json (~/.pigo/agent)
```
