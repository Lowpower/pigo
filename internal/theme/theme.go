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
// ({name, appearance, vars, colors, export}). Unknown keys are errors.
// Colours may be a palette index, #rgb, #rrggbb, oklch(), okhsl(), a var
// name, or "" for the terminal default. Known colour tokens are kept in
// Colors and mapped onto the 7 TUI fields.
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

// default is the shorthand fallback. dark and light are appended from the
// embedded v1.3.0 theme files.
var builtins = []Theme{
	{Name: "default", User: "42", Assistant: "252", Tool: "178", Error: "196", Muted: "244", Accent: "205"},
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

// colorVal is one JSON color. Set is false for a missing key or null.
type colorVal struct {
	Value string
	Set   bool
}

func (c *colorVal) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*c = colorVal{}
		return nil
	}
	c.Set = true
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		c.Value = s
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	c.Value = strconv.Itoa(n)
	return nil
}

type fileTheme struct {
	Name       string              `json:"name"`
	Appearance string              `json:"appearance"`
	User       colorVal            `json:"user"`
	Assistant  colorVal            `json:"assistant"`
	Tool       colorVal            `json:"tool"`
	Error      colorVal            `json:"error"`
	Muted      colorVal            `json:"muted"`
	Accent     colorVal            `json:"accent"`
	Vars       map[string]colorVal `json:"vars"`
	Colors     map[string]colorVal `json:"colors"`
	Export     *struct {
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
	"appearance": true,
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
	return materialize(raw)
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
	if rawAppearance, exists := doc["appearance"]; exists && string(rawAppearance) != "null" {
		var appearance string
		if json.Unmarshal(rawAppearance, &appearance) != nil || (appearance != "dark" && appearance != "light") {
			problems = append(problems, `invalid field "appearance"`)
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

func materialize(raw fileTheme) (Theme, []string) {
	vars := varMap(raw.Vars)
	if len(raw.Colors) > 0 {
		return materializeExtended(raw, vars)
	}
	return materializeShorthand(raw, vars)
}

func varMap(raw map[string]colorVal) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if v.Set {
			out[k] = v.Value
		}
	}
	return out
}

func materializeShorthand(raw fileTheme, vars map[string]string) (Theme, []string) {
	t := Theme{Name: raw.Name}
	set := map[string]bool{}
	var problems []string
	apply := func(dst *string, key string, cv colorVal) {
		if !cv.Set {
			return
		}
		n, err := resolveColor(cv.Value, vars, nil)
		if err != nil {
			problems = append(problems, fmt.Sprintf("invalid color %q: %s", key, err))
			return
		}
		*dst = n
		set[key] = true
	}
	apply(&t.User, "user", raw.User)
	apply(&t.Assistant, "assistant", raw.Assistant)
	apply(&t.Tool, "tool", raw.Tool)
	apply(&t.Error, "error", raw.Error)
	apply(&t.Muted, "muted", raw.Muted)
	apply(&t.Accent, "accent", raw.Accent)
	if len(problems) > 0 {
		sort.Strings(problems)
		return Theme{}, problems
	}
	if t.Name == "" && !set["user"] && !set["accent"] {
		return Theme{}, nil
	}
	t.Appearance = raw.Appearance
	if t.Appearance == "" {
		var fg []string
		add := func(key, value string) {
			if set[key] {
				fg = append(fg, value)
			}
		}
		add("user", t.User)
		add("assistant", t.Assistant)
		add("tool", t.Tool)
		add("error", t.Error)
		add("muted", t.Muted)
		add("accent", t.Accent)
		t.Appearance = detectAppearance(fg, nil)
	}
	return overlayUnset(t, set), nil
}

func materializeExtended(raw fileTheme, vars map[string]string) (Theme, []string) {
	colors := map[string]string{}
	var problems []string
	keys := make([]string, 0, len(raw.Colors))
	for k := range raw.Colors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cv := raw.Colors[k]
		if !cv.Set {
			continue
		}
		n, err := resolveColor(cv.Value, vars, nil)
		if err != nil {
			problems = append(problems, fmt.Sprintf("invalid color %q: %s", k, err))
			continue
		}
		colors[k] = n
	}
	var page, card, info string
	pageSet, cardSet, infoSet := false, false, false
	if raw.Export != nil {
		page, pageSet = resolveField(&problems, "pageBg", raw.Export.PageBg, vars)
		card, cardSet = resolveField(&problems, "cardBg", raw.Export.CardBg, vars)
		info, infoSet = resolveField(&problems, "infoBg", raw.Export.InfoBg, vars)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return Theme{}, problems
	}
	applyColorFallbacks(colors)
	base := builtins[0]
	t := Theme{Name: raw.Name, Colors: colors}
	if t.Name == "" {
		t.Name = base.Name
	}
	t.User = pickColor(colors, "userMessageText", "accent", base.User)
	t.Assistant = pickColor(colors, "text", "", base.Assistant)
	t.Tool = pickColor(colors, "toolTitle", "", base.Tool)
	t.Error = pickColor(colors, "error", "", base.Error)
	t.Muted = pickColor(colors, "muted", "", base.Muted)
	t.Accent = pickColor(colors, "accent", "", base.Accent)
	t.Appearance = raw.Appearance
	if t.Appearance == "" {
		var fg, bg []string
		for token, color := range colors {
			if backgroundTokens[token] {
				bg = append(bg, color)
				continue
			}
			fg = append(fg, color)
		}
		t.Appearance = detectAppearance(fg, bg)
	}
	if pageSet {
		t.ExportPageBg = page
	}
	if cardSet {
		t.ExportCardBg = card
	}
	if infoSet {
		t.ExportInfoBg = info
	}
	return t, nil
}

func resolveField(problems *[]string, key string, cv colorVal, vars map[string]string) (string, bool) {
	if !cv.Set {
		return "", false
	}
	n, err := resolveColor(cv.Value, vars, nil)
	if err != nil {
		*problems = append(*problems, fmt.Sprintf("invalid color %q: %s", key, err))
		return "", false
	}
	return n, true
}

func applyColorFallbacks(colors map[string]string) {
	copyMissing := func(key, fallback string) {
		if _, ok := colors[key]; ok {
			return
		}
		if v, ok := colors[fallback]; ok {
			colors[key] = v
		}
	}
	copyMissing("thinkingMax", "thinkingXhigh")
	copyMissing("scrollbarThumb", "selectedBg")
	copyMissing("searchMatchBg", "selectedBg")
	copyMissing("searchMatchText", "text")
}

func pickColor(colors map[string]string, primary, secondary, fallback string) string {
	if v, ok := colors[primary]; ok {
		return v
	}
	if secondary != "" {
		if v, ok := colors[secondary]; ok {
			return v
		}
	}
	return fallback
}

func overlayUnset(t Theme, set map[string]bool) Theme {
	base := builtins[0]
	if t.Name == "" {
		t.Name = base.Name
	}
	if !set["user"] {
		t.User = base.User
	}
	if !set["assistant"] {
		t.Assistant = base.Assistant
	}
	if !set["tool"] {
		t.Tool = base.Tool
	}
	if !set["error"] {
		t.Error = base.Error
	}
	if !set["muted"] {
		t.Muted = base.Muted
	}
	if !set["accent"] {
		t.Accent = base.Accent
	}
	return t
}
