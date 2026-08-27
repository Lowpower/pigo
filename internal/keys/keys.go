// Package keys is the TUI/app keybinding table (ids match pi).
package keys

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Manager resolves action ids to keys (defaults, then keybindings.json).
type Manager struct {
	defs     map[string]Def
	user     map[string][]string
	resolved map[string][]string
	path     string
}

// Def is one action's default keys and help text.
type Def struct {
	Keys        []string
	Description string
}

// UseWindowsKeys reports Win32 or WSL (pi useWindowsKeybindings).
func UseWindowsKeys() bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != ""
}

// NewManager loads ~/.pigo/agent/keybindings.json when agentDir is set.
func NewManager(agentDir string) *Manager {
	return newManager(agentDir, UseWindowsKeys())
}

func newManager(agentDir string, windows bool) *Manager {
	m := &Manager{defs: defaults(windows)}
	if agentDir != "" {
		m.path = filepath.Join(agentDir, "keybindings.json")
		m.user = loadFile(m.path)
	}
	m.rebuild()
	return m
}

// Reload re-reads keybindings.json (/reload).
func (m *Manager) Reload() {
	if m.path != "" {
		m.user = loadFile(m.path)
	}
	m.rebuild()
}

// Matches reports whether key (bubbletea KeyMsg.String()) fires action.
func (m *Manager) Matches(key, action string) bool {
	want := Normalize(key)
	for _, k := range m.resolved[action] {
		if Normalize(k) == want {
			return true
		}
	}
	return false
}

// Keys returns the effective key list for action.
func (m *Manager) Keys(action string) []string {
	out := append([]string(nil), m.resolved[action]...)
	return out
}

// Description is the help string for action.
func (m *Manager) Description(action string) string {
	return m.defs[action].Description
}

