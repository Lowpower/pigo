package ext

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestVirtualHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_EXT_HELPER") != "virtual" {
		return
	}
	_ = Serve(Handler{
		Name: "virtual-ext",
		Providers: []ProviderDef{{
			ID:   "cap-virtual",
			Args: map[string]any{"name": "cap", "stream": true},
		}},
		VirtualModels: []VirtualModelDef{{
			Provider:       "router",
			ID:             "auto",
			Name:           "Auto",
			ThinkingLevels: []string{"low", "high"},
			ContextWindow:  1000,
			MaxTokens:      50,
			Route: func(_ context.Context, req RouteRequest) (RouteResult, error) {
				if req.Reason == "bad" {
					return RouteResult{}, errString("nope")
				}
				return RouteResult{
					Model:         RouteModel{Provider: "vmphys", ID: req.Reason},
					ThinkingLevel: "low",
				}, nil
			},
		}},
	})
}

func TestHostVirtualModelRoundTrip(t *testing.T) {
	h, err := Spawn(context.Background(), "virtual-ext",
		[]string{os.Args[0], "-test.run=^TestVirtualHelperProcess$"},
		Options{Env: []string{"PIGO_EXT_HELPER=virtual"}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = h.Close() }()

	vms := h.VirtualModels()
	if len(vms) != 1 || vms[0].Provider != "router" || vms[0].ID != "auto" || vms[0].ContextWindow != 1000 {
		t.Fatalf("virtual models = %+v", vms)
	}
	if len(h.Providers()) != 1 || h.Providers()[0].ID != "cap-virtual" {
		t.Fatalf("providers = %+v", h.Providers())
	}
	res, err := h.Route(context.Background(), map[string]any{
		"model":         map[string]any{"provider": "router", "id": "auto"},
		"reason":        "user",
		"thinkingLevel": "high",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	model, _ := res["model"].(map[string]any)
	if model["provider"] != "vmphys" || model["id"] != "user" || res["thinkingLevel"] != "low" {
		t.Fatalf("result = %#v", res)
	}
	if _, err := h.Route(context.Background(), map[string]any{
		"model":  map[string]any{"provider": "router", "id": "auto"},
		"reason": "bad",
	}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("route error = %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
