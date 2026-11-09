package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestClassifyDecisionsMapsQuestionsAndPricesUsage(t *testing.T) {
	var got map[string]any
	var path, authz string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authz = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": []any{
				map[string]any{"type": "predicate", "name": "approved", "probability": 0.95},
				map[string]any{
					"type": "choice", "name": "category", "choice": "success", "confidence": 0.8,
					"probabilities": []any{
						map[string]any{"value": "success", "probability": 0.9},
						map[string]any{"value": "failure", "probability": 0.1},
					},
				},
			},
			"usage": map[string]any{"input_tokens": 164, "output_tokens": 0},
		})
	}))
	t.Cleanup(srv.Close)

	model := models.Model{
		Provider: "openai", ID: "gpt-6-luna", Type: "classifier", API: "openai-decisions",
		BaseURL: srv.URL + "/v1", Input: []string{"text", "image"},
		Cost: &models.Cost{Input: 0.1, Tiers: []models.CostTier{{InputTokensAbove: 272000, Input: 0.2}}},
	}
	res := Classify(t.Context(), model, ClassifyContext{
		State: map[string]any{"text": "thanks"},
		Questions: map[string]any{
			"category": map[string]any{
				"type": "choice", "instructions": "Classify the message",
				"criteria": map[string]any{"success": "Successful", "failure": ""},
			},
			"approved": map[string]any{
				"type": "bool", "instructions": "Does the user approve?",
				"criteria": map[string]any{"true": "Approval", "false": "No approval"},
			},
		},
	}, ClientConfig{APIKey: "secret"})
	if res.StopReason != "stop" {
		t.Fatalf("stop = %q err=%s", res.StopReason, res.ErrorMessage)
	}
	if path != "/v1/decisions" || authz != "Bearer secret" {
		t.Fatalf("path=%s auth=%s", path, authz)
	}
	if got["model"] != "gpt-6-luna" || got["input"] != `{"text":"thanks"}` {
		t.Fatalf("body = %#v", got)
	}
	qs, _ := got["questions"].([]any)
	if len(qs) != 2 {
		t.Fatalf("questions = %#v", got["questions"])
	}
	byName := map[string]map[string]any{}
	for _, q := range qs {
		m := q.(map[string]any)
		byName[m["name"].(string)] = m
	}
	if byName["approved"]["type"] != "predicate" {
		t.Fatalf("predicate = %#v", byName["approved"])
	}
	if byName["approved"]["instructions"] != "Does the user approve?\n\nTrue means: Approval\nFalse means: No approval" {
		t.Fatalf("instructions = %#v", byName["approved"]["instructions"])
	}
	choices := byName["category"]["choices"].([]any)
	if len(choices) != 2 {
		t.Fatalf("choices = %#v", choices)
	}
	approved, _ := res.Answers["approved"].(map[string]any)
	if approved["type"] != "bool" || approved["probability"] != 0.95 {
		t.Fatalf("approved = %#v", res.Answers["approved"])
	}
	if res.Usage == nil || res.Usage.Input != 164 || res.Usage.Cost.Total < 0.0000163 || res.Usage.Cost.Total > 0.0000165 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}

func TestClassifyDecisionsKeepsUsageWhenAnswerIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": []any{map[string]any{"type": "refusal", "name": "approved"}},
			"usage":   map[string]any{"input_tokens": 10, "output_tokens": 0},
		})
	}))
	t.Cleanup(srv.Close)
	model := models.Model{
		Provider: "openai", ID: "gpt-6-luna", Type: "classifier", API: "openai-decisions",
		BaseURL: srv.URL + "/v1", Cost: &models.Cost{Input: 1},
	}
	res := Classify(t.Context(), model, ClassifyContext{
		State:     map[string]any{"text": "x"},
		Questions: map[string]any{"approved": map[string]any{"type": "bool", "instructions": "yes?"}},
	}, ClientConfig{APIKey: "secret"})
	if res.StopReason != "error" || res.Usage == nil || res.Usage.Input != 10 || res.Usage.Cost.Total == 0 {
		t.Fatalf("%+v", res)
	}
	if len(res.Answers) != 0 {
		t.Fatalf("answers = %#v", res.Answers)
	}
}

func TestClassifySystemOne(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{
				"approved": map[string]any{"type": "noul", "noul": 0.8},
			},
			"usage": map[string]any{"input_tokens": 20, "output_tokens": 4},
		})
	}))
	t.Cleanup(srv.Close)
	model := models.Model{
		Provider: "llama.cpp", ID: "julia", Type: "classifier", API: "typesafe-system-one",
		BaseURL: srv.URL + "/v1", Input: []string{"text"},
		Cost: &models.Cost{Input: 2, Output: 3},
	}
	res := Classify(t.Context(), model, ClassifyContext{
		State: map[string]any{"text": "looks good"},
		Questions: map[string]any{
			"approved": map[string]any{
				"type": "bool", "instructions": "approve?",
				"criteria": map[string]any{"true": "yes", "false": "no"},
			},
		},
	}, ClientConfig{APIKey: "local"})
	if res.StopReason != "stop" || path != "/v1/systemone" {
		t.Fatalf("stop=%s path=%s err=%s", res.StopReason, path, res.ErrorMessage)
	}
	qs := got["questions"].(map[string]any)
	approved := qs["approved"].(map[string]any)
	if approved["type"] != "noul" || got["model"] != "julia" {
		t.Fatalf("body = %#v", got)
	}
	ans := res.Answers["approved"].(map[string]any)
	if ans["probability"] != 0.8 {
		t.Fatalf("answer = %#v", ans)
	}
	if res.Usage == nil || res.Usage.Input != 20 || res.Usage.Output != 4 || res.Usage.Cost.Total == 0 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}
