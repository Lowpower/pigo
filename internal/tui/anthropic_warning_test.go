package tui

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/runtime"
)

func TestMaybeWarnAnthropicExtraUsage(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	dir := t.TempDir()
	s := auth.Open(dir)
	if _, err := s.Modify("anthropic", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: auth.TypeOAuth, Access: "tok"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Config: m.cfg}}
	m.maybeWarnAnthropicExtraUsage()
	if len(m.transcript) != 1 || !strings.Contains(m.transcript[0].rendered, "extra usage") {
		t.Fatalf("transcript=%+v", m.transcript)
	}
	m.maybeWarnAnthropicExtraUsage()
	if len(m.transcript) != 1 {
		t.Fatal("should warn once")
	}

	m.cfg.SetAnthropicExtraUsageWarning(false)
	m.anthropicWarn = false
	m.transcript = nil
	m.maybeWarnAnthropicExtraUsage()
	if len(m.transcript) != 0 {
		t.Fatalf("disabled warning still shown: %+v", m.transcript)
	}
}
