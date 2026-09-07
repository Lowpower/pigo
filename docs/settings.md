# Settings

Global file: `~/.pigo/agent/settings.json` (or `$PIGO_CODING_AGENT_DIR/settings.json`).

Trusted projects may also read `cwd/.pigo/settings.json`. Project files cannot
set `defaultProjectTrust`.

Viper loads the JSON file, then applies `PIGO_`-prefixed environment variables
that match field names (`PIGO_PROVIDER`, `PIGO_MODEL`, …).

## Common fields

| Field | Notes |
| --- | --- |
| `provider` / `defaultProvider` | Default provider id |
| `model` / `defaultModel` | Default model id |
| `theme` | TUI theme name |
| `thinking` / `defaultThinkingLevel` | `off` … `max` |
| `thinkingBudgets` | Map of thinking level → token budget |
| `modelThinkingLevels` | Per-model default thinking level |
| `contextWindow` | Override catalog context window |
| `compactionEnabled` / `compaction.{enabled,reserveTokens,keepRecentTokens}` | [compaction.md](compaction.md) |
| `steeringMode` / `followUpMode` | Queue behaviour while streaming |
| `retry.{enabled,maxRetries,baseDelayMs}` and `retry.provider.{timeoutMs,maxRetries,maxRetryDelayMs}` | HTTP retry |
| `defaultTools` | Built-in tool allowlist (default `read,bash,edit,write`) |
| `enabledModels` | Ctrl+P cycle allowlist (globs) |
| `defaultProjectTrust` | `ask` \| `always` \| `never` (global only) |
| `sessionDir` | Session storage root |
| `enableSkillCommands` | `/skill:name` slash (default true) |
| `shellPath` / `shellCommandPrefix` | Bash wrapper |
| `externalEditor` | Ctrl+G editor |
| `doubleEscapeAction` | `tree` \| `fork` \| `none` |
| `treeFilterMode` | `default` \| `no-tools` \| `user-only` \| `labeled-only` \| `all` |
| `branchSummary.{skipPrompt,reserveTokens}` | Fork/tree summaries |
| `terminal.{showImages,imageWidthCells,hyperlinks,images,clearOnShrink,showTerminalProgress,trueColor}` | TUI terminal |
| `markdown.{mermaid,codeBlockIndent}` | Markdown rendering |
| `images.{blockImages,autoResize}` | Image attachments (autoResize default true, max edge 2000) |
| `tuiMode` / `fullscreenExitOutput` | TUI layout |
| `quietStartup` | Skip startup notes |
| `httpProxy` / `httpIdleTimeoutMs` / `websocketConnectTimeoutMs` | Networking |
| `hideThinkingBlock` / `showCacheMissNotices` | Display |
| `lastChangelogVersion` / `collapseChangelog` | `/changelog` |
| `enableInstallTelemetry` / `enableAnalytics` / `trackingId` | Telemetry |
| `packages` / `extensions` / `skills` / `prompts` / `themes` / `npmCommand` | Resource lists |

Related files in the agent dir:

- `auth.json` — credentials
- `keybindings.json` — [keybindings.md](keybindings.md)
- `trust.json` — project trust
- `models.json` — user model overlay
- `models-store.json` — catalog cache
- `extensions/sandbox.json` — [tools.md](tools.md)

Interactive editor: `pigo config` or `/settings`.
