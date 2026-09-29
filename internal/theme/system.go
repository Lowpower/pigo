package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const systemThemeName = "system"

// RGB is an sRGB color with channels in 0..255.
type RGB struct {
	R, G, B int
}

// SystemInput is what the terminal reported. A nil background selects the
// ANSI-index fallback. Saturation nil means 1.
type SystemInput struct {
	Foreground     *RGB
	Background     *RGB
	Palette        []RGB
	Saturation     *float64
	AppearanceHint string
}

type family struct {
	hue    float64
	satMin float64
	satMax float64
	slot   int
}

var families = map[string]family{
	"neutral":            {231.49, 0.02, 0.08, 8},
	"blue":               {231.49, 0.1, 0.68, 4},
	"green":              {158.68, 0.1, 0.76, 2},
	"red":                {20, 0.1, 0.92, 1},
	"yellow":             {82.36, 0.5, 1, 3},
	"orange":             {52, 0.12, 0.85, 3},
	"violet":             {295, 0.2, 0.6, 5},
	"calamine":           {202.43, 0.1, 0.74, 6},
	"thinkingSlate":      {231.49, 0.08, 0.2, 4},
	"thinkingBlue":       {231.49, 0.2, 0.45, 4},
	"thinkingPeriwinkle": {263.25, 0.3, 0.6, 6},
	"thinkingViolet":     {295, 0.4, 0.75, 5},
	"thinkingMagenta":    {337.5, 0.5, 0.85, 13},
	"thinkingRed":        {20, 0.95, 1, 1},
}

// tokenFamilies lists tokens in the same order as pi's TOKEN_FAMILIES.
var tokenFamilies = []struct{ token, family string }{
	{"selectedBg", "blue"},
	{"searchMatchBg", "orange"},
	{"userMessageBg", "blue"},
	{"customMessageBg", "violet"},
	{"toolPendingBg", "neutral"},
	{"toolSuccessBg", "green"},
	{"toolErrorBg", "red"},
	{"text", "neutral"},
	{"userMessageText", "neutral"},
	{"customMessageText", "neutral"},
	{"toolTitle", "neutral"},
	{"syntaxOperator", "neutral"},
	{"syntaxPunctuation", "neutral"},
	{"muted", "neutral"},
	{"dim", "neutral"},
	{"thinkingText", "neutral"},
	{"toolOutput", "neutral"},
	{"mdLinkUrl", "neutral"},
	{"mdQuote", "neutral"},
	{"mdQuoteBorder", "neutral"},
	{"mdHr", "neutral"},
	{"mdCodeBlockBorder", "neutral"},
	{"toolDiffContext", "neutral"},
	{"syntaxComment", "neutral"},
	{"scrollbarTrack", "neutral"},
	{"scrollbarThumb", "neutral"},
	{"searchMatchText", "neutral"},
	{"borderMuted", "neutral"},
	{"accent", "violet"},
	{"borderAccent", "violet"},
	{"customMessageLabel", "violet"},
	{"mdCode", "violet"},
	{"mdListBullet", "violet"},
	{"syntaxType", "violet"},
	{"border", "blue"},
	{"mdLink", "blue"},
	{"syntaxKeyword", "blue"},
	{"syntaxVariable", "calamine"},
	{"success", "green"},
	{"mdCodeBlock", "green"},
	{"toolDiffAdded", "green"},
	{"bashMode", "green"},
	{"syntaxNumber", "green"},
	{"error", "red"},
	{"toolDiffRemoved", "red"},
	{"warning", "yellow"},
	{"mdHeading", "yellow"},
	{"syntaxFunction", "yellow"},
	{"syntaxString", "orange"},
	{"thinkingOff", "neutral"},
	{"thinkingMinimal", "thinkingSlate"},
	{"thinkingLow", "thinkingBlue"},
	{"thinkingMedium", "thinkingPeriwinkle"},
	{"thinkingHigh", "thinkingViolet"},
	{"thinkingXhigh", "thinkingMagenta"},
	{"thinkingMax", "thinkingRed"},
}

var tokenSlots = map[string]int{
	"syntaxString":  2,
	"syntaxNumber":  5,
	"searchMatchBg": 3,
}

type curve struct {
	coeff  []float64
	lo, hi float64
}

