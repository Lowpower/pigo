package tui

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/session"
)

func decodeStatusMessage(sequence string) string {
	message := ""
	for _, part := range strings.Split(sequence, ":") {
		if rest, ok := strings.CutPrefix(part, "msg="); ok {
			message = rest
		}
	}
	if i := strings.IndexAny(message, "\x1b\x07"); i >= 0 {
		message = message[:i]
	}
	if message == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(message)
	if err != nil {
		return ""
	}
	return string(raw)
}

func TestFormatProgramStatus(t *testing.T) {
	got := formatProgramStatus(progStatus{
		state: stateBlocked, app: "pigo", kind: kindPermission, message: "Allow bash?",
	})
	want := "\x1b]7501;state=blocked:app=pigo:kind=permission:msg=" +
		base64.StdEncoding.EncodeToString([]byte("Allow bash?")) + "\x1b\\"
	if got != want {
		t.Fatalf("blocked = %q", got)
	}
	if formatProgramStatus(progStatus{state: stateClear}) != "\x1b]7501;state=clear\x1b\\" {
		t.Fatalf("clear = %q", formatProgramStatus(progStatus{state: stateClear}))
	}
	if formatProgramStatus(progStatus{state: stateWorking, app: "my app", kind: kindAuth, message: " \n "}) !=
		"\x1b]7501;state=working\x1b\\" {
		t.Fatal("invalid app, kind, and blank message should be omitted")
	}
	if !strings.Contains(formatProgramStatus(progStatus{state: stateIdle, app: strings.Repeat("a", 32)}), ":app="+strings.Repeat("a", 32)+"\x1b") {
		t.Fatal("32-character app should be kept")
	}
	if strings.Contains(formatProgramStatus(progStatus{state: stateIdle, app: strings.Repeat("a", 33)}), "app=") {
		t.Fatal("33-character app should be omitted")
	}
	if decodeStatusMessage(formatProgramStatus(progStatus{state: stateError, message: "first\nsecond\x1b[31m\u009bthird\t"})) !=
		"first second [31m third" {
		t.Fatalf("sanitized = %q", decodeStatusMessage(formatProgramStatus(progStatus{state: stateError, message: "first\nsecond\x1b[31m\u009bthird\t"})))
	}
	seq := formatProgramStatus(progStatus{state: stateWorking, app: "pigo", message: strings.Repeat("é", 2000)})
	if decodeStatusMessage(seq) != strings.Repeat("é", 1024) {
		t.Fatalf("truncated runes = %d", len([]rune(decodeStatusMessage(seq))))
	}
	if len(seq) > 4096 {
		t.Fatalf("sequence length = %d", len(seq))
	}
}

func TestIsProgramStatusReply(t *testing.T) {
	if !isProgramStatusReply("\x1b]7501;?\x1b\\") || !isProgramStatusReply("\x1b]7501;?\x07") {
		t.Fatal("query echo should match")
	}
	if !isProgramStatusReply("\x1b]7501;?version=2\x1b\\") {
		t.Fatal("future pairs should match")
	}
	if isProgramStatusReply("\x1b]7501;state=idle\x1b\\") || isProgramStatusReply("\x1b]11;rgb:0000/0000/0000\x07") {
		t.Fatal("reports and color replies are not the probe")
	}
}

func TestParseProgramStatusProbe(t *testing.T) {
	supported, done := parseProgramStatusProbe([]byte("\x1b]7501;?\x1b\\\x1b[?6c"))
	if !supported || !done {
		t.Fatalf("reply then DA: supported=%v done=%v", supported, done)
	}
	supported, done = parseProgramStatusProbe([]byte("\x1b]7501;?\x07\x1b[?1;2c"))
	if !supported || !done {
		t.Fatalf("BEL reply: supported=%v done=%v", supported, done)
	}
	supported, done = parseProgramStatusProbe([]byte("\x1b[?6c"))
	if supported || !done {
		t.Fatalf("DA only: supported=%v done=%v", supported, done)
	}
	supported, done = parseProgramStatusProbe([]byte("\x1b]7501;state=idle\x1b\\\x1b[?6c"))
	if supported || !done {
		t.Fatalf("status report is not support: supported=%v done=%v", supported, done)
	}
	_, done = parseProgramStatusProbe([]byte("\x1b]11;rgb:0000/0000/0000\x07"))
	if done {
		t.Fatal("color reply should not finish the probe")
	}
}

