# Codemode

`codemode` runs one JavaScript script that can call the session's other tools.
It is built in and off by default. Add it with `defaultTools` or `--tools +codemode`.
A plain `--tools` list replaces the enabled set, so name the other tools too.

The tool argument is `{ "code": "<javascript>" }`. The script is the body of an
async function: top-level `await` and `return` work. A single expression is the
return value. Details below stay in this file; the tool description itself is
short.

## Globals

| Global | Behavior |
| --- | --- |
| `tools.<name>(args)` | Calls one tool. The name is the tool name with non-identifier characters replaced by `_`, so `mcp__dev-radius__search` is `tools.mcp__dev_radius__search`. |
| `text(value)` | Adds text to the tool result. Strings are kept as written; other values are JSON. |
| `image(value)` | Records an image value on the tool result. |
| `console.log/info/warn/error/debug` | Adds one line per call. |
| `store(key, value)` / `load(key)` | Keeps one JSON value for later `codemode` calls in the same session. |
| `searchTools(query, { limit?, namespace? })` | Async. Resolves to `{ name, description }[]`. Default limit is 8. |
| `describeTool(name)` | Async. Resolves to `{ description, declaration }`, or `null`. |
| `describeNamespace(name)` | Async. Resolves to `{ name, description?, instructions?, tools }`, or `null`. |
| `models.getAvailableOfType(type, provider?)` | Async. Lists `chat`, `classifier`, or `image` models whose provider has credentials. |
| `models.classify(model, context)` | Async. Resolves to `{ stopReason: "error", errorMessage: "not implemented" }`. |
| `models.generateImages(model, context)` | Async. Same not-implemented result as `classify`. |

`searchTools`, `describeTool`, and `describeNamespace` return promises. Await
them.

`tools` includes `direct` tools from the current session, plus tools whose
exposure is `codemode` or `deferred`. `hidden`, `model-only`, and `codemode`
itself are not included. A missing name rejects. `tools.Bash` mentions
`tools.bash` when that tool exists.

`describeNamespace` accepts `mcp__server`, the same name with `-` and `_`
treated alike, and a namespace alias.

## Calls and declarations

`describeTool` prints one line for how to call the tool and what it resolves
to, then the input properties. Integer, minimum, maximum, pattern, and default
constraints are `@` tags on the property comment.

A tool with no output schema resolves to its text and rejects on failure. A
tool that declares an output schema resolves to its value on failure too.
MCP-shaped tools (a namespace, or a name containing `mcp__`) resolve to
`{ content, structuredContent, isError }`. Without an output schema,
`structuredContent` is `unknown`.

Several `tools` and `models` calls may run at the same time via `Promise.all`.

## Output

The tool result is the script's return value. Collected `text`, `console`, and
`image` values are appended after it. Output caps, separators, and image files
are not applied here.
