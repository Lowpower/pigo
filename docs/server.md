# Unix session server

`pigo server` listens on a Unix socket and speaks the same JSONL RPC as
`--mode rpc`. `pigo client` is a thin stdin/stdout bridge to that socket.

```bash
pigo server --listen "$XDG_RUNTIME_DIR/pigo.sock"
pigo client --connect "$XDG_RUNTIME_DIR/pigo.sock"
```

Defaults: `PIGO_SERVER_LISTEN` / `PIGO_SERVER_CONNECT`.

The server process starts with extensions disabled (`NoExtensions: true`) so
the socket is a headless control plane, not a second TUI. Commands match
[rpc.md](rpc.md).