func TestProgramStatusOverride(t *testing.T) {
	if !readProgramStatus(nil, nil, time.Second, "1") {
		t.Fatal("PIGO_PROGRAM_STATUS=1 should enable without a terminal")
	}
	if readProgramStatus(nil, nil, time.Second, "0") {
		t.Fatal("PIGO_PROGRAM_STATUS=0 should disable")
	}
	if readProgramStatus(nil, nil, time.Second, "") {
		t.Fatal("unset without a terminal should stay off")
	}
	t.Setenv("PI_PROGRAM_STATUS", "1")
	t.Setenv("PIGO_PROGRAM_STATUS", "0")
	if readProgramStatus(nil, nil, time.Second, osGetenvProgramStatus()) {
		t.Fatal("PI_PROGRAM_STATUS must not enable reports")
	}
}

func TestProgramStatusReporter(t *testing.T) {
	p := newProgramStatus()
	p.sessionName = "Fix login"
	p.enabled = true
	p.report()
	if st := p.current(); st.state != stateIdle || st.app != programStatusApp || st.message != "" {
		t.Fatalf("start = %+v", st)
	}

	p.noteRunStart()
	if st := p.current(); st.state != stateWorking || st.message != "Fix login" {
		t.Fatalf("working = %+v", st)
	}
	p.noteAssistant(&ai.AssistantMessage{
		StopReason: ai.StopToolUse,
		Content:    []*ai.Content{{Type: ai.KindText, Text: "secret assistant output"}},
	})
	if st := p.current(); st.state != stateWorking || st.message != "Fix login" {
		t.Fatalf("tool round = %+v", st)
	}
	if strings.Contains(p.pending, "secret assistant output") {
		t.Fatalf("assistant text leaked: %q", p.pending)
	}
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopStop})
	p.noteSettle()
	if st := p.current(); st.state != stateDone || st.message != "Fix login" {
		t.Fatalf("done = %+v", st)
	}

	p.noteRunStart()
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: "overloaded"})
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopStop})
	p.noteSettle()
	if st := p.current(); st.state != stateDone || st.message != "Fix login" {
		t.Fatalf("retried error = %+v", st)
	}

	p.sessionName = ""
	p.noteRunStart()
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: "Invalid API key\n{details}"})
	p.noteSettle()
	if st := p.current(); st.state != stateError || st.message != "Invalid API key" {
		t.Fatalf("error = %+v", st)
	}

	p.noteRunStart()
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopAborted})
	p.noteSettle()
	if p.current().state != stateIdle {
		t.Fatalf("aborted message = %+v", p.current())
	}
	p.noteRunStart()
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopStop})
	p.markAborted()
	p.noteSettle()
	if p.current().state != stateIdle {
		t.Fatalf("aborted after success = %+v", p.current())
	}

	p.noteRunStart()
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopLength})
	p.noteCompactionStart()
	p.noteCompactionEnd("overflow", false, "Compaction failed\nstack")
	p.noteSettle()
	if st := p.current(); st.state != stateError || st.message != "Compaction failed" {
		t.Fatalf("failed recovery = %+v", st)
	}
	p.noteRunStart()
	p.noteCompactionStart()
	p.noteCompactionEnd("threshold", false, "Compaction failed")
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopStop})
	p.noteSettle()
	if p.current().state != stateDone {
		t.Fatalf("recovery then success = %+v", p.current())
	}

	p.sessionName = "Session"
	p.noteRunStart()
	p.noteCompactionStart()
	if st := p.current(); st.state != stateWorking || st.message != "Compacting context" {
		t.Fatalf("compacting = %+v", st)
	}
	p.noteCompactionEnd("threshold", false, "")
	if st := p.current(); st.state != stateWorking || st.message != "Session" {
		t.Fatalf("after compact = %+v", st)
	}
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopStop})
	p.noteSettle()
	p.noteCompactionStart()
	p.noteCompactionEnd("manual", false, "")
	if st := p.current(); st.state != stateDone || st.message != "Session" {
		t.Fatalf("manual compact = %+v", st)
	}
	p.noteCompactionStart()
	p.noteCompactionEnd("manual", false, "No model")
	if st := p.current(); st.state != stateError || st.message != "No model" {
		t.Fatalf("manual compact error = %+v", st)
	}
	p.noteCompactionStart()
	p.noteCompactionEnd("manual", true, "")
	if p.current().state != stateIdle {
		t.Fatalf("manual compact abort = %+v", p.current())
	}

	p.noteRunStart()
	p.setBlocked("extension-selector", &blockedDialog{kind: kindPermission, message: "Allow bash?"})
	p.setBlocked("login", &blockedDialog{kind: kindAuth, message: "Log in to Anthropic"})
	if st := p.current(); st.state != stateBlocked || st.kind != kindAuth || st.message != "Log in to Anthropic" {
		t.Fatalf("top dialog = %+v", st)
	}
	p.setBlocked("login", nil)
	p.noteAssistant(&ai.AssistantMessage{StopReason: ai.StopStop})
	p.noteSettle()
	if st := p.current(); st.state != stateBlocked || st.kind != kindPermission || st.message != "Allow bash?" {
		t.Fatalf("dialog under the run = %+v", st)
	}
	p.setBlocked("extension-selector", &blockedDialog{kind: kindQuestion, message: "Pick one"})
	if st := p.current(); st.state != stateBlocked || st.kind != kindQuestion || st.message != "Pick one" {
		t.Fatalf("replaced dialog = %+v", st)
	}
	before := p.emits
	p.report()
	if p.emits != before {
		t.Fatalf("duplicate report emits = %d, was %d", p.emits, before)
	}
	p.setBlocked("extension-selector", nil)
	if p.current().state != stateDone {
		t.Fatalf("dialogs closed = %+v", p.current())
	}

	p.sessionName = "New"
	p.report()
	if st := p.current(); st.state != stateDone || st.message != "New" {
		t.Fatalf("renamed = %+v", st)
	}
	p.reset()
	if p.current().state != stateIdle {
		t.Fatalf("reset = %+v", p.current())
	}
}

