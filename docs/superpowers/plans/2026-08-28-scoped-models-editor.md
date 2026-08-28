# /scoped-models Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `/scoped-models` so the user can toggle the current session's Ctrl+P cycle set, persist it with Ctrl+S as `enabledModels` in `~/.pigo/agent/settings.json`, and force-refresh model catalogs while the editor is open.

**Architecture:** Pure `enabledIDs` helpers plus `ResolvePatternsIn` feed `Engine.Scoped`. A `scopedModelsPicker` embeds `listPicker` (same pattern as `/model`). Enter updates session only; Ctrl+S writes settings; a `tea.Cmd` calls `RefreshAll(..., force=true)` with a 15s timeout.

**Tech Stack:** Go 1.27, Bubble Tea, existing `internal/{config,models,runtime,tui,keys,slash}` packages. No new modules.

**Spec:** `docs/superpowers/specs/2026-08-28-scoped-models-editor-design.md` (behavior contract). This plan is the how.

## Global Constraints

- Settings file is `~/.pigo/agent/settings.json` (never `~/.pi`). Field name `enabledModels`, type string array.
- Do not change session JSONL or `~/.pigo/agent` layout.
- Do not change `/model` picker UI; it already reads `Engine.Scoped`.
- Do not rewrite glob/substring/`:thinking` matching; only add a list parameter.
- Do not preserve globs or `:thinking` on Ctrl+S; write `provider/id` strings.
- Status/footer copy is the English strings in Task 7 (verbatim).
- Startup `PrepareCatalog` stays `force=false`, 4s. Selector refresh is `force=true`, 15s.
- Tests must not call live `pi.dev`. Use `t.TempDir()`, `httptest`, and an injectable refresh func.
- Empty `Engine.Scoped` still means implicit-all; `models.Cycle` still walks `Catalog()` in that case. Do not change `Cycle`.
- CLI `--models` replaces settings (not a union). Empty CSV (`splitCSV` → nil) falls through to `enabledModels`.

---

## File structure

| File | Responsibility |
| --- | --- |
| Create `internal/tui/scoped_ids.go` | `enabledIDs` value type and toggle/enableAll/clearAll/move/sortedIDs/sessionScopeIDs. No TUI, no IO. |
| Create `internal/tui/scoped_ids_test.go` | Table tests for those helpers. |
| Create `internal/tui/scoped_models.go` | Picker overlay, slash open, key routing helpers, persist, refresh Cmd. |
| Create `internal/tui/scoped_models_test.go` | TUI tests: open, toggle, Esc, Ctrl+S, refresh messages. |
| Modify `internal/config/config.go` | `EnabledModels []string`; Load via raw JSON (nil vs `[]`); Save deletes or writes the key. |
| Modify `internal/config/config_test.go` | Three-state round-trip; unrelated Save keeps the key; all-enabled deletes it. |
| Modify `internal/models/models.go` | `ResolvePatternsIn`, `UnmatchedPatterns`; `ResolvePatterns` becomes a wrapper. |
| Modify `internal/models/models_test.go` | Order, unmatched, list restriction. |
| Modify `internal/models/remote.go` | `RefreshAll` returns failed provider ids. |
| Modify `internal/models/remote_test.go` | Failed ids from HTTP 500; empty baseURL returns nil. |
| Modify `internal/runtime/runtime.go` | Startup patterns; `SetScopedModels`; `PersistEnabledModels`. |
| Modify `internal/runtime/runtime_test.go` | CLI vs settings; persist three-state. |
| Modify `internal/slash/slash.go` | Description text. |
| Modify `internal/slash/slash_test.go` | Help/builtin description. |
| Modify `internal/tui/model.go` | Field, `Update`/`View`/`handleSlash` routing. |

Do not fatten `listPicker`. Do not add an `overlayKind`.

---

### Task 1: enabledIDs helpers

**Files:**
- Create: `internal/tui/scoped_ids.go`
- Test: `internal/tui/scoped_ids_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type enabledIDs struct { all bool; ids []string }`
  - `func (e enabledIDs) clone() enabledIDs`
  - `func (e enabledIDs) isEnabled(id string) bool`
  - `func toggleID(e enabledIDs, id string) enabledIDs`
  - `func enableAllIDs(e enabledIDs, allIDs, targetIDs []string) enabledIDs`
  - `func clearAllIDs(e enabledIDs, allIDs, targetIDs []string) enabledIDs`
  - `func moveID(e enabledIDs, id string, delta int) enabledIDs`
  - `func sortedIDs(e enabledIDs, allIDs []string) []string`
  - `func sessionScopeIDs(e enabledIDs, available []string) (ids []string, implicitAll bool)`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/scoped_ids_test.go`:

```go
package tui

import (
	"reflect"
	"testing"
)

func TestToggleFromAllBecomesSingleton(t *testing.T) {
	got := toggleID(enabledIDs{all: true}, "a/1")
	if got.all || !reflect.DeepEqual(got.ids, []string{"a/1"}) {
		t.Fatalf("%+v", got)
	}
}

func TestToggleRemovesAndAppends(t *testing.T) {
	e := enabledIDs{ids: []string{"a/1", "b/2"}}
	got := toggleID(e, "a/1")
	if !reflect.DeepEqual(got.ids, []string{"b/2"}) {
		t.Fatalf("remove: %+v", got)
	}
	got = toggleID(got, "c/3")
	if !reflect.DeepEqual(got.ids, []string{"b/2", "c/3"}) {
		t.Fatalf("append: %+v", got)
	}
}

func TestEnableAllCoversAllReturnsAll(t *testing.T) {
	all := []string{"a/1", "b/2"}
	got := enableAllIDs(enabledIDs{ids: []string{"a/1"}}, all, nil)
	if !got.all {
		t.Fatalf("%+v", got)
	}
	got = enableAllIDs(enabledIDs{all: true}, all, nil)
	if !got.all {
		t.Fatal("all stays all")
	}
}

func TestEnableAllWithTargets(t *testing.T) {
	all := []string{"a/1", "b/2", "c/3"}
	got := enableAllIDs(enabledIDs{ids: []string{"a/1"}}, all, []string{"b/2"})
	if got.all || !reflect.DeepEqual(got.ids, []string{"a/1", "b/2"}) {
		t.Fatalf("%+v", got)
	}
}

