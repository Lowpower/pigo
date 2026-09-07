# Compaction

When the estimated context (`chars/4`) exceeds
`contextWindow - reserveTokens`, pigo summarizes older messages and keeps a
recent tail.

## Settings (defaults)

| Key | Default |
| --- | --- |
| `compactionEnabled` / `compaction.enabled` | `true` |
| `compactionReserveTokens` / `compaction.reserveTokens` | `16384` |
| `compactionKeepRecentTokens` / `compaction.keepRecentTokens` | `20000` |
| `branchSummary.skipPrompt` / `branchSummary.reserveTokens` | branch summaries on fork/tree |

## Manual

- TUI: `/compact`
- RPC: `compact` (optional `customInstructions`), `set_auto_compaction`

## Extension hooks

Subscribe to `session_before_compact`, `session_compact`, and
`session_compact_failed`. A `session_before_compact` handler may supply a
custom summary (`fromHook`).
