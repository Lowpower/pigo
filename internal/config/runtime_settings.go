package config

import (
	"crypto/rand"
	"fmt"
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

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

func parseOnOffAuto(v string, auto bool) (enabled bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	case "auto":
		return auto, true
	default:
		return false, false
	}
}

// HyperlinksEnabled reports whether OSC 8 file links should be emitted.
// "auto" follows tty; unset defaults to auto. PIGO_HYPERLINKS / PI_HYPERLINKS win.
func (c Config) HyperlinksEnabled(tty bool) bool {
	if v := firstEnv("PIGO_HYPERLINKS", "PI_HYPERLINKS"); v != "" {
		if on, ok := parseOnOffAuto(v, tty); ok {
			return on
		}
	}
	switch v := c.Terminal.Hyperlinks.(type) {
	case bool:
		return v
	case string:
		if on, ok := parseOnOffAuto(v, tty); ok {
			return on
		}
		return tty
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

func parseImageProtocol(v, detected string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "kitty", "iterm2":
		return s
	case "false", "off", "none", "0":
		return ""
	case "auto", "true", "":
		return detected
	}
	return detected
}

// ImageProtocol is kitty, iterm2, or empty to disable. detected is the TTY guess.
func (c Config) ImageProtocol(detected string) string {
	if v := firstEnv("PIGO_IMAGE_PROTOCOL", "PI_IMAGE_PROTOCOL"); v != "" {
		return parseImageProtocol(v, detected)
	}
	switch v := c.Terminal.Images.(type) {
	case bool:
		if !v {
			return ""
		}
		return detected
	case string:
		return parseImageProtocol(v, detected)
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

// EditorPadX is settings.editorPaddingX (default 0).
func (c Config) EditorPadX() int {
	if c.EditorPaddingX == nil || *c.EditorPaddingX < 0 {
		return 0
	}
	return *c.EditorPaddingX
}

// OutputPadN is settings.outputPad (default 1).
func (c Config) OutputPadN() int {
	if c.OutputPad == nil || *c.OutputPad < 0 {
		return 1
	}
	return *c.OutputPad
}

// HardwareCursor reports whether the terminal hardware cursor should stay visible.
func (c Config) HardwareCursor() bool {
	return c.ShowHardwareCursor != nil && *c.ShowHardwareCursor
}

// ClearOnShrink reports whether a smaller terminal should clear the screen.
func (c Config) ClearOnShrink() bool {
	return c.Terminal.ClearOnShrink != nil && *c.Terminal.ClearOnShrink
}

// TerminalProgress reports whether OSC 9;4 progress should be emitted.
func (c Config) TerminalProgress() bool {
	return c.Terminal.ShowTerminalProgress != nil && *c.Terminal.ShowTerminalProgress
}

func trueColorMode(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "on"
		}
		return "off"
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		switch s {
		case "true", "1", "yes", "on":
			return "on"
		case "false", "0", "no", "off":
			return "off"
		}
	}
	return "auto"
}

// TrueColorMode is "on", "off", or "auto". PIGO_TRUE_COLOR / PI_TRUE_COLOR win.
func (c Config) TrueColorMode() string {
	if v := firstEnv("PIGO_TRUE_COLOR", "PI_TRUE_COLOR"); v != "" {
		return trueColorMode(v)
	}
	return trueColorMode(c.Terminal.TrueColor)
}

// CopyOnSelect reports whether fullscreen mouse-select copy is enabled (default true).
func (c Config) CopyOnSelect() bool {
	if c.FullscreenCopyOnSelect == nil {
		return true
	}
	return *c.FullscreenCopyOnSelect
}

// ScrollbarEnabled reports whether a fullscreen scrollbar should be drawn (default true).
func (c Config) ScrollbarEnabled() bool {
	if c.FullscreenScrollbar == nil {
		return true
	}
	return *c.FullscreenScrollbar
}

// CacheRetention is PIGO_CACHE_RETENTION / PI_CACHE_RETENTION (none|short|long).
func (c Config) CacheRetention() string {
	return firstEnv("PIGO_CACHE_RETENTION", "PI_CACHE_RETENTION")
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

// AnalyticsEnabled is settings.enableAnalytics (default false).
func (c Config) AnalyticsEnabled() bool {
	return c.EnableAnalytics != nil && *c.EnableAnalytics
}

// SetEnableAnalytics writes the opt-in flag. First enable mints trackingId;
// later toggles keep the same id.
func (c *Config) SetEnableAnalytics(enabled bool) {
	c.EnableAnalytics = &enabled
	if enabled {
		c.ensureAnalyticsTrackingID()
	}
}

func (c *Config) ensureAnalyticsTrackingID() {
	if c.EnableAnalytics == nil || !*c.EnableAnalytics {
		return
	}
	if strings.TrimSpace(c.TrackingID) != "" {
		return
	}
	c.TrackingID = newTrackingID()
}

func newTrackingID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("pigo-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
