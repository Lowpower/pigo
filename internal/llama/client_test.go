package llama

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeServerURL(t *testing.T) {
	got, err := NormalizeServerURL("http://127.0.0.1:8080/v1/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:8080" {
		t.Fatalf("got %q", got)
	}
	if InferenceURL(got) != "http://127.0.0.1:8080/v1" {
		t.Fatalf("inference %q", InferenceURL(got))
	}
}

func TestClientListLoadUnload(t *testing.T) {
	var loaded string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "qwen", "status": map[string]any{"value": "unloaded"}, "source": "preset"},
					{"id": "loaded-model", "status": map[string]any{"value": "loaded"}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/props":
			_ = json.NewEncoder(w).Encode(map[string]any{"models_autoload": true})
		case r.Method == http.MethodPost && r.URL.Path == "/models/load":
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			loaded = body.Model
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/models/unload":
			loaded = ""
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	models, err := c.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "qwen" {
		t.Fatalf("models=%+v", models)
	}
	props, err := c.Props()
	if err != nil || !props.ModelsAutoload {
		t.Fatalf("props=%+v err=%v", props, err)
	}
	if err := c.Load("qwen"); err != nil {
		t.Fatal(err)
	}
	if loaded != "qwen" {
		t.Fatalf("loaded=%q", loaded)
	}
	if err := c.Unload("qwen"); err != nil {
		t.Fatal(err)
	}
	if loaded != "" {
		t.Fatalf("still loaded %q", loaded)
	}
	text := FormatCatalog(models, true)
	if !strings.Contains(text, "qwen") || !strings.Contains(text, "loaded-model") {
		t.Fatalf("catalog:\n%s", text)
	}
}
