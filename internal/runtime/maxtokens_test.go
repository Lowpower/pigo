package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/cachewarm"
	"github.com/Lowpower/pigo/internal/compaction"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
)

func TestClampMaxTokensToContext(t *testing.T) {
	// pi #10497: window 10000, usage 2000, 3500 new characters.
	// ceil(3500/3.5) = 1000, so the estimate is 3000 and the cap is 2904.
	// chars/4 leaves 875 trailing tokens and a cap of 3029.
	const window = 10000
	const estimated = 3000
	const outputCap = 8000
	got := clampMaxTokensToContext(window, estimated, outputCap)
	if got != 2904 {
		t.Fatalf("clamp = %d, want 2904", got)
	}
	byFour := window - (2000 + (3500+3)/4) - contextSafetyTokens
	if got >= byFour {
		t.Fatalf("3.5 clamp %d is not smaller than chars/4 clamp %d", got, byFour)
	}
	if got > window-estimated {
		t.Fatalf("clamp %d exceeds window-estimated %d", got, window-estimated)
	}

	if got := clampMaxTokensToContext(window, window, outputCap); got != 1 {
		t.Fatalf("overshoot = %d, want 1", got)
	}
	if got := clampMaxTokensToContext(window, estimated, 100); got != 100 {
		t.Fatalf("caller cap = %d, want 100", got)
	}
	if got := clampMaxTokensToContext(window, estimated, 0); got != 0 {
		t.Fatalf("unset cap = %d, want 0", got)
	}
	if got := clampMaxTokensToContext(0, estimated, 500); got != 500 {
		t.Fatalf("unknown window = %d, want 500", got)
	}
}

func TestGatedStreamClampsMaxTokensOnFakeServer(t *testing.T) {
	const window = 10000
	models.RegisterProvider(models.ProviderSpec{
		ID: "est-cap", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "est-cap", ID: "m", API: "openai-completions",
			ContextWindow: window, MaxTokens: 8000,
			Compat: &models.Compat{MaxTokensField: "max_tokens"},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("est-cap") })

	var mu sync.Mutex
	var payloads []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(b, &payload)
		mu.Lock()
		payloads = append(payloads, payload)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n"))
	}))
	t.Cleanup(srv.Close)

	req := ai.Context{Messages: []ai.Message{
		usageMessage(ai.Usage{TotalTokens: 2000}, strings.Repeat("q", 400)),
		{Role: ai.RoleUser, Content: strings.Repeat("x", 3500)},
	}}
	e := &Engine{
		Provider: "est-cap",
		Opts:     Options{Config: config.Config{Provider: "est-cap", Model: "m"}},
	}
	fn := e.gatedStream((&ai.OpenAICompletionsClient{
		BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client(),
	}).StreamFn())
	stream, err := fn(context.Background(), req, ai.Options{Provider: "est-cap", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()

	stream, err = fn(cachewarm.WithRefresh(context.Background()), req, ai.Options{Provider: "est-cap", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()

	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 2 {
		t.Fatalf("requests = %d, want 2", len(payloads))
	}
	got, ok := payloads[0]["max_tokens"].(float64)
	if !ok {
		t.Fatalf("max_tokens = %#v", payloads[0]["max_tokens"])
	}
	estimated := compaction.ContextTokens(req.Messages, req.System, req.Tools)
	if estimated != 3000 {
		t.Fatalf("estimated = %d, want 3000 (usage, not the assistant text)", estimated)
	}
	if int(got) != 2904 {
		t.Fatalf("max_tokens = %v, want 2904", got)
	}
	if int(got) > window-estimated {
		t.Fatalf("max_tokens %v exceeds window-estimated %d", got, window-estimated)
	}
	byFour := window - (2000 + (3500+3)/4) - 4096
	if int(got) >= byFour {
		t.Fatalf("max_tokens %v is not smaller than the chars/4 cap %d", got, byFour)
	}
	if payloads[1]["max_tokens"] != float64(1) {
		t.Fatalf("refresh max_tokens = %#v, want 1", payloads[1]["max_tokens"])
	}
}

func TestGatedStreamLeavesMaxTokensUnsetWithoutCap(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "est-nocap", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "est-nocap", ID: "m", API: "openai-completions",
			ContextWindow: 10000,
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("est-nocap") })

	var got int
	e := &Engine{
		Provider: "est-nocap",
		Opts:     Options{Config: config.Config{Provider: "est-nocap", Model: "m"}},
	}
	fn := e.gatedStream(func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		got = opts.MaxTokens
		return ai.ScriptedStreamFn("ok", 0)(ctx, req, opts)
	})
	stream, err := fn(context.Background(), ai.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
	}, ai.Options{Provider: "est-nocap", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if got != 0 {
		t.Fatalf("MaxTokens = %d, want unset", got)
	}
}

func usageMessage(usage ai.Usage, text string) ai.Message {
	return ai.Message{
		Role: ai.RoleAssistant,
		Assistant: &ai.AssistantMessage{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopStop,
			Usage:      usage,
			Content:    []*ai.Content{{Type: ai.KindText, Text: text}},
		},
	}
}
