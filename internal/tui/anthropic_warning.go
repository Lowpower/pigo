package tui

import (
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
)

const anthropicExtraUsageWarning = "Anthropic subscription auth is active. Third-party harness usage draws from extra usage and is billed per token, not your Claude plan limits. Manage extra usage at https://claude.ai/settings/usage. Disable this warning in /settings."

func (m *Model) maybeWarnAnthropicExtraUsage() {
	if m == nil || m.anthropicWarn || !m.cfg.AnthropicExtraUsageWarning() {
		return
	}
	if m.cfg.ResolvedProvider() != "anthropic" {
		return
	}
	dir := m.settingsDir()
	if dir == "" {
		dir = config.DefaultConfigDir()
	}
	if !auth.IsAnthropicSubscriptionAuth(dir) {
		return
	}
	m.anthropicWarn = true
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(anthropicExtraUsageWarning)})
}
