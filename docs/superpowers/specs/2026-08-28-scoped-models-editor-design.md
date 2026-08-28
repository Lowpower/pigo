# /scoped-models editor

Issue: [#13](https://github.com/Lowpower/pigo/issues/13)
Date: 2026-08-28

Interactive editor for the current session's Ctrl+P model cycle set. Enter
toggles session state only. Ctrl+S writes `enabledModels` in
`~/.pigo/agent/settings.json`. Escape does not write the file.

Behaviour matches issue 13's acceptance list. Settings stay a string-pattern
array so the same `enabledModels` field remains readable as patterns.

## Non-goals

- Do not change `/model` picker UI. It already has a scoped tab that reads
  `Engine.Scoped`; this work only updates that slice at runtime.
- Do not change session JSONL or `~/.pigo/agent` layout.
- Do not rewrite `ResolvePatterns` matching (glob + substring + `:thinking`).
- Do not hit the network in tests; do not change startup `PrepareCatalog`
  (`force=false`, 4s).
- Do not preserve original glob text on Ctrl+S. Saving expands to
  `provider/id` strings, including still-checked unavailable ids.
- Do not keep `:thinking` suffixes after the first selector `onChange`.
  `--models sonnet:high` is session-only until then; re-resolve uses bare
  `provider/id`.

## Architecture

Four pieces, one-way dependencies. Session JSONL is untouched.

```
config.EnabledModels  ──startup──►  runtime.Engine.Scoped
       ▲                              ▲
       │ Ctrl+S                       │ every toggle / post-refresh
       │                              │
       └────────  scopedModelsPicker (embeds listPicker)
                       │
                       └── tea.Cmd: RefreshAll(force, 15s)
```

| Piece | Change |
| --- | --- |
| `internal/config` | `EnabledModels []string`. `Save` deletes the key when `nil`, writes the array otherwise. |
| `internal/runtime` | Startup `Scoped = ResolvePatterns(--models ?? enabledModels, available)`. `SetScopedModels([]Spec)`; empty slice means implicit-all. |
| `internal/tui` | New `scopedModelsPicker` embedding `listPicker`. `/scoped-models` opens it. Enter toggles; it does not confirm-and-close. Keys use existing `app.models.*`. |
| `internal/models` | `RefreshAll` returns failed provider ids (errors are currently discarded). Selector calls it with `force=true` and a 15s timeout. |

`/model` picker, session files, and the `enabledModels` JSON type (string
array) do not change.

## Settings and startup

File: `~/.pigo/agent/settings.json` (not `~/.pi`). Field `enabledModels`.

### In-memory states

| `Config.EnabledModels` | JSON | Meaning |
| --- | --- | --- |
| `nil` | key absent | All enabled. Ctrl+P walks the full available catalog. |
| non-nil empty slice | `"enabledModels": []` | Explicit none. Selector shows all ✗. Cycle still treats empty `Scoped` as implicit-all. |
| non-empty | `"enabledModels": ["anthropic/…", …]` | Ordered allowlist. Order is Ctrl+P order. |

Load through existing viper/mapstructure. Missing key and JSON `null` must
become Go `nil`, not an empty slice (empty slice is the explicit-none state).
If viper collapses those, read the raw object the same way `fillPackagesFromFile`
does and set `nil` when the key is absent.

`Save` follows the same three states: `nil` deletes the key; otherwise write
the slice. Other Save callers must not drop or invent the field: startup loads
the file value into `Config`, and they write it back unchanged. Only Ctrl+S in
this editor assigns `EnabledModels` to `nil` or to a new list.

### Ctrl+S

Delete the key when either:

- selector state is `all`, or
- `!all`, `len(ids) == len(available)`, and every id is in the available set

Otherwise write `ids` as `provider/id` strings, including unavailable
ids that are still checked. A list that contains every available id **plus**
unavailable ids is **not** cleared (length does not match). Globs such as
`claude-*` are expanded to concrete ids (this is intended).

Overlay stays open. Transcript meta: `Model selection saved to settings`.

Enter, Ctrl+A/X/P, and Alt+↑↓ do not write settings; they only update
`Engine.Scoped`. Esc does not write settings and does not revert `Scoped`.

### Startup

```
patterns = splitCSV(--models)   // cobra default "" → nil
if len(patterns) == 0 {
    patterns = settings.enabledModels
}
Engine.Scoped = ResolvePatterns(patterns, availableModels)
```

CLI `--models` replaces settings; it is not a union. Empty CSV (flag omitted
or blank) falls through to settings — cobra cannot tell "unset" from
`--models ""`, and we do not add `flag.Changed`. Both empty → empty `Scoped`
→ implicit-all.

`availableModels` is the same set `/model` uses: `Available(authenticated)`
when that list is non-empty, otherwise `Catalog()`. Matching stays the current
`ResolvePatterns` rules. Unmatched patterns are dropped at startup (same as
today's `--models`); the selector re-attaches them as unavailable rows.

When `Scoped` is empty, `Cycle` still walks `Catalog()` (today's `models.Cycle`).
This issue does not change that implicit-all path.

## Selector state machine

Go state in the picker (TypeScript `null` vs `string[]`):

```
type enabledIDs struct {
    all bool     // true = implicit-all (TS null)
    ids []string // explicit list when all == false; may be empty
}
```

- `all` — no ✓/✗. Footer `all enabled`. Alt+↑↓ is a no-op.
- explicit — enabled ids first (that order), then the rest of the catalog.
  Available rows get ✓/✗; ids missing from the catalog render as
  `[unavailable] ✗`.

Initial value when opening:

- `Engine.Scoped` non-empty → those `provider/id` strings (explicit list).
- Else resolve `settings.enabledModels`. Empty/missing → `all`. Unmatched
  patterns stay in the list as unavailable.

### Toggle (Enter)

- `all` → explicit list containing only this id, not "all minus this".
- Id already in the list → remove it.
- Else append at the end.

### Other keys

While the picker is open these bindings win over Ctrl+P cycle and Alt+↑
dequeue, using existing `app.models.*` / TUI select keys:

| Key | Behaviour |
| --- | --- |
| Ctrl+A | Enable all. With a search query, only filtered rows. If the result covers every available id → `all`. Already `all` → unchanged. |
| Ctrl+X | Clear. With a search query, only filtered rows. From `all`, result is available minus targets. |
| Ctrl+P | Current row's provider: if every model of that provider is enabled, clear them; else enable them. |
| Alt+↑↓ | Only on an explicit list, and only if the current row is enabled: swap inside `enabledIds` and move the cursor with the row. |
| Ctrl+S | Persist per Settings section; clear dirty; do not close. |
| Esc | Close overlay. Do not write settings. Do not roll back `Engine.Scoped`. |
| Ctrl+C | Non-empty search → clear search; else same as Esc. |
| Printable input | `listPicker` fuzzy filter. |

Each toggle / all / clear / provider / reorder sets `dirty`, shows
`(unsaved)` in the footer, and immediately syncs the session.

### Session sync

```
if !all
   and at least one id is currently available
   and not every available id is in enabledIds   // supersets still count as "fully checked"
→ Scoped = ResolvePatterns(enabledIds) in that order
else
→ Scoped = empty   // all, fully checked (even with extra unavailable), or only unavailable
```

Empty `Scoped` keeps today's `Cycle`: walk the full catalog. `/model`'s
scoped tab already reads `Engine.Scoped`; no picker changes.

`SetScopedModels` copies the slice onto `Engine.Scoped`.

### Chrome

Embed `listPicker` (scroll, fuzzy, `pickerMaxVisible`). Title
`Model Configuration`. Subtitle `Session-only. <app.models.save hint> to save
to settings.` (default hint is Ctrl+S). Slash description:
`Enable/disable models for Ctrl+P cycling`. Selected-row Aux:
`Model Name: …` or `Model unavailable`.

## Catalog refresh

On open, paint from the cached catalog immediately, then fire a `tea.Cmd`.
Skip the Cmd (and do not show `Refreshing model catalogs…`) when offline, when
there is no catalog URL, or when `engine == nil`.

The Cmd uses `context.WithTimeout(15s)` and `RefreshAll(..., force=true)`.
`force` must be true; otherwise the 4-hour cache makes the refresh a no-op
after startup `PrepareCatalog`. Closing the overlay cancels the context.
If the picker is already gone when the message arrives, drop it.

`RefreshAll` currently discards per-provider errors. Change it to return the
failed provider ids (including `RefreshModels` hook failures). Keep the
existing 4s HTTP client timeout per provider; 15s is the selector-level
budget.

When the Cmd returns and the picker is still open:

1. Rebuild the list from the new catalog.
2. If the user has not edited **and** session scoped was empty at open →
   re-resolve checks from `settings.enabledModels` (globs may pick up new
   models).
3. Otherwise keep current `enabledIds`. Newly appeared models start disabled.
4. If current state is not `all`, run `SetScopedModels` again.

### Footer / status copy (English, issue 13 / current pi wording)

| Case | Text |
| --- | --- |
| In flight | `Refreshing model catalogs…` |
| Context deadline | `Model refresh timed out; showing cached models.` |
| Providers failed, not a timeout | `Could not refresh anthropic, openai; showing cached models.` |
| Success | `Model catalogs refreshed.` |
| Ctrl+S | transcript meta `Model selection saved to settings` |

Timeout wins over partial failure. On failure keep the cached list; do not
close the picker.

Startup `PrepareCatalog` (`force=false`, 4s) is unchanged.

## Files (expected)

- `internal/config/config.go` — field + Save delete/write
- `internal/runtime/runtime.go` — startup patterns, `SetScopedModels`
- `internal/models/models.go` — `ResolvePatterns` takes the model list to
  match against (default callers pass available/catalog)
- `internal/models/remote.go` — `RefreshAll` returns failed ids
- `internal/tui/scoped_models.go` — picker, enabledIds helpers, refresh Cmd
- `internal/tui/model.go` — `/scoped-models` branch, key routing
- `internal/slash/slash.go` — description text
- tests next to those packages

No new settings file format. No session JSONL fields.

## Tests

Follow existing `*_test.go` style. Assert behaviour, not pixels. No live
`pi.dev` calls.

- **enabledIds helpers:** `all` + first toggle → single id; enableAll covering
  all → `all`; clearAll from `all` is a set-difference; reorder on `all` is
  a no-op; unavailable ids remain in the list.
- **config:** three-state round-trip; Save of unrelated fields keeps
  `enabledModels`; saving "all enabled" deletes the key.
- **startup:** CLI `--models` wins over settings; settings used when CLI is
  empty; both empty → empty `Scoped`.
- **session:** subset → `Scoped` order is check order, `Cycle` follows it;
  `all` / fully checked / only-unavailable → empty `Scoped`, `Cycle` walks
  the full list.
- **TUI:** `/scoped-models` opens; Enter does not write the file; Esc does not
  write and does not roll back `Engine.Scoped`; Ctrl+S writes. Inject the
  refresh func: timeout copy; dirty selection is not overwritten.

## Issue 13 acceptance

| Criterion | How |
| --- | --- |
| Selector lists models, ✓/✗, fuzzy, unavailable | `scopedModelsPicker` |
| Enter is session-only; close does not write | toggle → `SetScopedModels`; Esc skips `Save` |
| Ctrl+S writes `enabledModels`; all-enabled clears the key | Settings section |
| Explicit order = Ctrl+P / Shift+Ctrl+P; 0–1 or implicit-all matches current `Cycle` | `SetScopedModels` + existing `Cycle` |
| `--models` vs `enabledModels` replace-not-union; same pattern language | Startup section |
| `/model` scoped tab works when a scoped set exists | Existing picker + live `Engine.Scoped` |
| `enabledModels` remains a string pattern array | Ctrl+S writes `provider/id` strings |
| In-selector catalog refresh, 15s, status line | Catalog refresh section |
