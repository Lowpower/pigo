package runtime

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/models"
)

func TestVirtualRouteHelper(_ *testing.T) {
	if os.Getenv("PIGO_RUNTIME_EXT") != "virtual" {
		return
	}
	_ = ext.Serve(ext.Handler{
		Name: "virtual-route",
		Providers: []ext.ProviderDef{{
			ID:   "cap-virtual",
			Args: map[string]any{"name": "cap", "stream": true},
		}},
		VirtualModels: []ext.VirtualModelDef{{
			Provider:       "router",
			ID:             "auto",
			Name:           "Auto",
			ThinkingLevels: []string{"low", "high"},
			ContextWindow:  4096,
			MaxTokens:      128,
			Route:          helperRoute,
		}},
	})
}

func helperRoute(_ context.Context, req ext.RouteRequest) (ext.RouteResult, error) {
	for _, msg := range req.Messages {
		text, _ := msg["content"].(string)
		if strings.Contains(text, "boom") {
			return ext.RouteResult{Model: ext.RouteModel{Provider: "router", ID: "auto"}}, nil
		}
		if strings.Contains(text, "nocred") {
			return ext.RouteResult{Model: ext.RouteModel{Provider: "vmnone384", ID: "m"}, ThinkingLevel: "low"}, nil
		}
	}
	id := "small"
	switch req.Reason {
	case "continuation":
		id = "large"
		prevOK := req.Previous != nil && req.Previous.Model.ID == "small"
		phase, _ := req.State["phase"].(string)
		if prevOK && phase == "user" {
			id = "saw-user"
		}
	case "retry":
		id = "nofailed"
		if req.Failed != nil && req.Failed.Message != nil && req.Failed.Message["errorMessage"] == "overloaded" {
			id = "retry"
		}
	case "direct":
		id = "direct"
		if req.State != nil {
			id = "leak"
		}
	}
	level := "low"
	if req.Reason == "user" {
		level = "high"
	}
	return ext.RouteResult{
		Model:         ext.RouteModel{Provider: "vmphys384", ID: id},
		ThinkingLevel: level,
		State:         map[string]any{"phase": req.Reason},
	}, nil
}

func TestVirtualRouteByReason(t *testing.T) {
	low := "low"
	registerPhys(t, map[string]*string{
		"off": nil, "minimal": nil, "low": &low, "medium": nil, "high": nil, "xhigh": nil, "max": nil,
	})
	h := spawnVirtualExt(t)
	defer func() { _ = h.Close() }()

	var mu sync.Mutex
	var got []ai.Options
	e := newVirtualEngine(h, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		mu.Lock()
		got = append(got, opts)
		mu.Unlock()
		return textReply("ok")(ctx, req, opts)
	})

	ctx := context.Background()
	user := ai.Context{System: "sys", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}}}
	if _, err := e.Stream(ctx, user, ai.Options{Provider: "router", Model: "auto", Thinking: "high"}); err != nil {
		t.Fatalf("user route: %v", err)
	}
	cont := ai.Context{Messages: []ai.Message{
		{Role: ai.RoleUser, Content: "hi"},
		{Role: ai.RoleAssistant, Assistant: &ai.AssistantMessage{
			Role: ai.RoleAssistant, Provider: "vmphys384", Model: "small",
			ProviderThinkingLevel: "low", StopReason: ai.StopStop,
			Content: []*ai.Content{{Type: ai.KindText, Text: "ok"}},
		}},
		{Role: ai.RoleToolResult, Content: "done", ToolCallID: "1"},
	}}
	if _, err := e.Stream(ctx, cont, ai.Options{Provider: "router", Model: "auto", Thinking: "high"}); err != nil {
		t.Fatalf("continuation route: %v", err)
	}
	e.armRouteRetry(&ai.AssistantMessage{
		Role: ai.RoleAssistant, Provider: "vmphys384", Model: "small",
		ProviderThinkingLevel: "low", StopReason: ai.StopError, ErrorMessage: "overloaded",
	})
	if _, err := e.Stream(ctx, user, ai.Options{Provider: "router", Model: "auto", Thinking: "high"}); err != nil {
		t.Fatalf("retry route: %v", err)
	}
	if _, err := e.directStream()(ctx, user, ai.Options{Provider: "router", Model: "auto", Thinking: "high"}); err != nil {
		t.Fatalf("direct route: %v", err)
	}

	mu.Lock()
	gotCopy := append([]ai.Options(nil), got...)
	mu.Unlock()
	if len(gotCopy) != 4 {
		t.Fatalf("calls = %d, want 4", len(gotCopy))
	}
	assertOpt := func(i int, model, thinking string) {
		t.Helper()
		if gotCopy[i].Provider != "vmphys384" || gotCopy[i].Model != model || gotCopy[i].Thinking != thinking {
			t.Fatalf("call %d = %s/%s thinking %s", i, gotCopy[i].Provider, gotCopy[i].Model, gotCopy[i].Thinking)
		}
	}
	assertOpt(0, "small", "low")
	assertOpt(1, "saw-user", "low")
	assertOpt(2, "retry", "low")
	assertOpt(3, "direct", "low")

	events := e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if e.Provider != "router" || e.Opts.Config.ResolvedModel() != "auto" {
		t.Fatalf("selection = %s/%s", e.Provider, e.Opts.Config.ResolvedModel())
	}
	var assistant *ai.AssistantMessage
	for _, ev := range events {
		if ev.Type == "agent_end" || ev.Assistant != nil {
			if ev.Assistant != nil && ev.Assistant.StopReason != "" {
				assistant = ev.Assistant
			}
		}
	}
	if assistant == nil || assistant.StopReason == ai.StopError {
		t.Fatalf("prompt assistant = %#v", assistant)
	}
	if assistant.Provider != "vmphys384" || assistant.Model != "small" {
		t.Fatalf("dispatched assistant = %s/%s", assistant.Provider, assistant.Model)
	}
	if !hasModel(e.VisibleModels(), "router", "auto") {
		t.Fatal("virtual model missing from the model list")
	}
}

