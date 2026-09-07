# Providers

A provider is an id with a wire API, base URL, and credentials.

## Built-in

Core ids: `anthropic`, `openai`, `openai-codex`, `opencode`, `google`,
`amazon-bedrock`, `llama.cpp`, `radius`, `github-copilot`, `openrouter`, `xai`.

Additional catalog ids are registered from `internal/models` (OpenRouter-style
gateways, Cloudflare, Azure, Vertex, MiniMax, Moonshot, Z.AI, …). Run
`pigo --list-models` for the current set.

Wire APIs include `anthropic-messages`, `openai-completions`,
`openai-responses`, `openai-codex-responses`, `google-generative-ai`,
`bedrock-converse-stream`, `google-vertex`, `azure-openai-responses`,
`mistral-conversations`, and `opencode`.

## Credentials

`pigo auth login <provider>` stores keys in `~/.pigo/agent/auth.json`.
OAuth is available for several providers (Anthropic, OpenAI Codex, GitHub
Copilot, OpenRouter, Kimi, xAI, Radius). See [auth.md](auth.md).

`--api-key` injects a key for this process only.

## Custom providers

1. **models.json** — add a provider id and models ([models.md](models.md)).
2. **Extension** — `register_provider` plus `OnStream` / OAuth hooks
   ([extensions.md](extensions.md)). The `capdemo` example serves a scripted
   stream with no network.
