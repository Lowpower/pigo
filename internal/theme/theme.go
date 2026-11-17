// Package theme loads TUI colour sets and builds the system theme from the terminal palette.
package theme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Lowpower/pigo/internal/bom"
)

// Theme is a named colour set for the TUI.
//
// Disk files may be a 7-key colour object, or an extended theme
// ({name, vars, colors, export}). Unknown keys are errors. Known colour
// tokens are kept in Colors and mapped onto the 7 TUI fields.
type Theme struct {
	Name      string `json:"name"`
	User      string `json:"user"`
	Assistant string `json:"assistant"`
	Tool      string `json:"tool"`
	Error     string `json:"error"`
	Muted     string `json:"muted"`
	Accent    string `json:"accent"`

	// Colors is the resolved token map (accent, toolTitle, …). May be nil.
	Colors map[string]string `json:"-"`

	ExportPageBg string `json:"-"`
	ExportCardBg string `json:"-"`
	ExportInfoBg string `json:"-"`

	// Appearance is "dark" or "light" for a generated system theme.
	Appearance string `json:"-"`
	// Dim lists system-theme tokens drawn faint when the terminal reported no colors.
	Dim []string `json:"-"`
}

// LoadOptions is how CLI flags and the TUI ask for a theme.
type LoadOptions struct {
	Name        string
	Cwd         string
	AgentDir    string
	Extra       []string // --theme paths (files or directories)
	NoDiscovery bool     // --no-themes: skip agentDir/cwd discovery
	NoProject   bool     // skip cwd/.pigo/themes (untrusted project)
	Terminal    SystemInput
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

// LoadResult is a resolved theme plus file errors from the same scan.
// Errors name the path and the rejected field. The theme is still a builtin
// or another valid file when the requested file was rejected.
type LoadResult struct {
	Theme  Theme
	Errors []string
}

// LoadWith searches extra --theme paths, then discovered dirs, then builtins.
// An empty name and the reserved name "system" build the system theme.
func LoadWith(opt LoadOptions) Theme {
	return LoadWithResult(opt).Theme
}

// LoadWithResult is LoadWith plus theme-file errors from that scan.
// Startup and /reload both use it, including when the selected theme is a builtin.
func LoadWithResult(opt LoadOptions) LoadResult {
	catalog, errs := collect(opt)
	name := strings.TrimSpace(opt.Name)
	if name == "" {
		name = systemThemeName
	}
	if strings.Contains(name, "/") {
		parts := strings.SplitN(name, "/", 2)
		name = strings.TrimSpace(parts[1])
		if name == "" {
			name = "dark"
		}
	}
	res := LoadResult{Errors: errs}
	if strings.EqualFold(name, systemThemeName) {
		res.Theme = System(opt.Terminal)
		return res
	}
	for i := range catalog {
		if strings.EqualFold(catalog[i].Name, name) {
			res.Theme = catalog[i]
			return res
		}
	}
	for _, t := range builtins {
		if strings.EqualFold(t.Name, name) {
			res.Theme = t
			return res
		}
	}
	res.Theme = builtins[0]
	return res
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
	add(systemThemeName)
	catalog, _ := collect(opt)
	for _, t := range catalog {
		add(t.Name)
	}
	for _, t := range builtins {
		add(t.Name)
	}
	return out
}

func collect(opt LoadOptions) ([]Theme, []string) {
	var out []Theme
	var errs []string
	seen := map[string]bool{}
	add := func(t Theme) {
		key := strings.ToLower(strings.TrimSpace(t.Name))
		if key == "" || key == systemThemeName || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, t)
	}
	addFile := func(path string) {
		t, lines := loadFile(path)
		if len(lines) > 0 {
			errs = append(errs, lines...)
			return
		}
		if t.Name == "" {
			return
		}
		add(t)
	}
	if !opt.NoDiscovery {
		dirs := []string{filepath.Join(opt.AgentDir, "themes")}
		if !opt.NoProject && opt.Cwd != "" {
			dirs = append(dirs, filepath.Join(opt.Cwd, ".pigo", "themes"))
		}
		for _, dir := range dirs {
			if opt.AgentDir == "" && strings.HasPrefix(dir, "themes") {
				continue
			}
			themes, lines := loadDir(dir)
			errs = append(errs, lines...)
			for _, t := range themes {
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
			themes, lines := loadDir(p)
			errs = append(errs, lines...)
			for _, t := range themes {
				add(t)
			}
			continue
		}
		addFile(p)
	}
	return out, errs
}

func loadDir(dir string) ([]Theme, []string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	var out []Theme
	var errs []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		t, lines := loadFile(filepath.Join(dir, e.Name()))
		if len(lines) > 0 {
			errs = append(errs, lines...)
			continue
		}
		if t.Name == "" {
			continue
		}
		out = append(out, t)
	}
	return out, errs
}

