# Environment variables

All names use the `PIGO_` prefix.

| Variable | Purpose |
| --- | --- |
| `PIGO_CODING_AGENT_DIR` | Config root (default `~/.pigo/agent`) |
| `PIGO_CODING_AGENT_SESSION_DIR` | Session directory override |
| `PIGO_PROVIDER` / `PIGO_MODEL` / `PIGO_REASONING_LEVEL` | Settings overlays; also injected into bash |
| `PIGO_SESSION_ID` / `PIGO_SESSION_FILE` | Injected into bash for the active session |
| `PIGO_OFFLINE` | Skip network (also `--offline`) |
| `PIGO_TELEMETRY` | Override install telemetry |
| `PIGO_INSTALL_TELEMETRY_URL` | Telemetry endpoint |
| `PIGO_SHARE_VIEWER_URL` | Base URL printed by `/share` (otherwise the gist URL) |
| `PIGO_OAUTH_CALLBACK_HOST` | OAuth bind host (default `127.0.0.1`) |
| `PIGO_SERVER_LISTEN` / `PIGO_SERVER_CONNECT` | Default Unix socket for `server` / `client` |
| `PIGO_CATALOG_BASE_URL` | Remote model catalog |
| `PIGO_RADIUS_GATEWAY` / `PIGO_RADIUS_CLIENT_ID` | Radius (`RADIUS_GATEWAY` / `RADIUS_CLIENT_ID` also accepted) |
| `PIGO_CHANGELOG_PATH` | Changelog file override |
| `PIGO_HYPERLINKS` / `PIGO_TRUE_COLOR` / `PIGO_IMAGE_PROTOCOL` / `PIGO_CACHE_RETENTION` | Terminal / cache (`on`/`off`/`auto`; cache is `none\|short\|long`) |

Settings keys can also be overridden by matching `PIGO_` env vars via Viper
(`PIGO_THEME`, `PIGO_THINKING`, …).

Provider API keys use the provider's own env names (`ANTHROPIC_API_KEY`,
`OPENAI_API_KEY`, `OPENROUTER_API_KEY`, …). See [auth.md](auth.md).
