package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Theme is a named colour set for the TUI.
type Theme struct {
	Name      string `json:"name"`
	User      string `json:"user"`
	Assistant string `json:"assistant"`
	Tool      string `json:"tool"`
	Error     string `json:"error"`
	Muted     string `json:"muted"`
	Accent    string `json:"accent"`
}

var builtins = []Theme{
	{Name: "default", User: "42", Assistant: "252", Tool: "178", Error: "196", Muted: "244", Accent: "205"},
	{Name: "dark", User: "42", Assistant: "252", Tool: "178", Error: "196", Muted: "244", Accent: "205"},
	{Name: "light", User: "22", Assistant: "235", Tool: "130", Error: "160", Muted: "240", Accent: "162"},
}

// Load returns the named theme, searching agentDir/themes and cwd/.pi/themes.
func Load(name, cwd, agentDir string) Theme {
	if name == "" {
		name = "dark"
	}
	// auto pair "light/dark"
	if strings.Contains(name, "/") {
		parts := strings.SplitN(name, "/", 2)
		name = strings.TrimSpace(parts[1])
		if name == "" {
			name = "dark"
		}
	}
	for _, dir := range []string{filepath.Join(agentDir, "themes"), filepath.Join(cwd, ".pi", "themes")} {
		if t, ok := loadFile(filepath.Join(dir, name+".json")); ok {
			return t
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if t, ok := loadFile(filepath.Join(dir, e.Name())); ok && strings.EqualFold(t.Name, name) {
				return t
			}
		}
	}
	for _, t := range builtins {
		if strings.EqualFold(t.Name, name) {
			return t
		}
	}
	return builtins[0]
}

func loadFile(path string) (Theme, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, false
	}
	var t Theme
	if json.Unmarshal(b, &t) != nil || t.Name == "" && t.User == "" {
		return Theme{}, false
	}
	if t.Name == "" {
		t.Name = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	return t, true
}

// Names lists available theme names.
func Names(cwd, agentDir string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		n = strings.ToLower(n)
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	for _, t := range builtins {
		add(t.Name)
	}
	for _, dir := range []string{filepath.Join(agentDir, "themes"), filepath.Join(cwd, ".pi", "themes")} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".json") {
				add(strings.TrimSuffix(e.Name(), ".json"))
			}
		}
	}
	return out
}
