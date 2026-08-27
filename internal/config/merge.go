package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// projectOverlay is the JSON keys a trusted project settings.json may supply.
// defaultProjectTrust is global-only and is not read from project files.
type projectOverlay struct {
	DefaultProvider      string                 `json:"defaultProvider"`
	DefaultModel         string                 `json:"defaultModel"`
	Theme                string                 `json:"theme"`
	Thinking             string                 `json:"thinking"`
	ContextWindow        int                    `json:"contextWindow"`
	CompactionOn         *bool                  `json:"compactionEnabled"`
	ReserveTokens        int                    `json:"compactionReserveTokens"`
	KeepRecentTokens     int                    `json:"compactionKeepRecentTokens"`
	SteeringMode         string                 `json:"steeringMode"`
	FollowUpMode         string                 `json:"followUpMode"`
	Retry                *RetrySettings         `json:"retry"`
	ThinkingBudgets      map[string]int         `json:"thinkingBudgets"`
	ModelThinkingLevels  map[string]string      `json:"modelThinkingLevels"`
	HTTPIdleTimeoutMs    *int                   `json:"httpIdleTimeoutMs"`
	ExternalEditor       string                 `json:"externalEditor"`
	DoubleEscapeAction   string                 `json:"doubleEscapeAction"`
	TreeFilterMode       string                 `json:"treeFilterMode"`
	BranchSummary        *BranchSummarySettings `json:"branchSummary"`
	Terminal             *TerminalSettings      `json:"terminal"`
	Markdown             *MarkdownSettings      `json:"markdown"`
	Images               *ImageSettings         `json:"images"`
	TUIMode              string                 `json:"tuiMode"`
	FullscreenExitOutput string                 `json:"fullscreenExitOutput"`
	ShellPath            string                 `json:"shellPath"`
	DefaultTools         *[]string              `json:"defaultTools"`
	Packages             []PackageEntry         `json:"packages"`
	Extensions           []string               `json:"extensions"`
	Skills               []string               `json:"skills"`
	Prompts              []string               `json:"prompts"`
	Themes               []string               `json:"themes"`
	NpmCommand           []string               `json:"npmCommand"`
}

// ApplyProject overlays cwd/.pigo/settings.json onto user when trusted.
// Missing or unreadable project files leave user unchanged.
func ApplyProject(user Config, cwd string, trusted bool) Config {
	if !trusted || cwd == "" {
		return user
	}
	b, err := os.ReadFile(filepath.Join(ProjectDir(cwd), "settings.json"))
	if err != nil {
		return user
	}
	var over projectOverlay
	if err := json.Unmarshal(b, &over); err != nil {
		return user
	}
	return applyOverlay(user, over)
}

func applyOverlay(user Config, over projectOverlay) Config {
	out := user
	if over.DefaultProvider != "" {
		out.DefaultProvider = over.DefaultProvider
		out.Provider = over.DefaultProvider
	}
	if over.DefaultModel != "" {
		out.DefaultModel = over.DefaultModel
		out.Model = over.DefaultModel
	}
	if over.Theme != "" {
		out.Theme = over.Theme
	}
	if over.Thinking != "" {
		out.Thinking = over.Thinking
	}
	if over.ContextWindow > 0 {
		out.ContextWindow = over.ContextWindow
	}
	if over.CompactionOn != nil {
		out.CompactionOn = over.CompactionOn
	}
	if over.ReserveTokens > 0 {
		out.ReserveTokens = over.ReserveTokens
	}
	if over.KeepRecentTokens > 0 {
		out.KeepRecentTokens = over.KeepRecentTokens
	}
	if over.SteeringMode != "" {
		out.SteeringMode = over.SteeringMode
	}
	if over.FollowUpMode != "" {
		out.FollowUpMode = over.FollowUpMode
	}
	if over.Retry != nil {
		if over.Retry.Enabled != nil {
			out.Retry.Enabled = over.Retry.Enabled
		}
		if over.Retry.MaxRetries != nil {
			out.Retry.MaxRetries = over.Retry.MaxRetries
		}
		if over.Retry.BaseDelayMs != nil {
			out.Retry.BaseDelayMs = over.Retry.BaseDelayMs
		}
	}
	if over.ThinkingBudgets != nil {
		out.ThinkingBudgets = over.ThinkingBudgets
	}
	if over.ModelThinkingLevels != nil {
		out.ModelThinkingLevels = over.ModelThinkingLevels
	}
	if over.HTTPIdleTimeoutMs != nil {
		out.HTTPIdleTimeoutMs = over.HTTPIdleTimeoutMs
	}
	if over.ExternalEditor != "" {
		out.ExternalEditor = over.ExternalEditor
	}
	if over.DoubleEscapeAction != "" {
		out.DoubleEscapeAction = over.DoubleEscapeAction
	}
	if over.TreeFilterMode != "" {
		out.TreeFilterMode = over.TreeFilterMode
	}
	if over.BranchSummary != nil {
		if over.BranchSummary.SkipPrompt != nil {
			out.BranchSummary.SkipPrompt = over.BranchSummary.SkipPrompt
		}
		if over.BranchSummary.ReserveTokens > 0 {
			out.BranchSummary.ReserveTokens = over.BranchSummary.ReserveTokens
		}
	}
	if over.Terminal != nil && over.Terminal.ShowImages != nil {
		out.Terminal.ShowImages = over.Terminal.ShowImages
	}
	if over.Markdown != nil && over.Markdown.Mermaid != "" {
		out.Markdown.Mermaid = over.Markdown.Mermaid
	}
	if over.Images != nil && over.Images.BlockImages != nil {
		out.Images.BlockImages = over.Images.BlockImages
	}
	if over.TUIMode != "" {
		out.TUIMode = over.TUIMode
	}
	if over.FullscreenExitOutput != "" {
		out.FullscreenExitOutput = over.FullscreenExitOutput
	}
	if over.ShellPath != "" {
		out.ShellPath = over.ShellPath
	}
	if over.DefaultTools != nil {
		tools := append([]string(nil), (*over.DefaultTools)...)
		out.DefaultTools = &tools
	}
	if over.Packages != nil {
		out.Packages = over.Packages
	}
	if over.Extensions != nil {
		out.Extensions = over.Extensions
	}
	if over.Skills != nil {
		out.Skills = over.Skills
	}
	if over.Prompts != nil {
		out.Prompts = over.Prompts
	}
	if over.Themes != nil {
		out.Themes = over.Themes
	}
	if over.NpmCommand != nil {
		out.NpmCommand = over.NpmCommand
	}
	return out
}

// CopyUISettings copies /settings-menu fields from src onto dst (user file).
func CopyUISettings(dst *Config, src Config) {
	if dst == nil {
		return
	}
	dst.Theme = src.Theme
	dst.Thinking = src.Thinking
	dst.CompactionOn = src.CompactionOn
	dst.SteeringMode = src.SteeringMode
	dst.FollowUpMode = src.FollowUpMode
	dst.Markdown = src.Markdown
	dst.Terminal = src.Terminal
	dst.Images = src.Images
	dst.DefaultProjectTrust = src.DefaultProjectTrust
	dst.DoubleEscapeAction = src.DoubleEscapeAction
	dst.TreeFilterMode = src.TreeFilterMode
	dst.CollapseChangelog = src.CollapseChangelog
	dst.EnableInstallTelemetry = src.EnableInstallTelemetry
	dst.TUIMode = src.TUIMode
	dst.FullscreenExitOutput = src.FullscreenExitOutput
}
