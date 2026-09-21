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
`/trust`). User `~/.agents/skills` always loads. Credentials, extension
loading, and isolation: [security.md](security.md).

## Sandbox

Off by default. Linux/darwin only. `--no-sandbox` disables wrapping
and Docker tool isolation.

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

This OS wrapper applies to **bash** on the host. It is skipped when Docker
isolation is on.

## Docker (tools in container)

Opt-in via `settings.container.image`. pigo and `auth.json` stay on the host.
`read`, `write`, `edit`, `bash`, `grep`, `find`, `ls`, and `!` / `!!` run
against a session-long container: cwd is mounted at `/workspace`. Extra binds
and an env allowlist are optional. API keys and OAuth tokens are never passed
into the container, even if listed in `container.env`.

```json
{
  "container": {
    "image": "debian:bookworm-slim",
    "mounts": [{ "host": "/opt/cache", "container": "/cache" }],
    "env": ["GOPATH"]
  }
}
```

If `image` is set but Docker is missing or the daemon fails, the tool/`!`
call errors instead of falling back to the host. `--no-sandbox` turns this
off. Extension tools and Windows `powershell` stay on the host.
