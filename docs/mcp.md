# MCP

pigo loads MCP servers from `~/.pigo/agent/mcp.json` and, once the project is
trusted, from `.pigo/mcp.json`. An untrusted project does not read
`.pigo/mcp.json` and does not start commands from it.

`auth` and `oauth` stay in the file. The loader does not interpret them.
`pigo mcp login` reads `oauth` when it signs in.

## Commands

```text
pigo mcp add <name> -- <command> [args...]
pigo mcp add <name> --url <url>
pigo mcp remove <name>
pigo mcp list [--json]
pigo mcp login <name> [--timeout 300s]
pigo mcp logout <name>
```

`--local` on add and remove writes `.pigo/mcp.json`. An untrusted project is
refused. `add` does not connect. `remove` leaves stored OAuth credentials.
`list --json` reports `resources: true` when a server has resources, not a
count. `login --timeout` limits the whole sign-in.

`--no-mcp` skips MCP for that run. A plain `--tools` list keeps MCP tools.
If any entry starts with `mcp__`, only the matching MCP tools stay.

`/mcp` lists servers without waiting for every connection. Enable, disable,
and reconnect update that server in place.

## File

```json
{
  "mcpServers": {
    "demo": {
      "command": "demo-server",
      "args": ["--stdio"],
      "exposure": "codemode"
    }
  }
}
```

`command` starts a stdio server. `url` is a streamable HTTP server. Both can
set `timeout` (seconds, default 60), `enabled`, `exposure`, `toolExposure`,
and `description`. Stdio also has `args`, `env`, and `cwd`. HTTP also has
`headers` and `bearerTokenEnvVar`.

Omit `exposure` and the server defaults to `codemode`.

A project entry with no `command` and no `url` may only set `enabled`,
`exposure`, and `toolExposure`. It overrides the user-level server of the
same name and keeps the rest, including `env` and `headers`. An entry that
has `command` or `url` replaces that server. Any other shape, or an override
for a name the user file does not define, is a config error and names the
file and the server.

Extensions can register a server for the current session with
`register_mcp_server`. The fields match one `mcpServers` entry. A file entry
with the same name wins. See [Extensions](extensions.md).

## Tools and startup

Each tool is registered as `mcp__<server>__<tool>`. `-` in the server name and
the tool name becomes `_`. Two tools on one server that collide after that
gain a stable hash of the original name. Two server names that collide are
not loaded, and the error names both.

`toolExposure` overrides the server exposure for one tool. An exact tool name
wins over a pattern. `*` matches any characters. Among patterns, the first
match wins.

A server blocks the first prompt only when its exposure is `direct` or a
`toolExposure` rule is `direct`. Other servers connect in the background.
A later call, `tool_search`, or a tool whose `reachesTools` names one of them
waits for that server. Connecting does not list resources. pigo only records
whether the server says it has resources.

## System prompt

Enabled servers are listed in the `mcp_servers` section, one per line: the
server name, how its tools are reached, and the description or the first line
of the server instructions. When the section changes, pigo appends a system
message. It does not rewrite earlier session lines. Extensions subscribed to
`mcp_servers_change` are told when the set changes.
