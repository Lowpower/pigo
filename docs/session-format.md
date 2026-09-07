# Session JSONL format

First line is a header. Every later line is an entry. `CurrentVersion` is `3`.

## Header

```json
{
  "type": "session",
  "version": 3,
  "id": "…",
  "timestamp": "2026-09-07T12:00:00Z",
  "cwd": "/path/to/project",
  "parentSession": "optional-parent-id",
  "name": "optional display name"
}
```

## Entries

Common fields: `type`, `id`, `parentId` (null on the first entry), `timestamp`.

| `type` | Extra fields |
| --- | --- |
| `message` | `message`, `usage?`, `provider?`, `modelId?`, `thinkingLevel?` |
| `compaction` | `summary`, `firstKeptEntryId`, `tokensBefore?`, `fromHook?`, `details?`, `usage?` |
| `branch_summary` | `fromId`, `summary`, `fromHook?` |
| `label` | `targetId`, `label` |
| `session_info` | `name` |
| `custom` | `customType`, `data` (e.g. `pigo.share`) |
| `custom_message` | `customType`, `content`, `display` (included in the LLM context) |

Entries are buffered until the first assistant message, then flushed and
appended. Do not rewrite historical lines in place; fork instead.
