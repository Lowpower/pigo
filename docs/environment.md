# Environment variables

Most names use the `PIGO_` prefix. Span export also reads standard
`OTEL_EXPORTER_OTLP_*` variables.

| Variable | Purpose |
| --- | --- |
| `PIGO_CODING_AGENT_DIR` | Config root (default `~/.pigo/agent`) |
| `PIGO_CODING_AGENT_SESSION_DIR` | Session directory override |
| `PIGO_PROVIDER` / `PIGO_MODEL` / `PIGO_REASONING_LEVEL` | Settings overlays; also injected into bash |
| `PIGO_SESSION_ID` / `PIGO_SESSION_FILE` | Injected into bash for the active session |
| `PIGO_OFFLINE` | Skip network (also `--offline`) |
| `PIGO_TELEMETRY` | Override install ping **and** span export (`1`/`true`/`yes` or `0`/`false`/`no`) |
| `PIGO_TELEMETRY_JSONL` | When truthy, write finished spans as JSONL to stderr |
| `PIGO_INSTALL_TELEMETRY_URL` | Install-ping endpoint |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Built-in OTLP/HTTP JSON traces; pigo appends `/v1/traces` unless already present |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Full traces URL (wins over `ENDPOINT`) |
| `OTEL_EXPORTER_OTLP_HEADERS` / `OTEL_EXPORTER_OTLP_TRACES_HEADERS` | Comma-separated `k=v` request headers |
| `PIGO_SHARE_VIEWER_URL` | Base URL printed by `/share` (otherwise the gist URL) |
| `PIGO_OAUTH_CALLBACK_HOST` | OAuth bind host; non-loopback values are ignored (default `127.0.0.1`) |
| `PIGO_SERVER_LISTEN` / `PIGO_SERVER_CONNECT` | Default Unix socket for `server` / `client` |
| `PIGO_CATALOG_BASE_URL` | Remote model catalog |
| `PIGO_RADIUS_GATEWAY` / `PIGO_RADIUS_CLIENT_ID` | Radius (`RADIUS_GATEWAY` / `RADIUS_CLIENT_ID` also accepted) |
| `PIGO_CHANGELOG_PATH` | Changelog file override |
| `PIGO_HYPERLINKS` / `PIGO_TRUE_COLOR` / `PIGO_IMAGE_PROTOCOL` / `PIGO_CACHE_RETENTION` | Terminal / cache (`on`/`off`/`auto`; cache is `none\|short\|long`) |

Span export is off unless `PIGO_TELEMETRY_JSONL` or `OTEL_EXPORTER_OTLP_ENDPOINT` is set (or an extension subscribes to `telemetry_span`). `PIGO_TELEMETRY=0` / `enableInstallTelemetry: false` disables install ping **and** spans. `--no-extensions` does not disable built-in JSONL/OTLP.

If the built-in OTLP exporter and a `telemetry_span` plugin both post to the same collector, spans are sent twice. When debugging a plugin, leave `OTEL_EXPORTER_OTLP_ENDPOINT` unset on the pigo process, or point the plugin at a different URL.

Settings keys can also be overridden by matching `PIGO_` env vars via Viper
(`PIGO_THEME`, `PIGO_THINKING`, …).

Provider API keys use the provider's own env names (`ANTHROPIC_API_KEY`,
`OPENAI_API_KEY`, `OPENROUTER_API_KEY`, …). See [auth.md](auth.md).
