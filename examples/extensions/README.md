# Extension examples

Minimal custom-tool extension. Build the binary, then load it with `-e`:

```bash
go build -o /tmp/hello-ext ./examples/extensions/hello
go run ./cmd/pigo -e /tmp/hello-ext -p "say hello to pigo"
```

`capdemo` registers a slash command, blocks tool calls, and serves a scripted
provider stream (no network):

```bash
go build -o /tmp/capdemo ./examples/extensions/capdemo
go run ./cmd/pigo -e /tmp/capdemo --provider capdemo --model demo -p "hi"
```

Or copy the binary into `~/.pigo/agent/extensions/` for auto-discovery.