func TestVirtualRouteErrors(t *testing.T) {
	registerPhys(t, nil)
	models.RegisterProvider(models.ProviderSpec{
		ID: "vmnone384", DefaultAPI: "openai-responses",
		Models: []models.Model{{ID: "m", API: "openai-responses"}},
	})
	h := spawnVirtualExt(t)
	defer func() { _ = h.Close() }()
	e := newVirtualEngine(h, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		t.Errorf("physical stream called for %s/%s", opts.Provider, opts.Model)
		return textReply("no")(ctx, req, opts)
	})
	ctx := context.Background()
	opts := ai.Options{Provider: "router", Model: "auto", Thinking: "high"}
	if _, err := e.Stream(ctx, ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "boom"}}}, opts); err == nil || !strings.Contains(err.Error(), "virtual model") {
		t.Fatalf("virtual return = %v", err)
	}
	if _, err := e.Stream(ctx, ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: "nocred"}}}, opts); err == nil || !strings.Contains(err.Error(), "no credentials") {
		t.Fatalf("credentials = %v", err)
	}
	if e.Provider != "router" || e.Opts.Config.Model != "auto" {
		t.Fatalf("selection changed to %s/%s", e.Provider, e.Opts.Config.Model)
	}
	events := e.RunPrompt(context.Background(), nil, "boom", nil).Collect()
	var stop ai.StopReason
	for _, ev := range events {
		if ev.Assistant != nil {
			stop = ev.Assistant.StopReason
		}
	}
	if stop != ai.StopError {
		t.Fatalf("stop = %s", stop)
	}
	if e.Opts.Config.Model != "auto" {
		t.Fatalf("model after error = %s", e.Opts.Config.Model)
	}
}

