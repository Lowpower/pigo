# Extension examples

Build a binary, then load it with `-e`. Copy into `~/.pigo/agent/extensions/`
for auto-discovery.

## hello

Registers one tool, `hello`.

```bash
go build -o /tmp/hello-ext ./examples/extensions/hello
go run ./cmd/pigo -e /tmp/hello-ext -p "say hello to pigo"
```

## capdemo

Registers a slash command, blocks `tool_call`, and serves a scripted provider
stream (no network):

```bash
go build -o /tmp/capdemo ./examples/extensions/capdemo
go run ./cmd/pigo -e /tmp/capdemo --provider capdemo --model demo -p "hi"
```

## guard

Claims `--plan`, registers `ctrl+shift+g`, and prefixes user input when the
flag is set:

```bash
go build -o /tmp/guard-ext ./examples/extensions/guard
go run ./cmd/pigo -e /tmp/guard-ext --plan -p "summarize this"
```

## searchtools

Registers `tool_search` plus a deferred `lookup` tool. `session_start` keeps
only the loader active; searching then calls `ext.SetActiveTools`.

```bash
go build -o /tmp/searchtools-ext ./examples/extensions/searchtools
go run ./cmd/pigo -e /tmp/searchtools-ext -p "look up alpha"
```

## telemetry

Subscribes to `telemetry_span` and POSTs OTLP/HTTP JSON to
`OTEL_EXPORTER_OTLP_ENDPOINT` (stderr JSONL if unset). Built-in export already
uses the same env without a plugin; this binary is a copyable sink. If both
are on, spans are sent twice.

```bash
go build -o /tmp/pigo-telemetry ./examples/extensions/telemetry
pigo -e /tmp/pigo-telemetry -p "hi"
```

See [docs/extensions.md](../../docs/extensions.md).
