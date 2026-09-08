# Keybindings

User file: `~/.pigo/agent/keybindings.json`. Map action id → string or
string array. `/hotkeys` lists the bindings the editor currently honours.
`/reload` re-reads the file.

Extensions may `register_shortcut` with a key id; reserved host actions cannot
be stolen.

Windows / WSL use a slightly different default table (follow-up is `ctrl+q`,
paste-image is `alt+v`, …).

## Main editor (non-Windows defaults)

| Action | Keys |
| --- | --- |
| `tui.input.submit` | enter |
| `tui.input.newLine` | shift+enter, ctrl+j |
| `app.message.followUp` | alt+enter |
| `app.interrupt` | esc |
| `app.clear` | ctrl+c (twice to quit) |
| `app.exit` | ctrl+d (empty editor) |
| `app.model.cycleForward` / `cycleBackward` | ctrl+p / shift+ctrl+p |
| `app.thinking.cycle` | shift+tab |
| `app.thinking.toggle` | ctrl+t |
| `app.thinking.save` | ctrl+s |
| `app.tools.expand` | ctrl+o |
| `app.model.select` | ctrl+l |
| `app.editor.external` | ctrl+g |
| `app.message.copy` | ctrl+x |
| `app.message.dequeue` | alt+up |
| `app.clipboard.pasteImage` | ctrl+v |
| `app.suspend` | ctrl+z |
| `app.session.toggleNamedFilter` | ctrl+n |

Editor motions follow emacs-style ctrl/alt chords (`ctrl+a`/`e`, `ctrl+w`,
`ctrl+k`, `ctrl+y`, …). Tree, session picker, model editor, and fullscreen
search have their own action ids (`app.tree.*`, `app.session.*`,
`app.models.*`, `tui.altScreen.*`). Empty `Keys` in the default table means
unbound until set in `keybindings.json`. In fullscreen, `app.message.copy`
copies the active mouse selection when one exists, otherwise the last
assistant message.
