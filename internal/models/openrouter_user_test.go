package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestPersonalizeOpenRouterKeepsKeyChatModels(t *testing.T) {
	zero := 0
	million := 1_000_000
	topCtx := 262_144
	topMax := 32_768
	catalog := []Model{
		{Provider: "openrouter", ID: "listed", API: "openai-completions", ContextWindow: 200_000, MaxTokens: 50_000, Cost: &Cost{Input: 1, Output: 2, CacheRead: 0.1}},
		{Provider: "openrouter", ID: "no-limits", API: "openai-completions", ContextWindow: 200_000, MaxTokens: 50_000, Cost: &Cost{Input: 1, Output: 2}},
		{Provider: "openrouter", ID: "router", API: "openai-completions", ContextWindow: 2_000_000, MaxTokens: 2_000_000, Cost: &Cost{Input: 9}},
		{Provider: "openrouter", ID: "blocked", API: "openai-completions"},
		{Provider: "openrouter", ID: "image", Type: "image", API: "openrouter-images"},
		{Provider: "openrouter", ID: "classifier", Type: "classifier", API: "typesafe-system-one"},
	}
	entries := []openRouterAPIModel{
		{ID: "listed", ContextLength: &million, TopProvider: &openRouterTopProvider{ContextLength: &topCtx, MaxCompletionTokens: &topMax}, Pricing: &openRouterAPIPricing{Prompt: "0.000003", Completion: "0.000015"}},
		{ID: "no-limits", ContextLength: &zero, TopProvider: &openRouterTopProvider{ContextLength: &zero, MaxCompletionTokens: &zero}},
		{ID: "router", Pricing: &openRouterAPIPricing{Prompt: "-1", Completion: "-1"}},
		{ID: "only-on-key"},
	}
	got := personalizeOpenRouter(catalog, entries)
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	wantIDs := []string{"listed", "no-limits", "router", "image", "classifier"}
	if len(ids) != len(wantIDs) {
		t.Fatalf("ids = %v", ids)
	}
	for i := range wantIDs {
		if ids[i] != wantIDs[i] {
			t.Fatalf("ids = %v", ids)
		}
	}
	if got[0].ContextWindow != topCtx || got[0].MaxTokens != topMax || got[0].Cost == nil || got[0].Cost.Input != 3 || got[0].Cost.Output != 15 || got[0].Cost.CacheRead != 0 || got[0].Cost.CacheWrite != 0 {
		t.Fatalf("listed = %+v cost=%+v", got[0], got[0].Cost)
	}
	if got[1].ContextWindow != 200_000 || got[1].MaxTokens != 50_000 || got[1].Cost == nil || got[1].Cost.Input != 1 {
		t.Fatalf("no-limits = %+v cost=%+v", got[1], got[1].Cost)
	}
	if got[2].Cost == nil || got[2].Cost.Input != 0 || got[2].Cost.Output != 0 || got[2].Cost.CacheRead != 0 || got[2].Cost.CacheWrite != 0 {
		t.Fatalf("router cost = %+v", got[2].Cost)
	}
	if got[3].Type != "image" || got[4].Type != "classifier" {
		t.Fatalf("non-chat = %+v %+v", got[3], got[4])
	}
}

func TestOpenRouterPerMillionClampsNegative(t *testing.T) {
	if got := openRouterPerMillion("-1"); got != 0 {
		t.Fatalf("negative = %v", got)
	}
	if got := openRouterPerMillion("0.000003"); got != 3 {
		t.Fatalf("prompt = %v", got)
	}
	if got := openRouterPerMillion(""); got != 0 {
		t.Fatalf("empty = %v", got)
	}
	if got := openRouterPerMillion("nope"); got != 0 {
		t.Fatalf("invalid = %v", got)
	}
}

