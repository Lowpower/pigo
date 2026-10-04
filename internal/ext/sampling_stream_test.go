package ext

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

func TestStreamStartOptionsSamplingParams(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-ext", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-ext", ID: "m", API: "openai-completions",
			SamplingParams: map[string]any{"temperature": 1, "top_k": 0, "min_p": nil},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-ext") })

	got := streamStartOptions("samp-ext", ai.Options{
		Model:          "m",
		SamplingParams: map[string]any{"temperature": 0},
	})
	sp, _ := got["samplingParams"].(map[string]any)
	if sp["temperature"] != 0 || sp["top_k"] != 0 {
		t.Fatalf("options = %#v", got["samplingParams"])
	}
	if _, ok := sp["min_p"]; !ok {
		t.Fatal("null min_p dropped")
	}

	plain := streamStartOptions("samp-ext", ai.Options{Model: "missing"})
	if _, ok := plain["samplingParams"]; ok {
		t.Fatalf("empty samplingParams sent: %#v", plain["samplingParams"])
	}
}

func TestSamplingStreamHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_EXT_HELPER") != "sampling" {
		return
	}
	_ = Serve(Handler{
		Name: "sampling-ext",
		OnStream: func(req map[string]any, emit func(event string, payload map[string]any), _ <-chan struct{}) {
			raw, _ := json.Marshal(req["options"])
			text := string(raw)
			emit("start", map[string]any{})
			emit("text_start", map[string]any{"contentIndex": 0.0})
			emit("text_delta", map[string]any{"contentIndex": 0.0, "delta": text})
			emit("text_end", map[string]any{"contentIndex": 0.0, "content": text})
			emit("done", map[string]any{
				"message": map[string]any{
					"role": "assistant", "stopReason": "stop",
					"content": []any{map[string]any{"type": "text", "text": text}},
				},
			})
		},
	})
	os.Exit(0)
}

func TestHostStreamSendsSamplingParams(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "samp-wire", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{{
			Provider: "samp-wire", ID: "m", API: "openai-completions",
			SamplingParams: map[string]any{"temperature": 1, "top_p": 0.2, "top_k": 0},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("samp-wire") })

	h, err := Spawn(context.Background(), "sampling-ext",
		[]string{os.Args[0], "-test.run=^TestSamplingStreamHelperProcess$"},
		Options{Env: []string{"PIGO_EXT_HELPER=sampling"}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = h.Close() }()

	es, err := h.Stream("samp-wire")(context.Background(), ai.Context{}, ai.Options{
		Model:          "m",
		SamplingParams: map[string]any{"temperature": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, msg := es.Collect()
	if msg == nil {
		t.Fatal("no message")
	}
	var opts map[string]any
	if err := json.Unmarshal([]byte(msg.Text()), &opts); err != nil {
		t.Fatalf("options text %q: %v", msg.Text(), err)
	}
	sp, _ := opts["samplingParams"].(map[string]any)
	if sp["temperature"] != float64(0) || sp["top_p"] != 0.2 || sp["top_k"] != float64(0) {
		t.Fatalf("wire samplingParams = %#v", sp)
	}
}
