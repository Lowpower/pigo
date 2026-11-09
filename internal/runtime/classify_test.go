package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/session"
)

func TestHostClassifyRecordsSessionCost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/decisions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"answers": []any{map[string]any{"type": "predicate", "name": "approved", "probability": 0.9}},
				"usage":   map[string]any{"input_tokens": 1000, "output_tokens": 0},
			})
		case "/v1/systemone":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"answers": map[string]any{"approved": map[string]any{"type": "noul", "noul": 0.4}},
				"usage":   map[string]any{"input_tokens": 1000, "output_tokens": 0},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	const provider = "clf-host"
	models.RegisterProvider(models.ProviderSpec{
		ID: provider, DefaultAPI: "openai-completions", DefaultID: "chatty",
		Models: []models.Model{
			{Provider: provider, ID: "chatty", API: "openai-completions"},
			{
				Provider: provider, ID: "luna", Type: "classifier", API: "openai-decisions",
				BaseURL: srv.URL + "/v1", Cost: &models.Cost{Input: 1}, Input: []string{"text"},
			},
			{
				Provider: provider, ID: "julia", Type: "classifier", API: "typesafe-system-one",
				BaseURL: srv.URL + "/v1", Cost: &models.Cost{Input: 1}, Input: []string{"text"},
			},
		},
	})
	auth.RegisterProvider(auth.Provider{
		ID:     provider,
		APIKey: &auth.APIKeyHandler{Name: provider},
	})
	t.Cleanup(func() {
		models.UnregisterProvider(provider)
		auth.UnregisterProvider(provider)
		auth.ClearProcessAPIKey(provider)
	})
	auth.SetProcessAPIKey(provider, "secret")

	dir := t.TempDir()
	e := &Engine{Opts: Options{AgentDir: dir, Session: session.New(dir, dir)}}
	questions := map[string]any{"approved": map[string]any{"type": "bool", "instructions": "approve?"}}
	state := map[string]any{"text": "ok"}

	chat := e.HandleHostCall(nil, "model.getAll", nil)
	if hostHasModel(chat["models"], "luna") || hostHasModel(chat["models"], "julia") {
		t.Fatalf("chat list = %#v", chat["models"])
	}
	all := e.HandleHostCall(nil, "model.getAllModels", nil)
	if !hostHasModel(all["models"], "luna") || !hostHasModel(all["models"], "julia") || !hostHasModel(all["models"], "chatty") {
		t.Fatalf("all models = %#v", all["models"])
	}
	one := e.HandleHostCall(nil, "model.getModelOfType", map[string]any{
		"type": "classifier", "provider": provider, "id": "julia",
	})
	model, _ := one["model"].(map[string]any)
	if model["api"] != "typesafe-system-one" {
		t.Fatalf("model = %#v", one)
	}

	before := session.CollectStats(e.Opts.Session, nil, 0, "", nil).Cost
	decisions := e.HandleHostCall(nil, "model.classify", map[string]any{
		"provider": provider, "id": "luna", "state": state, "questions": questions,
	})
	if decisions["stopReason"] != "stop" {
		t.Fatalf("decisions = %#v", decisions)
	}
	mid := session.CollectStats(e.Opts.Session, nil, 0, "", nil).Cost
	if mid <= before {
		t.Fatalf("cost after decisions = %v, before %v", mid, before)
	}
	systemone := e.HandleHostCall(nil, "model.classify", map[string]any{
		"provider": provider, "id": "julia", "state": state, "questions": questions,
	})
	if systemone["stopReason"] != "stop" {
		t.Fatalf("systemone = %#v", systemone)
	}
	after := session.CollectStats(e.Opts.Session, nil, 0, "", nil).Cost
	if after <= mid {
		t.Fatalf("cost after systemone = %v, mid %v", after, mid)
	}
}