var levels = map[string]map[string]curve{
	"panel": {
		"dark":  {[]float64{0.29131, -0.39746, 2.33185, -0.85524, -1.2076, 0.86276}, 0, 0.979},
		"light": {[]float64{-3.74073, 27.94549, -78.44258, 112.6798, -79.60015, 22.11277}, 0.348, 1},
	},
	"track": {
		"dark":  {[]float64{0.39028, -0.23015, 0.83573, 2.43829, -4.38292, 2.01582}, 0, 0.946},
		"light": {[]float64{-5.24921, 38.37322, -107.28833, 152.10005, -106.17127, 29.18061}, 0.368, 1},
	},
	"thinking0": {
		"dark":  {[]float64{0.52988, -0.05809, -0.30924, 4.63567, -6.52933, 2.89108}, 0, 0.873},
		"light": {[]float64{-28.27749, 182.85284, -469.62416, 603.15916, -384.59976, 97.35147}, 0.51, 1},
	},
	"thinking1": {
		"dark":  {[]float64{0.55278, -0.03667, -0.45659, 4.95347, -6.90265, 3.0706}, 0, 0.858},
		"light": {[]float64{-37.10484, 235.86282, -596.62344, 754.3633, -474.00763, 118.3551}, 0.535, 1},
	},
	"thinking2": {
		"dark":  {[]float64{0.57486, -0.01765, -0.58987, 5.25227, -7.27175, 3.25532}, 0, 0.842},
		"light": {[]float64{-59.89653, 377.05024, -945.07843, 1182.03145, -734.96375, 181.68658}, 0.556, 1},
	},
	"thinking3": {
		"dark":  {[]float64{0.59621, -0.00062, -0.71148, 5.53588, -7.6392, 3.44606}, 0, 0.827},
		"light": {[]float64{-72.07122, 445.84082, -1099.57352, 1353.88793, -829.53392, 202.26164}, 0.58, 1},
	},
	"thinking4": {
		"dark":  {[]float64{0.61691, 0.01462, -0.82288, 5.80651, -8.00641, 3.64333}, 0, 0.811},
		"light": {[]float64{-110.14338, 674.21488, -1645.75941, 2004.32367, -1215.15899, 293.3183}, 0.6, 1},
	},
	"thinking5": {
		"dark":  {[]float64{0.63702, 0.02826, -0.92498, 6.06465, -8.37246, 3.84651}, 0, 0.795},
		"light": {[]float64{-175.47701, 1063.54495, -2570.70594, 3098.80776, -1860.15527, 444.76392}, 0.62, 1},
	},
	"thinking6": {
		"dark":  {[]float64{0.65658, 0.04044, -1.01835, 6.30989, -8.73529, 4.05439}, 0, 0.779},
		"light": {[]float64{-183.81712, 1094.70055, -2602.68539, 3088.71276, -1826.91131, 430.75931}, 0.643, 1},
	},
	"subtle": {
		"dark":  {[]float64{0.56762, -0.02475, -0.5383, 5.12628, -7.10931, 3.17324}, 0, 0.848},
		"light": {[]float64{-232.85459, 1376.54473, -3249.11801, 3827.91186, -2248.29472, 526.55751}, 0.657, 1},
	},
	"thumb": {
		"dark":  {[]float64{0.60323, 0.00278, -0.73328, 5.57157, -7.68067, 3.46933}, 0, 0.823},
		"light": {[]float64{-82.89897, 511.01355, -1255.98095, 1540.76821, -940.68087, 228.58523}, 0.586, 1},
	},
	"readable": {
		"dark":  {[]float64{0.66937, 0.04704, -1.06871, 6.43941, -8.9332, 4.17229}, 0, 0.77},
		"light": {[]float64{-1554.52576, 8733.56817, -19604.93507, 21977.72696, -12300.99599, 2749.81288}, 0.751, 1},
	},
	"emphasis": {
		"dark":  {[]float64{0.7303, 0.07695, -1.31626, 7.1681, -10.14436, 4.92846}, 0, 0.712},
		"light": {[]float64{-4948.31942, 26870.91986, -58334.48399, 63280.17197, -34298.01053, 7430.30146}, 0.811, 1},
	},
	"textOnPanel": {
		"dark":  {[]float64{0.86713, 0.05232, -0.89428, 4.79014, -5.5432, 1.75023}, 0, 0.542},
		"light": {[]float64{-8570.89457, 43954.60805, -90084.00702, 92220.6791, -47152.15802, 9632.27113}, 0.867, 1},
	},
	"text": {
		"dark":  {[]float64{0.89242, 0.02311, -0.44862, 2.34417, -0.06084, -2.63844}, 0, 0.5},
		"light": {[]float64{-2004.67048, 6664.47299, -6060.70202, -1792.61209, 5133.82359, -1939.85583}, 0.894, 1},
	},
}

