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

See [docs/extensions.md](../../docs/extensions.md).