func TestClearAllFromAll(t *testing.T) {
	all := []string{"a/1", "b/2", "c/3"}
	got := clearAllIDs(enabledIDs{all: true}, all, nil)
	if got.all || len(got.ids) != 0 {
		t.Fatalf("clear all: %+v", got)
	}
	got = clearAllIDs(enabledIDs{all: true}, all, []string{"b/2"})
	if got.all || !reflect.DeepEqual(got.ids, []string{"a/1", "c/3"}) {
		t.Fatalf("clear targets from all: %+v", got)
	}
}

func TestMoveNoopWhenAll(t *testing.T) {
	got := moveID(enabledIDs{all: true}, "a/1", 1)
	if !got.all {
		t.Fatalf("%+v", got)
	}
}

func TestMoveSwapsEnabled(t *testing.T) {
	e := enabledIDs{ids: []string{"a/1", "b/2", "c/3"}}
	got := moveID(e, "a/1", 1)
	if !reflect.DeepEqual(got.ids, []string{"b/2", "a/1", "c/3"}) {
		t.Fatalf("%+v", got)
	}
	got = moveID(e, "a/1", -1)
	if !reflect.DeepEqual(got.ids, e.ids) {
		t.Fatalf("oob: %+v", got)
	}
}

func TestSortedIDsEnabledFirst(t *testing.T) {
	all := []string{"a/1", "b/2", "c/3"}
	got := sortedIDs(enabledIDs{ids: []string{"c/3", "a/1"}}, all)
	if !reflect.DeepEqual(got, []string{"c/3", "a/1", "b/2"}) {
		t.Fatalf("%v", got)
	}
	got = sortedIDs(enabledIDs{all: true}, all)
	if !reflect.DeepEqual(got, all) {
		t.Fatalf("all order: %v", got)
	}
}

func TestSortedIDsKeepsUnavailable(t *testing.T) {
	all := []string{"a/1"}
	got := sortedIDs(enabledIDs{ids: []string{"gone/x", "a/1"}}, all)
	if !reflect.DeepEqual(got, []string{"gone/x", "a/1"}) {
		t.Fatalf("%v", got)
	}
}

