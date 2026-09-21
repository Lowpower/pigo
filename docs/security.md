# Security

pigo runs tools on your machine (or in an optional Docker container). Treat
project-local resources and credentials as the two trust boundaries.

## Credentials

| Location | What |
| --- | --- |
| `~/.pigo/agent/auth.json` | API keys and OAuth tokens (mode `0600`) |
| Provider env vars | `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `OPENROUTER_API_KEY`, … |
| `--api-key` | In-process only; not written to disk |

Override the config root with `PIGO_CODING_AGENT_DIR` (or `--config-dir`).
See [auth.md](auth.md).

Print a stored secret only when you mean to: `pigo auth print-api-key` /
`print-bearer-token`. OAuth loopback binds `127.0.0.1` by default
(`PIGO_OAUTH_CALLBACK_HOST`).

## Project trust

Local `.pigo/` files, ancestor `.agents/skills`, and project `sandbox.json`
do **not** load until the project is trusted. User `~/.agents/skills` always
loads.

Resolution order:

1. `--approve` / `--no-approve` (this process only)
2. No project-local resources → treated as trusted
3. Saved decision in `~/.pigo/agent/trust.json` (cwd or a parent)
4. `settings.defaultProjectTrust`: `ask` (default), `always`, or `never`

`ask` without a saved decision is untrusted. In the TUI use `/trust`, then
restart the session so project resources actually load. Print / JSON / RPC
modes never prompt; they stay untrusted until `--approve` or a stored
`always`.

Untrusted projects skip `cwd/.pigo/settings.json`, `SYSTEM.md`,
`APPEND_SYSTEM.md`, prompts, themes, skills, extensions, package trees under
`.pigo/npm` / `.pigo/git`, and `cwd/.pigo/sandbox.json`.

## Extensions

An extension is a **subprocess**, not an in-process plugin. The host talks
length-prefixed JSON over stdin/stdout. pigo does not load `*.ts` in-process.

Load paths (see [extensions.md](extensions.md)):

- `-e` / `--extension` (local command, `npm:<pkg>`, or `git:<url>`)
- `~/.pigo/agent/extensions/`
- package manifests and `settings.extensions[]`

`--no-extensions` skips discovery. Explicit `-e` still loads.
Project-local extension trees need trust, the same as other `.pigo/`
resources.

Extensions can register tools, intercept events (including `tool_call`),
and call host methods. Only run extensions you would run as a binary on
this machine.

## Tool isolation

Two optional layers; both off by default. `--no-sandbox` disables both.

**OS sandbox** ([tools.md](tools.md#sandbox)): Linux/darwin only. Wraps
**host bash** via `sandbox.json` (`bwrap` on Linux, seatbelt on Darwin).
Skipped when Docker isolation is on. Windows never wraps.

**Docker** ([tools.md](tools.md#docker-tools-in-container)): opt-in
`settings.container.image`. pigo and `auth.json` stay on the host.
`read` / `write` / `edit` / `bash` / `grep` / `find` / `ls` and `!` / `!!`
run in a session-long container. API keys and OAuth tokens are never passed
into the container, even if named in `container.env`. Extension tools and
Windows `powershell` stay on the host. Missing Docker is an error, not a
fallback to the host.
