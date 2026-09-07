# Tools

Default allowlist (`settings.defaultTools`): `read`, `bash`, `edit`, `write`.
Also built in: `grep`, `find`, `ls`, and `powershell` (Windows).

| Flag | Effect |
| --- | --- |
| `--no-tools` | No tools |
| `--tools a,b` | Allowlist |
| `--exclude-tools a,b` | Denylist |
| `--no-builtin-tools` | Only extension tools |

`/tools` lists what the current session actually has.

## Built-ins

| Name | Arguments |
| --- | --- |
| `read` | `path`, `offset?`, `limit?` (images auto-resize unless `images.autoResize` is false) |
| `write` | `path`, `content` |
| `edit` | `path`, `edits[{oldText,newText}]` |
| `bash` | `command`, `timeout?` |
| `grep` | `pattern`, `path?`, `glob?`, `ignoreCase?`, `literal?`, `context?`, `limit?` — gitignore only inside a git repo |
| `find` | `pattern`, `path?`, `limit?` — gitignore even outside git |
| `ls` | `path?`, `limit?` |

`images.blockImages` rejects image reads. `terminal.showImages` controls TUI
display.

## Project trust

Local `.pigo/` resources, ancestor `.agents/skills`, and project
`sandbox.json` require trust (`ask` / `always` / `never`, `--approve`,
`/trust`). User `~/.agents/skills` always loads.

## Sandbox

Off by default. Linux/darwin only. `--no-sandbox` disables wrapping.

Files (project overrides global):

- `~/.pigo/agent/extensions/sandbox.json`
- `cwd/.pigo/sandbox.json` (trusted projects)

```json
{
  "enabled": true,
  "network": {
    "allowedDomains": ["github.com", "*.npmjs.org"],
    "deniedDomains": []
  },
  "filesystem": {
    "denyRead": ["~/.ssh"],
    "allowWrite": [".", "/tmp"],
    "denyWrite": [".env", "*.pem"]
  }
}
```
