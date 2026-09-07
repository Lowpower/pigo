# pigo documentation

User-facing docs for the pigo CLI/TUI coding agent. They describe the
implementation in this repository.

pigo does **not** export a stable Go library. Embeddings go through
`--mode rpc` / `--mode json` or `pigo server` / `pigo client`. Extensions are
subprocess binaries that speak the framed JSON protocol (`internal/ext`).

## Start here

- [Quickstart](quickstart.md)
- [Usage](usage.md) — flags, slash commands, TUI
- [Settings](settings.md)
- [Environment variables](environment.md)
- [Sessions](sessions.md) / [session JSONL](session-format.md) / [compaction](compaction.md)

## Customization

- [Extensions](extensions.md)
- [Skills](skills.md)
- [Prompt templates](prompt-templates.md)
- [Themes](themes.md)
- [Packages](packages.md)
- [Models](models.md)
- [Providers](providers.md)
- [Keybindings](keybindings.md)
- [Tools](tools.md)
- [Auth](auth.md)

## Programmatic usage

- [RPC mode](rpc.md)
- [JSON event stream](json.md)
- [Unix session server](server.md)

Working samples live under [`examples/`](../examples/).
