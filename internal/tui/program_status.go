package tui

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Lowpower/pigo/internal/ai"
)

// Program status (OSC 7501) tells a terminal whether the TUI is idle, working,
// blocked, done, or in error. The wire format matches pi v1.3.0. clear removes
// the terminal's record on exit and suspend.

const (
	stateIdle    = "idle"
	stateWorking = "working"
	stateBlocked = "blocked"
	stateDone    = "done"
	stateError   = "error"
	stateClear   = "clear"

	kindPermission = "permission"
	kindQuestion   = "question"
	kindAuth       = "auth"

	programStatusApp      = "pigo"
	programStatusQuery    = "\x1b]7501;?\x1b\\"
	deviceAttributesQuery = "\x1b[c"
	maxStatusMessageBytes = 2048
)

var (
	programStatusAppPattern   = regexp.MustCompile(`^[A-Za-z0-9_.+\-]{1,32}$`)
	programStatusReplyPattern = regexp.MustCompile("^\x1b\\]7501;\\?[^\x07\x1b]*(?:\x07|\x1b\\\\)$")
)

// progStatus is one OSC 7501 record. kind is only sent for blocked.
type progStatus struct {
	state   string
	app     string
	kind    string
	message string
}

type blockedDialog struct {
	kind    string
	message string
}

type blockedSlot struct {
	source string
	dialog blockedDialog
}

// programStatus is the interactive-mode reporter. Reports are deduplicated.
// The same pointer is shared by model copies so a status update survives Update.
type programStatus struct {
	enabled     bool
	sessionName string
	runActive   bool
	compacting  bool
	aborted     bool
	runResult   progStatus
	resting     progStatus
	blocked     []blockedSlot
	last        progStatus
	hasLast     bool
	force       bool
	emits       int
	pending     string
	clearNext   bool
}

func newProgramStatus() *programStatus {
	return &programStatus{
		runResult: progStatus{state: stateDone},
		resting:   progStatus{state: stateIdle},
	}
}

func formatProgramStatus(status progStatus) string {
	pairs := []string{"state=" + status.state}
	if status.app != "" && programStatusAppPattern.MatchString(status.app) {
		pairs = append(pairs, "app="+status.app)
	}
	if status.state == stateBlocked && status.kind != "" {
		pairs = append(pairs, "kind="+status.kind)
	}
	message := truncateUTF8(sanitizeStatusMessage(status.message), maxStatusMessageBytes)
	if message != "" {
		pairs = append(pairs, "msg="+base64.StdEncoding.EncodeToString([]byte(message)))
	}
	return "\x1b]7501;" + strings.Join(pairs, ":") + "\x1b\\"
}

func programStatusClearSequence() string {
	return formatProgramStatus(progStatus{state: stateClear})
}

func programStatusProbeRequest() string {
	return programStatusQuery + deviceAttributesQuery
}

// isProgramStatusReply reports the terminal's answer to the support query.
// Later revisions may append pairs after the question mark.
func isProgramStatusReply(sequence string) bool {
	return programStatusReplyPattern.MatchString(sequence)
}

// parseProgramStatusProbe reads a support query reply and the DA1 sentinel.
// supported is true when the terminal echoed the query before DA1. done is
// true once DA1 arrives, which means an unsupported terminal has answered.
func parseProgramStatusProbe(data []byte) (supported, done bool) {
	rest := data
	for len(rest) > 0 && !done {
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
			_, next, ok := oscEnd(rest)
			if !ok {
				return supported, false
			}
			if isProgramStatusReply(string(rest[:next])) {
				supported = true
			}
			rest = rest[next:]
		case '[':
			end, ok := csiEnd(rest)
			if !ok {
				return supported, false
			}
			if da1Pattern.Match(rest[:end]) {
				done = true
			}
			rest = rest[end:]
		default:
			rest = rest[1:]
		}
	}
	return supported, done
}

func sanitizeStatusMessage(text string) string {
	var b strings.Builder
	gap := false
	for _, r := range text {
		if r <= 0x1f || (r >= 0x7f && r <= 0x9f) {
			gap = true
			continue
		}
		if gap && b.Len() > 0 {
			b.WriteByte(' ')
		}
		gap = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func truncateUTF8(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	n := 0
	for i := 0; i < len(text); {
		_, size := utf8.DecodeRuneInString(text[i:])
		if size < 1 {
			size = 1
		}
		if n+size > maxBytes {
			break
		}
		n += size
		i += size
	}
	return text[:n]
}

func statusFirstLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Error"
	}
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	if text == "" {
		return "Error"
	}
	return text
}

func (p *programStatus) setEnabled(on bool) {
	if p == nil {
		return
	}
	p.enabled = on
	if !on {
		p.pending = ""
	}
	p.force = true
	p.report()
}

func (p *programStatus) takePending() string {
	if p == nil {
		return ""
	}
	seq := p.pending
	p.pending = ""
	return seq
}

func (p *programStatus) noteRunStart() {
	if p == nil {
		return
	}
	p.runActive = true
	p.runResult = progStatus{state: stateDone}
	p.report()
}

func (p *programStatus) noteAssistant(msg *ai.AssistantMessage) {
	if p == nil || msg == nil {
		return
	}
	if msg.StopReason == ai.StopAborted {
		p.aborted = true
	}
	if msg.StopReason == ai.StopError {
		p.runResult = progStatus{state: stateError, message: statusFirstLine(msg.ErrorMessage)}
	} else {
		p.runResult = progStatus{state: stateDone}
	}
	p.report()
}

