package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefreshProviderParsesKeyedCatalogAnd304(t *testing.T) {
	t.Cleanup(ClearOverlays)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/api/models/providers/openai" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if hits == 1 {
			w.Header().Set("etag", `"catalog-1"`)
			_ = json.NewEncoder(w).Encode(map[string]Model{
				"extra": {ID: "extra", API: "openai-responses"},
			})
			return
		}
		if r.Header.Get("if-none-match") != `"catalog-1"` {
			t.Errorf("missing if-none-match, got %q", r.Header.Get("if-none-match"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	store := &MemoryStore{}
	if err := RefreshProvider(context.Background(), store, srv.URL, "openai", true); err != nil {
		t.Fatal(err)
	}
	m, ok := Lookup("openai", "extra")
	if !ok || m.API != "openai-responses" {
		t.Fatalf("overlay extra = %+v ok=%v", m, ok)
	}
	e, ok, err := store.Read("openai")
	if err != nil || !ok || e.ETag != `"catalog-1"` {
		t.Fatalf("store = %+v ok=%v err=%v", e, ok, err)
	}

	if err := RefreshProvider(context.Background(), store, srv.URL, "openai", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup("openai", "extra"); !ok {
		t.Fatal("304 dropped overlay")
	}
	if hits != 2 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestRefreshProvider404ClearsOverlay(t *testing.T) {
	t.Cleanup(ClearOverlays)
	SetRemoteOverlay("openai", []Model{{Provider: "openai", ID: "gone", API: "openai-responses"}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	store := &MemoryStore{}
	if err := RefreshProvider(context.Background(), store, srv.URL, "openai", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup("openai", "gone"); ok {
		t.Fatal("404 should drop overlay")
	}
	e, ok, _ := store.Read("openai")
	if !ok || e.ETag != "" || len(e.Models) != 0 {
		t.Fatalf("store after 404 = %+v ok=%v", e, ok)
	}
}

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

func TestRefreshAllSkipsNetworkWhenOfflinePrepare(t *testing.T) {
	t.Cleanup(ClearOverlays)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := PrepareCatalog(dir, srv.URL, true); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatalf("offline still fetched %d times", hits)
	}
}
