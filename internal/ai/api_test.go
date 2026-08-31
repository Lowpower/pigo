package ai

import (
	"context"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestStreamForDispatchesByModelAPI(t *testing.T) {
	RegisterAPI("dispatch-test-api", func(_ ClientConfig) StreamFn {
		return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
			return ScriptedStreamFn("from-custom-api", 0)(ctx, reqCtx, opts)
		}
	})
	models.RegisterProvider(models.ProviderSpec{
		ID:         "dispatch-test",
		DefaultAPI: "missing-api",
		DefaultID:  "m",
		Models: []models.Model{
			{Provider: "dispatch-test", ID: "m", API: "dispatch-test-api"},
		},
	})
	stream, err := StreamFor("dispatch-test", ClientConfig{APIKey: "k"})(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "from-custom-api" {
		t.Fatalf("got %+v", final)
	}
}

func TestStreamForSetsThinkingBudget(t *testing.T) {
	t.Cleanup(func() { models.SetThinkingBudgets(nil) })
	models.SetThinkingBudgets(map[string]int{"high": 77})
	var got int
	RegisterAPI("budget-api", func(_ ClientConfig) StreamFn {
		return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
			got = opts.ThinkingBudget
			return ScriptedStreamFn("ok", 0)(ctx, reqCtx, opts)
		}
	})
	models.RegisterProvider(models.ProviderSpec{
		ID: "budget-prov", DefaultAPI: "budget-api", DefaultID: "m",
		Models: []models.Model{{Provider: "budget-prov", ID: "m", API: "budget-api"}},
	})
	stream, err := StreamFor("budget-prov", ClientConfig{})(context.Background(), Context{}, Options{Model: "m", Thinking: "high"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if got != 77 {
		t.Fatalf("budget = %d", got)
	}
}

func TestStreamForMergesProviderHeaders(t *testing.T) {
	var got map[string]string
	RegisterAPI("hdr-api", func(cfg ClientConfig) StreamFn {
		return func(ctx context.Context, reqCtx Context, opts Options) (*EventStream, error) {
			got = cfg.Headers
			return ScriptedStreamFn("ok", 0)(ctx, reqCtx, opts)
		}
	})
	models.RegisterProvider(models.ProviderSpec{
		ID:         "hdr-prov",
		DefaultAPI: "hdr-api",
		DefaultID:  "m",
		Headers:    map[string]string{"NVCF-POLL-SECONDS": "3600", "X-Default": "a"},
		Models:     []models.Model{{Provider: "hdr-prov", ID: "m", API: "hdr-api"}},
	})
	stream, err := StreamFor("hdr-prov", ClientConfig{Headers: map[string]string{"X-Override": "b"}})(context.Background(), Context{}, Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if got["NVCF-POLL-SECONDS"] != "3600" || got["X-Default"] != "a" || got["X-Override"] != "b" {
		t.Fatalf("headers = %#v", got)
	}
}

func TestStreamForUnknownAPIErrors(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID:         "no-api-prov",
		DefaultAPI: "does-not-exist",
		DefaultID:  "x",
		Models:     []models.Model{{Provider: "no-api-prov", ID: "x", API: "does-not-exist"}},
	})
	stream, err := StreamFor("no-api-prov", ClientConfig{})(context.Background(), Context{}, Options{Model: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopError {
		t.Fatalf("want error stream, got %+v", final)
	}
}