// HotkeysText is /hotkeys output for bindings this TUI currently honours.
func (m *Manager) HotkeysText() string {
	var b strings.Builder
	b.WriteString("keybindings:\n")
	for _, id := range helpOrder {
		d, ok := m.defs[id]
		if !ok {
			continue
		}
		keys := m.resolved[id]
		label := strings.Join(displayKeys(keys), " / ")
		if label == "" {
			label = "(unbound)"
		}
		b.WriteString("  ")
		b.WriteString(pad(label, 22))
		b.WriteString(" ")
		b.WriteString(d.Description)
		b.WriteByte('\n')
	}
	b.WriteString("  /help                 list slash commands\n")
	return b.String()
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func displayKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, k := range keys {
		n := Normalize(k)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func (m *Manager) rebuild() {
	m.resolved = make(map[string][]string, len(m.defs))
	for id, d := range m.defs {
		if u, ok := m.user[id]; ok {
			m.resolved[id] = append([]string(nil), u...)
			continue
		}
		m.resolved[id] = append([]string(nil), d.Keys...)
	}
}

var legacyNames = map[string]string{
	"cursorUp":                 "tui.editor.cursorUp",
	"cursorDown":               "tui.editor.cursorDown",
	"cursorLeft":               "tui.editor.cursorLeft",
	"cursorRight":              "tui.editor.cursorRight",
	"cursorWordLeft":           "tui.editor.cursorWordLeft",
	"cursorWordRight":          "tui.editor.cursorWordRight",
	"cursorLineStart":          "tui.editor.cursorLineStart",
	"cursorLineEnd":            "tui.editor.cursorLineEnd",
	"jumpForward":              "tui.editor.jumpForward",
	"jumpBackward":             "tui.editor.jumpBackward",
	"pageUp":                   "tui.editor.pageUp",
	"pageDown":                 "tui.editor.pageDown",
	"deleteCharBackward":       "tui.editor.deleteCharBackward",
	"deleteCharForward":        "tui.editor.deleteCharForward",
	"deleteWordBackward":       "tui.editor.deleteWordBackward",
	"deleteWordForward":        "tui.editor.deleteWordForward",
	"deleteToLineStart":        "tui.editor.deleteToLineStart",
	"deleteToLineEnd":          "tui.editor.deleteToLineEnd",
	"yank":                     "tui.editor.yank",
	"yankPop":                  "tui.editor.yankPop",
	"undo":                     "tui.editor.undo",
	"newLine":                  "tui.input.newLine",
	"submit":                   "tui.input.submit",
	"tab":                      "tui.input.tab",
	"copy":                     "tui.input.copy",
	"selectUp":                 "tui.select.up",
	"selectDown":               "tui.select.down",
	"selectPageUp":             "tui.select.pageUp",
	"selectPageDown":           "tui.select.pageDown",
	"selectConfirm":            "tui.select.confirm",
	"selectCancel":             "tui.select.cancel",
	"interrupt":                "app.interrupt",
	"clear":                    "app.clear",
	"exit":                     "app.exit",
	"suspend":                  "app.suspend",
	"cycleThinkingLevel":       "app.thinking.cycle",
	"cycleModelForward":        "app.model.cycleForward",
	"cycleModelBackward":       "app.model.cycleBackward",
	"selectModel":              "app.model.select",
	"expandTools":              "app.tools.expand",
	"toggleThinking":           "app.thinking.toggle",
	"toggleSessionNamedFilter": "app.session.toggleNamedFilter",
	"externalEditor":           "app.editor.external",
	"followUp":                 "app.message.followUp",
	"dequeue":                  "app.message.dequeue",
	"pasteImage":               "app.clipboard.pasteImage",
	"newSession":               "app.session.new",
	"tree":                     "app.session.tree",
	"fork":                     "app.session.fork",
	"resume":                   "app.session.resume",
	"treeFoldOrUp":             "app.tree.foldOrUp",
	"treeUnfoldOrDown":         "app.tree.unfoldOrDown",
	"treeEditLabel":            "app.tree.editLabel",
	"treeToggleLabelTimestamp": "app.tree.toggleLabelTimestamp",
	"toggleSessionPath":        "app.session.togglePath",
	"toggleSessionSort":        "app.session.toggleSort",
	"renameSession":            "app.session.rename",
	"deleteSession":            "app.session.delete",
	"deleteSessionNoninvasive": "app.session.deleteNoninvasive",
}

func loadFile(path string) map[string][]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	out := map[string][]string{}
	for k, v := range raw {
		if next, ok := legacyNames[k]; ok {
			if _, exists := raw[next]; exists {
				continue
			}
			k = next
		}
		out[k] = parseKeys(v)
	}
	return out
}

// RewriteLegacyFile rewrites keybindings.json when it still uses pre-dot action ids.
func RewriteLegacyFile(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil || raw == nil {
		return false
	}
	out := map[string]any{}
	migrated := false
	for k, v := range raw {
		next := k
		if mapped, ok := legacyNames[k]; ok {
			next = mapped
			if _, exists := raw[next]; exists {
				migrated = true
				continue
			}
			if next != k {
				migrated = true
			}
		}
		out[next] = v
	}
	if !migrated {
		return false
	}
	nb, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return false
	}
	if err := os.WriteFile(path, append(nb, '\n'), 0o644); err != nil {
		return false
	}
	return true
}

func parseKeys(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		var keys []string
		for _, x := range t {
			s, ok := x.(string)
			if ok && s != "" {
				keys = append(keys, s)
			}
		}
		return keys
	default:
		return nil
	}
}

// Normalize canonicalizes a key id (ctrl+shift+p, esc, enter).
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "escape":
		return "esc"
	case "return":
		return "enter"
	}
	parts := strings.Split(s, "+")
	if len(parts) == 1 {
		if parts[0] == "escape" {
			return "esc"
		}
		if parts[0] == "return" {
			return "enter"
		}
		return parts[0]
	}
	key := parts[len(parts)-1]
	if key == "escape" {
		key = "esc"
	}
	if key == "return" {
		key = "enter"
	}
	mods := parts[:len(parts)-1]
	order := []string{"ctrl", "alt", "shift", "super"}
	var sorted []string
	have := map[string]bool{}
	for _, m := range mods {
		have[m] = true
	}
	for _, m := range order {
		if have[m] {
			sorted = append(sorted, m)
		}
	}
	// keep unknown modifiers
	for _, m := range mods {
		known := false
		for _, o := range order {
			if m == o {
				known = true
				break
			}
		}
		if !known && !contains(sorted, m) {
			sorted = append(sorted, m)
		}
	}
	if key == "" {
		return strings.Join(sorted, "+")
	}
	return strings.Join(append(sorted, key), "+")
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func strs(v ...string) []string { return v }

