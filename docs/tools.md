# Tools

Built-in tools stay registered: `read`, `write`, `edit`, `bash`, `grep`, `find`, `ls`, and `powershell` (Windows).
`settings.defaultTools` chooses which built-ins start enabled. Unset means `read`, `bash`, `edit`, `write`.

| Flag | Effect |
| --- | --- |
| `--no-tools` | No tools, and extensions are not loaded |
| `--tools a,b` | Replace the enabled set. `*` matches any characters (`ba*`, `mcp__radius__*`) |
| `--tools +a,-b` | Add and remove on top of the resolved default. Exact names only, and not mixed with plain names |
| `--exclude-tools a,b*` | Drop matching tools from the enabled set |
| `--no-builtin-tools` | No built-in tools (extensions still register) |

`defaultTools` uses the same entries. A list of only `+name` / `-name` adjusts the previous layer: the built-in default when the user did not set one, otherwise the user list when a trusted project sets it. Any other list, including `[]`, replaces that layer. A plain `--tools` list, `--no-tools`, or `--no-builtin-tools` replaces the enabled set and does not restore a session's tools. `--tools +name,-name` restores the session's enabled tools first, then applies those changes.

`/reload` enables tools the resolved default newly gained. Tools removed from settings stay enabled. Tools turned off in the session stay off unless the new default adds them.

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
    "denyRead": ["~/.ssh", "~/.aws", "~/.gnupg", "~/.pigo"],
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
Extra binds and an env allowlist are optional. The container is started with
`--network=none` unless `container.network` is `true`. API keys and OAuth
tokens are never passed into the container, even if listed in `container.env`.

```json
{
  "container": {
    "image": "debian:bookworm-slim",
    "network": false,
    "mounts": [{ "host": "/opt/cache", "container": "/cache" }],
    "env": ["GOPATH"]
  }
}
```

If `image` is set but Docker is missing or the daemon fails, the tool/`!`
call errors instead of falling back to the host. `--no-sandbox` turns this
off. Extension tools and Windows `powershell` stay on the host.
