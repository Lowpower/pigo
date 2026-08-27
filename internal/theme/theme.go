package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Theme is a named colour set for the TUI.
//
// Disk files may be the original 7-key object, or a pi theme
// ({name, vars, colors}). Unknown keys are ignored. Known pi tokens
// are kept in Colors and mapped onto the 7 TUI fields.
type Theme struct {
	Name      string `json:"name"`
	User      string `json:"user"`
	Assistant string `json:"assistant"`
	Tool      string `json:"tool"`
	Error     string `json:"error"`
	Muted     string `json:"muted"`
	Accent    string `json:"accent"`

	// Colors is the resolved pi token map (accent, toolTitle, …). May be nil.
	Colors map[string]string `json:"-"`
}

// LoadOptions is how CLI flags and the TUI ask for a theme.
type LoadOptions struct {
	Name        string
	Cwd         string
	AgentDir    string
	Extra       []string // --theme paths (files or directories)
	NoDiscovery bool     // --no-themes: skip agentDir/cwd discovery
}

var builtins = []Theme{
	{Name: "default", User: "42", Assistant: "252", Tool: "178", Error: "196", Muted: "244", Accent: "205"},
	{Name: "dark", User: "42", Assistant: "252", Tool: "178", Error: "196", Muted: "244", Accent: "205"},
	{Name: "light", User: "22", Assistant: "235", Tool: "130", Error: "160", Muted: "240", Accent: "162"},
}

// Load returns the named theme, searching agentDir/themes and cwd/.pigo/themes.
func Load(name, cwd, agentDir string) Theme {
	return LoadWith(LoadOptions{Name: name, Cwd: cwd, AgentDir: agentDir})
}

// LoadWith searches extra --theme paths, then discovered dirs, then builtins.
func LoadWith(opt LoadOptions) Theme {
	name := strings.TrimSpace(opt.Name)
	if name == "" {
		name = "dark"
	}
	if strings.Contains(name, "/") {
		parts := strings.SplitN(name, "/", 2)
		name = strings.TrimSpace(parts[1])
		if name == "" {
			name = "dark"
		}
	}
	catalog := collect(opt)
	for i := range catalog {
		if strings.EqualFold(catalog[i].Name, name) {
			return catalog[i]
		}
	}
	for _, t := range builtins {
		if strings.EqualFold(t.Name, name) {
			return t
		}
	}
	return builtins[0]
}

// Names lists available theme names (builtins, discovered files, --theme extras).
func Names(cwd, agentDir string) []string {
	return NamesWith(LoadOptions{Cwd: cwd, AgentDir: agentDir})
}

// NamesWith lists theme names under the same search rules as LoadWith.
func NamesWith(opt LoadOptions) []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	for _, t := range collect(opt) {
		add(t.Name)
	}
	for _, t := range builtins {
		add(t.Name)
	}
	return out
}

func collect(opt LoadOptions) []Theme {
	var out []Theme
	seen := map[string]bool{}
	add := func(t Theme) {
		key := strings.ToLower(strings.TrimSpace(t.Name))
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, t)
	}
	if !opt.NoDiscovery {
		for _, dir := range []string{filepath.Join(opt.AgentDir, "themes"), filepath.Join(opt.Cwd, ".pigo", "themes")} {
			if opt.AgentDir == "" && strings.HasPrefix(dir, "themes") {
				continue
			}
			if opt.Cwd == "" && strings.Contains(dir, ".pigo") {
				continue
			}
			for _, t := range loadDir(dir) {
				add(t)
			}
		}
	}
	for _, p := range opt.Extra {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			for _, t := range loadDir(p) {
				add(t)
			}
			continue
		}
		if t, ok := loadFile(p); ok {
			add(t)
		}
	}
	return out
}

func loadDir(dir string) []Theme {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Theme
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		if t, ok := loadFile(filepath.Join(dir, e.Name())); ok {
			out = append(out, t)
		}
	}
	return out
}

func loadFile(path string) (Theme, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, false
	}
	t, ok := parseThemeJSON(b)
	if !ok {
		return Theme{}, false
	}
	if t.Name == "" {
		t.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if strings.Contains(t.Name, "/") {
		return Theme{}, false
	}
	return t, true
}

type colorVal string

func (c *colorVal) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*c = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*c = colorVal(s)
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*c = colorVal(strconv.Itoa(n))
	return nil
}

type fileTheme struct {
	Name      string              `json:"name"`
	User      string              `json:"user"`
	Assistant string              `json:"assistant"`
	Tool      string              `json:"tool"`
	Error     string              `json:"error"`
	Muted     string              `json:"muted"`
	Accent    string              `json:"accent"`
	Vars      map[string]colorVal `json:"vars"`
	Colors    map[string]colorVal `json:"colors"`
}

func parseThemeJSON(b []byte) (Theme, bool) {
	var raw fileTheme
	if json.Unmarshal(b, &raw) != nil {
		return Theme{}, false
	}
	if len(raw.Colors) > 0 {
		return fromPi(raw), true
	}
	t := Theme{
		Name:      raw.Name,
		User:      raw.User,
		Assistant: raw.Assistant,
		Tool:      raw.Tool,
		Error:     raw.Error,
		Muted:     raw.Muted,
		Accent:    raw.Accent,
	}
	if t.Name == "" && t.User == "" && t.Accent == "" {
		return Theme{}, false
	}
	return overlayBuiltin(t), true
}

func fromPi(raw fileTheme) Theme {
	vars := map[string]string{}
	for k, v := range raw.Vars {
		vars[k] = string(v)
	}
	colors := map[string]string{}
	for k, v := range raw.Colors {
		colors[k] = resolveRef(string(v), vars, nil)
	}
	if _, ok := colors["thinkingMax"]; !ok {
		colors["thinkingMax"] = colors["thinkingXhigh"]
	}
	if _, ok := colors["scrollbarThumb"]; !ok {
		colors["scrollbarThumb"] = colors["selectedBg"]
	}
	if _, ok := colors["searchMatchBg"]; !ok {
		colors["searchMatchBg"] = colors["selectedBg"]
	}
	if _, ok := colors["searchMatchText"]; !ok {
		colors["searchMatchText"] = colors["text"]
	}
	t := Theme{Name: raw.Name, Colors: colors}
	if c := colors["userMessageText"]; c != "" {
		t.User = c
	} else {
		t.User = colors["accent"]
	}
	t.Assistant = colors["text"]
	t.Tool = colors["toolTitle"]
	t.Error = colors["error"]
	t.Muted = colors["muted"]
	t.Accent = colors["accent"]
	return overlayBuiltin(t)
}

func overlayBuiltin(t Theme) Theme {
	base := builtins[0]
	if t.Name == "" {
		t.Name = base.Name
	}
	if t.User == "" {
		t.User = base.User
	}
	if t.Assistant == "" {
		t.Assistant = base.Assistant
	}
	if t.Tool == "" {
		t.Tool = base.Tool
	}
	if t.Error == "" {
		t.Error = base.Error
	}
	if t.Muted == "" {
		t.Muted = base.Muted
	}
	if t.Accent == "" {
		t.Accent = base.Accent
	}
	return t
}

func resolveRef(v string, vars map[string]string, seen map[string]bool) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "#") {
		return v
	}
	if _, err := strconv.Atoi(v); err == nil {
		return v
	}
	if vars == nil {
		return v
	}
	next, ok := vars[v]
	if !ok {
		return v
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	if seen[v] {
		return ""
	}
	seen[v] = true
	return resolveRef(next, vars, seen)
}
