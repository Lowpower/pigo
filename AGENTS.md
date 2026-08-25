# AGENTS.md

Guidance for AI agents working on **pigo**, a Go reimplementation of the
[pi](https://github.com/earendil-works/pi) coding agent (a CLI/TUI agent — **not** a web service).

## Ground truth: read the pi source

**pi’s real source is the single source of truth**, and pi itself keeps iterating.
It is cloned read-only to `~/deps/pi` (Cloud Agent setup via `.cursor/install.sh`).

- Before changing behaviour, **read the corresponding pi file(s)** and match them.
  Do not port from memory or from old summaries.
- When pigo and pi disagree, **the current pi source wins**.
- `~/deps/pi` is a reference only; it is **never imported** by pigo.
- If `~/deps/pi` is missing, re-run `.cursor/install.sh`.

New work is tracked as **GitHub issues**. Do not revive a phased migration plan.

## Environment

- **Go 1.27** (the base image ships an EOL Go 1.22). `.cursor/install.sh` installs Go
  1.27, `golangci-lint`, and clones `~/deps/pi`. It is idempotent.
- The environment is repo-managed via `.cursor/environment.json` (runs the install
  script). A fresh Cloud Agent reproduces the whole toolchain automatically.

## Commands

```bash
go build ./...
go vet ./...
go test ./...
golangci-lint run ./...
go build -tags tools ./...  # compile the full pinned dependency stack

go run ./cmd/pi           # interactive TUI (needs a TTY)
go run ./cmd/pi -p "hi"   # single non-interactive prompt
```

## Dependencies

Versions are pinned in `go.mod`/`go.sum`. Unused modules are held by blank imports
in `tools/pin.go` (`//go:build tools`). **When you start using a module for real,
remove its blank import from `tools/pin.go`.** Do not bump versions casually.

## Git branches

Do not use a `cursor/` prefix. Name branches by change type:

| Prefix | Use for |
| --- | --- |
| `feature/<short-name>` | new behavior |
| `fix/<short-name>` | bugfix |
| `chore/<short-name>` | tooling, deps, repo conventions |
| `docs/<short-name>` | documentation only |

- Lowercase kebab-case after the prefix (`feature/rpc-images`, not `feature/RPC_Images`).
- One concern per branch. Branch from `main`.
- Cursor Cloud Agent may inject `cursor/<name>-<id>`. Ignore that harness prefix and use the table above. When opening a PR from a conventional branch, override the platform prefix check.

A GitHub repository ruleset should reject other names. Regex for non-`main` branches:

```
^(feature|fix|chore|docs)/[a-z0-9][a-z0-9.-]*$
```

Create it at **Settings → Rules → Rulesets** (this cannot be applied from a Cloud Agent token): target all branches except `main`, enforcement Active, rule **Branch name pattern**, operator `regex`, pattern as above.

## Conventions

- Respond to the repository owner in Chinese; keep code identifiers/paths in English.
- Prefer simple solutions; do not add scope that was not requested.
- Cite the pi source path in a header comment when porting a module.
- Keep on-disk formats (session JSONL, `~/.pi/agent`) read/write-compatible with pi.