var readableFloor = map[string]string{"dark": "readable", "light": "subtle"}

const (
	foregroundLevel         = "emphasis"
	textMinimumWCAGContrast = 4.5
)

var foregroundTokens = map[string]bool{
	"text": true, "userMessageText": true, "toolTitle": true,
}

var panels = map[string]bool{
	"userMessageBg": true, "toolPendingBg": true, "toolSuccessBg": true,
	"toolErrorBg": true, "selectedBg": true, "searchMatchBg": true, "customMessageBg": true,
}

type rule struct {
	token string
	on    []string
	level string
}

var (
	rules      []rule
	solveOrder []string
	familyOf   map[string]string
)

func init() {
	familyOf = make(map[string]string, len(tokenFamilies))
	for _, tf := range tokenFamilies {
		familyOf[tf.token] = tf.family
	}
	rules = buildRules()
	solveOrder = buildSolveOrder()
}

func each(tokens []string, on []string, level string) []rule {
	out := make([]rule, len(tokens))
	for i, token := range tokens {
		out[i] = rule{token: token, on: on, level: level}
	}
	return out
}

func buildRules() []rule {
	toolPanels := []string{"toolPendingBg", "toolSuccessBg", "toolErrorBg"}
	messagePanels := []string{"userMessageBg", "customMessageBg"}
	panelList := []string{
		"userMessageBg", "toolPendingBg", "toolSuccessBg", "toolErrorBg",
		"selectedBg", "searchMatchBg", "customMessageBg",
	}
	var out []rule
	out = append(out, each(panelList, []string{"background"}, "panel")...)
	out = append(out, rule{"text", []string{"background"}, "text"})
	out = append(out, rule{"text", []string{"selectedBg"}, "textOnPanel"})
	out = append(out, rule{"userMessageText", []string{"userMessageBg"}, "textOnPanel"})
	out = append(out, rule{"toolTitle", toolPanels, "textOnPanel"})
	out = append(out, each([]string{"accent", "success", "error", "warning"}, append([]string{"background", "selectedBg"}, toolPanels...), "readable")...)
	out = append(out, rule{"muted", append([]string{"background", "selectedBg", "customMessageBg"}, toolPanels...), "readable"})
	out = append(out, rule{"dim", append([]string{"background", "selectedBg", "customMessageBg"}, toolPanels...), "subtle"})
	out = append(out, rule{"thinkingText", []string{"background"}, "readable"})
	out = append(out, rule{"customMessageText", append([]string{"customMessageBg"}, toolPanels...), "readable"})
	out = append(out, rule{"customMessageLabel", append([]string{"background", "customMessageBg", "selectedBg"}, toolPanels...), "readable"})
	out = append(out, rule{"toolOutput", append([]string{"background"}, toolPanels...), "readable"})
	out = append(out, each([]string{"mdHeading", "mdLink", "mdLinkUrl", "mdCode", "mdQuote", "mdCodeBlockBorder", "mdListBullet"}, append([]string{"background"}, messagePanels...), "readable")...)
	out = append(out, rule{"mdCodeBlock", append(append([]string{"background"}, messagePanels...), toolPanels...), "readable"})
	out = append(out, each([]string{"toolDiffAdded", "toolDiffRemoved", "toolDiffContext"}, append([]string{"background"}, toolPanels...), "readable")...)
	out = append(out, each([]string{
		"syntaxComment", "syntaxKeyword", "syntaxFunction", "syntaxVariable",
		"syntaxString", "syntaxNumber", "syntaxType", "syntaxOperator", "syntaxPunctuation",
	}, append(append([]string{"background"}, messagePanels...), toolPanels...), "readable")...)
	out = append(out, rule{"searchMatchText", []string{"searchMatchBg"}, "readable"})
	out = append(out, each([]string{"bashMode", "border", "borderAccent"}, []string{"background"}, "readable")...)
	out = append(out, rule{"borderMuted", []string{"background"}, "subtle"})
	out = append(out, each([]string{"mdQuoteBorder", "mdHr"}, append(append([]string{"background"}, messagePanels...), toolPanels...), "readable")...)
	out = append(out, rule{"scrollbarTrack", []string{"background"}, "track"})
	out = append(out, rule{"scrollbarThumb", []string{"scrollbarTrack"}, "thumb"})
	thinking := []string{"thinkingOff", "thinkingMinimal", "thinkingLow", "thinkingMedium", "thinkingHigh", "thinkingXhigh", "thinkingMax"}
	thinkingLevels := []string{"thinking0", "thinking1", "thinking2", "thinking3", "thinking4", "thinking5", "thinking6"}
	for i, token := range thinking {
		out = append(out, rule{token, []string{"background"}, thinkingLevels[i]})
	}
	return out
}

