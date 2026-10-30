# Providers

A provider is an id with a wire API, base URL, and credentials.

## Built-in

Core ids: `anthropic`, `openai`, `openai-codex`, `opencode`, `google`,
`amazon-bedrock`, `llama.cpp`, `radius`, `github-copilot`, `openrouter`, `xai`.

Additional catalog ids are registered from `internal/models` (OpenRouter-style
gateways, Cloudflare, Azure, Vertex, MiniMax, Moonshot, Z.AI, Meta, …). Run
`pigo --list-models` for the current set. `meta` uses `openai-responses` at
`https://api.meta.ai/v1` with offline model `muse-spark-1.3`.

Wire APIs include `anthropic-messages`, `openai-completions`,
`openai-responses`, `openai-codex-responses`, `google-generative-ai`,
`bedrock-converse-stream`, `google-vertex`, `azure-openai-responses`,
`mistral-conversations`, and `opencode`.

## Credentials

`pigo auth login <provider>` stores keys in `~/.pigo/agent/auth.json`.
OAuth is available for several providers (Anthropic, OpenAI ChatGPT
subscription, legacy OpenAI Codex, GitHub Copilot, OpenRouter, Kimi, Meta,
xAI, Radius). See [auth.md](auth.md).

`--api-key` injects a key for this process only.

## Azure

The built-in provider id is `azure`. Responses deployments keep the wire API
`azure-openai-responses`. `azure/deepseek-v4-pro` uses Chat Completions
(`openai-completions`). Add other Foundry chat deployments under `azure` in
`models.json` with `"api": "openai-completions"`.

This id used to be `azure-openai-responses`. pigo does not rewrite existing
config. Rename the old id by hand in three places:

- `auth.json`: the provider key (or run `pigo auth login azure`)
- `models.json`: the provider id
- `settings.json`: `defaultProvider`, `enabledModels` patterns, and
  `modelThinkingLevels` keys

A resumed session that still names the old provider falls back to another
model, and its prompt cache is not reused. `AZURE_OPENAI_*` environment
variable names are unchanged.

## Kimi Coding

The built-in provider id is `kimi-coding`. It uses Chat Completions
(`openai-completions`) at `https://api.kimi.com/coding/v1`.

`k3`, `k3-256k`, and `kimi-for-coding` accept tools added mid-conversation.
Those definitions go in a later `system` message, and the cached tool prefix
stays in the top-level `tools` list. `kimi-for-coding-highspeed` keeps added
tools in that top-level list.

A `models.json` override of this provider's `baseUrl` must include `/v1`.
pigo does not rewrite an existing file. A base without `/v1` is requested as
`https://api.kimi.com/coding/chat/completions`.

Sessions recorded with the previous Anthropic Messages shape are replayed as
Chat Completions. Their prompt cache is not reused.

## Custom providers

1. **models.json** — add a provider id, models, and optional `apiKey`
   ([models.md](models.md)). Unknown ids get API-key login (`pigo auth login <id>`
   or a key under that id in `auth.json`).
2. **Extension** — `register_provider` plus `OnStream` / OAuth hooks
   ([extensions.md](extensions.md)). The `capdemo` example serves a scripted
   stream with no network.
