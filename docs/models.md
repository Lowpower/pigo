# Models

Specify a model as `provider/id` or `id`, optionally with `:<thinking>`:

```bash
pigo --model anthropic/claude-sonnet-4:high
pigo --thinking medium
pigo --models "claude-*,gpt-4o"
pigo --list-models
```

Thinking levels: `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`.
Per-level token budgets: `settings.thinkingBudgets`. Per-model defaults:
`settings.modelThinkingLevels`.

Ctrl+P / Shift+Ctrl+P cycle models that match `--models` and
`settings.enabledModels`. `/scoped-models` edits that list. `/model` opens the
selector. `/llama` manages a llama.cpp router.

## Catalog

Built-in providers include `anthropic`, `openai`, `opencode`, `google`,
`amazon-bedrock`, `llama.cpp`, `radius`, plus catalog providers such as
`openrouter`, `xai`, `github-copilot`, `deepseek`, `groq`, `mistral`,
`google-vertex`, `azure-openai-responses`, Cloudflare, MiniMax, Moonshot, Z.AI,
and others. `pigo --list-models` is the live list.

Remote overlays: `PIGO_CATALOG_BASE_URL` and `pigo update --models` (cache
`models-store.json`).

## User overlay

`~/.pigo/agent/models.json`:

```json
{
  "providers": {
    "local": {
      "baseUrl": "http://127.0.0.1:8080/v1",
      "api": "openai-completions",
      "models": [{ "id": "my-model", "contextWindow": 32768 }]
    }
  }
}
```

Unknown provider ids are registered. See [providers.md](providers.md).