func (p *programStatus) markAborted() {
	if p == nil {
		return
	}
	p.aborted = true
}

func (p *programStatus) noteSettle() {
	if p == nil || !p.runActive {
		return
	}
	p.runActive = false
	if p.aborted {
		p.resting = progStatus{state: stateIdle}
	} else {
		p.resting = p.runResult
	}
	p.aborted = false
	p.report()
}

func (p *programStatus) noteCompactionStart() {
	if p == nil {
		return
	}
	p.compacting = true
	p.report()
}

func (p *programStatus) noteCompactionEnd(reason string, aborted bool, errMsg string) {
	if p == nil {
		return
	}
	p.compacting = false
	if p.runActive {
		if aborted {
			p.runResult = progStatus{state: stateIdle}
		} else if strings.TrimSpace(errMsg) != "" {
			p.runResult = progStatus{state: stateError, message: statusFirstLine(errMsg)}
		}
	} else if aborted {
		p.resting = progStatus{state: stateIdle}
	} else if reason == "manual" {
		if strings.TrimSpace(errMsg) != "" {
			p.resting = progStatus{state: stateError, message: statusFirstLine(errMsg)}
		} else {
			p.resting = progStatus{state: stateDone}
		}
	}
	p.report()
}

func (p *programStatus) handleSession(ev map[string]any) {
	if p == nil {
		return
	}
	switch eventString(ev, "type") {
	case "compaction_start":
		p.noteCompactionStart()
	case "compaction_end":
		p.noteCompactionEnd(eventString(ev, "reason"), eventBool(ev, "aborted"), eventString(ev, "errorMessage"))
	default:
		return
	}
}

func (p *programStatus) setBlocked(source string, dialog *blockedDialog) {
	if p == nil {
		return
	}
	kept := make([]blockedSlot, 0, len(p.blocked)+1)
	for _, slot := range p.blocked {
		if slot.source != source {
			kept = append(kept, slot)
		}
	}
	if dialog != nil {
		kept = append(kept, blockedSlot{source: source, dialog: *dialog})
	}
	p.blocked = kept
	p.report()
}

func (p *programStatus) syncBlocked(source string, dialog *blockedDialog) {
	if p == nil {
		return
	}
	cur, ok := p.blockedDialog(source)
	if dialog == nil && !ok {
		return
	}
	if dialog != nil && ok && cur == *dialog {
		return
	}
	p.setBlocked(source, dialog)
}

func (p *programStatus) blockedDialog(source string) (blockedDialog, bool) {
	for _, slot := range p.blocked {
		if slot.source == source {
			return slot.dialog, true
		}
	}
	return blockedDialog{}, false
}

// reset forgets the previous session's run. Open dialogs stay blocked.
func (p *programStatus) reset() {
	if p == nil {
		return
	}
	p.runActive = false
	p.compacting = false
	p.aborted = false
	p.runResult = progStatus{state: stateDone}
	p.resting = progStatus{state: stateIdle}
	p.report()
}

func (p *programStatus) queueClear() {
	if p == nil {
		return
	}
	p.clearNext = true
}

func (p *programStatus) current() progStatus {
	if p == nil {
		return progStatus{state: stateIdle}
	}
	if n := len(p.blocked); n > 0 {
		d := p.blocked[n-1].dialog
		return progStatus{state: stateBlocked, app: programStatusApp, kind: d.kind, message: d.message}
	}
	if p.compacting {
		return progStatus{state: stateWorking, app: programStatusApp, message: "Compacting context"}
	}
	status := p.resting
	if p.runActive {
		status = progStatus{state: stateWorking}
	}
	status.app = programStatusApp
	if status.state == stateWorking || status.state == stateDone {
		status.message = p.sessionName
	}
	return status
}

func (p *programStatus) report() {
	if p == nil {
		return
	}
	status := p.current()
	if !p.force && p.hasLast && status == p.last {
		return
	}
	p.force = false
	p.last = status
	p.hasLast = true
	p.emits++
	if p.enabled {
		p.pending = formatProgramStatus(status)
	}
}

func (m *Model) publishProgramStatus() {
	if m == nil || m.prog == nil {
		return
	}
	m.prog.syncBlocked("login", m.loginDialog())
	m.prog.syncBlocked("extension-dialog", m.extensionDialog())
	name := m.sessionStatusName()
	if name != m.prog.sessionName {
		m.prog.sessionName = name
		m.prog.report()
	}
	if m.prog.clearNext {
		m.prog.clearNext = false
		if m.prog.enabled && m.prog.hasLast {
			m.prog.pending = programStatusClearSequence()
		}
	}
	if m.prog.pending != "" {
		m.statusSeq = m.prog.pending
		m.prog.pending = ""
	}
}

func (m Model) sessionStatusName() string {
	if m.engine == nil || m.engine.Opts.Session == nil {
		return ""
	}
	return m.engine.Opts.Session.Name()
}

func (m Model) loginDialog() *blockedDialog {
	if !m.loginActive() {
		return nil
	}
	message := "Log in"
	if provider := strings.TrimSpace(m.login.provider); provider != "" {
		message = "Log in to " + provider
	}
	return &blockedDialog{kind: kindAuth, message: message}
}

func (m Model) extensionDialog() *blockedDialog {
	if !m.extUI.active {
		return nil
	}
	switch m.extUI.method {
	case "confirm":
		return &blockedDialog{kind: kindPermission, message: m.extUI.title}
	case "select", "input", "editor":
		return &blockedDialog{kind: kindQuestion, message: m.extUI.title}
	default:
		return nil
	}
}
