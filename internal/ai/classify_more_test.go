package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestClassifyLlamaReadsNextTokenLabels(t *testing.T) {
	var completionDepths []float64
	var prompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Header.Get("Authorization") != "Bearer local" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/tokenize":
			content, _ := body["content"].(string)
			tokens := map[string][]int{
				"\n":    {1},
				"\nYes": {1, 2},
				"\nNo":  {1, 3},
			}[content]
			if tokens == nil {
				t.Errorf("tokenize %q", content)
				tokens = []int{9, 9}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tokens": tokens})
		case "/apply-template":
			msgs, _ := body["messages"].([]any)
			user, _ := msgs[1].(map[string]any)
			prompt, _ = user["content"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"prompt": prompt + "<think>"})
		case "/completion":
			completionDepths = append(completionDepths, body["n_probs"].(float64))
			sent, _ := body["prompt"].(string)
			if !strings.HasSuffix(sent, "</think>") {
				t.Errorf("prompt = %q", sent)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"completion_probabilities": []any{map[string]any{
					"top_logprobs": []any{
						map[string]any{"id": 2, "logprob": 0},
						map[string]any{"id": 3, "logprob": 0},
					},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	model := models.Model{
		Provider: "llama.cpp", ID: "qwen", Type: "classifier", API: "llama-cpp-classify",
		BaseURL: srv.URL + "/v1", Input: []string{"text"},
	}
	res := Classify(t.Context(), model, ClassifyContext{
		State: map[string]any{"text": "ok"},
		Questions: map[string]any{"approved": map[string]any{
			"type": "bool", "instructions": "approve?",
			"criteria": map[string]any{"true": "yes", "false": "no"},
		}},
	}, ClientConfig{APIKey: "local"})
	if res.StopReason != "stop" {
		t.Fatalf("stop=%s err=%s", res.StopReason, res.ErrorMessage)
	}
	if res.Usage != nil {
		t.Fatalf("local classifier reported usage %+v", res.Usage)
	}
	approved := res.Answers["approved"].(map[string]any)
	if approved["probability"] != 0.5 {
		t.Fatalf("answer = %#v", approved)
	}
	if len(completionDepths) != 1 || completionDepths[0] != 256 {
		t.Fatalf("depths = %v", completionDepths)
	}
	if !strings.Contains(prompt, "Answer Yes or No.") || !strings.Contains(prompt, "Yes means: yes") {
		t.Fatalf("prompt = %s", prompt)
	}
}

func TestClassifyCloudflareRun(t *testing.T) {
	var path string
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"state": "Completed",
				"result": map[string]any{
					"answers": map[string]any{"approved": map[string]any{"type": "noul", "noul": 0.8}},
					"usage":   map[string]any{"input_tokens": 10, "output_tokens": 1},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	model := models.Model{
		Provider: "cloudflare-workers-ai", ID: "typesafe/jev", Type: "classifier",
		API:     "cloudflare-workers-ai-system-one",
		BaseURL: srv.URL + "/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai",
		Cost:    &models.Cost{Input: 1, Output: 1}, Input: []string{"text"},
	}
	res := Classify(t.Context(), model, ClassifyContext{
		State: map[string]any{"text": "hi"},
		Questions: map[string]any{"approved": map[string]any{
			"type": "bool", "instructions": "yes?",
		}},
	}, ClientConfig{APIKey: "cf"})
	if res.StopReason != "stop" || path != "/accounts/acct/ai/run" {
		t.Fatalf("stop=%s path=%s err=%s", res.StopReason, path, res.ErrorMessage)
	}
	if payload["model"] != "typesafe/jev" {
		t.Fatalf("payload = %#v", payload)
	}
	input := payload["input"].(map[string]any)
	qs := input["questions"].(map[string]any)
	if qs["approved"].(map[string]any)["type"] != "noul" {
		t.Fatalf("questions = %#v", qs)
	}
	if res.Usage == nil || res.Usage.Input != 10 || res.Usage.Cost.Total == 0 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}

func TestClassifyCloudflareDirectAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"answers": map[string]any{"approved": map[string]any{"type": "noul", "noul": 0.2}},
				"usage":   map[string]any{"input_tokens": 4, "output_tokens": 0},
			},
		})
	}))
	t.Cleanup(srv.Close)
	model := models.Model{
		Provider: "cloudflare-workers-ai", ID: "@cf/cloudflare/clef", Type: "classifier",
		API: "cloudflare-workers-ai-system-one", BaseURL: srv.URL + "/ai",
		Cost: &models.Cost{Input: 0.24}, Input: []string{"text"},
	}
	res := Classify(t.Context(), model, ClassifyContext{
		State:     map[string]any{"text": "x"},
		Questions: map[string]any{"approved": map[string]any{"type": "bool", "instructions": "yes?"}},
	}, ClientConfig{APIKey: "cf"})
	if res.StopReason != "stop" || res.Usage == nil || res.Usage.Cost.Input == 0 {
		t.Fatalf("%+v", res)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"errors":  []any{map[string]any{"message": "No such model"}},
		})
	}))
	t.Cleanup(bad.Close)
	model.BaseURL = bad.URL
	failed := Classify(t.Context(), model, ClassifyContext{
		State:     map[string]any{"text": "x"},
		Questions: map[string]any{"approved": map[string]any{"type": "bool", "instructions": "yes?"}},
	}, ClientConfig{APIKey: "cf"})
	if failed.StopReason != "error" || !strings.Contains(failed.ErrorMessage, "No such model") {
		t.Fatalf("%+v", failed)
	}
}
