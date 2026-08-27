package session

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Lowpower/pigo/internal/theme"
)

type themeFile struct {
	Name   string                     `json:"name"`
	Vars   map[string]json.RawMessage `json:"vars"`
	Colors map[string]json.RawMessage `json:"colors"`
	Export struct {
		PageBg json.RawMessage `json:"pageBg"`
		CardBg json.RawMessage `json:"cardBg"`
		InfoBg json.RawMessage `json:"infoBg"`
	} `json:"export"`
}

type exportColors struct {
	CSS       map[string]string
	PageBg    string
	CardBg    string
	InfoBg    string
	ThemeVars string
}

func resolveExportColors(themeName, cwd, agentDir string) exportColors {
	name := strings.TrimSpace(themeName)
	if name == "" {
		name = "dark"
	}
	if i := strings.Index(name, "/"); i >= 0 {
		name = strings.TrimSpace(name[i+1:])
		if name == "" {
			name = "dark"
		}
	}
	fileName := "dark.json"
	if strings.EqualFold(name, "light") {
		fileName = "light.json"
	}
	raw, err := htmlFS.ReadFile("html/themes/" + fileName)
	if err != nil {
		raw, _ = htmlFS.ReadFile("html/themes/dark.json")
	}
	base := parseThemeFile(raw)
	switch strings.ToLower(name) {
	case "dark", "default", "light", "":
		// built-in export palettes already match the TUI names
	default:
		overlayPigoTheme(base.CSS, theme.Load(name, cwd, agentDir))
	}

	userBg := base.CSS["userMessageBg"]
	if userBg == "" {
		userBg = "#343541"
	}
	derived := deriveExportColors(userBg)
	pageBg := firstNonEmpty(base.PageBg, derived.pageBg)
	cardBg := firstNonEmpty(base.CardBg, derived.cardBg)
	infoBg := firstNonEmpty(base.InfoBg, derived.infoBg)

	var lines []string
	for k, v := range base.CSS {
		lines = append(lines, fmt.Sprintf("--%s: %s;", k, v))
	}
	lines = append(lines,
		fmt.Sprintf("--exportPageBg: %s;", pageBg),
		fmt.Sprintf("--exportCardBg: %s;", cardBg),
		fmt.Sprintf("--exportInfoBg: %s;", infoBg),
	)
	sort.Strings(lines)

	return exportColors{
		CSS:       base.CSS,
		PageBg:    pageBg,
		CardBg:    cardBg,
		InfoBg:    infoBg,
		ThemeVars: strings.Join(lines, "\n      "),
	}
}

func parseThemeFile(raw []byte) exportColors {
	out := exportColors{CSS: map[string]string{}}
	var tf themeFile
	if json.Unmarshal(raw, &tf) != nil {
		return out
	}
	vars := map[string]string{}
	for k, v := range tf.Vars {
		if s, ok := decodeColor(v, nil); ok {
			vars[k] = s
		}
	}
	// second pass for var-to-var
	for k, v := range tf.Vars {
		if s, ok := decodeColor(v, vars); ok {
			vars[k] = s
		}
	}
	isLight := strings.EqualFold(tf.Name, "light")
	defaultText := "#e5e5e7"
	if isLight {
		defaultText = "#000000"
	}
	for k, v := range tf.Colors {
		s, ok := decodeColor(v, vars)
		if !ok || s == "" {
			s = defaultText
		}
		out.CSS[k] = s
	}
	if s, ok := decodeColor(tf.Export.PageBg, vars); ok {
		out.PageBg = s
	}
	if s, ok := decodeColor(tf.Export.CardBg, vars); ok {
		out.CardBg = s
	}
	if s, ok := decodeColor(tf.Export.InfoBg, vars); ok {
		out.InfoBg = s
	}
	return out
}

func decodeColor(raw json.RawMessage, vars map[string]string) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return ansi256ToHex(int(n)), true
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	if s == "" {
		return "", true
	}
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "rgb") {
		return s, true
	}
	if vars != nil {
		if v, ok := vars[s]; ok {
			return v, true
		}
	}
	if idx, err := strconv.Atoi(s); err == nil {
		return ansi256ToHex(idx), true
	}
	return s, true
}

