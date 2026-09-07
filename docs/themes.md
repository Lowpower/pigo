# Themes

TUI colour sets. Built-ins: `default`, `dark`, `light` (seven ANSI colour
numbers: `user`, `assistant`, `tool`, `error`, `muted`, `accent`). Missing name
falls back to `dark`.

## Discovery

- `~/.pigo/agent/themes/*.json`
- `cwd/.pigo/themes/*.json` (trusted project)
- `--theme` files or directories
- `settings.themes[]` / packages

`--use-theme NAME` selects for this process. `--no-themes` skips discovery.
`/theme` lists and switches.

## JSON

Minimal 7-key file (name defaults to the file stem):

```json
{
  "user": "42",
  "assistant": "252",
  "tool": "178",
  "error": "196",
  "muted": "244",
  "accent": "205"
}
```

Extended form: `{ "name", "vars", "colors", "export": { "pageBg", "cardBg", "infoBg" } }`.
`vars` are expanded inside `colors`. Unknown keys are ignored.

A sample lives at [`examples/themes/high-contrast.json`](../examples/themes/high-contrast.json).