func buildSolveOrder() []string {
	var order []string
	seen := map[string]bool{}
	var visit func(string)
	visit = func(token string) {
		if seen[token] {
			return
		}
		seen[token] = true
		for _, rule := range rules {
			if rule.token != token {
				continue
			}
			for _, surface := range rule.on {
				if surface != "background" {
					visit(surface)
				}
			}
		}
		order = append(order, token)
	}
	for _, rule := range rules {
		visit(rule.token)
	}
	return order
}

// System builds the system theme from reported terminal colors.
func System(in SystemInput) Theme {
	gen := generateSystem(in)
	th := Theme{
		Name:       systemThemeName,
		Appearance: gen.appearance,
		Dim:        gen.dim,
		Colors:     gen.colors,
	}
	th.User = gen.colors["userMessageText"]
	th.Assistant = gen.colors["text"]
	th.Tool = gen.colors["toolTitle"]
	th.Error = gen.colors["error"]
	th.Muted = gen.colors["muted"]
	th.Accent = gen.colors["accent"]
	return th
}

// ColorFgBgAppearance classifies a COLORFGBG value by its background index.
// Indexes 0-6 and 8 are dark; 7 and 9-15 are light. Anything else is empty.
func ColorFgBgAppearance(value string) string {
	parts := strings.Split(value, ";")
	bg := strings.TrimSpace(parts[len(parts)-1])
	if bg == "" || len(bg) > 2 {
		return ""
	}
	for _, r := range bg {
		if r < '0' || r > '9' {
			return ""
		}
	}
	n, err := strconv.Atoi(bg)
	if err != nil || n > 15 {
		return ""
	}
	if n <= 6 || n == 8 {
		return "dark"
	}
	return "light"
}

func clamp(v, lo, hi float64) float64 {
	return math.Min(hi, math.Max(lo, v))
}

func saturationOf(in SystemInput) float64 {
	if in.Saturation == nil {
		return 1
	}
	return clamp(*in.Saturation, 0, 1)
}