func TestVirtualModelRegistry(t *testing.T) {
	h := spawnVirtualExt(t)
	defer func() { _ = h.Close() }()
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Config: config.Config{}}}
	e.Opts.Models = []string{"router/*"}
	e.installVirtual(h, ext.VirtualModelDef{Provider: "router", ID: "auto", Name: "Auto", ContextWindow: 10})
	e.installVirtual(h, ext.VirtualModelDef{Provider: "router", ID: "auto", Name: "Auto", ContextWindow: 22})
	if len(e.Scoped) != 1 || e.Scoped[0].Provider != "router" || e.Scoped[0].ID != "auto" {
		t.Fatalf("scoped = %+v", e.Scoped)
	}
	vm, ok := e.lookupVirtual("router", "auto")
	if !ok || vm.ContextWindow != 22 {
		t.Fatalf("replaced = %+v ok=%v", vm, ok)
	}
	e.dropProvider("cap-virtual")
	if _, ok := e.lookupVirtual("router", "auto"); !ok {
		t.Fatal("unregister_provider removed the virtual model")
	}

	models.RegisterProvider(models.ProviderSpec{
		ID: "vmcollide384", DefaultAPI: "openai-responses",
		Models: []models.Model{{ID: "auto", API: "openai-responses"}},
	})
	e.installVirtual(h, ext.VirtualModelDef{Provider: "vmcollide384", ID: "auto"})
	if _, ok := e.lookupVirtual("vmcollide384", "auto"); ok {
		t.Fatal("registered over a physical model")
	}

	e.installVirtual(h, ext.VirtualModelDef{Provider: "vmhide384", ID: "auto", ContextWindow: 42, Name: "Auto"})
	models.RegisterProvider(models.ProviderSpec{
		ID: "vmhide384", DefaultAPI: "openai-responses",
		Models: []models.Model{
			{ID: "auto", Name: "Physical", API: "openai-responses"},
			{ID: "keep", API: "openai-responses"},
		},
	})
	auth.RegisterProvider(auth.Provider{
		ID:     "vmhide384",
		APIKey: &auth.APIKeyHandler{Env: []string{"PIGO_VMHIDE384_KEY"}},
	})
	t.Setenv("PIGO_VMHIDE384_KEY", "k")
	var autos []models.Model
	var kept bool
	for _, m := range e.mergeVirtual(models.Catalog()) {
		if m.Provider == "vmhide384" && m.ID == "auto" {
			autos = append(autos, m)
		}
		if m.Provider == "vmhide384" && m.ID == "keep" {
			kept = true
		}
	}
	if len(autos) != 1 || autos[0].ContextWindow != 42 || autos[0].Name != "Auto" {
		t.Fatalf("hidden list = %+v", autos)
	}
	if !kept {
		t.Fatal("other physical model was hidden")
	}
}

func registerPhys(t *testing.T, small map[string]*string) {
	t.Helper()
	auth.RegisterProvider(auth.Provider{
		ID:     "vmphys384",
		APIKey: &auth.APIKeyHandler{Env: []string{"PIGO_VMPHYS384_KEY"}},
	})
	t.Setenv("PIGO_VMPHYS384_KEY", "k")
	rows := []models.Model{
		{ID: "small", API: "openai-responses", ThinkingLevelMap: small},
		{ID: "large", API: "openai-responses"},
		{ID: "saw-user", API: "openai-responses"},
		{ID: "retry", API: "openai-responses"},
		{ID: "nofailed", API: "openai-responses"},
		{ID: "direct", API: "openai-responses"},
		{ID: "leak", API: "openai-responses"},
	}
	models.RegisterProvider(models.ProviderSpec{
		ID: "vmphys384", DefaultAPI: "openai-responses", Models: rows,
	})
}

func spawnVirtualExt(t *testing.T) *ext.Host {
	t.Helper()
	h, err := ext.Spawn(context.Background(), "virtual-route",
		[]string{os.Args[0], "-test.run=^TestVirtualRouteHelper$"},
		ext.Options{Env: []string{"PIGO_RUNTIME_EXT=virtual"}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	return h
}

func newVirtualEngine(h *ext.Host, phys ai.StreamFn) *Engine {
	e := &Engine{
		Provider: "router",
		Opts: Options{Config: config.Config{
			Provider: "router", Model: "auto", Thinking: "high",
		}},
		Stream: func(context.Context, ai.Context, ai.Options) (*ai.EventStream, error) {
			return nil, errSentSelected()
		},
		Hosts:      []*ext.Host{h},
		extStreams: map[string]ai.StreamFn{"vmphys384": phys, "vmnone384": phys},
	}
	e.applyVirtualModels()
	return e
}

func errSentSelected() error {
	return errString("selected virtual stream was called")
}

type errString string

func (e errString) Error() string { return string(e) }

func hasModel(list []models.Model, provider, id string) bool {
	for _, m := range list {
		if m.Provider == provider && m.ID == id {
			return true
		}
	}
	return false
}
