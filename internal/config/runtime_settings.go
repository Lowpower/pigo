package config

import (
	"os"
	"strings"
	"time"
)

// AutoResize reports whether images should be scaled to 2000px (default true).
func (c Config) AutoResize() bool {
	if c.Images.AutoResize == nil {
		return true
	}
	return *c.Images.AutoResize
}

// ImageWidthCells is the preferred inline image width (default 60).
func (c Config) ImageWidthCells() int {
	if c.Terminal.ImageWidthCells == nil || *c.Terminal.ImageWidthCells < 1 {
		return 60
	}
	return *c.Terminal.ImageWidthCells
}

// HyperlinksEnabled reports whether OSC 8 file links should be emitted.
// "auto" follows tty; unset defaults to auto.
func (c Config) HyperlinksEnabled(tty bool) bool {
	switch v := c.Terminal.Hyperlinks.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		default:
			return tty
		}
	default:
		return tty
	}
}

// CodeBlockIndent is the markdown code-block prefix (default two spaces).
func (c Config) CodeBlockIndent() string {
	if c.Markdown.CodeBlockIndent == "" {
		return "  "
	}
	return c.Markdown.CodeBlockIndent
}

// HideThinking reports whether thinking blocks start hidden (default false).
func (c Config) HideThinking() bool {
	return c.HideThinkingBlock != nil && *c.HideThinkingBlock
}

// CacheMissNotices reports whether cache-miss notices are shown (default false).
func (c Config) CacheMissNotices() bool {
	return c.ShowCacheMissNotices != nil && *c.ShowCacheMissNotices
}

// ShellPrefix is prepended to every bash command (empty when unset).
func (c Config) ShellPrefix() string {
	return c.ShellCommandPrefix
}

// AutocompleteVisible is the max visible autocomplete rows (default 5, clamped 3–20).
func (c Config) AutocompleteVisible() int {
	n := 5
	if c.AutocompleteMaxVisible != nil {
		n = *c.AutocompleteMaxVisible
	}
	if n < 3 {
		return 3
	}
	if n > 20 {
		return 20
	}
	return n
}

// ImageProtocol is kitty, iterm2, or empty to disable. detected is the TTY guess.
func (c Config) ImageProtocol(detected string) string {
	switch v := c.Terminal.Images.(type) {
	case bool:
		if !v {
			return ""
		}
		return detected
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		switch s {
		case "kitty", "iterm2":
			return s
		case "false", "off", "none", "0":
			return ""
		case "auto", "true", "":
			return detected
		}
	}
	return detected
}

// ProviderRetryTimeout overrides httpIdleTimeout when set.
func (c Config) ProviderRetryTimeout() time.Duration {
	if c.Retry.Provider == nil || c.Retry.Provider.TimeoutMs == nil {
		return 0
	}
	if *c.Retry.Provider.TimeoutMs <= 0 {
		return 0
	}
	return time.Duration(*c.Retry.Provider.TimeoutMs) * time.Millisecond
}

// ProviderRetryMaxRetries is settings.retry.provider.maxRetries (default 0).
func (c Config) ProviderRetryMaxRetries() int {
	if c.Retry.Provider == nil || c.Retry.Provider.MaxRetries == nil {
		return 0
	}
	if *c.Retry.Provider.MaxRetries < 0 {
		return 0
	}
	return *c.Retry.Provider.MaxRetries
}

// ProviderRetryMaxDelay is the provider-retry delay cap (default 60s).
func (c Config) ProviderRetryMaxDelay() time.Duration {
	ms := 60000
	if c.Retry.Provider != nil && c.Retry.Provider.MaxRetryDelayMs != nil {
		ms = *c.Retry.Provider.MaxRetryDelayMs
	}
	if ms < 0 {
		ms = 0
	}
	return time.Duration(ms) * time.Millisecond
}

// StreamIdleTimeout is provider.timeoutMs if set, else HTTPIdleTimeout.
func (c Config) StreamIdleTimeout() time.Duration {
	if d := c.ProviderRetryTimeout(); d > 0 {
		return d
	}
	return c.HTTPIdleTimeout()
}

// WebSocketConnectTimeout is settings.websocketConnectTimeoutMs when set.
func (c Config) WebSocketConnectTimeout() time.Duration {
	if c.WebsocketConnectTimeoutMs == nil || *c.WebsocketConnectTimeoutMs <= 0 {
		return 0
	}
	return time.Duration(*c.WebsocketConnectTimeoutMs) * time.Millisecond
}

// StdoutIsTTY is true when stdout is a terminal.
func StdoutIsTTY() bool {
	st, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}
