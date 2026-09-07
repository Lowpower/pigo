# Sessions

Each run that persists writes a JSONL file:

```
~/.pigo/agent/sessions/--<cwd-safe>--/{timestamp}_{id}.jsonl
```

Override the root with `--session-dir`, `settings.sessionDir`, or
`PIGO_CODING_AGENT_SESSION_DIR`. `--no-session` skips the file.

Schema version is **3**. See [session-format.md](session-format.md) for the
on-disk shape. Keep that format stable.

## Resume, continue, fork

| Action | How |
| --- | --- |
| Continue latest for this cwd | `--continue` / `-c` |
| Pick or load a session | `--resume` / `-r`, `--session <path\|id>`, `/resume`, `/import` |
| Fork into a new file | `--fork`, `/fork`, `/clone` (header `parentSession`) |
| Name | `--name` / `-n`, `/name` |
| Tree navigation | `/tree`, double-Escape (see `doubleEscapeAction`) |

The log is a tree: each entry has `parentId`. `/tree` walks leaves. Forking
copies history up to a chosen entry.

## Export and share

- `--export <session.jsonl> [out.html]`, `/export`, RPC `export_html` — themed HTML
- `/share` — Radius when authenticated, otherwise a private `gh` gist

HTML themes live next to the exporter (`light` / `dark`).
