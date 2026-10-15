# Compaction

pigo has two summarizers:

| Mechanism | Trigger | Purpose |
| --- | --- | --- |
| Compaction | Estimated context exceeds the threshold, or `/compact` | Replace older messages with a checkpoint |
| Branch summary | `/tree` navigation when a summary is requested | Keep context from the branch being left |

Token estimates are `chars/4`. Compaction runs when
`contextTokens > contextWindow - reserveTokens`.

## Settings (defaults)

| Key | Default |
| --- | --- |
| `compactionEnabled` / `compaction.enabled` | `true` |
| `compactionReserveTokens` / `compaction.reserveTokens` | `16384` |
| `compactionKeepRecentTokens` / `compaction.keepRecentTokens` | `20000` |
| `compaction.modelOverrides["provider/modelId"]` | Per-model `reserveTokens` / `keepRecentTokens` |
| `branchSummary.skipPrompt` / `branchSummary.reserveTokens` | branch summaries on fork/tree |

## Cut

Compaction walks the current context from the end, adding each message's
token estimate, until the tail reaches `keepRecentTokens`. The message that
crosses that budget is not always the cut. A cut is a user or assistant
message. Bash commands, custom messages, branch summaries, and earlier
compaction summaries are already user messages by the time the cut is chosen,
so they are legal cuts and they start a turn. Tool results are not cuts.

The cut is the nearest legal message at or after the one that crossed the
budget. When the crossing messages are trailing tool results and nothing
legal follows them, the cut moves back to the newest legal message before
them, which keeps those results with the assistant that called the tool.
A span that contains only tool results is not compacted. The failure is
reported on `compaction_end.errorMessage` and `session_compact_failed`, and
no compaction entry is written. A span that still fits, or whose only legal
cut leaves nothing to summarize, is left unchanged.

That tail stays verbatim. Older messages in the span are summarized.

A turn starts at a user message and runs until the next user message. When
the cut lands on an assistant message inside that turn, the turn is split.
Messages before the turn are one summary. Messages from the turn start up to
the cut are a second summary, from the turn-prefix prompt, without the
previous summary or `/compact` instructions. The stored summary is the history
text, then a `**Turn Context (split turn):**` section, then the prefix. If
there is no earlier history, the history text is the previous compaction
summary, or `No prior history.` when this is the first compaction. File lists
include tool calls from both halves.

On a later compaction the span starts at the previous compaction's
`firstKeptEntryId`, so messages that survived the last cut are included in
the next summary. The previous summary itself is not one of those messages.
If that id is missing, or it is the compaction entry's own id, the span starts
at the entry after the compaction.

The new compaction records the id of the first entry that remains verbatim.

## Summary request

The transcript is serialized into one user message, with a system prompt that
asks only for the structured summary. Tool calls stay as text
(`[Assistant tool calls]: read(path="foo.ts")`). Each tool result is cut at
2000 characters and the marker records how many characters were removed.
`maxTokens` is `floor(0.8 * reserveTokens)`, and no larger than the model's
output cap when the catalog lists one. A `reserveTokens` of 0 uses 16384.
A length stop is a failure and is not stored. The request sets
`cacheRetention` to `none`.

## Iterative summary

The first compaction uses the initial checkpoint prompt (Goal, Constraints,
Progress, Key Decisions, Next Steps, Critical Context).

When a previous compaction summary exists, the summarizer receives it in a
`<previous-summary>` block and an update prompt. The update prompt keeps
existing goals and constraints and merges progress from the new messages.
An extension summary (`fromHook: true`) is still passed as the previous
summary.

`/compact` instructions are appended as `Additional focus:`.

## File tracking

Generated compaction and branch summaries record

```json
{ "readFiles": ["a.go"], "modifiedFiles": ["b.go"] }
```

Paths come from assistant tool calls whose `arguments.path` is a string:

| Tool | List |
| --- | --- |
| `read` | `readFiles`, unless the same path was also modified |
| `write`, `edit` | `modifiedFiles` |

`grep`, `find`, `ls`, and `bash` are not tracked. Both lists are sorted.
A path that was read and later written or edited appears only in
`modifiedFiles`.

The lists are appended to the summary after the model replies, as
`<read-files>` and `<modified-files>`. An empty list adds no tag. The same
lists are stored on the entry's `details`, including when both are empty.

Compaction carries lists forward from the previous compaction when that
entry is not `fromHook`. Extension compactions are skipped even if they
have `details`. A `session_before_compact` replacement is stored with
`fromHook: true` and without file `details`.

Branch summaries carry lists forward from earlier non-hook branch summaries
on the abandoned branch, including ones that did not fit in the summary
token budget. Tool calls are taken only from messages that were actually
summarized. An extension-supplied branch summary does not get file `details`.

## Manual

- TUI: `/compact`
- RPC: `compact` (optional `customInstructions`), `set_auto_compaction`

## Extension hooks

Subscribe to `session_before_compact`, `session_compact`, and
`session_compact_failed`. `session_before_compact` includes `firstKeptEntryId`,
the same id stored on the compaction entry. A handler may cancel compaction or
supply `compaction` text. That text is stored as the summary with
`fromHook: true`.

Branch navigation uses `session_before_tree`. A handler may supply `summary`,
which is stored as a `fromHook` branch summary.
