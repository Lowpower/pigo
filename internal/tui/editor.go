package tui

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/keys"
)

const (
	maxPromptHistory = 100
	pasteLineLimit   = 10
	pasteCharLimit   = 1000
)

var csiURe = regexp.MustCompile("\x1b\\[(\\d+);5u")

// promptEditor wraps bubbles textarea with history, kill-ring, paste folding, and jump-to-char.
type promptEditor struct {
	ta      textarea.Model
	prompts []string
	promptI int
	draft   string

	ring  killRing
	last  string // "", "kill", "yank"
	yankN int

	pastes map[int]string
	pasteN int
	jump   string // "", "forward", "backward"

	readImage func() *clipImage
	readText  func() string
}

func newPromptEditor() promptEditor {
	ta := textarea.New()
	ta.Placeholder = "Ask pigo…  (Enter send, Ctrl+G editor, Ctrl+L model, Ctrl+D exit)"
	ta.Prompt = "│ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	ta.KeyMap.Paste.SetEnabled(false)
	ta.KeyMap.DeleteWordBackward.SetEnabled(false)
	ta.KeyMap.DeleteWordForward.SetEnabled(false)
	ta.KeyMap.DeleteAfterCursor.SetEnabled(false)
	ta.KeyMap.DeleteBeforeCursor.SetEnabled(false)
	ta.KeyMap.LinePrevious = key.NewBinding(key.WithKeys("up"))
	ta.KeyMap.LineNext = key.NewBinding(key.WithKeys("down"))
	ta.Focus()
	return promptEditor{
		ta:      ta,
		promptI: -1,
		pastes:  map[int]string{},
	}
}

func (e *promptEditor) bashMode() bool {
	return strings.HasPrefix(strings.TrimLeft(e.ta.Value(), " \t"), "!")
}

func (e *promptEditor) refreshPrompt() {
	if e.bashMode() {
		e.ta.Prompt = "$ "
		return
	}
	e.ta.Prompt = "│ "
}

func (e *promptEditor) applyComplete(prefix string, item completeItem) {
	line, col := e.cursorLC()
	lines := strings.Split(e.ta.Value(), "\n")
	if line < 0 || line >= len(lines) {
		return
	}
	next, newCol := applyComplete(lines[line], prefix, col, item)
	lines[line] = next
	e.last = ""
	e.exitHistory()
	e.setLines(lines, line, newCol)
	e.refreshPrompt()
}

func (e *promptEditor) Value() string { return e.ta.Value() }

func (e *promptEditor) View() string { return e.ta.View() }

func (e *promptEditor) SetWidth(w int) { e.ta.SetWidth(w) }

func (e *promptEditor) SetValue(s string) {
	e.pastes = map[int]string{}
	e.pasteN = 0
	e.jump = ""
	e.last = ""
	e.yankN = 0
	e.exitHistory()
	e.ta.SetValue(s)
	e.refreshPrompt()
}

func (e *promptEditor) Reset() {
	e.SetValue("")
}

func (e *promptEditor) Expanded() string {
	text := e.ta.Value()
	for id, body := range e.pastes {
		re := regexp.MustCompile(`\[paste #` + strconv.Itoa(id) + `( (\+\d+ lines|\d+ chars))?\]`)
		text = re.ReplaceAllStringFunc(text, func(string) string { return body })
	}
	return text
}

func (e *promptEditor) AddHistory(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	if len(e.prompts) > 0 && e.prompts[0] == trimmed {
		return
	}
	e.prompts = append([]string{trimmed}, e.prompts...)
	if len(e.prompts) > maxPromptHistory {
		e.prompts = e.prompts[:maxPromptHistory]
	}
}

func (e *promptEditor) insert(text string) {
	if text == "" {
		return
	}
	e.last = ""
	e.exitHistory()
	e.ta.InsertString(text)
	e.refreshPrompt()
}

func (e *promptEditor) insertPaste(raw string) {
	e.last = ""
	e.exitHistory()
	text := normalizePaste(raw)
	if text == "" {
		return
	}
	if looksLikePath(text) && wordCharBeforeCursor(e) {
		text = " " + text
	}
	lines := strings.Split(text, "\n")
	if len(lines) > pasteLineLimit || len(text) > pasteCharLimit {
		e.pasteN++
		e.pastes[e.pasteN] = text
		marker := "[paste #" + strconv.Itoa(e.pasteN)
		if len(lines) > pasteLineLimit {
			marker += " +" + strconv.Itoa(len(lines)) + " lines]"
		} else {
			marker += " " + strconv.Itoa(len(text)) + " chars]"
		}
		e.ta.InsertString(marker)
		e.refreshPrompt()
		return
	}
	e.ta.InsertString(text)
	e.refreshPrompt()
}

