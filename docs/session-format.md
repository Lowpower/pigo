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
| `compaction` | `summary`, `firstKeptEntryId`, `tokensBefore?`, `fromHook?`, `details?`, `usage?`, `systemMessage?` |
| `context_edit` | `targetId`, `replacement` (`null` omits the target from later model context) |
| `branch_summary` | `fromId`, `summary`, `fromHook?` |
| `label` | `targetId`, `label` |
| `session_info` | `name` |
| `usage` | `kind` (e.g. `cache_warm`), `provider`, `model`, `usage`, `note?`. Counted in session totals. Not sent to the model |
| `custom` | `customType`, `data` (e.g. `pigo.share`, `pigo.bug-report`) |
| `custom_message` | `customType`, `content`, `display` (included in the LLM context) |

Entries are buffered until the first user message, then flushed and
appended. Do not rewrite historical lines in place; fork instead.

`customType: pigo.bug-report` records a local `/bug` zip. `data` has `id`,
`createdAt`, `hint`, `sessionIncluded`, `summaryIncluded`, `delivery` (`zip`),
and `path`. Crash records live in `agentDir/crashes.json` (`timestamp`,
`version`, `kind`, `message`, `stack`, `sessionFile`, `cwd`, `notified`), not
in the session file.

## System messages

A `message` entry may carry `message.role` `"system"`. The first request of a
session writes one with every prompt section and `toolsAdded`. Later changes
are patches: `sections` replaces a section by name (`null` removes it) and
`toolsRemoved` drops tools by `{ "name": "..." }`. Replaying them in order
yields the current prompt and tools. `replace: true` discards earlier state.

```json
{"type":"message","message":{"role":"system","content":"","sections":{"preamble":"You are an expert coding assistant in pigo.","cwd":"Current working directory: /project"},"toolsAdded":[{"name":"read","description":"read a file","parameters":{}}],"timestamp":1733234400000}}
{"type":"message","message":{"role":"system","content":"","sections":{"skills":null},"toolsRemoved":[{"name":"write"}],"timestamp":1733234640000}}
```

Section names pigo writes: `preamble`, `append`, `tools`, `project_context`,
`skills`, `cwd`, `date`. Unknown names are kept in first-seen order.

A compaction entry may include `systemMessage`, a complete checkpoint (every
current section and the full `toolsAdded` list). Replay starts from that
checkpoint and ignores system messages that fall before it, including ones in
the kept tail. Older compaction entries omit the field and keep the previous
behavior. Sessions without a leading system message declare it on the next
request. The session version stays `3`.

`firstKeptEntryId` is the first entry kept from before the compaction. A
retain-none compaction stores its own id there, so nothing before it stays in
model context. The field is always a string; JSON `null` is not written.

## Context edits

A `context_edit` changes only later model context. The target entry stays in
the file, the tree, exports, and session usage. `replacement: null` omits the
target. A string replacement of an assistant or tool-result message becomes one
text block; role and tool metadata stay. When several edits name the same
target, the latest one on the current branch wins. Navigating to a point before
the edit shows the original text again. `context_edit` itself is not a model
message.