func TestClassifyKeepsBilledUsageOnBadAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": []any{map[string]any{"type": "refusal", "name": "approved"}},
			"usage":   map[string]any{"input_tokens": 1000, "output_tokens": 0},
		})
	}))
	t.Cleanup(srv.Close)
	const provider = "clf-bill"
	models.RegisterProvider(models.ProviderSpec{
		ID: provider,
		Models: []models.Model{{
			Provider: provider, ID: "luna", Type: "classifier", API: "openai-decisions",
			BaseURL: srv.URL + "/v1", Cost: &models.Cost{Input: 1},
		}},
	})
	auth.RegisterProvider(auth.Provider{ID: provider, APIKey: &auth.APIKeyHandler{Name: provider}})
	t.Cleanup(func() {
		models.UnregisterProvider(provider)
		auth.UnregisterProvider(provider)
		auth.ClearProcessAPIKey(provider)
	})
	auth.SetProcessAPIKey(provider, "secret")
	dir := t.TempDir()
	e := &Engine{Opts: Options{AgentDir: dir, Session: session.New(dir, dir)}}
	got := e.HandleHostCall(nil, "model.classify", map[string]any{
		"provider": provider, "id": "luna",
		"state":     map[string]any{"text": "x"},
		"questions": map[string]any{"approved": map[string]any{"type": "bool", "instructions": "yes?"}},
	})
	if got["stopReason"] != "error" {
		t.Fatalf("result = %#v", got)
	}
	if session.CollectStats(e.Opts.Session, nil, 0, "", nil).Cost == 0 {
		t.Fatal("billed error did not increase session cost")
	}
}

func TestClassifyOpenAIOAuthDoesNotCallDecisions(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	models.SetRemoteOverlay("openai", []models.Model{{
		ID: "gpt-6-luna", Type: "classifier", API: "openai-decisions", BaseURL: srv.URL + "/v1",
	}})
	t.Cleanup(models.ClearOverlays)

	dir := t.TempDir()
	if _, err := auth.Open(dir).Modify("openai", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: auth.TypeOAuth, Access: "sub-token"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Opts: Options{AgentDir: dir, Session: session.New(dir, dir)}}
	got := e.HandleHostCall(nil, "model.classify", map[string]any{
		"provider": "openai", "id": "gpt-6-luna",
		"state":     map[string]any{"text": "hi"},
		"questions": map[string]any{"approved": map[string]any{"type": "bool", "instructions": "yes?"}},
	})
	if got["stopReason"] != "error" || hits != 0 {
		t.Fatalf("result=%#v hits=%d", got, hits)
	}
	if session.CollectStats(e.Opts.Session, nil, 0, "", nil).Cost != 0 {
		t.Fatal("oauth refusal recorded a cost")
	}
}

func TestAvailableClassifiersHideOpenAIOAuth(t *testing.T) {
	dir := t.TempDir()
	if _, err := auth.Open(dir).Modify("openai", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: auth.TypeOAuth, Access: "sub-token"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Opts: Options{AgentDir: dir}}
	hidden := e.HandleHostCall(nil, "model.getAvailableOfType", map[string]any{
		"type": "classifier", "provider": "openai",
	})
	if hostHasModel(hidden["models"], "gpt-6-luna") {
		t.Fatalf("oauth listed classifier: %#v", hidden["models"])
	}
	auth.SetProcessAPIKey("openai", "sk-runtime")
	t.Cleanup(func() { auth.ClearProcessAPIKey("openai") })
	shown := e.HandleHostCall(nil, "model.getAvailableOfType", map[string]any{
		"type": "classifier", "provider": "openai",
	})
	if !hostHasModel(shown["models"], "gpt-6-luna") {
		t.Fatalf("runtime key hid classifier: %#v", shown["models"])
	}
}

func hostHasModel(raw any, id string) bool {
	list, _ := raw.([]map[string]any)
	if list == nil {
		if items, ok := raw.([]any); ok {
			for _, item := range items {
				m, _ := item.(map[string]any)
				if m["id"] == id {
					return true
				}
			}
		}
		return false
	}
	for _, m := range list {
		if m["id"] == id {
			return true
		}
	}
	return false
}
