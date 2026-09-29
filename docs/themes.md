# Themes

TUI colour sets. Built-ins: `system` (default), `default`, `dark`, `light`.
`default`, `dark`, and `light` are seven ANSI colour numbers (`user`,
`assistant`, `tool`, `error`, `muted`, `accent`). An unknown name falls back
to `default`.

`system` is reserved: a theme file of that name is ignored. It is built from
the terminal when the TUI starts (OSC 10, OSC 11, and ANSI colors 0–15, at
most 100ms). Light or dark follows the background color, not the terminal's
light/dark report alone. With a background and a full palette, hues come from
the palette. With only a background, hues come from built-in families. With
neither, tokens use ANSI indexes and the terminal's own default colors. An
explicit `dark`, `light`, `default`, or custom theme does not query the terminal.

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