func (e *promptEditor) pasteClipboard() {
	readImg := e.readImage
	if readImg == nil {
		readImg = readClipboardImage
	}
	if img := readImg(); img != nil {
		path, err := writeClipboardImage(img)
		if err == nil {
			e.insert(path)
		}
		return
	}
	readTxt := e.readText
	if readTxt == nil {
		readTxt = readClipboardText
	}
	if t := readTxt(); t != "" {
		e.insert(t)
	}
}

func (e *promptEditor) handle(msg tea.KeyMsg, kb *keys.Manager) bool {
	if msg.Paste {
		e.insertPaste(string(msg.Runes))
		return true
	}
	key := msg.String()
	switch {
	case kb.Matches(key, "tui.editor.jumpForward"):
		e.jump = "forward"
		return true
	case kb.Matches(key, "tui.editor.jumpBackward"):
		e.jump = "backward"
		return true
	case kb.Matches(key, "tui.editor.yank"):
		e.yank()
		return true
	case kb.Matches(key, "tui.editor.yankPop"):
		e.yankPop()
		return true
	case kb.Matches(key, "tui.editor.historyPrevious"):
		e.navigateHistory(-1)
		return true
	case kb.Matches(key, "tui.editor.historyNext"):
		e.navigateHistory(1)
		return true
	case kb.Matches(key, "tui.editor.deleteWordBackward"):
		e.deleteWord(true)
		return true
	case kb.Matches(key, "tui.editor.deleteWordForward"):
		e.deleteWord(false)
		return true
	case kb.Matches(key, "tui.editor.deleteToLineStart"):
		e.deleteToLineEdge(true)
		return true
	case kb.Matches(key, "tui.editor.deleteToLineEnd"):
		e.deleteToLineEdge(false)
		return true
	case kb.Matches(key, "tui.editor.cursorUp"):
		e.handleUp()
		return true
	case kb.Matches(key, "tui.editor.cursorDown"):
		e.handleDown()
		return true
	}
	return false
}

func (e *promptEditor) afterTextareaKey(msg tea.KeyMsg, kb *keys.Manager) {
	key := msg.String()
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace ||
		kb.Matches(key, "tui.input.newLine") ||
		kb.Matches(key, "tui.editor.deleteCharBackward") ||
		kb.Matches(key, "tui.editor.deleteCharForward") {
		e.last = ""
		e.exitHistory()
	}
	e.refreshPrompt()
}

func (e *promptEditor) handleUp() {
	_, col := e.cursorLC()
	if e.onFirstVisual() && (e.ta.Value() == "" || e.promptI > -1 || col == 0) {
		e.navigateHistory(-1)
		return
	}
	if e.onFirstVisual() {
		e.ta.CursorStart()
		e.last = ""
		return
	}
	e.ta.CursorUp()
	e.last = ""
}

func (e *promptEditor) handleDown() {
	if e.promptI > -1 && e.onLastVisual() {
		e.navigateHistory(1)
		return
	}
	if e.onLastVisual() {
		e.ta.CursorEnd()
		e.last = ""
		return
	}
	e.ta.CursorDown()
	e.last = ""
}

func (e *promptEditor) navigateHistory(direction int) {
	e.last = ""
	if len(e.prompts) == 0 {
		return
	}
	newIndex := e.promptI - direction
	if newIndex < -1 || newIndex >= len(e.prompts) {
		return
	}
	if e.promptI == -1 && newIndex >= 0 {
		e.draft = e.ta.Value()
	}
	e.promptI = newIndex
	if e.promptI == -1 {
		e.ta.SetValue(e.draft)
		e.draft = ""
		return
	}
	text := e.prompts[e.promptI]
	e.ta.SetValue(text)
	if direction == -1 {
		e.moveTo(0, 0)
	}
}

func (e *promptEditor) exitHistory() {
	e.promptI = -1
	e.draft = ""
}

