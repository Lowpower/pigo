# Changelog

## 0.0.1 - 2026-08-27

### Added

- Themed HTML session export (`--export`, `/export`, RPC `export_html`) with CSS variables, embedded session data, and client-side rendering.
- HTML export hides custom messages with `display: false` by default. `H` or the header button shows them, and opening one from the sidebar reveals the set.
- `/share` uploads the current session through Radius when authenticated, otherwise a private GitHub gist, and prints a viewer URL (`PIGO_SHARE_VIEWER_URL`).
- `/changelog` and new-session startup notes driven by `lastChangelogVersion` in settings.json.
