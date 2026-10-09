package tui

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Lowpower/pigo/internal/runtime"
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
	items      []completeItem
	prefix     string
	sel        int
	active     bool
	maxVisible int
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
	visible := c.maxVisible
	if visible <= 0 {
		visible = completeVisible
	}
	start := max(0, min(c.sel-visible/2, n-visible))
	end := min(start+visible, n)
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
	// Skill commands are stored as skill:name. Rank by the bare name first so the
	// fixed prefix does not outscore the skill, then match leftover skill commands
	// on the full name so /skill and /skill: still list them.
	primary := fuzzyFilter(cmds, q, func(c slash.Command) string { return skillBareName(c.Name) })
	seen := make(map[string]bool, len(primary))
	for _, c := range primary {
		seen[c.Name] = true
	}
	var rest []slash.Command
	for _, c := range cmds {
		if strings.HasPrefix(c.Name, "skill:") && !seen[c.Name] {
			rest = append(rest, c)
		}
	}
	filtered := append(primary, fuzzyFilter(rest, q, func(c slash.Command) string { return c.Name })...)
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

func skillBareName(name string) string {
	const prefix = "skill:"
	if strings.HasPrefix(name, prefix) {
		return name[len(prefix):]
	}
	return name
}

func commandArgSuggestions(before string, eng *runtime.Engine) (items []completeItem, prefix string, ok bool) {
	if eng == nil || !strings.HasPrefix(before, "/") || strings.HasPrefix(before, "//") {
		return nil, "", false
	}
	rest := before[1:]
	sp := strings.IndexAny(rest, " \t")
	if sp < 0 {
		return nil, "", false
	}
	name := rest[:sp]
	arg := strings.TrimLeft(rest[sp+1:], " \t")
	raw := eng.CommandArgCompletions(name, arg)
	if len(raw) == 0 {
		return nil, "", false
	}
	for _, it := range raw {
		val, _ := it["value"].(string)
		if val == "" {
			val, _ = it["label"].(string)
		}
		if val == "" {
			continue
		}
		label, _ := it["label"].(string)
		if label == "" {
			label = val
		}
		desc, _ := it["description"].(string)
		if desc == "" {
			desc, _ = it["desc"].(string)
		}
		items = append(items, completeItem{Value: val, Label: label, Desc: desc})
		if len(items) >= completeMaxItems {
			break
		}
	}
	if len(items) == 0 {
		return nil, "", false
	}
	return items, arg, true
}

func extensionAutocomplete(before string, eng *runtime.Engine) (items []completeItem, prefix string, ok bool) {
	if eng == nil || strings.TrimSpace(before) == "" {
		return nil, "", false
	}
	raw := eng.AutocompleteQuery(before)
	if len(raw) == 0 {
		return nil, "", false
	}
	for _, it := range raw {
		val, _ := it["value"].(string)
		if val == "" {
			val, _ = it["insertText"].(string)
		}
		if val == "" {
			continue
		}
		label, _ := it["label"].(string)
		if label == "" {
			label = val
		}
		desc, _ := it["description"].(string)
		items = append(items, completeItem{Value: val, Label: label, Desc: desc})
		if len(items) >= completeMaxItems {
			break
		}
	}
	if len(items) == 0 {
		return nil, "", false
	}
	token := lastPathToken(before)
	if token == "" {
		token = before
	}
	return items, token, true
}

func fileSuggestions(before, cwd string, force bool) (items []completeItem, prefix string, ok bool) {
	token := filePathToken(before)
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
			if quoted || pathNeedsQuotes(path) {
				val = "@\"" + path + "\""
			} else {
				val = "@" + path
			}
		} else if quoted || pathNeedsQuotes(path) {
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

// pathWrappers are opening marks in front of a path. They are removed only
// while the matching closer is still absent, so "(./src" completes and
// "(group)/page" stays intact.
var pathWrappers = map[rune]rune{
	'(': ')',
	'[': ']',
	'{': '}',
	'<': '>',
	'`': '`',
}

// cjkPathPunct separates prose from a path. Han, kana, and hangul letters stay
// inside the token; this set is the punctuation pi treats as a boundary.
const cjkPathPunct = "，．：；！？（）［］｛｝“”‘’…—。、「」『』《》【】"

func filePathToken(before string) string {
	runes := []rune(before)
	if q := quotedFileToken(runes); q != "" {
		return q
	}
	last := -1
	for i, r := range runes {
		if isFilePathDelimiter(r) {
			last = i
		}
	}
	return stripPathWrappers(string(runes[last+1:]))
}

func quotedFileToken(runes []rune) string {
	in := false
	start := -1
	for i, r := range runes {
		if r != '"' {
			continue
		}
		in = !in
		if in {
			start = i
		}
	}
	if !in || start < 0 {
		return ""
	}
	at := start
	if start > 0 && runes[start-1] == '@' {
		at = start - 1
	}
	if !isFileTokenStart(runes, at) {
		return ""
	}
	return string(runes[at:])
}

func isFileTokenStart(runes []rune, index int) bool {
	if index <= 0 {
		return true
	}
	switch runes[index-1] {
	case ' ', '\t', '"', '\'', '=':
		return true
	}
	start := index
	for start > 0 {
		if _, ok := pathWrappers[runes[start-1]]; !ok {
			break
		}
		start--
	}
	if start == 0 {
		return true
	}
	return isFilePathDelimiter(runes[start-1])
}

func isFilePathDelimiter(r rune) bool {
	switch r {
	case ' ', '\t', '"', '\'', '=':
		return true
	}
	return unicode.IsSpace(r) || isCJKPunct(r)
}

func isCJKPunct(r rune) bool {
	if strings.ContainsRune(cjkPathPunct, r) {
		return true
	}
	return unicode.IsPunct(r) && unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo)
}

func stripPathWrappers(token string) string {
	runes := []rune(token)
	for len(runes) > 0 {
		closer, ok := pathWrappers[runes[0]]
		if !ok || containsRune(runes[1:], closer) {
			break
		}
		runes = runes[1:]
	}
	return string(runes)
}

func containsRune(runes []rune, r rune) bool {
	for _, c := range runes {
		if c == r {
			return true
		}
	}
	return false
}

func pathNeedsQuotes(path string) bool {
	if strings.Contains(path, " ") {
		return true
	}
	for _, r := range path {
		if isCJKPunct(r) {
			return true
		}
	}
	return false
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
	quotedPrefix := strings.HasPrefix(prefix, "\"") || strings.HasPrefix(prefix, "@\"")
	if quotedPrefix && strings.HasSuffix(repl, "\"") && col < len(runes) && runes[col] == '"' {
		col++
	}
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
