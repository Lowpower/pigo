# pigo

A Go reimplementation of the [pi](https://github.com/earendil-works/pi) coding agent
(agent loop, streaming LLM, built-in tools, TUI, session persistence, extensions).

This repository currently contains the **Phase 0 scaffold**: flag parsing (cobra),
configuration loading (viper), and the `internal/` package skeleton. The agent loop,
provider adapters, tools, and TUI land in later phases.

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

### Flags

| Flag              | Description                                   |
| ----------------- | --------------------------------------------- |
| `-p`, `--prompt`  | Run a single prompt non-interactively         |
| `--mode`          | `interactive` (default) or `print`            |
| `--config-dir`    | Config directory (default `~/.pi`)            |
| `--version`       | Print version and exit                        |

Configuration is read from `settings.json` in the config directory and can be
overridden with `PIGO_`-prefixed environment variables (e.g. `PIGO_PROVIDER`,
`PIGO_MODEL`, `PIGO_THEME`).

## Layout

```
cmd/pi/            # entrypoint (cobra), flags aligned with pi
internal/
├── ai/            # StreamFn abstraction + provider adapters   (Phase 1)
├── agent/         # agent loop, tool scheduling, cancellation  (Phase 2)
├── tools/         # read bash edit write grep find ls          (Phase 3)
├── session/       # JSONL append-only session log              (Phase 4)
├── compaction/    # history compaction
├── tui/           # bubbletea TUI                              (Phase 5)
├── ext/           # extension system (subprocess RPC)          (Phase 6)
├── protocol/      # cross-process wire format
├── server/        # headless server/RPC mode                  (Phase 7)
└── config/        # settings/auth storage (viper, ~/.pi)
```

## Reference

The original TypeScript implementation is cloned read-only to `~/deps/pi` in the
Cloud Agent environment. It is a reference only and is never imported by `pigo`.
