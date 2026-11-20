# Themes

TUI colour sets. Built-ins: `system` (default), `default`, `dark`, `light`.
`default` is seven ANSI colour numbers (`user`, `assistant`, `tool`, `error`,
`muted`, `accent`) and fills those fields when a shorthand file omits them.
`dark` and `light` are the pi v1.3.0 OKHSL palettes. An unknown name falls
back to `default`.

`system` is reserved: a theme file of that name is ignored. It is built from
the terminal when the TUI starts (mode 2031, OSC 10, OSC 11, and ANSI colors
0–15, at most 100ms). Light or dark follows the background color when the
terminal reports one. Otherwise it follows the terminal's light/dark report,
then `COLORFGBG`, then dark. With a background and a full palette, hues come
from the palette. With only a background, hues come from built-in families.
With neither, tokens use ANSI indexes and the terminal's own default colors.
An explicit `dark`, `light`, `default`, or custom theme does not query the
terminal.

## Discovery

- `~/.pigo/agent/themes/*.json`
- `cwd/.pigo/themes/*.json` (trusted project)
- `--theme` files or directories
- `settings.themes[]` / packages

`--use-theme NAME` selects for this process. `--no-themes` skips discovery.
`/theme` lists and switches.

## JSON

Set `$schema` to
`https://raw.githubusercontent.com/Lowpower/pigo/main/docs/schemas/theme.schema.json`
for editor completion. The key is ignored at runtime. Unknown keys are errors
when the file is loaded and again on `/reload`. The process still starts, and
that file is left out of the theme list.

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

Extended form: `{ "name", "appearance", "vars", "colors", "export": { "pageBg", "cardBg", "infoBg" } }`.
`vars` are expanded inside `colors`. A shorthand file may also include `colors`.
`colors` only accepts known tokens. `export` only accepts `pageBg`, `cardBg`,
and `infoBg`.

`appearance` is `"dark"` or `"light"`: the background the theme is designed for.
When it is omitted, pigo detects it from the lightness of the theme's own
foreground and background colours. Palette indexes 0–15 are ignored for that
detection. Any other value is an error. A setting such as `light/dark` still
selects the name after the slash.

A colour is one of:

| Form | Example |
| --- | --- |
| RGB hexadecimal | `"#0af"` or `"#00aaff"` |
| OKLCH | `"oklch(62% 0.1 200)"` |
| OKHSL | `"okhsl(250 60% 55%)"` |
| 256-colour index | `39` or `"39"` |
| Variable reference | `"primary"` |
| Terminal default | `""` |

`#rgb` is stored as `#rrggbb`. `oklch()` and `okhsl()` are converted to
`#rrggbb` (out-of-gamut OKLCH keeps its hue and loses chroma until it fits).
An index stays an index, so the terminal's own palette draws it. An empty
string stays empty and the terminal default colour is used. A missing
variable, a circular variable, or any other syntax is an error at load and
on `/reload`. HTML export uses these same resolved values. OKHSL in an export
colour is already hexadecimal because CSS does not support it.

A sample lives at [`examples/themes/high-contrast.json`](../examples/themes/high-contrast.json).
