# Extension examples

Minimal custom-tool extension. Build the binary, then load it with `-e`:

```bash
go build -o /tmp/hello-ext ./examples/extensions/hello
go run ./cmd/pigo -e /tmp/hello-ext -p "say hello to pigo"
```

Or copy the binary into `~/.pigo/agent/extensions/` for auto-discovery.