func defaults(windows bool) map[string]Def {
	undo := "ctrl+-"
	follow := "alt+enter"
	cycleBack := []string{"shift+ctrl+p", "ctrl+shift+p"}
	pasteImg := "ctrl+v"
	if windows {
		undo = "alt+z"
		if runtime.GOOS == "windows" {
			undo = "ctrl+z"
		}
		follow = "ctrl+q"
		cycleBack = []string{"alt+p"}
		pasteImg = "alt+v"
	}
	d := map[string]Def{
		"tui.editor.cursorUp":           {Keys: strs("up"), Description: "Move cursor up, browsing older history at the top"},
		"tui.editor.cursorDown":         {Keys: strs("down"), Description: "Move cursor down, browsing newer history at the bottom"},
		"tui.editor.historyPrevious":    {Description: "Select the previous prompt history entry"},
		"tui.editor.historyNext":        {Description: "Select the next prompt history entry"},
		"tui.editor.cursorLeft":         {Keys: strs("left", "ctrl+b"), Description: "Move cursor left"},
		"tui.editor.cursorRight":        {Keys: strs("right", "ctrl+f"), Description: "Move cursor right"},
		"tui.editor.cursorWordLeft":     {Keys: strs("alt+left", "ctrl+left", "alt+b"), Description: "Move cursor word left"},
		"tui.editor.cursorWordRight":    {Keys: strs("alt+right", "ctrl+right", "alt+f"), Description: "Move cursor word right"},
		"tui.editor.cursorLineStart":    {Keys: strs("home", "ctrl+home", "ctrl+a"), Description: "Move to line start"},
		"tui.editor.cursorLineEnd":      {Keys: strs("end", "ctrl+end", "ctrl+e"), Description: "Move to line end"},
		"tui.editor.jumpForward":        {Keys: strs("ctrl+]"), Description: "Jump forward to character"},
		"tui.editor.jumpBackward":       {Keys: strs("ctrl+alt+]"), Description: "Jump backward to character"},
		"tui.editor.pageUp":             {Keys: strs("pageup", "ctrl+pageup"), Description: "Scroll up by page"},
		"tui.editor.pageDown":           {Keys: strs("pagedown", "ctrl+pagedown"), Description: "Scroll down by page"},
		"tui.editor.deleteCharBackward": {Keys: strs("backspace"), Description: "Delete character backward"},
		"tui.editor.deleteCharForward":  {Keys: strs("delete", "ctrl+d"), Description: "Delete character forward"},
		"tui.editor.deleteWordBackward": {Keys: strs("ctrl+w", "alt+backspace"), Description: "Delete word backward"},
		"tui.editor.deleteWordForward":  {Keys: strs("alt+d", "alt+delete"), Description: "Delete word forward"},
		"tui.editor.deleteToLineStart":  {Keys: strs("ctrl+u"), Description: "Delete to line start"},
		"tui.editor.deleteToLineEnd":    {Keys: strs("ctrl+k"), Description: "Delete to line end"},
		"tui.editor.yank":               {Keys: strs("ctrl+y"), Description: "Paste most recently deleted text"},
		"tui.editor.yankPop":            {Keys: strs("alt+y"), Description: "Cycle through deleted text after yank"},
		"tui.editor.undo":               {Keys: strs(undo), Description: "Undo last edit"},
		"tui.input.newLine":             {Keys: strs("shift+enter", "ctrl+j"), Description: "newline"},
		"tui.input.submit":              {Keys: strs("enter"), Description: "send (steer while streaming)"},
		"tui.input.tab":                 {Keys: strs("tab"), Description: "Tab / autocomplete"},
		"tui.input.copy":                {Keys: strs("ctrl+c"), Description: "Copy selection"},
		"tui.select.up":                 {Keys: strs("up"), Description: "Move selection up"},
		"tui.select.down":               {Keys: strs("down"), Description: "Move selection down"},
		"tui.select.pageUp":             {Keys: strs("pageup"), Description: "Selection page up"},
		"tui.select.pageDown":           {Keys: strs("pagedown"), Description: "Selection page down"},
		"tui.select.confirm":            {Keys: strs("enter"), Description: "Confirm selection"},
		"tui.select.cancel":             {Keys: strs("esc", "ctrl+c"), Description: "Cancel selection"},
		"app.interrupt":                 {Keys: strs("esc"), Description: "interrupt current turn"},
		"app.clear":                     {Keys: strs("ctrl+c"), Description: "clear editor (twice to quit)"},
		"app.exit":                      {Keys: strs("ctrl+d"), Description: "exit when editor is empty"},
		"app.suspend":                   {Keys: strs("ctrl+z"), Description: "Suspend to background"},
		"app.thinking.cycle":            {Keys: strs("shift+tab"), Description: "cycle thinking level"},
		"app.model.cycleForward":        {Keys: strs("ctrl+p"), Description: "cycle model forward"},
		"app.model.cycleBackward":       {Keys: cycleBack, Description: "cycle model backward"},
		"app.model.select":              {Keys: strs("ctrl+l"), Description: "open model selector"},
		"app.tools.expand":              {Keys: strs("ctrl+o"), Description: "Toggle tool output"},
		"app.thinking.toggle":           {Keys: strs("ctrl+t"), Description: "Toggle thinking blocks"},
		"app.session.toggleNamedFilter": {Keys: strs("ctrl+n"), Description: "Toggle named session filter"},
		"app.editor.external":           {Keys: strs("ctrl+g"), Description: "Open external editor"},
		"app.message.copy":              {Keys: strs("ctrl+x"), Description: "Copy message to clipboard"},
		"app.message.followUp":          {Keys: strs(follow), Description: "queue follow-up while streaming"},
		"app.message.dequeue":           {Keys: strs("alt+up"), Description: "Restore queued messages"},
		"app.clipboard.pasteImage":      {Keys: strs(pasteImg), Description: "Paste image from clipboard (text fallback)"},
		"app.session.new":               {Description: "Start a new session"},
		"app.session.tree":              {Description: "Open session tree"},
		"app.session.fork":              {Description: "Fork current session"},
		"app.session.resume":            {Description: "Resume a session"},
		"app.session.togglePath":        {Keys: strs("ctrl+p"), Description: "Toggle session path display"},
		"app.session.toggleSort":        {Keys: strs("ctrl+s"), Description: "Toggle session sort mode"},
		"app.session.rename":            {Keys: strs("ctrl+r"), Description: "Rename session"},
		"app.session.delete":            {Keys: strs("ctrl+d"), Description: "Delete session"},
		"app.session.deleteNoninvasive": {Keys: strs("ctrl+backspace"), Description: "Delete session when query is empty"},
		"app.models.save":               {Keys: strs("ctrl+s"), Description: "Save model selection as default"},
		"app.models.enableAll":          {Keys: strs("ctrl+a"), Description: "Enable all models"},
		"app.models.clearAll":           {Keys: strs("ctrl+x"), Description: "Clear all models"},
		"app.models.toggleProvider":     {Keys: strs("ctrl+p"), Description: "Toggle all models for provider"},
		"app.models.reorderUp":          {Keys: strs("alt+up"), Description: "Move model up in order"},
		"app.models.reorderDown":        {Keys: strs("alt+down"), Description: "Move model down in order"},
	}
	if runtime.GOOS == "windows" {
		d["app.suspend"] = Def{Description: "Suspend to background"}
	}
	if windows {
		d["app.message.dequeue"] = Def{Keys: strs("alt+q"), Description: "Restore queued messages"}
	}
	return d
}

// helpOrder is the /hotkeys list for bindings the main editor currently honours.
var helpOrder = []string{
	"tui.input.submit",
	"app.message.followUp",
	"tui.input.newLine",
	"app.interrupt",
	"app.clear",
	"app.exit",
	"app.model.cycleForward",
	"app.model.cycleBackward",
	"app.thinking.cycle",
	"app.thinking.toggle",
	"app.tools.expand",
	"app.model.select",
	"app.editor.external",
	"app.clipboard.pasteImage",
	"tui.editor.yank",
	"tui.editor.yankPop",
	"tui.editor.jumpForward",
	"tui.editor.jumpBackward",
	"tui.input.tab",
}