func (e *promptEditor) yank() {
	text := e.ring.peek()
	if text == "" {
		return
	}
	e.exitHistory()
	before := e.runeOffset()
	e.ta.InsertString(text)
	e.yankN = e.runeOffset() - before
	e.last = "yank"
}

func (e *promptEditor) yankPop() {
	if e.last != "yank" || e.ring.len() <= 1 {
		return
	}
	e.deleteRunesBefore(e.yankN)
	e.ring.rotate()
	text := e.ring.peek()
	before := e.runeOffset()
	e.ta.InsertString(text)
	e.yankN = e.runeOffset() - before
	e.last = "yank"
}

func (e *promptEditor) deleteWord(back bool) {
	e.exitHistory()
	lines := strings.Split(e.ta.Value(), "\n")
	line, col := e.cursorLC()
	if line < 0 || line >= len(lines) {
		return
	}
	runes := []rune(lines[line])
	if col > len(runes) {
		col = len(runes)
	}
	if back {
		if col == 0 {
			if line > 0 {
				e.ring.push("\n", true, e.last == "kill")
				e.last = "kill"
				e.applyLineEdit(lines, line, true)
			}
			return
		}
		from := wordStart(runes, col)
		deleted := string(runes[from:col])
		e.ring.push(deleted, true, e.last == "kill")
		e.last = "kill"
		lines[line] = string(runes[:from]) + string(runes[col:])
		e.setLines(lines, line, from)
		return
	}
	if col >= len(runes) {
		if line < len(lines)-1 {
			e.ring.push("\n", false, e.last == "kill")
			e.last = "kill"
			e.applyLineEdit(lines, line, false)
		}
		return
	}
	to := wordEnd(runes, col)
	deleted := string(runes[col:to])
	e.ring.push(deleted, false, e.last == "kill")
	e.last = "kill"
	lines[line] = string(runes[:col]) + string(runes[to:])
	e.setLines(lines, line, col)
}

func (e *promptEditor) deleteToLineEdge(toStart bool) {
	e.exitHistory()
	lines := strings.Split(e.ta.Value(), "\n")
	line, col := e.cursorLC()
	if line < 0 || line >= len(lines) {
		return
	}
	runes := []rune(lines[line])
	if col > len(runes) {
		col = len(runes)
	}
	if toStart {
		if col > 0 {
			e.ring.push(string(runes[:col]), true, e.last == "kill")
			e.last = "kill"
			lines[line] = string(runes[col:])
			e.setLines(lines, line, 0)
			return
		}
		if line > 0 {
			e.ring.push("\n", true, e.last == "kill")
			e.last = "kill"
			e.applyLineEdit(lines, line, true)
		}
		return
	}
	if col < len(runes) {
		e.ring.push(string(runes[col:]), false, e.last == "kill")
		e.last = "kill"
		lines[line] = string(runes[:col])
		e.setLines(lines, line, col)
		return
	}
	if line < len(lines)-1 {
		e.ring.push("\n", false, e.last == "kill")
		e.last = "kill"
		e.applyLineEdit(lines, line, false)
	}
}

func (e *promptEditor) applyLineEdit(lines []string, line int, mergePrev bool) {
	if mergePrev {
		prev := lines[line-1]
		cur := lines[line]
		merged := prev + cur
		out := append(append([]string{}, lines[:line-1]...), merged)
		out = append(out, lines[line+1:]...)
		e.setLines(out, line-1, len([]rune(prev)))
		return
	}
	merged := lines[line] + lines[line+1]
	out := append(append([]string{}, lines[:line]...), merged)
	out = append(out, lines[line+2:]...)
	e.setLines(out, line, len([]rune(lines[line])))
}

func (e *promptEditor) jumpTo(char, dir string) {
	e.last = ""
	if char == "" {
		return
	}
	want := []rune(char)[0]
	lines := strings.Split(e.ta.Value(), "\n")
	line, col := e.cursorLC()
	forward := dir != "backward"
	step := 1
	end := len(lines)
	if !forward {
		step = -1
		end = -1
	}
	for i := line; i != end; i += step {
		runes := []rune(lines[i])
		from := 0
		to := len(runes)
		if i == line {
			if forward {
				from = col + 1
			} else {
				to = col
			}
		}
		if forward {
			for j := from; j < to; j++ {
				if runes[j] == want {
					e.moveTo(i, j)
					return
				}
			}
		} else {
			for j := to - 1; j >= from; j-- {
				if runes[j] == want {
					e.moveTo(i, j)
					return
				}
			}
		}
	}
}

