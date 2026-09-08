# Auth

Storage: `~/.pigo/agent/auth.json` (mode 0600). Credential `type` is `api_key`
or `oauth`.

```bash
pigo auth login anthropic
pigo auth logout anthropic
pigo auth print-api-key anthropic
pigo auth print-bearer-token --min-expiry 60
pigo auth check --json
```

`pigo auth login <id>` also stores a key for a custom `models.json` provider id.
See [providers.md](providers.md).

`check` flags: `--json`, `--credentials`, `--no-refresh`.

## Built-in login targets

API key (env in parentheses): `anthropic` (`ANTHROPIC_API_KEY` and related),
`openai` / `openai-codex` (`OPENAI_API_KEY`), `opencode`, `openrouter`, `xai`,
`google` (`GEMINI_API_KEY`), `amazon-bedrock`, `llama.cpp`, plus catalog
providers (`OPENROUTER_API_KEY`, `GROQ_API_KEY`, …).

OAuth (device or loopback): `anthropic`, `openai-codex`, `github-copilot`,
`openrouter`, `kimi-coding`, `xai`, `radius`. Callback bind host:
`PIGO_OAUTH_CALLBACK_HOST` (default `127.0.0.1`).
