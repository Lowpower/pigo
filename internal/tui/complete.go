package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/slash"
)

const completeMaxItems = 30
const completeVisible = 5

type completeItem struct {
	Value string
	Label string
	Desc  string
	Dir   bool
}

type completer struct {
	items  []completeItem
	prefix string
	sel    int
	active bool
}

func (c *completer) set(items []completeItem, prefix string) {
	if len(items) == 0 {
		c.active = false
		c.items = nil
		c.prefix = ""
		c.sel = 0
		return
	}
	c.items = items
	c.prefix = prefix
	c.active = true
	if c.sel >= len(items) {
		c.sel = 0
	}
}

func (c *completer) hide() {
	c.active = false
	c.items = nil
	c.prefix = ""
	c.sel = 0
}

func (c *completer) current() (completeItem, bool) {
	if !c.active || c.sel < 0 || c.sel >= len(c.items) {
		return completeItem{}, false
	}
	return c.items[c.sel], true
}

func (c *completer) move(delta int) {
	n := len(c.items)
	if n == 0 {
		return
	}
	c.sel = (c.sel + delta%n + n) % n
}

func (c completer) view() string {
	if !c.active || len(c.items) == 0 {
		return ""
	}
	var b strings.Builder
	n := len(c.items)
	start := max(0, min(c.sel-completeVisible/2, n-completeVisible))
	end := min(start+completeVisible, n)
	for i := start; i < end; i++ {
		it := c.items[i]
		mark := "  "
		if i == c.sel {
			mark = "→ "
		}
		b.WriteString(mark)
		b.WriteString(it.Label)
		if it.Desc != "" {
			b.WriteString("  ")
			b.WriteString(it.Desc)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func slashCommands(extra []slash.Command) []slash.Command {
	cmds := slash.Builtins()
	if len(extra) > 0 {
		cmds = append(append([]slash.Command{}, cmds...), extra...)
	}
	return cmds
}

func slashSuggestions(before string, cmds []slash.Command) (items []completeItem, prefix string, ok bool) {
	if !strings.HasPrefix(before, "/") || strings.HasPrefix(before, "//") {
		return nil, "", false
	}
	if strings.ContainsAny(before[1:], " \t") {
		return nil, "", false
	}
	q := before[1:]
	filtered := fuzzyFilter(cmds, q, func(c slash.Command) string { return c.Name })
	for _, c := range filtered {
		if len(items) >= completeMaxItems {
			break
		}
		items = append(items, completeItem{Value: c.Name, Label: "/" + c.Name, Desc: c.Description})
	}
	if len(items) == 0 {
		return nil, "", false
	}
	return items, before, true
}

func fileSuggestions(before, cwd string, force bool) (items []completeItem, prefix string, ok bool) {
	token := lastPathToken(before)
	if token == "" && !force {
		return nil, "", false
	}
	if !force && !looksLikeFileToken(token) {
		return nil, "", false
	}
	at := strings.HasPrefix(token, "@")
	raw := token
	if at {
		raw = token[1:]
	}
	quoted := strings.HasPrefix(raw, "\"")
	if quoted {
		raw = raw[1:]
	}
	entries, displayDir, filePrefix, err := listCompleteDir(raw, cwd)
	if err != nil {
		return nil, "", false
	}
	lower := strings.ToLower(filePrefix)
	for _, name := range entries {
		base := name
		dir := false
		if strings.HasSuffix(name, "/") {
			dir = true
			base = strings.TrimSuffix(name, "/")
		}
		if lower != "" && !strings.HasPrefix(strings.ToLower(base), lower) {
			continue
		}
		path := base
		if displayDir != "" {
			path = displayDir + base
		}
		if dir {
			path += "/"
		}
		val := path
		if at {
			if quoted || strings.Contains(path, " ") {
				val = "@\"" + path + "\""
			} else {
				val = "@" + path
			}
		} else if quoted || strings.Contains(path, " ") {
			val = "\"" + path + "\""
		}
		items = append(items, completeItem{Value: val, Label: base + boolSlash(dir), Dir: dir})
		if len(items) >= completeMaxItems {
			break
		}
	}
	if len(items) == 0 {
		return nil, "", false
	}
	return items, token, true
}

func boolSlash(dir bool) string {
	if dir {
		return "/"
	}
	return ""
}

func looksLikeFileToken(token string) bool {
	if token == "" {
		return false
	}
	if strings.HasPrefix(token, "@") {
		return true
	}
	return strings.Contains(token, "/") || strings.HasPrefix(token, ".") || strings.HasPrefix(token, "~/")
}

func lastPathToken(before string) string {
	if q := unclosedQuoteToken(before); q != "" {
		return q
	}
	i := len(before) - 1
	for i >= 0 {
		r := rune(before[i])
		if r == ' ' || r == '\t' || r == '=' || r == '\'' {
			break
		}
		i--
	}
	return before[i+1:]
}

func unclosedQuoteToken(before string) string {
	in := false
	start := -1
	for i := 0; i < len(before); i++ {
		if before[i] == '"' {
			in = !in
			if in {
				start = i
				if i > 0 && before[i-1] == '@' {
					start = i - 1
				}
			}
		}
	}
	if in && start >= 0 {
		return before[start:]
	}
	return ""
}

func listCompleteDir(raw, cwd string) (names []string, displayDir, filePrefix string, err error) {
	expanded := expandHome(raw)
	searchDir := cwd
	displayDir = ""
	if expanded == "" || expanded == "./" || expanded == "../" || expanded == "~" || expanded == "~/" || expanded == "/" {
		if strings.HasPrefix(raw, "~") || strings.HasPrefix(expanded, "/") {
			searchDir = expanded
			if raw == "~" {
				searchDir, _ = os.UserHomeDir()
			}
			displayDir = raw
			if displayDir != "" && !strings.HasSuffix(displayDir, "/") && displayDir != "~" {
				displayDir += "/"
			}
			if raw == "~" {
				displayDir = "~/"
			}
		} else if expanded != "" {
			searchDir = filepath.Join(cwd, expanded)
			displayDir = raw
		}
		filePrefix = ""
	} else if strings.HasSuffix(raw, "/") {
		if strings.HasPrefix(raw, "~") || strings.HasPrefix(expanded, "/") {
			searchDir = expanded
		} else {
			searchDir = filepath.Join(cwd, expanded)
		}
		displayDir = raw
		filePrefix = ""
	} else {
		dir := filepath.Dir(expanded)
		filePrefix = filepath.Base(expanded)
		if strings.HasPrefix(raw, "~") || strings.HasPrefix(expanded, "/") {
			searchDir = dir
			displayDir = filepath.Dir(raw)
			if displayDir == "." {
				displayDir = ""
			} else if !strings.HasSuffix(displayDir, "/") {
				displayDir += "/"
			}
			if strings.HasPrefix(raw, "~/") && !strings.HasPrefix(displayDir, "~") {
				displayDir = "~/" + strings.TrimPrefix(displayDir, "/")
			}
		} else {
			searchDir = filepath.Join(cwd, dir)
			if strings.Contains(raw, "/") {
				displayDir = filepath.Dir(raw)
				if displayDir == "." {
					displayDir = ""
				} else {
					if strings.HasPrefix(raw, "./") && !strings.HasPrefix(displayDir, ".") {
						displayDir = "./" + displayDir
					}
					if !strings.HasSuffix(displayDir, "/") {
						displayDir += "/"
					}
				}
			}
		}
	}
	ents, err := os.ReadDir(searchDir)
	if err != nil {
		return nil, "", "", err
	}
	var dirs, files []string
	for _, e := range ents {
		name := e.Name()
		if name == ".git" {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, name+"/")
			continue
		}
		files = append(files, name)
	}
	return append(dirs, files...), displayDir, filePrefix, nil
}

func applyComplete(line, prefix string, col int, item completeItem) (string, int) {
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}
	pre := []rune(prefix)
	start := col - len(pre)
	if start < 0 {
		start = 0
	}
	repl := item.Value
	if strings.HasPrefix(prefix, "/") && !strings.Contains(prefix[1:], "/") {
		repl = "/" + item.Value + " "
	} else if !item.Dir && !strings.HasSuffix(item.Value, " ") && !strings.HasSuffix(item.Value, "/") {
		repl += " "
	}
	out := string(runes[:start]) + repl + string(runes[col:])
	return out, start + len([]rune(repl))
}

func textBeforeCursor(value string, line, col int) string {
	lines := strings.Split(value, "\n")
	if line < 0 || line >= len(lines) {
		return value
	}
	runes := []rune(lines[line])
	if col > len(runes) {
		col = len(runes)
	}
	if col < 0 {
		col = 0
	}
	return string(runes[:col])
}