func loadFile(path string) (Theme, []string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, []string{fmt.Sprintf("theme %s: %s", path, err)}
	}
	t, lines := parseThemeFile(bom.Strip(b))
	if len(lines) > 0 {
		out := make([]string, len(lines))
		for i, line := range lines {
			out[i] = fmt.Sprintf("theme %s: %s", path, line)
		}
		return Theme{}, out
	}
	if t.Name == "" && t.User == "" && t.Accent == "" {
		return Theme{}, nil
	}
	if t.Name == "" {
		t.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if strings.Contains(t.Name, "/") {
		return Theme{}, []string{fmt.Sprintf("theme %s: invalid name %q", path, t.Name)}
	}
	return t, nil
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
	User      colorVal            `json:"user"`
	Assistant colorVal            `json:"assistant"`
	Tool      colorVal            `json:"tool"`
	Error     colorVal            `json:"error"`
	Muted     colorVal            `json:"muted"`
	Accent    colorVal            `json:"accent"`
	Vars      map[string]colorVal `json:"vars"`
	Colors    map[string]colorVal `json:"colors"`
	Export    *struct {
		PageBg colorVal `json:"pageBg"`
		CardBg colorVal `json:"cardBg"`
		InfoBg colorVal `json:"infoBg"`
	} `json:"export"`
}

// themeFileKeys are the top-level keys a theme file may contain.
// $schema is accepted and ignored.
var themeFileKeys = map[string]bool{
	"$schema": true, "name": true,
	"user": true, "assistant": true, "tool": true,
	"error": true, "muted": true, "accent": true,
	"vars": true, "colors": true, "export": true,
}

// themeExportKeys are the only keys allowed under export.
var themeExportKeys = map[string]bool{
	"pageBg": true, "cardBg": true, "infoBg": true,
}

var shorthandColorKeys = []string{"user", "assistant", "tool", "error", "muted", "accent"}

func colorTokenSet() map[string]bool {
	set := make(map[string]bool, len(tokenFamilies))
	for _, tf := range tokenFamilies {
		set[tf.token] = true
	}
	return set
}

// parseThemeFile validates keys, then builds a theme.
// A second empty result with no lines means the file has no theme content.
// Lines are problems without the path prefix.
func parseThemeFile(b []byte) (Theme, []string) {
	problems, raw, ok := validateThemeJSON(b)
	if len(problems) > 0 || !ok {
		return Theme{}, problems
	}
	if len(raw.Colors) > 0 {
		return fromPi(raw), nil
	}
	t := Theme{
		Name:      raw.Name,
		User:      string(raw.User),
		Assistant: string(raw.Assistant),
		Tool:      string(raw.Tool),
		Error:     string(raw.Error),
		Muted:     string(raw.Muted),
		Accent:    string(raw.Accent),
	}
	if t.Name == "" && t.User == "" && t.Accent == "" {
		return Theme{}, nil
	}
	return overlayBuiltin(t), nil
}

func validateThemeJSON(b []byte) (problems []string, raw fileTheme, ok bool) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil || doc == nil {
		return []string{"invalid JSON"}, fileTheme{}, false
	}
	tokens := colorTokenSet()
	var unknown []string
	for k := range doc {
		if !themeFileKeys[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		problems = append(problems, fmt.Sprintf("unknown field %q", k))
	}
	if rawName, exists := doc["name"]; exists && string(rawName) != "null" {
		var name string
		if json.Unmarshal(rawName, &name) != nil {
			problems = append(problems, `invalid field "name"`)
		}
	}
	if rawSchema, exists := doc["$schema"]; exists && string(rawSchema) != "null" {
		var schema string
		if json.Unmarshal(rawSchema, &schema) != nil {
			problems = append(problems, `invalid field "$schema"`)
		}
	}
	for _, key := range shorthandColorKeys {
		rawColor, exists := doc[key]
		if !exists || string(rawColor) == "null" {
			continue
		}
		if !validColorRaw(rawColor) {
			problems = append(problems, fmt.Sprintf("invalid color %q", key))
		}
	}
	problems = append(problems, objectColorProblems("vars", doc["vars"], nil)...)
	problems = append(problems, objectColorProblems("colors", doc["colors"], tokens)...)
	problems = append(problems, objectColorProblems("export", doc["export"], themeExportKeys)...)
	if len(problems) > 0 {
		return problems, fileTheme{}, false
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return []string{"invalid JSON"}, fileTheme{}, false
	}
	return nil, raw, true
}

// objectColorProblems checks a nested object. allowed nil means any key
// (vars). A missing raw value is not an error. null is treated as absent.
func objectColorProblems(label string, raw json.RawMessage, allowed map[string]bool) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return []string{fmt.Sprintf("invalid field %q", label)}
	}
	var unknown []string
	var problems []string
	for k, v := range obj {
		if allowed != nil && !allowed[k] {
			unknown = append(unknown, k)
			continue
		}
		if !validColorRaw(v) {
			problems = append(problems, fmt.Sprintf("invalid color %q", k))
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		if label == "colors" {
			problems = append(problems, fmt.Sprintf("unknown color %q", k))
			continue
		}
		problems = append(problems, fmt.Sprintf("unknown field %q", k))
	}
	sort.Strings(problems)
	return problems
}

func validColorRaw(raw json.RawMessage) bool {
	if string(raw) == "null" {
		return true
	}
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		return json.Unmarshal(raw, &s) == nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	var n int
	if err := dec.Decode(&n); err != nil {
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != nil {
		return err.Error() == "EOF"
	}
	return false
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
	if raw.Export != nil {
		t.ExportPageBg = resolveRef(string(raw.Export.PageBg), vars, nil)
		t.ExportCardBg = resolveRef(string(raw.Export.CardBg), vars, nil)
		t.ExportInfoBg = resolveRef(string(raw.Export.InfoBg), vars, nil)
	}
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
