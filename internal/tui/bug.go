package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/bugreport"
	"github.com/Lowpower/pigo/internal/runtime"
)

type bugSummaryMsg struct {
	args    bugreport.Args
	summary string
	err     error
}

func (m Model) startBugReport(rest string) (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	args, err := bugreport.ParseArgs(rest)
	if err != nil {
		return note(err.Error())
	}
	if m.engine == nil {
		return note("bug report requires a runtime engine")
	}
	if args.Transcript && m.engine.Opts.Session == nil {
		return note("no session to include in the bug report")
	}
	if args.Summary {
		msgs := append([]ai.Message(nil), m.history...)
		eng := m.engine
		hint := args.Hint
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		m.running = true
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("writing bug report summary…")})
		return m, func() tea.Msg {
			summary, err := eng.SummarizeBugReport(ctx, msgs, hint)
			return bugSummaryMsg{args: args, summary: summary, err: err}
		}
	}
	return m.finishBugReport(args, "")
}

func (m Model) finishBugSummary(msg bugSummaryMsg) (tea.Model, tea.Cmd) {
	m.running = false
	m.cancel = nil
	if errors.Is(msg.err, bugreport.ErrCancelled) || errors.Is(msg.err, context.Canceled) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("Bug report cancelled")})
		return m, nil
	}
	if msg.err != nil {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("bug report summary: " + msg.err.Error())})
		return m, nil
	}
	return m.finishBugReport(msg.args, msg.summary)
}

func (m Model) finishBugReport(args bugreport.Args, summary string) (tea.Model, tea.Cmd) {
	note := func(s string) (tea.Model, tea.Cmd) {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(s)})
		return m, nil
	}
	dir := m.engine.Opts.Cwd
	if dir == "" {
		dir, _ = os.Getwd()
	}
	written, err := bugreport.Write(dir, m.engine.BugInput(args.Hint, args.Transcript, summary))
	if err != nil {
		return note("bug report: " + err.Error())
	}
	if written.CrashCount > 0 {
		bugreport.ClearCrashLog(m.engine.Opts.AgentDir)
	}
	if m.engine.Opts.Session != nil {
		_, _ = m.engine.Opts.Session.AppendCustomEntry(bugreport.CustomType, bugreport.SessionData(written, args.Hint, args.Transcript, summary != ""))
	}
	link := written.URL
	if m.cfg.HyperlinksEnabled(true) {
		link = osc8(written.URL, written.URL)
	}
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("bug report: "+written.Path) + "\n" + link})
	return m, nil
}

func (m *Model) noteBugHint(msg *ai.AssistantMessage) {
	if m.bugHintShown || !bugreport.ShouldSuggestBug(msg) {
		return
	}
	m.bugHintShown = true
	if msg.ErrorMessage != "" {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.errStyle.Render(msg.ErrorMessage)})
	}
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render("If this looks like a pigo bug, /bug writes a report you can attach to a GitHub issue.")})
}

func (m *Model) noteCrash() {
	if m.engine == nil {
		return
	}
	crash := bugreport.TakeUnnotifiedCrash(m.engine.Opts.AgentDir, time.Now())
	if crash == nil {
		return
	}
	when := crash.Timestamp
	if ts, err := time.Parse(time.RFC3339Nano, crash.Timestamp); err == nil {
		when = ts.Local().Format("2006-01-02 15:04:05")
	}
	text := fmt.Sprintf("pigo crashed on %s (%s). Run /bug to report it; the crash details are attached automatically.", when, crash.Message)
	stack := ""
	if crash.Stack != nil {
		stack = *crash.Stack
	}
	if hint := bugreport.ExtensionHint(stack, m.engine.LoadedExtensions()); hint != "" {
		text += "\n" + hint
	}
	m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(text)})
}

func recordTUIPanic(eng *runtime.Engine, rec any) {
	recordPanic(eng, rec, string(debug.Stack()))
}

// recordCaughtPanic stores a panic Bubble Tea already recovered. The original
// stack was printed by Bubble Tea and is not available here.
func recordCaughtPanic(eng *runtime.Engine, rec any) {
	recordPanic(eng, rec, "")
}

func recordPanic(eng *runtime.Engine, rec any, stack string) {
	cwd, _ := os.Getwd()
	dir := ""
	sessionFile := ""
	var exts []bugreport.Extension
	if eng != nil {
		if eng.Opts.Cwd != "" {
			cwd = eng.Opts.Cwd
		}
		dir = eng.Opts.AgentDir
		exts = eng.LoadedExtensions()
		if eng.Opts.Session != nil {
			sessionFile = eng.Opts.Session.File()
		}
	}
	bugreport.RecordCrash(dir, bugreport.CrashInput{
		Kind:        bugreport.KindFatal,
		Err:         rec,
		Stack:       stack,
		SessionFile: sessionFile,
		Cwd:         cwd,
	})
	if stack != "" {
		fmt.Fprintf(os.Stderr, "pigo panicked: %v\n\n%s\n", rec, stack)
	} else {
		fmt.Fprintf(os.Stderr, "pigo panicked: %v\n", rec)
	}
	if hint := bugreport.ExtensionHint(stack, exts); hint != "" {
		fmt.Fprintln(os.Stderr, hint)
	}
	fmt.Fprintln(os.Stderr, "To report this crash: start pigo and run /bug. The crash details are attached automatically.")
}
