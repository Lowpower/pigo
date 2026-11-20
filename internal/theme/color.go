package theme

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const numberPattern = `[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?`

var (
	hexPattern   = regexp.MustCompile(`(?i)^#([\da-f]{3}|[\da-f]{6})$`)
	oklchPattern = regexp.MustCompile(`(?i)^oklch\(\s*(` + numberPattern + `)(%)?\s+(` + numberPattern + `)\s+(` + numberPattern + `)(?:deg)?\s*\)$`)
	okhslPattern = regexp.MustCompile(`(?i)^okhsl\(\s*(` + numberPattern + `)(?:deg)?\s+(` + numberPattern + `)(%)?\s+(` + numberPattern + `)(%)?\s*\)$`)
)

// resolveColor turns one theme color into a painter value: "", a palette
// index, or #rrggbb. Names that are not literals are looked up in vars.
func resolveColor(v string, vars map[string]string, seen map[string]bool) (string, error) {
	v = strings.TrimSpace(v)
	if lit, ok, err := normalizeLiteral(v); ok || err != nil {
		return lit, err
	}
	next, ok := vars[v]
	if !ok {
		return "", fmt.Errorf("unknown variable %q", v)
	}
	if seen[v] {
		return "", fmt.Errorf("circular variable %q", v)
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	seen[v] = true
	return resolveColor(next, vars, seen)
}

func normalizeLiteral(v string) (string, bool, error) {
	if v == "" {
		return "", true, nil
	}
	if strings.HasPrefix(v, "#") {
		hex, err := normalizeHex(v)
		return hex, true, err
	}
	lower := strings.ToLower(v)
	switch {
	case strings.HasPrefix(lower, "oklch("):
		hex, err := normalizeOklch(v)
		return hex, true, err
	case strings.HasPrefix(lower, "okhsl("):
		hex, err := normalizeOkhsl(v)
		return hex, true, err
	}
	if index, ok, err := normalizeIndex(v); ok || err != nil {
		return index, true, err
	}
	return "", false, nil
}

func normalizeHex(v string) (string, error) {
	m := hexPattern.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("invalid hex")
	}
	digits := m[1]
	if len(digits) == 3 {
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	}
	return "#" + strings.ToLower(digits), nil
}

func normalizeIndex(v string) (string, bool, error) {
	if v == "" {
		return "", false, nil
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return "", false, nil
		}
	}
	n, err := strconv.Atoi(v)
	if err != nil || n > 255 {
		return "", true, fmt.Errorf("palette index out of range")
	}
	return strconv.Itoa(n), true, nil
}

func normalizeOklch(v string) (string, error) {
	m := oklchPattern.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("invalid oklch")
	}
	lightness, err := colorNumber(m[1], m[2] != "")
	if err != nil {
		return "", err
	}
	chroma, err := colorNumber(m[3], false)
	if err != nil {
		return "", err
	}
	hue, err := colorNumber(m[4], false)
	if err != nil {
		return "", err
	}
	if lightness < 0 || lightness > 1 {
		return "", fmt.Errorf("oklch lightness out of range")
	}
	if chroma < 0 {
		return "", fmt.Errorf("oklch chroma out of range")
	}
	return formatHex(oklchToRgb(lightness, chroma, hue)), nil
}

func normalizeOkhsl(v string) (string, error) {
	m := okhslPattern.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("invalid okhsl")
	}
	hue, err := colorNumber(m[1], false)
	if err != nil {
		return "", err
	}
	saturation, err := colorNumber(m[2], m[3] != "")
	if err != nil {
		return "", err
	}
	lightness, err := colorNumber(m[4], m[5] != "")
	if err != nil {
		return "", err
	}
	if saturation < 0 || saturation > 1 {
		return "", fmt.Errorf("okhsl saturation out of range")
	}
	if lightness < 0 || lightness > 1 {
		return "", fmt.Errorf("okhsl lightness out of range")
	}
	return formatHex(okhslToRgb(hue, saturation, lightness)), nil
}

func colorNumber(text string, percent bool) (float64, error) {
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("invalid number")
	}
	if percent {
		n /= 100
	}
	return n, nil
}

// backgroundTokens are the theme tokens drawn behind text.
var backgroundTokens = map[string]bool{
	"selectedBg":      true,
	"searchMatchBg":   true,
	"userMessageBg":   true,
	"customMessageBg": true,
	"toolPendingBg":   true,
	"toolSuccessBg":   true,
	"toolErrorBg":     true,
}

// detectAppearance reports the background a theme is designed for.
// Indexed colors 0–15 are skipped: they follow the terminal palette.
func detectAppearance(foregrounds, backgrounds []string) string {
	fg, fgOK := averageLightness(foregrounds)
	bg, bgOK := averageLightness(backgrounds)
	switch {
	case fgOK && bgOK:
		if bg < fg {
			return "dark"
		}
		return "light"
	case bgOK:
		if bg < 0.5 {
			return "dark"
		}
		return "light"
	case fgOK:
		if fg > 0.5 {
			return "dark"
		}
		return "light"
	default:
		return ""
	}
}

func averageLightness(colors []string) (float64, bool) {
	var sum float64
	var n int
	for _, color := range colors {
		lightness, ok := concreteLightness(color)
		if !ok {
			continue
		}
		sum += lightness
		n++
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

func concreteLightness(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "#") && len(v) == 7 {
		r, err1 := strconv.ParseInt(v[1:3], 16, 0)
		g, err2 := strconv.ParseInt(v[3:5], 16, 0)
		b, err3 := strconv.ParseInt(v[5:7], 16, 0)
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, false
		}
		return oklabLightness(RGB{R: int(r), G: int(g), B: int(b)}), true
	}
	index, ok, err := normalizeIndex(v)
	if !ok || err != nil {
		return 0, false
	}
	n, _ := strconv.Atoi(index)
	if n < 16 {
		return 0, false
	}
	return oklabLightness(ansi256RGB(n)), true
}

func ansi256RGB(index int) RGB {
	basic := []RGB{
		{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
		{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
		{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
		{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
	}
	if index < 16 {
		return basic[index]
	}
	if index < 232 {
		cube := index - 16
		level := func(n int) int {
			if n == 0 {
				return 0
			}
			return 55 + n*40
		}
		return RGB{R: level(cube / 36), G: level((cube % 36) / 6), B: level(cube % 6)}
	}
	gray := 8 + (index-232)*10
	return RGB{R: gray, G: gray, B: gray}
}

func formatHex(c RGB) string {
	ch := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return v
	}
	return fmt.Sprintf("#%02x%02x%02x", ch(c.R), ch(c.G), ch(c.B))
}
