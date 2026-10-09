package runtime

import (
	"testing"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
)

func TestContextWindowUsesCatalog(t *testing.T) {
	models.ClearOverlays()
	t.Cleanup(models.ClearOverlays)
	models.SetUserOverlay("anthropic", []models.Model{{
		ID: "claude-sonnet-4", ContextWindow: 1000,
	}})
	e := &Engine{
		Provider: "anthropic",
		Opts: Options{Config: config.Config{
			Provider: "anthropic",
			Model:    "claude-sonnet-4",
		}},
	}
	if got := e.ContextWindow(); got != 1000 {
		t.Fatalf("ContextWindow = %d, want catalog 1000", got)
	}
}

func TestContextWindowExplicitOverridesCatalog(t *testing.T) {
	models.ClearOverlays()
	t.Cleanup(models.ClearOverlays)
	models.SetUserOverlay("anthropic", []models.Model{{
		ID: "claude-sonnet-4", ContextWindow: 1000,
	}})
	e := &Engine{
		Provider: "anthropic",
		Opts: Options{
			ContextWindow: 0,
			Config: config.Config{
				Provider:      "anthropic",
				Model:         "claude-sonnet-4",
				ContextWindow: 50000,
			},
		},
	}
	if got := e.ContextWindow(); got != 50000 {
		t.Fatalf("ContextWindow = %d, want explicit 50000", got)
	}
	e.Opts.ContextWindow = 8000
	if got := e.ContextWindow(); got != 8000 {
		t.Fatalf("ContextWindow = %d, want Options override 8000", got)
	}
}

func TestContextWindowFollowsModelSwitch(t *testing.T) {
	models.ClearOverlays()
	t.Cleanup(models.ClearOverlays)
	models.SetUserOverlay("anthropic", []models.Model{
		{ID: "claude-sonnet-4", ContextWindow: 1000},
		{ID: "claude-opus-4", ContextWindow: 32000},
	})
	e := &Engine{
		Provider: "anthropic",
		Opts: Options{Config: config.Config{
			Provider: "anthropic",
			Model:    "claude-sonnet-4",
		}},
	}
	if got := e.ContextWindow(); got != 1000 {
		t.Fatalf("before switch = %d, want 1000", got)
	}
	e.setModel("anthropic", "claude-opus-4", "", false)
	if got := e.ContextWindow(); got != 32000 {
		t.Fatalf("after switch = %d, want 32000", got)
	}
}

func TestContextWindowFallsBackWithoutCatalog(t *testing.T) {
	models.ClearOverlays()
	t.Cleanup(models.ClearOverlays)
	e := &Engine{
		Provider: "anthropic",
		Opts: Options{Config: config.Config{
			Provider: "anthropic",
			Model:    "claude-sonnet-4",
		}},
	}
	if got := e.ContextWindow(); got != config.DefaultContextWindow {
		t.Fatalf("ContextWindow = %d, want %d", got, config.DefaultContextWindow)
	}
}

func TestRPCGetStateContextWindow(t *testing.T) {
	models.ClearOverlays()
	t.Cleanup(models.ClearOverlays)
	models.SetUserOverlay("anthropic", []models.Model{{
		ID: "claude-sonnet-4", ContextWindow: 1000,
	}})
	e := &Engine{
		Provider: "anthropic",
		Opts: Options{Config: config.Config{
			Provider: "anthropic",
			Model:    "claude-sonnet-4",
		}},
	}
	out := serveRPC(t, e, `{"type":"get_state"}
{"type":"quit"}
`)
	rows := decodeRPCRows(t, out)
	var data map[string]any
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "get_state" {
			data, _ = r["data"].(map[string]any)
		}
	}
	if data["contextWindow"] != float64(1000) {
		t.Fatalf("get_state contextWindow = %#v in %s", data["contextWindow"], out)
	}
}
