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
| `image(value)` | Records an image on the tool result. The value is a base64 data URL, an `{ image_url }` object, or `{ type: "image", data, mimeType }`. PNG, JPEG, GIF, and WebP are accepted. Invalid base64 or mime is rejected and is not written into the result. A valid image is saved to a user-only temporary file, and the result names that path. |
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

The tool result lists each `text()` value and then a non-empty return value.
When that is more than one text item, every item starts with `==> text N/M <==`.
`console` calls follow in one `<console_output>` block, one call per line.
Each accepted image is a content block after the text, and the text names the
temporary file.

The script fails, and keeps the output collected so far, when `text()`,
`image()`, and `console` together pass 100000 calls, or when the text plus
base64 image data passes 16777216 characters. Catching the error does not
resume output.

While the tool is running the transcript shows `Elapsed`. The result ends with
`Took` and the same duration text as bash.

`settings.codemode.mode` is `on` or `only` (`on` when unset). `on` leaves the
other tools declared and notes that a script can call them. The codemode
description then lists tools that are not declared. `only`, when codemode
itself is enabled, declares only codemode to the model. `settings.codemode.inlineBudget`
is how many estimated tokens (characters / 4, default 3000) that description
may spend on declarations. Declarations that do not fit are omitted. `0` lists
namespace names only. Scripts find the rest with `searchTools()`.
