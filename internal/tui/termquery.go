package tui

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/theme"
)

const terminalColorQueryTimeout = 100 * time.Millisecond

// OSC 10, OSC 11, OSC 4;0-15, then DA1. DA1 is answered even when color queries are ignored.
var terminalColorQuery = func() string {
	var b strings.Builder
	b.WriteString("\x1b]10;?\x07\x1b]11;?\x07")
	for i := 0; i < 16; i++ {
		b.WriteString("\x1b]4;")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(";?\x07")
	}
	b.WriteString("\x1b[c")
	return b.String()
}()

var (
	oscColorPattern = regexp.MustCompile(`(?i)^(?:(1[01])|4;(\d{1,3}));(.*)$`)
	da1Pattern      = regexp.MustCompile(`^\x1b\[\?[\d;]*c$`)
)

type colorQueryState struct {
	foreground *theme.RGB
	background *theme.RGB
	palette    [16]theme.RGB
	have       [16]bool
	replied    map[string]bool
	done       bool
}

func newColorQueryState() *colorQueryState {
	return &colorQueryState{replied: map[string]bool{}}
}

func (q *colorQueryState) result() (theme.SystemInput, bool) {
	in := theme.SystemInput{Foreground: q.foreground, Background: q.background}
	complete := true
	for _, ok := range q.have {
		if !ok {
			complete = false
			break
		}
	}
	if complete {
		in.Palette = append([]theme.RGB(nil), q.palette[:]...)
	}
	return in, q.done || len(q.replied) >= 18
}

// parseColorReplies consumes OSC 10/11/4 replies and a DA1 response.
// The bool is true when the query is complete (DA1, or all 18 color replies).
func parseColorReplies(data []byte) (theme.SystemInput, bool) {
	q := newColorQueryState()
	rest := data
	for len(rest) > 0 && !q.done {
		i := bytes.IndexByte(rest, 0x1b)
		if i < 0 {
			break
		}
		rest = rest[i:]
		if len(rest) < 2 {
			break
		}
		switch rest[1] {
		case ']':
			content, next, ok := oscEnd(rest)
			if !ok {
				return q.result()
			}
			q.consumeOSC(rest[2:content])
			rest = rest[next:]
		case '[':
			end, ok := csiEnd(rest)
			if !ok {
				return q.result()
			}
			if da1Pattern.Match(rest[:end]) {
				q.done = true
			}
			rest = rest[end:]
		default:
			rest = rest[1:]
		}
	}
	return q.result()
}

func oscEnd(buf []byte) (content, next int, ok bool) {
	for i := 2; i < len(buf); i++ {
		if buf[i] == 0x07 {
			return i, i + 1, true
		}
		if buf[i] == 0x1b && i+1 < len(buf) && buf[i+1] == '\\' {
			return i, i + 2, true
		}
	}
	return 0, 0, false
}

func csiEnd(buf []byte) (int, bool) {
	for i := 2; i < len(buf); i++ {
		if buf[i] >= 0x40 && buf[i] <= 0x7e {
			return i + 1, true
		}
	}
	return 0, false
}

func (q *colorQueryState) consumeOSC(payload []byte) {
	// Drop a trailing ST if the caller included it. oscEnd excludes the terminator.
	raw := strings.TrimSpace(string(payload))
	m := oscColorPattern.FindStringSubmatch(raw)
	if m == nil {
		return
	}
	var key string
	var slot int
	switch {
	case m[1] == "10" || strings.EqualFold(m[1], "10"):
		key = "foreground"
	case m[1] == "11" || strings.EqualFold(m[1], "11"):
		key = "background"
	default:
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return
		}
		key = "4:" + strconv.Itoa(n)
		slot = n
	}
	if q.replied[key] {
		return
	}
	q.replied[key] = true
	rgb, ok := parseOscColorValue(m[3])
	switch key {
	case "foreground":
		if ok {
			q.foreground = &rgb
		}
	case "background":
		if ok {
			q.background = &rgb
		}
	default:
		if ok && slot >= 0 && slot < 16 {
			q.palette[slot] = rgb
			q.have[slot] = true
		}
	}
}

func parseOscColorValue(raw string) (theme.RGB, bool) {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "#") {
		hex := value[1:]
		switch len(hex) {
		case 6:
			return hexRGB(hex[:2], hex[2:4], hex[4:6])
		case 12:
			return hexRGB(hex[:4], hex[4:8], hex[8:12])
		default:
			return theme.RGB{}, false
		}
	}
	value = strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(value), "rgba:"), "rgb:")
	parts := strings.Split(value, "/")
	if len(parts) < 3 {
		return theme.RGB{}, false
	}
	return hexRGB(parts[0], parts[1], parts[2])
}

func hexRGB(rs, gs, bs string) (theme.RGB, bool) {
	r, okR := parseOscChannel(rs)
	g, okG := parseOscChannel(gs)
	b, okB := parseOscChannel(bs)
	if !okR || !okG || !okB {
		return theme.RGB{}, false
	}
	return theme.RGB{R: r, G: g, B: b}, true
}

func parseOscChannel(channel string) (int, bool) {
	channel = strings.TrimSpace(channel)
	if channel == "" || len(channel) > 8 {
		return 0, false
	}
	for _, r := range channel {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(channel, 16, 64)
	if err != nil {
		return 0, false
	}
	span := int64(1)<<uint(4*len(channel)) - 1
	if span <= 0 {
		return 0, false
	}
	return int(mathRound(float64(n) / float64(span) * 255)), true
}

func mathRound(v float64) int {
	if v < 0 {
		return int(v - 0.5)
	}
	return int(v + 0.5)
}

func systemThemeSelected(cfgName, loadedName string) bool {
	name := strings.TrimSpace(cfgName)
	if name == "" {
		name = loadedName
	}
	return strings.EqualFold(name, "system")
}

func finishSystemInput(in theme.SystemInput) theme.SystemInput {
	if in.Background != nil || in.AppearanceHint != "" {
		return in
	}
	in.AppearanceHint = theme.ColorFgBgAppearance(os.Getenv("COLORFGBG"))
	if in.AppearanceHint == "" {
		in.AppearanceHint = "dark"
	}
	return in
}