func TestSessionScopeIDs(t *testing.T) {
	avail := []string{"a/1", "b/2"}
	ids, implicit := sessionScopeIDs(enabledIDs{all: true}, avail)
	if !implicit || ids != nil {
		t.Fatalf("all: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{"a/1"}}, avail)
	if implicit || !reflect.DeepEqual(ids, []string{"a/1"}) {
		t.Fatalf("subset: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{"a/1", "b/2"}}, avail)
	if !implicit {
		t.Fatalf("fully checked: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{"a/1", "b/2", "gone/x"}}, avail)
	if !implicit {
		t.Fatalf("superset: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{"gone/x"}}, avail)
	if !implicit {
		t.Fatalf("only unavailable: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{}}, avail)
	if !implicit {
		t.Fatalf("empty explicit: %v %v", ids, implicit)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'TestToggleFromAll|TestToggleRemoves|TestEnableAll|TestClearAll|TestMove|TestSortedIDs|TestSessionScopeIDs' -count=1`

Expected: FAIL compile: `undefined: enabledIDs`

- [ ] **Step 3: Write minimal implementation**

Create `internal/tui/scoped_ids.go`:

```go
package tui

type enabledIDs struct {
	all bool
	ids []string
}

func (e enabledIDs) clone() enabledIDs {
	if e.all {
		return enabledIDs{all: true}
	}
	return enabledIDs{ids: append([]string(nil), e.ids...)}
}

func (e enabledIDs) isEnabled(id string) bool {
	if e.all {
		return true
	}
	for _, x := range e.ids {
		if x == id {
			return true
		}
	}
	return false
}

func toggleID(e enabledIDs, id string) enabledIDs {
	if e.all {
		return enabledIDs{ids: []string{id}}
	}
	out := make([]string, 0, len(e.ids)+1)
	found := false
	for _, x := range e.ids {
		if x == id {
			found = true
			continue
		}
		out = append(out, x)
	}
	if !found {
		out = append(out, id)
	}
	return enabledIDs{ids: out}
}

func enableAllIDs(e enabledIDs, allIDs, targetIDs []string) enabledIDs {
	if e.all {
		return enabledIDs{all: true}
	}
	targets := targetIDs
	if targets == nil {
		targets = allIDs
	}
	out := append([]string(nil), e.ids...)
	have := map[string]bool{}
	for _, x := range out {
		have[x] = true
	}
	for _, id := range targets {
		if !have[id] {
			out = append(out, id)
			have[id] = true
		}
	}
	if len(out) == len(allIDs) {
		ok := true
		allow := map[string]bool{}
		for _, id := range allIDs {
			allow[id] = true
		}
		for _, id := range out {
			if !allow[id] {
				ok = false
				break
			}
		}
		if ok {
			return enabledIDs{all: true}
		}
	}
	return enabledIDs{ids: out}
}

func clearAllIDs(e enabledIDs, allIDs, targetIDs []string) enabledIDs {
	if e.all {
		if targetIDs == nil {
			return enabledIDs{ids: []string{}}
		}
		drop := map[string]bool{}
		for _, id := range targetIDs {
			drop[id] = true
		}
		var out []string
		for _, id := range allIDs {
			if !drop[id] {
				out = append(out, id)
			}
		}
		if out == nil {
			out = []string{}
		}
		return enabledIDs{ids: out}
	}
	drop := map[string]bool{}
	if targetIDs == nil {
		for _, id := range e.ids {
			drop[id] = true
		}
	} else {
		for _, id := range targetIDs {
			drop[id] = true
		}
	}
	var out []string
	for _, id := range e.ids {
		if !drop[id] {
			out = append(out, id)
		}
	}
	if out == nil {
		out = []string{}
	}
	return enabledIDs{ids: out}
}

func moveID(e enabledIDs, id string, delta int) enabledIDs {
	if e.all {
		return enabledIDs{all: true}
	}
	list := append([]string(nil), e.ids...)
	idx := -1
	for i, x := range list {
		if x == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return enabledIDs{ids: list}
	}
	j := idx + delta
	if j < 0 || j >= len(list) {
		return enabledIDs{ids: list}
	}
	list[idx], list[j] = list[j], list[idx]
	return enabledIDs{ids: list}
}

func sortedIDs(e enabledIDs, allIDs []string) []string {
	if e.all {
		return append([]string(nil), allIDs...)
	}
	have := map[string]bool{}
	for _, id := range e.ids {
		have[id] = true
	}
	out := append([]string(nil), e.ids...)
	for _, id := range allIDs {
		if !have[id] {
			out = append(out, id)
		}
	}
	return out
}

func sessionScopeIDs(e enabledIDs, available []string) (ids []string, implicitAll bool) {
	if e.all {
		return nil, true
	}
	allow := map[string]bool{}
	for _, id := range available {
		allow[id] = true
	}
	hasAvailable := false
	for _, id := range e.ids {
		if allow[id] {
			hasAvailable = true
			break
		}
	}
	allChecked := true
	for _, id := range available {
		if !e.isEnabled(id) {
			allChecked = false
			break
		}
	}
	if !hasAvailable || allChecked {
		return nil, true
	}
	return append([]string(nil), e.ids...), false
}
```

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/tui -run 'TestToggleFromAll|TestToggleRemoves|TestEnableAll|TestClearAll|TestMove|TestSortedIDs|TestSessionScopeIDs' -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/scoped_ids.go internal/tui/scoped_ids_test.go
git commit -m "feat: add enabledIDs helpers for scoped-models"
```

---

### Task 2: settings.json `enabledModels`

**Files:**
- Modify: `internal/config/config.go` (`Config` struct ~line 17, `fillPackagesFromFile` ~line 379, `Save` ~line 412)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: existing `Load` / `Save` merge map
- Produces: `Config.EnabledModels []string` (`nil` = key absent; non-nil empty = `"enabledModels": []`)

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestEnabledModelsThreeStates(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnabledModels != nil {
		t.Fatalf("missing file: %v", cfg.EnabledModels)
	}

	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"enabledModels":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnabledModels == nil || len(cfg.EnabledModels) != 0 {
		t.Fatalf("empty array: %#v", cfg.EnabledModels)
	}

	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"enabledModels":["anthropic/claude-sonnet-4"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.EnabledModels) != 1 || cfg.EnabledModels[0] != "anthropic/claude-sonnet-4" {
		t.Fatalf("list: %#v", cfg.EnabledModels)
	}
}

func TestSaveEnabledModelsDeleteAndKeep(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DefaultProvider: "openai", DefaultModel: "gpt-4o", Theme: "default", EnabledModels: []string{"openai/gpt-4o"}}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"enabledModels"`) || !strings.Contains(string(b), "openai/gpt-4o") {
		t.Fatalf("write: %s", b)
	}
	cfg.Theme = "dark"
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"enabledModels"`) || !strings.Contains(string(b), `"dark"`) {
		t.Fatalf("preserve: %s", b)
	}
	cfg.EnabledModels = nil
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "enabledModels") {
		t.Fatalf("delete: %s", b)
	}
}

func TestSaveEnabledModelsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Theme: "default", EnabledModels: []string{}}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"enabledModels"`) {
		t.Fatalf("empty array should write the key: %s", b)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EnabledModels == nil {
		t.Fatal("empty array loaded as nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config -run 'TestEnabledModels|TestSaveEnabledModels' -count=1`

Expected: FAIL compile: `cfg.EnabledModels undefined`

- [ ] **Step 3: Write minimal implementation**

In `Config` (after `DefaultProjectTrust`):

```go
EnabledModels []string `mapstructure:"enabledModels"`
```

In `fillPackagesFromFile`, extend the anonymous struct with `EnabledModels json.RawMessage \`json:"enabledModels"\`` and after the other assignments:

```go
if extra.EnabledModels == nil {
	cfg.EnabledModels = nil
} else {
	var ids []string
	if json.Unmarshal(extra.EnabledModels, &ids) == nil {
		if ids == nil {
			ids = []string{}
		}
		cfg.EnabledModels = ids
	}
}
```

In `Save`, after writing `defaultProjectTrust` (or immediately before `json.MarshalIndent`):

```go
if cfg.EnabledModels == nil {
	delete(existing, "enabledModels")
} else {
	existing["enabledModels"] = cfg.EnabledModels
}
```

This must run even when the slice is empty so `"enabledModels": []` is written.

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/config -count=1`

Expected: PASS (including existing merge tests)

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: load and save settings enabledModels"
```

---

### Task 3: ResolvePatternsIn, UnmatchedPatterns, RefreshAll errors

**Files:**
- Modify: `internal/models/models.go` (`ResolvePatterns` ~line 65)
- Modify: `internal/models/remote.go` (`RefreshAll` ~line 137)
- Test: `internal/models/models_test.go`, `internal/models/remote_test.go`

**Interfaces:**
- Consumes: `ParseSpec`, `Catalog`, `RefreshProvider`
- Produces:
  - `func ResolvePatterns(patterns []string) []Spec` — wrapper, still matches `Catalog()`
  - `func ResolvePatternsIn(patterns []string, list []Model) []Spec`
  - `func UnmatchedPatterns(patterns []string, list []Model) []string`
  - `func RefreshAll(ctx context.Context, store CatalogStore, baseURL string, force bool) []string` — failed provider ids

- [ ] **Step 1: Write the failing tests**

Append to `internal/models/models_test.go`:

```go
func TestResolvePatternsInRestrictsList(t *testing.T) {
	list := []Model{
		{Provider: "anthropic", ID: "claude-sonnet-4"},
		{Provider: "openai", ID: "gpt-4o"},
	}
	got := ResolvePatternsIn([]string{"anthropic/*"}, list)
	if len(got) != 1 || got[0].Provider != "anthropic" {
		t.Fatalf("%+v", got)
	}
}

func TestResolvePatternsInPreservesPatternOrder(t *testing.T) {
	list := []Model{
		{Provider: "anthropic", ID: "claude-sonnet-4"},
		{Provider: "openai", ID: "gpt-4o"},
	}
	got := ResolvePatternsIn([]string{"openai/gpt-4o", "anthropic/claude-sonnet-4"}, list)
	if len(got) != 2 || got[0].ID != "gpt-4o" || got[1].ID != "claude-sonnet-4" {
		t.Fatalf("order %+v", got)
	}
}

func TestUnmatchedPatterns(t *testing.T) {
	list := []Model{{Provider: "anthropic", ID: "claude-sonnet-4"}}
	got := UnmatchedPatterns([]string{"anthropic/claude-sonnet-4", "missing/model", "nope/*"}, list)
	if len(got) != 2 || got[0] != "missing/model" || got[1] != "nope/*" {
		t.Fatalf("%v", got)
	}
}
```

Append to `internal/models/remote_test.go`:

```go
func TestRefreshAllReturnsFailedProviders(t *testing.T) {
	t.Cleanup(ClearOverlays)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	failed := RefreshAll(context.Background(), &MemoryStore{}, srv.URL, true)
	if len(failed) == 0 {
		t.Fatal("expected failed provider ids")
	}
}

func TestRefreshAllEmptyBaseURL(t *testing.T) {
	if got := RefreshAll(context.Background(), &MemoryStore{}, "", true); got != nil {
		t.Fatalf("%v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models -run 'TestResolvePatternsIn|TestUnmatchedPatterns|TestRefreshAllReturnsFailed|TestRefreshAllEmpty' -count=1`

Expected: FAIL compile: `undefined: ResolvePatternsIn` (and `RefreshAll` assignment mismatch once that compiles)

- [ ] **Step 3: Write minimal implementation**

Replace `ResolvePatterns` in `internal/models/models.go` with:

```go
// ResolvePatterns maps --models patterns onto the catalog (globs + substring).
func ResolvePatterns(patterns []string) []Spec {
	return ResolvePatternsIn(patterns, Catalog())
}

// ResolvePatternsIn maps patterns onto list (same matching as ResolvePatterns).
func ResolvePatternsIn(patterns []string, list []Model) []Spec {
	if len(patterns) == 0 {
		return nil
	}
	var out []Spec
	seen := map[string]bool{}
	for _, raw := range patterns {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, _, thinking := ParseSpec(raw)
		pattern := raw
		if thinking != "" {
			if i := strings.LastIndex(raw, ":"); i >= 0 {
				pattern = raw[:i]
			}
		}
		g, gerr := glob.Compile(strings.ToLower(pattern))
		for _, m := range list {
			hay := strings.ToLower(m.Provider + "/" + m.ID)
			match := strings.Contains(hay, strings.ToLower(pattern)) ||
				strings.Contains(strings.ToLower(m.ID), strings.ToLower(pattern))
			if gerr == nil && g.Match(hay) {
				match = true
			}
			if !match {
				continue
			}
			key := m.Provider + "/" + m.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Spec{Model: m, Thinking: thinking})
		}
	}
	return out
}

// UnmatchedPatterns returns patterns that resolve to no models in list.
func UnmatchedPatterns(patterns []string, list []Model) []string {
	var out []string
	for _, raw := range patterns {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if len(ResolvePatternsIn([]string{raw}, list)) == 0 {
			out = append(out, raw)
		}
	}
	return out
}
```

Keep `Cycle` unchanged.

In `internal/models/remote.go` change `RefreshAll` to:

```go
// RefreshAll revalidates remote catalogs for every registered provider.
// It returns provider ids whose refresh failed.
func RefreshAll(ctx context.Context, store CatalogStore, baseURL string, force bool) []string {
	if store == nil || baseURL == "" {
		return nil
	}
	var (
		mu     sync.Mutex
		failed []string
		wg     sync.WaitGroup
	)
	for _, id := range ProviderIDs() {
		spec, _ := LookupProvider(id)
		if spec.RefreshModels != nil {
			if err := spec.RefreshModels(store); err != nil {
				failed = append(failed, id)
			}
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := RefreshProvider(ctx, store, baseURL, id, force); err != nil {
				mu.Lock()
				failed = append(failed, id)
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	return failed
}
```

`PrepareCatalog` currently calls `RefreshAll(...)` and ignores the result — that still compiles.

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/models -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/models/models.go internal/models/models_test.go internal/models/remote.go internal/models/remote_test.go
git commit -m "feat: resolve model patterns against a list and report catalog refresh failures"
```

---

### Task 4: Engine.Scoped startup, SetScopedModels, PersistEnabledModels

**Files:**
- Modify: `internal/runtime/runtime.go` (`New` Scoped assignment ~line 196; after `CycleModel` ~line 723)
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `config.EnabledModels`, `models.ResolvePatternsIn`, `models.Available`, `auth.AuthenticatedIDs`, `config.Save`
- Produces:
  - `func (e *Engine) SetScopedModels(specs []models.Spec)`
  - `func (e *Engine) PersistEnabledModels(patterns *[]string) error` — `nil` pointer deletes the key; non-nil (including empty) writes the slice
  - Startup: `patterns = opts.Models`; if empty, `UserConfig.EnabledModels` if `UserConfig != nil` else `Config.EnabledModels`; `Scoped = ResolvePatternsIn(patterns, available)` where available is `Available(auth)` or `Catalog()` if empty

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/runtime_test.go` (add `"github.com/Lowpower/pigo/internal/models"` to imports):

```go
func TestNewScopedFromSettingsWhenCLIEmpty(t *testing.T) {
	dir := t.TempDir()
	e, err := New(context.Background(), Options{
		AgentDir: dir,
		Cwd:      t.TempDir(),
		Offline:  true,
		NoTools:      true,
		NoSkills:     true,
		NoExtensions: true,
		Config: config.Config{
			Provider: "anthropic", Model: "claude-sonnet-4",
			EnabledModels: []string{"anthropic/claude-sonnet-4", "anthropic/claude-haiku-4"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if len(e.Scoped) != 2 || e.Scoped[0].ID != "claude-sonnet-4" || e.Scoped[1].ID != "claude-haiku-4" {
		t.Fatalf("%+v", e.Scoped)
	}
}

func TestNewCLIModelsReplaceSettings(t *testing.T) {
	dir := t.TempDir()
	e, err := New(context.Background(), Options{
		AgentDir: dir,
		Cwd:      t.TempDir(),
		Offline:  true,
		NoTools:      true,
		NoSkills:     true,
		NoExtensions: true,
		Models:       []string{"openai/gpt-4o"},
		Config: config.Config{
			Provider: "anthropic", Model: "claude-sonnet-4",
			EnabledModels: []string{"anthropic/claude-sonnet-4", "anthropic/claude-haiku-4"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if len(e.Scoped) != 1 || e.Scoped[0].ID != "gpt-4o" {
		t.Fatalf("%+v", e.Scoped)
	}
}

func TestSetScopedModelsAndCycleOrder(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	e.SetScopedModels([]models.Spec{
		{Model: models.Model{Provider: "openai", ID: "gpt-4o"}},
		{Model: models.Model{Provider: "anthropic", ID: "claude-sonnet-4"}},
		{Model: models.Model{Provider: "anthropic", ID: "claude-haiku-4"}},
	})
	next, ok := e.CycleModel(false)
	if !ok || next.ID != "claude-haiku-4" {
		t.Fatalf("cycle from sonnet in custom order = %+v ok=%v", next, ok)
	}
}

func TestPersistEnabledModels(t *testing.T) {
	dir := t.TempDir()
	e := &Engine{Opts: Options{AgentDir: dir, Config: config.Config{Theme: "default"}}}
	ids := []string{"openai/gpt-4o"}
	if err := e.PersistEnabledModels(&ids); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.EnabledModels) != 1 || loaded.EnabledModels[0] != "openai/gpt-4o" {
		t.Fatalf("%v", loaded.EnabledModels)
	}
	if err := e.PersistEnabledModels(nil); err != nil {
		t.Fatal(err)
	}
	loaded, err = config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EnabledModels != nil {
		t.Fatalf("clear: %v", loaded.EnabledModels)
	}
}
```

Use `NoTools: true`, `NoSkills: true`, `NoExtensions: true`, `Offline: true` so `New` does not spawn extensions or hit the catalog network.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime -run 'TestNewScoped|TestNewCLIModels|TestSetScopedModels|TestPersistEnabledModels' -count=1`

Expected: FAIL: settings-based Scoped is empty / `SetScopedModels` undefined

- [ ] **Step 3: Write minimal implementation**

In `New`, after `store := auth.Open(opts.AgentDir)` is available and before constructing `Engine`, compute:

```go
patterns := opts.Models
if len(patterns) == 0 {
	if opts.UserConfig != nil {
		patterns = opts.UserConfig.EnabledModels
	} else {
		patterns = opts.Config.EnabledModels
	}
}
avail := models.Available(auth.AuthenticatedIDs(store))
if len(avail) == 0 {
	avail = models.Catalog()
}
scoped := models.ResolvePatternsIn(patterns, avail)
```

Set `Scoped: scoped` on the Engine literal instead of `models.ResolvePatterns(opts.Models)`.

After `CycleModel`:

```go
// SetScopedModels replaces the Ctrl+P cycle list. An empty slice means implicit-all.
func (e *Engine) SetScopedModels(specs []models.Spec) {
	e.Scoped = append([]models.Spec(nil), specs...)
}

// PersistEnabledModels writes settings.json enabledModels.
// A nil pointer deletes the key (all enabled). A non-nil slice (including empty) is written as-is.
func (e *Engine) PersistEnabledModels(patterns *[]string) error {
	apply := func(c *config.Config) {
		if patterns == nil {
			c.EnabledModels = nil
			return
		}
		c.EnabledModels = append([]string(nil), (*patterns)...)
	}
	apply(&e.Opts.Config)
	if e.Opts.UserConfig != nil {
		apply(e.Opts.UserConfig)
	}
	if e.Opts.AgentDir == "" {
		return nil
	}
	return config.Save(e.Opts.AgentDir, e.persistableConfig())
}
```

Cycle list is `[gpt-4o, sonnet, haiku]`, current sonnet, forward → haiku.

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/runtime -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go
git commit -m "feat: apply enabledModels at startup and persist scoped models"
```

---

### Task 5: slash description

**Files:**
- Modify: `internal/slash/slash.go` (Builtins `scoped-models` line)
- Test: `internal/slash/slash_test.go`

**Interfaces:**
- Consumes: `Builtins()`
- Produces: `{Name: "scoped-models", Description: "Enable/disable models for Ctrl+P cycling"}`

- [ ] **Step 1: Write the failing test**

Append to `internal/slash/slash_test.go`:

```go
func TestScopedModelsDescription(t *testing.T) {
	c, ok := Parse("/scoped-models")
	if !ok || c.Name != "scoped-models" {
		t.Fatalf("%+v ok=%v", c, ok)
	}
	if c.Description != "Enable/disable models for Ctrl+P cycling" {
		t.Fatalf("description = %q", c.Description)
	}
	if !contains(HelpText(), "Enable/disable models for Ctrl+P cycling") {
		t.Fatalf("help:\n%s", HelpText())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slash -run TestScopedModelsDescription -count=1`

Expected: FAIL: description is `not implemented`

- [ ] **Step 3: Write minimal implementation**

In `Builtins()`, change the `scoped-models` entry to:

```go
{Name: "scoped-models", Description: "Enable/disable models for Ctrl+P cycling"},
```

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/slash -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/slash/slash.go internal/slash/slash_test.go
git commit -m "feat: document /scoped-models slash command"
```

---

### Task 6: scopedModelsPicker overlay (no network refresh yet)

**Files:**
- Create: `internal/tui/scoped_models.go`
- Create: `internal/tui/scoped_models_test.go`
- Modify: `internal/tui/model.go` (`Model` struct, `Update` KeyMsg, `View`, `handleSlash`)

**Interfaces:**
- Consumes: Task 1 helpers, `Engine.SetScopedModels`, `Engine.PersistEnabledModels`, `catalogModels()`, `listPicker`, `keys.Manager`, `models.ResolvePatternsIn`, `models.UnmatchedPatterns`
- Produces:
  - `type scopedModelsPicker struct` embedding `listPicker` with `enabled enabledIDs`, `available []models.Model`, `allIDs []string`, `dirty bool`, `refreshStatus string`, `openedEmptyScoped bool`, `gen int`, `cancel context.CancelFunc`
  - `func (m Model) scopedModelsActive() bool`
  - `func (m Model) openScopedModels() (tea.Model, tea.Cmd)` — Task 6 returns `nil` cmd; Task 7 adds refresh
  - Ctrl+S persist rule: `all` **or** (`!all` && `len(ids)==len(available)` && every id is in available) → `PersistEnabledModels(nil)`; else `PersistEnabledModels(&ids)` including unavailable ids

Initial `enabledIDs` when opening:

- `engine.Scoped` non-empty → explicit list of `provider/id` in that order
- else resolve `m.cfg.EnabledModels` (or `engine.Opts.UserConfig.EnabledModels` if set) against available; empty/missing → `all`; unmatched patterns appended as extra ids (unavailable)

Chrome:

- Title: `Model Configuration`
- Subtitle: `Session-only. ` + first `keys.Keys("app.models.save")` (fallback `ctrl+s`) + ` to save to settings.`
- Hint/footer: `enter toggle · ctrl+a all · ctrl+x clear · ctrl+p provider · alt+up/alt+down reorder · ctrl+s save · ` + count + optional ` (unsaved)`
- Count: `all enabled` or `N/M enabled` plus ` · K unavailable` when K>0
- Selected detail: implement `func (p scopedModelsPicker) view() string` copying `listPicker.view` (title, `> query`, rows with `→ `, Meta, scroll, hint). Available selected row: `Model Name: <id>`. Unavailable: `Model unavailable`.

Key routing (picker must be checked **before** `/model` picker and **before** editor `app.clear`):

| Key | Action |
| --- | --- |
| Enter (`tui.select.confirm`) | `toggleID` current, dirty, `SetScopedModels`, do not close |
| Ctrl+A | `enableAllIDs` (targets = filtered ids if `query != ""`, else nil) |
| Ctrl+X | `clearAllIDs` (same targets rule) |
| Ctrl+P | provider of current available row: if every provider id `isEnabled` then `clearAllIDs` those ids else `enableAllIDs` those ids |
| Alt+↑ / Alt+↓ | if `!all` and current is enabled, `moveID` ±1 and `p.selected += delta` if move succeeded |
| Ctrl+S | persist, `dirty=false`, transcript `Model selection saved to settings`, stay open |
| Esc | close; do not persist; do not revert `Engine.Scoped` |
| Ctrl+C (`app.clear`) | if query non-empty, clear query and rebuild; else close like Esc |

After every mutating key, rebuild items from `sortedIDs`, preserve selected fullId when possible, call `applyScopedSession`.

`applyScopedSession`:

```go
ids, implicit := sessionScopeIDs(p.enabled, availableIDList(p.available))
if implicit || m.engine == nil {
    if m.engine != nil {
        m.engine.SetScopedModels(nil)
    }
    return
}
m.engine.SetScopedModels(models.ResolvePatternsIn(ids, p.available))
```

`SetScopedModels(nil)` is empty implicit-all.

Label formatting: if `enabled.all`, no ✓/✗; else append ` ✓` or ` ✗`. Meta: `[provider]` or `[unavailable]`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/scoped_models_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/runtime"
)

func TestSlashScopedModelsOpensPicker(t *testing.T) {
	m := New(testCfg())
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.scopedModelsActive() {
		t.Fatal("expected scoped-models picker")
	}
	view := m.View()
	if !strings.Contains(view, "Model Configuration") {
		t.Fatalf("view =\n%s", view)
	}
}

func TestScopedModelsEnterDoesNotWriteSettings(t *testing.T) {
	dir := t.TempDir()
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: m.cfg}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter}) // toggle current
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
		t.Fatal("toggle must not write settings.json")
	}
	if m.engine == nil || len(m.engine.Scoped) == 0 {
		t.Fatal("toggle should set a singleton scoped list from all")
	}
	if !m.scopedModelsActive() {
		t.Fatal("enter must not close the picker")
	}
}

func TestScopedModelsEscKeepsSessionDoesNotSave(t *testing.T) {
	dir := t.TempDir()
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: m.cfg}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	scoped := append([]models.Spec(nil), m.engine.Scoped...)
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.scopedModelsActive() {
		t.Fatal("esc closes")
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
		t.Fatal("esc must not write settings")
	}
	if len(m.engine.Scoped) != len(scoped) {
		t.Fatalf("esc rolled back scoped: %+v vs %+v", m.engine.Scoped, scoped)
	}
}

func TestScopedModelsCtrlSWritesSettings(t *testing.T) {
	dir := t.TempDir()
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: m.cfg}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.scopedModelsActive() {
		t.Fatal("ctrl+s must keep picker open")
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.EnabledModels) == 0 {
		t.Fatalf("expected ids, got %v", loaded.EnabledModels)
	}
	found := false
	for _, e := range m.transcript {
		if strings.Contains(e.rendered, "Model selection saved to settings") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing save status")
	}
}

func TestModelPickerSeesUpdatedScoped(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.models.scope != scopeScoped {
		t.Fatalf("scope = %s", m.models.scope)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'TestSlashScopedModels|TestScopedModelsEnter|TestScopedModelsEsc|TestScopedModelsCtrlS|TestModelPickerSeesUpdatedScoped' -count=1`

Expected: FAIL: `/scoped-models is not implemented` / picker not active

- [ ] **Step 3: Write minimal implementation**

Add to `Model` in `internal/tui/model.go`:

```go
scoped scopedModelsPicker
```

In `Update` → `tea.KeyMsg`, immediately after `settingsActive()` (before overlay/model picker):

```go
if m.scopedModelsActive() {
	return m.handleScopedModelsKey(msg)
}
```

Also handle nothing else in Task 6 for msgs.

In `View`, before `modelPickerActive()`:

```go
if m.scopedModelsActive() {
	return m.scoped.view()
}
```

In `handleSlash`, add before `default`:

```go
case "scoped-models":
	return m.openScopedModels()
```

Create `internal/tui/scoped_models.go` with the types and methods described in Interfaces. Concrete pieces the implementer must include:

`openScopedModels`:

```go
func (m Model) openScopedModels() (tea.Model, tea.Cmd) {
	avail := m.catalogModels()
	allIDs := make([]string, 0, len(avail))
	for _, mod := range avail {
		allIDs = append(allIDs, mod.Provider+"/"+mod.ID)
	}
	patterns := m.cfg.EnabledModels
	if m.engine != nil && m.engine.Opts.UserConfig != nil {
		patterns = m.engine.Opts.UserConfig.EnabledModels
	}
	openedEmpty := m.engine == nil || len(m.engine.Scoped) == 0
	var en enabledIDs
	if m.engine != nil && len(m.engine.Scoped) > 0 {
		ids := make([]string, 0, len(m.engine.Scoped))
		for _, s := range m.engine.Scoped {
			ids = append(ids, s.Provider+"/"+s.ID)
		}
		en = enabledIDs{ids: ids}
	} else if len(patterns) == 0 {
		en = enabledIDs{all: true}
	} else {
		resolved := models.ResolvePatternsIn(patterns, avail)
		ids := make([]string, 0, len(resolved))
		for _, s := range resolved {
			ids = append(ids, s.Provider+"/"+s.ID)
		}
		ids = append(ids, models.UnmatchedPatterns(patterns, avail)...)
		en = enabledIDs{ids: ids}
	}
	saveHint := "ctrl+s"
	if m.keys != nil {
		if ks := m.keys.Keys("app.models.save"); len(ks) > 0 {
			saveHint = ks[0]
		}
	}
	p := scopedModelsPicker{
		listPicker: listPicker{
			title:  "Model Configuration\nSession-only. " + saveHint + " to save to settings.",
			active: true,
		},
		enabled:            en,
		available:          avail,
		allIDs:             allIDs,
		openedEmptyScoped:  openedEmpty,
	}
	p.rebuild()
	m.scoped = p
	return m, nil
}
```

`rebuild` sets items from `sortedIDs`, formats labels, sets `hint` from footer parts + dirty + `refreshStatus`.

`handleScopedModelsKey` as the key table. Use `m.keyIs(msg, "app.models.save")` etc. For Alt+↑ use `m.keyIs(msg, "app.models.reorderUp")`.

`persistScopedModels`: compute available id set from `p.available`; if `p.enabled.all` or (`!p.enabled.all` && `len(p.enabled.ids)==len(p.available)` && every id is in the set) then `PersistEnabledModels(nil)` else copy `p.enabled.ids` and `PersistEnabledModels(&ids)`. Also copy into `m.cfg.EnabledModels`. Then `dirty=false`, append transcript meta `Model selection saved to settings`.

`closeScopedModels`: `m.scoped = scopedModelsPicker{}`.

Provider toggle: from current item ID, split provider, collect `allIDs` with that prefix `provider+"/"`, then enable or clear those as targets.

If `engine == nil`, toggle still mutates picker state (tests attach an engine).

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/tui -count=1`

Expected: PASS. Also run `go test ./internal/slash ./internal/config ./internal/models ./internal/runtime -count=1`.

If Ctrl+S test has empty `EnabledModels` because toggle from `all` on a catalog with many models should write a singleton — assert `len==1` and that it is `provider/id`.

If catalogModels() without engine returns full `Catalog()` (see `model_picker.go`), first row may be anthropic/claude-sonnet-4 depending on Catalog order. Do not assert a specific id unless you set `selected` like `TestModelPickerEnterSelectsSessionOnly`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/scoped_models.go internal/tui/scoped_models_test.go internal/tui/model.go
git commit -m "feat: add /scoped-models session editor overlay"
```

---

### Task 7: in-selector catalog refresh

**Files:**
- Modify: `internal/tui/scoped_models.go`
- Modify: `internal/tui/model.go` (`Update` switch for `scopedRefreshMsg`)
- Modify: `internal/tui/scoped_models_test.go`

**Interfaces:**
- Consumes: `models.RefreshAll`, `models.OpenFileStore`, `models.PrepareCatalog` overlays already in memory
- Produces:
  - `type scopedRefreshMsg struct { gen int; failed []string; timedOut bool }`
  - package var `var scopedCatalogRefresh = defaultScopedCatalogRefresh` with
    `func defaultScopedCatalogRefresh(ctx context.Context, agentDir, baseURL string) (failed []string)`
    calling `models.RefreshAll(ctx, models.OpenFileStore(filepath.Join(agentDir, "models-store.json")), baseURL, true)`
  - `openScopedModels` returns a Cmd unless offline / empty baseURL / `engine==nil`
  - skip Cmd when `engine.Opts.Offline` or `PIGO_OFFLINE` or `CatalogBaseURL==""`
  - in-flight footer `Refreshing model catalogs…`
  - on msg: if `gen != m.scoped.gen` or picker inactive, drop
  - rebuild catalog via `catalogModels()`
  - if `!dirty && openedEmptyScoped`, re-resolve checks from settings patterns
  - else keep `enabled`; new models start disabled
  - if `!enabled.all`, `applyScopedSession` again
  - status: timedOut → `Model refresh timed out; showing cached models.` ; else if `len(failed)>0` → `Could not refresh ` + `strings.Join(failed, ", ")` + `; showing cached models.` ; else `Model catalogs refreshed.`
  - Timeout wins over failed list
  - 15s: `context.WithTimeout(context.Background(), 15*time.Second)` stored in `p.cancel`; close overlay calls `cancel`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/scoped_models_test.go`:

```go
func TestScopedRefreshTimeoutCopy(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, CatalogBaseURL: "http://127.0.0.1:1"}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.scoped.refreshStatus = "Refreshing model catalogs…"
	next, _ := m.Update(scopedRefreshMsg{gen: m.scoped.gen, timedOut: true, failed: []string{"anthropic"}})
	m = next.(Model)
	if !strings.Contains(m.scoped.refreshStatus, "Model refresh timed out; showing cached models.") {
		t.Fatalf("status = %q", m.scoped.refreshStatus)
	}
	if strings.Contains(m.scoped.refreshStatus, "Could not refresh") {
		t.Fatal("timeout must win over failed providers")
	}
}

func TestScopedRefreshDoesNotClobberDirty(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, CatalogBaseURL: "http://example"}}
	m.editor.SetValue("/scoped-models")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	before := m.scoped.enabled.clone()
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.scoped.dirty {
		t.Fatal("expected dirty after toggle")
	}
	after := m.scoped.enabled.clone()
	next, _ := m.Update(scopedRefreshMsg{gen: m.scoped.gen})
	m = next.(Model)
	if m.scoped.enabled.all != after.all || len(m.scoped.enabled.ids) != len(after.ids) {
		t.Fatalf("clobbered %+v -> %+v (started %+v)", after, m.scoped.enabled, before)
	}
}

func TestScopedRefreshSkippedOffline(t *testing.T) {
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Config: m.cfg, Offline: true, CatalogBaseURL: "http://x"}}
	m.editor.SetValue("/scoped-models")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("offline must not start refresh")
	}
	if strings.Contains(m.View(), "Refreshing model catalogs…") {
		t.Fatal("no refreshing status offline")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run 'TestScopedRefresh' -count=1`

Expected: FAIL: `scopedRefreshMsg` undefined and/or offline still showing refresh / cmd non-nil

- [ ] **Step 3: Write minimal implementation**

In `scoped_models.go`:

```go
type scopedRefreshMsg struct {
	gen      int
	failed   []string
	timedOut bool
}

var scopedCatalogRefresh = defaultScopedCatalogRefresh

func defaultScopedCatalogRefresh(ctx context.Context, agentDir, baseURL string) []string {
	if agentDir == "" || baseURL == "" {
		return nil
	}
	return models.RefreshAll(ctx, models.OpenFileStore(filepath.Join(agentDir, "models-store.json")), baseURL, true)
}

func (m Model) startScopedRefresh() tea.Cmd {
	if m.engine == nil || m.engine.Opts.Offline || m.engine.Opts.CatalogBaseURL == "" {
		return nil
	}
	if os.Getenv("PIGO_OFFLINE") != "" {
		return nil
	}
	gen := m.scoped.gen
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.scoped.cancel = cancel
	m.scoped.refreshStatus = "Refreshing model catalogs…"
	dir := m.engine.Opts.AgentDir
	base := m.engine.Opts.CatalogBaseURL
	return func() tea.Msg {
		failed := scopedCatalogRefresh(ctx, dir, base)
		timedOut := ctx.Err() == context.DeadlineExceeded
		return scopedRefreshMsg{gen: gen, failed: failed, timedOut: timedOut}
	}
}
```

`openScopedModels` should increment `gen`, set status only if the Cmd will run, and `return m, m.startScopedRefresh()` — but `startScopedRefresh` mutates picker then returns Cmd; call it after assigning `m.scoped`.

`closeScopedModels`: if `m.scoped.cancel != nil { m.scoped.cancel() }`, then zero the struct.

In `Update` add `case scopedRefreshMsg:` calling `handleScopedRefresh`.

`handleScopedRefresh`:

```go
func (m Model) handleScopedRefresh(msg scopedRefreshMsg) (tea.Model, tea.Cmd) {
	if !m.scoped.active || msg.gen != m.scoped.gen {
		return m, nil
	}
	m.scoped.available = m.catalogModels()
	m.scoped.allIDs = nil
	for _, mod := range m.scoped.available {
		m.scoped.allIDs = append(m.scoped.allIDs, mod.Provider+"/"+mod.ID)
	}
	if !m.scoped.dirty && m.scoped.openedEmptyScoped {
		patterns := m.cfg.EnabledModels
		if m.engine != nil && m.engine.Opts.UserConfig != nil {
			patterns = m.engine.Opts.UserConfig.EnabledModels
		}
		if len(patterns) == 0 {
			m.scoped.enabled = enabledIDs{all: true}
		} else {
			resolved := models.ResolvePatternsIn(patterns, m.scoped.available)
			ids := make([]string, 0, len(resolved))
			for _, s := range resolved {
				ids = append(ids, s.Provider+"/"+s.ID)
			}
			ids = append(ids, models.UnmatchedPatterns(patterns, m.scoped.available)...)
			m.scoped.enabled = enabledIDs{ids: ids}
		}
	}
	if !m.scoped.enabled.all {
		m.applyScopedSession()
	}
	switch {
	case msg.timedOut:
		m.scoped.refreshStatus = "Model refresh timed out; showing cached models."
	case len(msg.failed) > 0:
		m.scoped.refreshStatus = "Could not refresh " + strings.Join(msg.failed, ", ") + "; showing cached models."
	default:
		m.scoped.refreshStatus = "Model catalogs refreshed."
	}
	m.scoped.rebuild()
	return m, nil
}
```

`applyScopedSession` is the same function as Task 6 (must exist as a method on `Model`).

Show `refreshStatus` in the picker hint/footer (muted line above the key hints, matching the spec).

- [ ] **Step 4: Run the tests and make sure they pass**

Run:

```
go test ./internal/tui -count=1
go test ./internal/config ./internal/models ./internal/runtime ./internal/slash -count=1
go test ./...
golangci-lint run ./...
```

Expected: all PASS. `go test ./...` may need network-free env; do not add tests that dial `pi.dev`.

If Task 6 tests now receive a non-nil Cmd from `/scoped-models` because tests set `CatalogBaseURL` empty (zero value), `startScopedRefresh` returns nil — good. `TestSlashScopedModelsOpensPicker` has `engine==nil` so no Cmd.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/scoped_models.go internal/tui/scoped_models_test.go internal/tui/model.go
git commit -m "feat: refresh model catalogs when opening /scoped-models"
```

---

## Self-review (plan vs spec)

| Spec section | Task |
| --- | --- |
| Architecture four pieces | Tasks 2–7 |
| `EnabledModels` three-state Load/Save | Task 2 |
| Ctrl+S delete-key rules including superset-not-cleared | Task 6 persist |
| Startup CLI replaces settings | Task 4 |
| `available` = `Available` else `Catalog` | Task 4 / Task 6 `catalogModels` |
| `Cycle` unchanged when Scoped empty | Task 4 (no Cycle edit) |
| `enabledIDs` toggle/all/clear/move/sorted | Task 1 |
| Session sync implicit-all | Task 1 `sessionScopeIDs` + Task 6 |
| Esc no save no rollback | Task 6 |
| Ctrl+C clears search | Task 6 |
| `/model` scoped tab | Task 6 `TestModelPickerSeesUpdatedScoped` |
| Slash copy | Task 5 |
| Refresh force/15s/status/timeout wins/dirty keep | Task 7 |
| `:thinking` dropped on re-resolve | Task 6 uses bare ids from `Scoped` / patterns via `ResolvePatternsIn` |
| Unmatched → unavailable | Task 3 + Task 6 open |
| No live network tests | Global + Task 7 injectable / offline skip |