func (e *promptEditor) onFirstVisual() bool {
	li := e.ta.LineInfo()
	return e.ta.Line() == 0 && li.RowOffset == 0
}

func (e *promptEditor) onLastVisual() bool {
	li := e.ta.LineInfo()
	last := e.ta.LineCount() - 1
	if last < 0 {
		last = 0
	}
	return e.ta.Line() == last && li.RowOffset >= li.Height-1
}

func (e *promptEditor) cursorLC() (line, col int) {
	line = e.ta.Line()
	li := e.ta.LineInfo()
	col = li.StartColumn + li.ColumnOffset
	if col < 0 {
		col = 0
	}
	return line, col
}

func (e *promptEditor) runeOffset() int {
	lines := strings.Split(e.ta.Value(), "\n")
	line, col := e.cursorLC()
	n := 0
	for i := 0; i < line && i < len(lines); i++ {
		n += len([]rune(lines[i])) + 1
	}
	return n + col
}

func (e *promptEditor) setLines(lines []string, line, col int) {
	e.ta.SetValue(strings.Join(lines, "\n"))
	e.moveTo(line, col)
}

func (e *promptEditor) moveTo(line, col int) {
	for safety := 0; e.ta.Line() > line && safety < 10000; safety++ {
		prev := e.ta.Line()
		e.ta.CursorUp()
		if e.ta.Line() == prev && e.ta.LineInfo().RowOffset == 0 {
			break
		}
	}
	for safety := 0; e.ta.Line() < line && safety < 10000; safety++ {
		prev := e.ta.Line()
		e.ta.CursorDown()
		if e.ta.Line() == prev {
			break
		}
	}
	e.ta.SetCursor(col)
}

func (e *promptEditor) deleteRunesBefore(n int) {
	if n <= 0 {
		return
	}
	runes := []rune(e.ta.Value())
	off := e.runeOffset()
	if n > off {
		n = off
	}
	next := string(append(runes[:off-n], runes[off:]...))
	e.ta.SetValue(next)
	e.moveToOffset(off - n)
}

func (e *promptEditor) moveToOffset(off int) {
	runes := []rune(e.ta.Value())
	if off < 0 {
		off = 0
	}
	if off > len(runes) {
		off = len(runes)
	}
	prefix := string(runes[:off])
	line := strings.Count(prefix, "\n")
	col := len([]rune(prefix))
	if i := strings.LastIndex(prefix, "\n"); i >= 0 {
		col = len([]rune(prefix[i+1:]))
	}
	e.moveTo(line, col)
}

func wordStart(line []rune, col int) int {
	i := col
	if i > len(line) {
		i = len(line)
	}
	for i > 0 && unicode.IsSpace(line[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(line[i-1]) {
		i--
	}
	return i
}

func wordEnd(line []rune, col int) int {
	i := col
	if i < 0 {
		i = 0
	}
	n := len(line)
	for i < n && unicode.IsSpace(line[i]) {
		i++
	}
	for i < n && !unicode.IsSpace(line[i]) {
		i++
	}
	return i
}

func normalizePaste(s string) string {
	s = csiURe.ReplaceAllStringFunc(s, func(m string) string {
		sub := csiURe.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		n := 0
		for _, c := range sub[1] {
			n = n*10 + int(c-'0')
		}
		if n >= 97 && n <= 122 {
			return string(rune(n - 96))
		}
		if n >= 65 && n <= 90 {
			return string(rune(n - 64))
		}
		return m
	})
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", "    ")
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r >= 32 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func looksLikePath(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '/', '~', '.':
		return true
	default:
		return false
	}
}

func wordCharBeforeCursor(e *promptEditor) bool {
	line, col := e.cursorLC()
	lines := strings.Split(e.ta.Value(), "\n")
	if line < 0 || line >= len(lines) {
		return false
	}
	runes := []rune(lines[line])
	if col <= 0 || col > len(runes) {
		return false
	}
	r := runes[col-1]
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func printableJump(msg tea.KeyMsg) (string, bool) {
	if msg.Alt || msg.Paste {
		return "", false
	}
	if msg.Type == tea.KeySpace {
		return " ", true
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && msg.Runes[0] >= 32 {
		return string(msg.Runes[0]), true
	}
	return "", false
}