func TestPrepareCatalogUsesOpenRouterUserModels(t *testing.T) {
	orig, _ := LookupProvider("openrouter")
	t.Cleanup(func() {
		ClearOverlays()
		RegisterProvider(orig)
	})
	useOpenRouterKey(t, "sk-or-test")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/models/user" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-or-test" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		topCtx, topMax := 262_144, 32_768
		body := map[string]any{"data": []openRouterAPIModel{
			{ID: "stepfun/step-5-preview", TopProvider: &openRouterTopProvider{ContextLength: &topCtx, MaxCompletionTokens: &topMax}, Pricing: &openRouterAPIPricing{Prompt: "0.000003", Completion: "0.000015"}},
			{ID: "openrouter/bodybuilder", ContextLength: intPtr(0), TopProvider: &openRouterTopProvider{ContextLength: intPtr(0), MaxCompletionTokens: intPtr(0)}},
			{ID: "openrouter/auto", Pricing: &openRouterAPIPricing{Prompt: "-1", Completion: "-1"}},
			{ID: "remote-only", TopProvider: &openRouterTopProvider{ContextLength: intPtr(500)}, Pricing: &openRouterAPIPricing{Prompt: "0.000001", Completion: "0.000002"}},
			{ID: "only-on-key"},
		}}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(`{"providers":{"openrouter":{"baseUrl":"`+srv.URL+`","models":[{"id":"user-added","contextWindow":42}]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := OpenFileStore(filepath.Join(dir, "models-store.json"))
	if err := store.Write("openrouter", StoreEntry{Models: []Model{
		{Provider: "openrouter", ID: "remote-only", API: "openai-completions", ContextWindow: 111, Cost: &Cost{Input: 9}},
		{Provider: "openrouter", ID: "openrouter/free", API: "openai-completions", ContextWindow: 1},
	}}); err != nil {
		t.Fatal(err)
	}

	if err := PrepareCatalog(dir, "", false); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
	step, ok := Lookup("openrouter", "stepfun/step-5-preview")
	if !ok || step.ContextWindow != 262_144 || step.MaxTokens != 32_768 || step.Cost == nil || step.Cost.Input != 3 || step.Cost.Output != 15 {
		t.Fatalf("step = %+v cost=%+v ok=%v", step, step.Cost, ok)
	}
	body, ok := Lookup("openrouter", "openrouter/bodybuilder")
	if !ok || body.ContextWindow != 128_000 || body.MaxTokens != 128_000 {
		t.Fatalf("bodybuilder = %+v ok=%v", body, ok)
	}
	auto, ok := Lookup("openrouter", "openrouter/auto")
	if !ok || auto.Cost == nil || auto.Cost.Input != 0 || auto.Cost.Output != 0 {
		t.Fatalf("auto = %+v cost=%+v ok=%v", auto, auto.Cost, ok)
	}
	remote, ok := Lookup("openrouter", "remote-only")
	if !ok || remote.ContextWindow != 500 || remote.Cost == nil || remote.Cost.Input != 1 || remote.Cost.Output != 2 {
		t.Fatalf("remote-only = %+v cost=%+v ok=%v", remote, remote.Cost, ok)
	}
	if _, ok := Lookup("openrouter", "openrouter/free"); ok {
		t.Fatal("openrouter/free should be dropped")
	}
	if _, ok := Lookup("openrouter", "only-on-key"); ok {
		t.Fatal("key-only id should stay out of the snapshot")
	}
	if _, ok := ModelOfType("classifier", "openrouter", "typesafe/jev-1.13"); !ok {
		t.Fatal("classifier dropped")
	}
	user, ok := Lookup("openrouter", "user-added")
	if !ok || user.ContextWindow != 42 {
		t.Fatalf("user-added = %+v ok=%v", user, ok)
	}

	saved, ok, err := store.Read(openRouterUserStoreID)
	if err != nil || !ok {
		t.Fatalf("store ok=%v err=%v", ok, err)
	}
	for _, m := range saved.Models {
		if m.ID == "openrouter/free" || m.ID == "only-on-key" {
			t.Fatalf("saved %s", m.ID)
		}
	}

	if err := PrepareCatalog(dir, "", true); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("offline hits = %d", hits.Load())
	}
	step, ok = Lookup("openrouter", "stepfun/step-5-preview")
	if !ok || step.ContextWindow != 262_144 {
		t.Fatalf("offline step = %+v ok=%v", step, ok)
	}
	if _, ok := Lookup("openrouter", "openrouter/free"); ok {
		t.Fatal("offline restored a chat model the key cannot use")
	}
}

func TestPrepareCatalogOpenRouterNoKeyKeepsSnapshot(t *testing.T) {
	t.Cleanup(ClearOverlays)
	useOpenRouterKey(t, "")
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("request without a key: %s", r.URL.Path)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(`{"providers":{"openrouter":{"baseUrl":"`+srv.URL+`"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	orig, _ := LookupProvider("openrouter")
	t.Cleanup(func() { RegisterProvider(orig) })
	if err := PrepareCatalog(dir, "", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup("openrouter", "openrouter/free"); !ok {
		t.Fatal("public chat model missing")
	}
}

func TestPrepareCatalogOpenRouterUnauthorizedKeepsSnapshotAndCache(t *testing.T) {
	orig, _ := LookupProvider("openrouter")
	t.Cleanup(func() {
		ClearOverlays()
		RegisterProvider(orig)
	})
	useOpenRouterKey(t, "sk-or-test")
	var mode atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if mode.Load() == 0 {
			top := 111
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []openRouterAPIModel{
				{ID: "stepfun/step-5-preview", TopProvider: &openRouterTopProvider{ContextLength: &top}},
			}})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(`{"providers":{"openrouter":{"baseUrl":"`+srv.URL+`"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PrepareCatalog(dir, "", false); err != nil {
		t.Fatal(err)
	}
	store := OpenFileStore(filepath.Join(dir, "models-store.json"))
	saved, ok, err := store.Read(openRouterUserStoreID)
	if err != nil || !ok || saved.CheckedAt == 0 {
		t.Fatalf("saved ok=%v err=%v entry=%+v", ok, err, saved)
	}
	checked := saved.CheckedAt
	mode.Store(1)
	if err := PrepareCatalog(dir, "", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup("openrouter", "openrouter/free"); !ok {
		t.Fatal("401 should keep the public snapshot")
	}
	again, ok, err := store.Read(openRouterUserStoreID)
	if err != nil || !ok || again.CheckedAt != checked {
		t.Fatalf("cache rewritten: ok=%v err=%v checked %d -> %d", ok, err, checked, again.CheckedAt)
	}
	for _, m := range again.Models {
		if m.ID == "openrouter/free" {
			t.Fatal("401 wrote the public list over the cache")
		}
	}
}

func useOpenRouterKey(t *testing.T, key string) {
	t.Helper()
	overlayMu.Lock()
	prev := openRouterKey
	openRouterKey = func(string) string { return key }
	overlayMu.Unlock()
	t.Cleanup(func() {
		overlayMu.Lock()
		openRouterKey = prev
		overlayMu.Unlock()
		setOpenRouterUserCatalog(nil, false)
	})
}

func intPtr(n int) *int { return &n }
