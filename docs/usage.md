# Usage

## Modes

`--mode` is `text`, `json`, or `rpc`. Default: a TTY starts the interactive TUI;
otherwise print/text mode.

| Mode | Behaviour |
| --- | --- |
| interactive TUI | editor, slash commands, streaming |
| `--print` / `-p` | one prompt, print the answer, exit |
| `--mode json` | JSONL agent events on stdout ([json.md](json.md)) |
| `--mode rpc` | JSONL request/response on stdin/stdout ([rpc.md](rpc.md)) |

`--tui-mode` is `regular` or `fullscreen`.

## Root flags

| Flag | Description |
| --- | --- |
| `-p`, `--print` | Non-interactive: process prompt and exit |
| `--mode` | `text` / `json` / `rpc` |
| `--prompt` | Prompt text (same as a positional prompt) |
| `--config-dir` | Agent dir (default `~/.pigo/agent`; env `PIGO_CODING_AGENT_DIR`) |
| `--continue`, `-c` | Continue the latest session for this cwd |
| `--resume`, `-r` | Resume picker (interactive) or resume by `--session` |
| `--session` | Session path or id |
| `--no-session` | Do not persist |
| `--session-dir` | Override session storage directory |
| `--session-id` | Force the session file id |
| `--fork <path\|id>` | Fork a session into a new file |
| `--name`, `-n` | Session display name |
| `--provider` / `--model` / `--thinking` | Model selection; thinking: `off\|minimal\|low\|medium\|high\|xhigh\|max` |
| `--models` | Comma-separated patterns for Ctrl+P cycling |
| `--system-prompt` / `--append-system-prompt` | Replace or append system prompt (repeatable; text or file path) |
| `--no-context-files`, `-nc` | Skip `AGENTS.md` / `CLAUDE.md` |
| `--no-skills`, `-ns` / `--skill` | Disable skills or add extra skill paths |
| `--no-tools`, `-nt` / `--tools`, `-t` / `--exclude-tools`, `-xt` | Tool filters |
| `--no-builtin-tools`, `-nbt` | Disable built-in tools (extensions still register) |
| `--extension`, `-e` / `--no-extensions`, `-ne` | Load extra extension binaries / skip discovery |
| `--use-theme` / `--theme` / `--no-themes` | Theme name or extra theme files |
| `--prompt-template` / `--no-prompt-templates`, `-np` | Extra prompt templates |
| `--list-models` / `--list-models-query` | Print known models |
| `--offline` | Set `PIGO_OFFLINE=1` |
| `--export <session.jsonl> [out.html]` | Export a session to HTML |
| `--api-key` | In-process API key (not written to disk) |
| `--tui-mode` | `regular` \| `fullscreen` |
| `--no-sandbox` | Do not wrap bash in the sandbox |
| `--approve`, `-a` / `--no-approve`, `-na` | Trust / skip project-local resources this run |
| `--verbose` / `--version`, `-v` | |

Unknown `--flags` on the root command are held for extensions that
`register_flag`. Unclaimed leftovers become a CLI error.

## Subcommands

- `auth login|logout|print-api-key|print-bearer-token|check` — [auth.md](auth.md)
- `config` — TTY settings picker (`--print` dumps JSON; `--local` writes `.pigo/settings.json`)
- `install` / `remove` (`uninstall`) / `list` / `update` — [packages.md](packages.md)
- `server` / `client` — [server.md](server.md)
- cobra `help` / `completion`

## Slash commands (TUI)

Type `/` in the editor. Built-ins:

`settings`, `model`, `tree`, `thinking`, `scoped-models`, `image`, `export`,
`import`, `share`, `copy`, `name`, `session`, `changelog`, `hotkeys`, `help`
(`?`), `fork`, `clone`, `trust`, `login`, `logout`, `new`, `compact`, `resume`,
`reload`, `quit` (`exit`, `q`), `provider`, `theme`, `skills`, `tools`, `llama`,
`clear`.

File-based `/name` commands come from [prompt templates](prompt-templates.md)
and `/skill:name` from [skills](skills.md). Extensions can
`register_command`.

Prefix a line with `!` to run bash without going through the model.

`/image` generates an image through OpenRouter (`OPENROUTER_API_KEY`).
`/share` uploads via Radius when authenticated, otherwise a private GitHub gist
(`PIGO_SHARE_VIEWER_URL`).

## Context files

Unless `--no-context-files` is set, pigo appends `AGENTS.md` / `CLAUDE.md` from
the cwd (and `cwd/.pigo/AGENTS.md` when the project is trusted).
