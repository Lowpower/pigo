package tui

import (
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestLoginDoneWarnsWhenRadiusCatalogMissing(t *testing.T) {
	t.Cleanup(models.ClearOverlays)
	models.ClearOverlays()
	m := New(testCfg())
	m.login.provider = "radius"
	m.login.phase = loginRunning
	m = send(m, loginDoneMsg{})
	if m.loginActive() {
		t.Fatal("login UI should close")
	}
	if !transcriptContains(m, "login complete") {
		t.Fatalf("missing login complete: %s", transcriptText(m))
	}
	if !transcriptContains(m, "catalog") {
		t.Fatalf("expected catalog timeout hint, got %s", transcriptText(m))
	}
}

func TestLoginDoneSkipsRadiusHintWhenCatalogReady(t *testing.T) {
	t.Cleanup(models.ClearOverlays)
	models.SetRemoteOverlay("radius", []models.Model{{
		Provider: "radius",
		ID:       "balanced",
		API:      "pigo-messages",
	}})
	m := New(testCfg())
	m.login.provider = "radius"
	m.login.phase = loginRunning
	m = send(m, loginDoneMsg{})
	if transcriptContains(m, "catalog") {
		t.Fatalf("ready catalog should not warn: %s", transcriptText(m))
	}
}

func TestLoginDoneOtherProviderSkipsRadiusHint(t *testing.T) {
	t.Cleanup(models.ClearOverlays)
	models.ClearOverlays()
	m := New(testCfg())
	m.login.provider = "anthropic"
	m.login.phase = loginRunning
	m = send(m, loginDoneMsg{})
	if transcriptContains(m, "catalog") {
		t.Fatalf("non-radius login should not warn: %s", transcriptText(m))
	}
}