func relativeLuminance(c RGB) float64 {
	lin := func(channel int) float64 {
		value := float64(channel) / 255
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

func wcagContrast(a, b RGB) float64 {
	la := relativeLuminance(a)
	lb := relativeLuminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

func terminalAppearance(background RGB, foreground *RGB) string {
	white := RGB{255, 255, 255}
	black := RGB{}
	whiteContrast := wcagContrast(white, background)
	blackContrast := wcagContrast(black, background)
	if foreground != nil {
		fgL := oklabLightness(*foreground)
		bgL := oklabLightness(background)
		if math.Abs(fgL-bgL) > 0.05 {
			appearance := "light"
			best := blackContrast
			if fgL > bgL {
				appearance = "dark"
				best = whiteContrast
			}
			if best >= textMinimumWCAGContrast {
				return appearance
			}
		}
	}
	if whiteContrast >= blackContrast {
		return "dark"
	}
	return "light"
}

func hexOf(c RGB) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func bellWeight(lightness float64) float64 {
	gaussian := func(x float64) float64 {
		d := x - 0.5
		return math.Exp(-(d * d) / (2 * 0.25 * 0.25))
	}
	return (gaussian(lightness) - gaussian(0)) / (1 - gaussian(0))
}

func saturationCurve(f family, lightness float64) float64 {
	floor := 1.0
	if f.satMax > 0 {
		floor = f.satMin / f.satMax
	}
	return floor + (1-floor)*bellWeight(lightness)
}

func levelTarget(level, appearance string, surfaceL float64) (float64, bool) {
	curve, ok := levels[level][appearance]
	if !ok || surfaceL < curve.lo || surfaceL > curve.hi {
		return 0, false
	}
	sum := 0.0
	pow := 1.0
	for _, coeff := range curve.coeff {
		sum += coeff * pow
		pow *= surfaceL
	}
	return sum, true
}

func slotOf(token, familyName string) int {
	if slot, ok := tokenSlots[token]; ok {
		return slot
	}
	return families[familyName].slot
}

type generated struct {
	colors     map[string]string
	dim        []string
	appearance string
}

func generateSystem(in SystemInput) generated {
	saturation := saturationOf(in)
	if in.Background == nil {
		return indexedColors(saturation, in.AppearanceHint)
	}
	var palette []okhsl
	if len(in.Palette) == 16 {
		palette = make([]okhsl, 16)
		for i, c := range in.Palette {
			palette[i] = rgbToOkhsl(c)
		}
	}
	background := *in.Background
	appearance := terminalAppearance(background, in.Foreground)
	s := &sysSolver{
		saturation:  saturation,
		palette:     palette,
		appearance:  appearance,
		lighter:     appearance == "dark",
		background:  background,
		backgroundL: oklabLightness(background),
	}
	if s.lighter {
		s.extreme = 1
	}
	colors, ok := s.solve(0)
	relaxation := 0.0
	if !ok {
		low, high := 0.0, 2.0
		colors, _ = s.solve(high)
		for i := 0; i < 20; i++ {
			middle := (low + high) / 2
			if attempt, ok := s.solve(middle); ok {
				high = middle
				colors = attempt
			} else {
				low = middle
			}
		}
		relaxation = high
	}
	if colors == nil {
		colors = map[string]RGB{"background": background}
	}
	result := map[string]string{}
	for _, tf := range tokenFamilies {
		if c, ok := colors[tf.token]; ok {
			result[tf.token] = hexOf(c)
		} else {
			result[tf.token] = ""
		}
	}
	for token := range foregroundTokens {
		surfaces := surfacesOf(token, colors, background)
		text, has := colors[token]
		if in.Foreground != nil {
			targets := make([]float64, 0, len(surfaces))
			all := true
			for _, surface := range surfaces {
				value, ok := s.target(foregroundLevel, oklabLightness(surface), relaxation)
				if !ok || value < 0 || value > 1 {
					all = false
					break
				}
				targets = append(targets, value)
			}
			if all && len(targets) > 0 {
				needed := targets[0]
				for _, v := range targets[1:] {
					if s.lighter {
						needed = math.Max(needed, v)
					} else {
						needed = math.Min(needed, v)
					}
				}
				fgL := oklabLightness(*in.Foreground)
				if (s.lighter && fgL >= needed) || (!s.lighter && fgL <= needed) {
					result[token] = ""
					continue
				}
				text = anchored(rgbToOkhsl(*in.Foreground), families["neutral"], oklabToOkhslLightness(needed), saturation)
				has = true
			}
		}
		if has {
			result[token] = hexOf(withTextContrast(text, surfaces, s.lighter))
		}
	}
	return generated{colors: result, appearance: appearance}
}

type sysSolver struct {
	saturation  float64
	palette     []okhsl
	appearance  string
	lighter     bool
	extreme     float64
	background  RGB
	backgroundL float64
}

func (s *sysSolver) paint(token string, oklabL float64) RGB {
	lightness := oklabToOkhslLightness(oklabL)
	fam := families[familyOf[token]]
	if s.palette == nil {
		sat := (fam.satMin + (fam.satMax-fam.satMin)*bellWeight(lightness)) * s.saturation
		return okhslToRgb(fam.hue, sat, lightness)
	}
	return anchored(s.palette[slotOf(token, familyOf[token])], fam, lightness, s.saturation)
}

func (s *sysSolver) target(level string, surfaceL, t float64) (float64, bool) {
	reached, ok := levelTarget(level, s.appearance, surfaceL)
	if !ok && t == 0 {
		return 0, false
	}
	base := s.extreme
	if ok {
		base = reached
	}
	distance := base - surfaceL
	floorReached, floorOK := levelTarget(readableFloor[s.appearance], s.appearance, surfaceL)
	floorBase := s.extreme
	if floorOK {
		floorBase = floorReached
	}
	floor := floorBase - surfaceL
	compressed := distance
	if math.Abs(distance) > math.Abs(floor) {
		compressed = distance - (distance-floor)*math.Min(t, 1)
	}
	return surfaceL + compressed*(1-math.Max(0, t-1)), true
}

func (s *sysSolver) limitPanel(token string, l float64) RGB {
	color := s.paint(token, l)
	extreme := RGB{}
	if s.lighter {
		extreme = RGB{255, 255, 255}
	}
	readable := func(c RGB) bool {
		return wcagContrast(extreme, c) >= textMinimumWCAGContrast
	}
	if readable(color) {
		return color
	}
	low, high := s.backgroundL, l
	for i := 0; i < 20; i++ {
		middle := (low + high) / 2
		if readable(s.paint(token, middle)) {
			low = middle
		} else {
			high = middle
		}
	}
	return s.paint(token, low)
}

func (s *sysSolver) solve(t float64) (map[string]RGB, bool) {
	colors := map[string]RGB{"background": s.background}
	for _, token := range solveOrder {
		var targets []float64
		for _, rule := range rules {
			if rule.token != token {
				continue
			}
			for _, surface := range rule.on {
				surf, ok := colors[surface]
				if !ok {
					surf = s.background
				}
				value, ok := s.target(rule.level, oklabLightness(surf), t)
				if !ok || value < 0 || value > 1 {
					return nil, false
				}
				targets = append(targets, value)
			}
		}
		l := targets[0]
		for _, v := range targets[1:] {
			if s.lighter {
				l = math.Max(l, v)
			} else {
				l = math.Min(l, v)
			}
		}
		if panels[token] {
			colors[token] = s.limitPanel(token, l)
		} else {
			colors[token] = s.paint(token, l)
		}
	}
	return colors, true
}

func surfacesOf(token string, solved map[string]RGB, background RGB) []RGB {
	var out []RGB
	for _, rule := range rules {
		if rule.token != token {
			continue
		}
		for _, surface := range rule.on {
			if c, ok := solved[surface]; ok {
				out = append(out, c)
			} else {
				out = append(out, background)
			}
		}
	}
	return out
}

func anchored(source okhsl, fam family, lightness, saturation float64) RGB {
	anchor := saturationCurve(fam, source.l)
	falloff := 1.0
	if anchor > 0 {
		falloff = math.Min(1, saturationCurve(fam, lightness)/anchor)
	}
	return okhslToRgb(source.h, source.s*falloff*saturation, lightness)
}

func withTextContrast(color RGB, surfaces []RGB, lighter bool) RGB {
	meets := func(candidate RGB) bool {
		for _, surface := range surfaces {
			if wcagContrast(candidate, surface) < textMinimumWCAGContrast {
				return false
			}
		}
		return true
	}
	if meets(color) {
		return color
	}
	src := rgbToOkhsl(color)
	at := func(lightness float64) RGB {
		return okhslToRgb(src.h, src.s, lightness)
	}
	extreme := 0.0
	if lighter {
		extreme = 1
	}
	if !meets(at(extreme)) {
		return at(extreme)
	}
	low, high := src.l, extreme
	for i := 0; i < 20; i++ {
		middle := (low + high) / 2
		if meets(at(middle)) {
			high = middle
		} else {
			low = middle
		}
	}
	return at(high)
}

func indexedColors(saturation float64, appearance string) generated {
	colors := make(map[string]string, len(tokenFamilies))
	var dim []string
	for _, tf := range tokenFamilies {
		if panels[tf.token] {
			colors[tf.token] = ""
			continue
		}
		neutral := tf.family == "neutral"
		if !neutral && saturation > 0 {
			colors[tf.token] = strconv.Itoa(slotOf(tf.token, tf.family))
		} else {
			colors[tf.token] = ""
		}
		if neutral && !foregroundTokens[tf.token] {
			dim = append(dim, tf.token)
		}
	}
	return generated{colors: colors, dim: dim, appearance: appearance}
}
