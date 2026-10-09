# Quickstart

## Install

Download a release archive for your OS from
[GitHub Releases](https://github.com/Lowpower/pigo/releases), extract it, and
put `pigo` on your `PATH`.

With Go 1.27+:

```bash
go install github.com/Lowpower/pigo/cmd/pigo@latest
```

## First run

```bash
pigo auth login anthropic          # or openai, openrouter, …
pigo                               # interactive TUI (needs a TTY)
pigo -p "summarize this repo"      # one prompt, then exit
```

Credentials live in `~/.pigo/agent/auth.json`. Settings live in
`~/.pigo/agent/settings.json`. Override the config root with
`PIGO_CODING_AGENT_DIR`.

## Typical flags

```bash
pigo --model anthropic/claude-sonnet-4:high "review this"
pigo --continue                    # resume the latest session for this cwd
pigo --tools read,grep,find,ls -p "review only"
pigo --export session.jsonl out.html
```

Positional `@file` arguments go into the first prompt. Text files are inlined
as `<file name="...">` blocks. PNG, JPEG, GIF, WebP, and BMP are attached as
images (BMP is sent as PNG). Other binary files are rejected.

See [Usage](usage.md) for the full flag and slash-command list.