func TestProgramStatusViewEmitsOnce(t *testing.T) {
	m := New(testCfg())
	m.prog.setEnabled(true)
	m.statusSeq = m.prog.takePending()
	if !strings.Contains(m.View(), "state=idle") {
		t.Fatalf("startup view missing idle:\n%s", m.View())
	}
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if strings.Contains(m.View(), "7501") {
		t.Fatalf("idle repeated:\n%s", m.View())
	}

	sess := session.New(t.TempDir(), t.TempDir())
	sess.SetName("Fix login")
	m.engine = &runtime.Engine{Opts: runtime.Options{Session: sess}}
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventAgentStart}})
	view := m.View()
	if !strings.Contains(view, "state=working") || decodeStatusMessage(view) != "Fix login" {
		t.Fatalf("working view = %q", view)
	}
	if strings.Contains(view, "secret") {
		t.Fatal("startup view leaked")
	}
	m = send(m, agentEventMsg{agent.Event{
		Type: agent.EventMessageEnd,
		Assistant: &ai.AssistantMessage{
			StopReason: ai.StopToolUse,
			Content:    []*ai.Content{{Type: ai.KindText, Text: "secret assistant output"}},
		},
	}})
	if strings.Contains(m.statusSeq, "7501") || strings.Contains(m.statusSeq, "secret assistant output") {
		t.Fatalf("tool round re-emitted or leaked: %q", m.statusSeq)
	}
	m = send(m, agentEventMsg{agent.Event{
		Type:      agent.EventMessageEnd,
		Assistant: &ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: "Invalid API key\n{details}"},
	}})
	m = finishTurn(m)
	view = m.View()
	if !strings.Contains(view, "state=error") || decodeStatusMessage(view) != "Invalid API key" {
		t.Fatalf("error view = %q", view)
	}

	m.prog.setEnabled(false)
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventAgentStart}})
	if strings.Contains(m.View(), "7501") {
		t.Fatalf("disabled view emitted:\n%s", m.View())
	}
}

