package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

func TestSummarizePassesSamplingParams(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-sum", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-sum", ID: "m", API: "openai-completions",
			SamplingParams: map[string]any{
				"temperature": 1,
				"top_k":       0,
				"min_p":       nil,
			},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-sum") })

	var got ai.Options
	sf := func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		got = opts
		return ai.ScriptedStreamFn("done summarizing", 0)(ctx, req, opts)
	}
	text, err := Summarize(context.Background(), sf, "m", []ai.Message{{Role: ai.RoleUser, Content: "hello"}}, "", "samp-sum", "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "done summarizing" {
		t.Fatalf("text = %q", text)
	}
	if got.Provider != "samp-sum" || got.Model != "m" {
		t.Fatalf("target = %s/%s", got.Provider, got.Model)
	}
	if got.SamplingParams["temperature"] != 1 || got.SamplingParams["top_k"] != 0 {
		t.Fatalf("params = %#v", got.SamplingParams)
	}
	if _, ok := got.SamplingParams["min_p"]; !ok {
		t.Fatal("null min_p dropped")
	}
}

func TestBranchSummaryPassesSamplingParams(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-branch", DefaultAPI: "openai-responses", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-branch", ID: "m", API: "openai-responses",
			SamplingParams: map[string]any{"top_p": 0.4, "vendor_flag": false},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-branch") })

	var got ai.Options
	sf := func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		got = opts
		return ai.ScriptedStreamFn("branch done", 0)(ctx, req, opts)
	}
	text, _, err := GenerateBranchSummary(context.Background(), sf, "m", []ai.Message{{Role: ai.RoleUser, Content: "hello"}}, BranchSummaryOpts{Provider: "samp-branch"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "branch done") {
		t.Fatalf("text = %q", text)
	}
	if got.Provider != "samp-branch" || got.SamplingParams["top_p"] != 0.4 || got.SamplingParams["vendor_flag"] != false {
		t.Fatalf("opts = %+v params=%#v", got, got.SamplingParams)
	}
}
