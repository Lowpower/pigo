# AGENTS.md

Guidance for AI agents working on **pigo**, a CLI/TUI coding agent written in Go
(**not** a web service).

New work is tracked as **GitHub issues**. Do not revive a phased migration plan.

When working an issue that needs a behaviour reference, read the corresponding
files under `~/deps/pi` (cloned by `.cursor/install.sh`; never imported). If that
directory is missing, re-run `.cursor/install.sh`. Do not sprinkle source-path
citations or “align with …” comments into this repo.

GitHub issue and PR titles/bodies describe **pigo** behavior only: what changed,
how to use it, how it was tested. Do not name the Cloud Agent harness. Do not
frame work as copying, aligning with, or replicating another product.

## Environment

- **Go 1.27** (the base image ships an EOL Go 1.22). `.cursor/install.sh` installs Go
  1.27, `golangci-lint`, and the read-only clone at `~/deps/pi`. It is idempotent.
- The environment is repo-managed via `.cursor/environment.json` (runs the install
  script). A fresh Cloud Agent reproduces the whole toolchain automatically.

## Commands

```bash
go build ./...
go vet ./...
go test ./...
golangci-lint run ./...
go build -tags tools ./...  # compile the full pinned dependency stack

go run ./cmd/pigo           # interactive TUI (needs a TTY)
go run ./cmd/pigo -p "hi"   # single non-interactive prompt
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
- Keep on-disk formats (session JSONL, `~/.pigo/agent`) stable.
- Issue/PR text is user-facing: no harness names, no “复刻/对齐/align with” wording.