func TestLoginAndExtensionDialogStatus(t *testing.T) {
	m := New(testCfg())
	m.prog.setEnabled(true)
	next, _ := m.startLogin("openai")
	m = send(next.(Model), tea.WindowSizeMsg{Width: 80, Height: 24})
	view := m.View()
	if !strings.Contains(view, "state=blocked") || !strings.Contains(view, "kind=auth") || decodeStatusMessage(view) != "Log in to openai" {
		t.Fatalf("login = %q", view)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(m.View(), "state=idle") {
		t.Fatalf("login closed = %q", m.View())
	}

	reply := make(chan map[string]any, 1)
	m = send(m, extUIReqMsg{method: "confirm", args: map[string]any{"title": "Allow bash?"}, reply: reply})
	view = m.View()
	if !strings.Contains(view, "state=blocked") || !strings.Contains(view, "kind=permission") || decodeStatusMessage(view) != "Allow bash?" {
		t.Fatalf("confirm = %q", view)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.View(), "state=idle") {
		t.Fatalf("confirm closed = %q", m.View())
	}
	select {
	case <-reply:
	default:
		t.Fatal("confirm did not answer")
	}

	reply = make(chan map[string]any, 1)
	m = send(m, extUIReqMsg{method: "input", args: map[string]any{"title": "Pick one"}, reply: reply})
	if !strings.Contains(m.View(), "kind=question") || decodeStatusMessage(m.View()) != "Pick one" {
		t.Fatalf("input = %q", m.View())
	}
}

func TestCompactionSessionSwitchAndSuspend(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	sess.SetName("Session")
	m := New(testCfg())
	m.engine = &runtime.Engine{Opts: runtime.Options{Session: sess}}
	m.prog.setEnabled(true)
	m = send(m, sessionEventMsg{"type": "compaction_start", "reason": "manual"})
	if !strings.Contains(m.View(), "state=working") || decodeStatusMessage(m.View()) != "Compacting context" {
		t.Fatalf("compact start = %q", m.View())
	}
	m = send(m, sessionEventMsg{"type": "compaction_end", "reason": "manual", "aborted": false})
	if !strings.Contains(m.View(), "state=done") || decodeStatusMessage(m.View()) != "Session" {
		t.Fatalf("compact end = %q", m.View())
	}

	sess.SetName("Renamed")
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if decodeStatusMessage(m.View()) != "Renamed" {
		t.Fatalf("rename = %q", m.View())
	}

	m.reloadFromSession()
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(m.View(), "state=idle") {
		t.Fatalf("session reload = %q", m.View())
	}

	m = send(m, agentEventMsg{agent.Event{Type: agent.EventAgentStart}})
	m.running = true
	m.cancel = func() {}
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = finishTurn(m)
	if !strings.Contains(m.View(), "state=idle") {
		t.Fatalf("interrupt = %q", m.View())
	}

	m = send(m, agentEventMsg{agent.Event{Type: agent.EventAgentStart}})
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlZ})
	if !strings.Contains(m.statusSeq, "state=clear") {
		t.Fatalf("suspend = %q", m.statusSeq)
	}
	m = send(m, tea.ResumeMsg{})
	if !strings.Contains(m.View(), "state=working") {
		t.Fatalf("resume = %q", m.View())
	}
}

func TestCustomOverlayDoesNotBlockStatus(t *testing.T) {
	m := New(testCfg())
	m.prog.setEnabled(true)
	s := &extCustomState{}
	s.applyOpen(nil, map[string]any{
		"id":      "dlg",
		"overlay": true,
		"widgets": []any{map[string]any{"id": "t", "kind": "text", "props": map[string]any{"text": "PICK-ONE"}}},
	})
	m.extCustomOverlays = []*extCustomState{s}
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if st := m.prog.current(); st.state != stateIdle {
		t.Fatalf("custom overlay status = %+v", st)
	}
}

func TestProgramStatusLeakIsDropped(t *testing.T) {
	m := New(testCfg())
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]7501;?\x1b\\")})
	if m.editor.Value() != "" {
		t.Fatalf("probe reply leaked into editor: %q", m.editor.Value())
	}
}

func osGetenvProgramStatus() string {
	return os.Getenv("PIGO_PROGRAM_STATUS")
}

func finishTurn(m Model) Model {
	next, cmd := m.Update(agentEventMsg{agent.Event{Type: agent.EventAgentEnd}})
	m = next.(Model)
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	return send(m, msg)
}
