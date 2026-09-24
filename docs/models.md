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
      "apiKey": "sk-local",
      "models": [{ "id": "my-model", "contextWindow": 32768 }]
    }
  }
}
```

A model may declare prompt-cache lifetimes in seconds. Built-in Anthropic models use `short` 300 and `long` 3600. Other built-ins are unannotated. Cache warming runs only when the active retention tier has a lifetime.

```json
{ "id": "my-model", "promptCache": { "short": 300, "long": 3600 } }
```

Unknown provider ids are registered, including API-key auth. `apiKey` in
`models.json`, or the same id in `auth.json`, is enough for `--list-models` and
requests. Overlaying a custom gateway onto a builtin id such as `openai` still
merges catalogs; prefer a distinct provider id. See [providers.md](providers.md).

## compat

Per-model `compat` on a catalog entry:

- `supportsStrictMode`: Chat Completions sends `strict: true` only when this is
  `true` and the tool asked for constrained sampling. Unknown providers leave
  it off. `cerebras` never sends `strict`.
- `allowedFallbackModels`: Anthropic server-side fallbacks (`provider`, `model`,
  `cost`). A non-empty list is sent as `fallbacks`. An empty list omits the
  field. Overlaying an existing model replaces these two fields and keeps the
  rest of `compat`.