func overlayPigoTheme(css map[string]string, t theme.Theme) {
	if v := ansiFieldToHex(t.User); v != "" {
		css["userMessageBg"] = v
	}
	if v := ansiFieldToHex(t.Assistant); v != "" {
		css["text"] = v
		css["userMessageText"] = v
		css["assistant"] = v
	}
	if v := ansiFieldToHex(t.Tool); v != "" {
		css["toolTitle"] = v
	}
	if v := ansiFieldToHex(t.Error); v != "" {
		css["error"] = v
	}
	if v := ansiFieldToHex(t.Muted); v != "" {
		css["muted"] = v
		css["dim"] = v
	}
	if v := ansiFieldToHex(t.Accent); v != "" {
		css["accent"] = v
	}
}

func ansiFieldToHex(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "rgb") {
		return s
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return ""
	}
	return ansi256ToHex(n)
}

func ansi256ToHex(index int) string {
	if index < 0 {
		index = 0
	}
	if index > 255 {
		index = 255
	}
	basic := []string{
		"#000000", "#800000", "#008000", "#808000", "#000080", "#800080", "#008080", "#c0c0c0",
		"#808080", "#ff0000", "#00ff00", "#ffff00", "#0000ff", "#ff00ff", "#00ffff", "#ffffff",
	}
	if index < 16 {
		return basic[index]
	}
	if index < 232 {
		cube := index - 16
		r := cube / 36
		g := (cube % 36) / 6
		b := cube % 6
		to := func(n int) string {
			v := 0
			if n != 0 {
				v = 55 + n*40
			}
			return fmt.Sprintf("%02x", v)
		}
		return "#" + to(r) + to(g) + to(b)
	}
	gray := 8 + (index-232)*10
	h := fmt.Sprintf("%02x", gray)
	return "#" + h + h + h
}

type derivedBg struct {
	pageBg, cardBg, infoBg string
}

func deriveExportColors(baseColor string) derivedBg {
	parsed := parseColor(baseColor)
	if parsed == nil {
		return derivedBg{
			pageBg: "rgb(24, 24, 30)",
			cardBg: "rgb(30, 30, 36)",
			infoBg: "rgb(60, 55, 40)",
		}
	}
	r, g, b := parsed[0], parsed[1], parsed[2]
	if luminance(r, g, b) > 0.5 {
		return derivedBg{
			pageBg: adjustBrightness(baseColor, 0.96),
			cardBg: baseColor,
			infoBg: fmt.Sprintf("rgb(%d, %d, %d)", min255(r+10), min255(g+5), max0(b-20)),
		}
	}
	return derivedBg{
		pageBg: adjustBrightness(baseColor, 0.7),
		cardBg: adjustBrightness(baseColor, 0.85),
		infoBg: fmt.Sprintf("rgb(%d, %d, %d)", min255(r+20), min255(g+15), b),
	}
}

func parseColor(color string) []int {
	color = strings.TrimSpace(color)
	if strings.HasPrefix(color, "#") && len(color) == 7 {
		r, err1 := strconv.ParseInt(color[1:3], 16, 0)
		g, err2 := strconv.ParseInt(color[3:5], 16, 0)
		b, err3 := strconv.ParseInt(color[5:7], 16, 0)
		if err1 == nil && err2 == nil && err3 == nil {
			return []int{int(r), int(g), int(b)}
		}
	}
	var r, g, b int
	if _, err := fmt.Sscanf(color, "rgb(%d, %d, %d)", &r, &g, &b); err == nil {
		return []int{r, g, b}
	}
	if _, err := fmt.Sscanf(color, "rgb(%d,%d,%d)", &r, &g, &b); err == nil {
		return []int{r, g, b}
	}
	return nil
}

func luminance(r, g, b int) float64 {
	toLinear := func(c int) float64 {
		s := float64(c) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*toLinear(r) + 0.7152*toLinear(g) + 0.0722*toLinear(b)
}

func adjustBrightness(color string, factor float64) string {
	p := parseColor(color)
	if p == nil {
		return color
	}
	adj := func(c int) int {
		v := int(math.Round(float64(c) * factor))
		return min255(max0(v))
	}
	return fmt.Sprintf("rgb(%d, %d, %d)", adj(p[0]), adj(p[1]), adj(p[2]))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func min255(v int) int {
	if v > 255 {
		return 255
	}
	return v
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
