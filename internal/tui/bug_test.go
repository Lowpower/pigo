package tui

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/bugreport"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/slash"
)

func TestBugHintOnceAndSkipsRetryable(t *testing.T) {
	m := New(config.Config{})
	bad := &ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: "invalid json from provider"}
	m.applyAgentEvent(agent.Event{Type: agent.EventMessageEnd, Assistant: bad})
	m.applyAgentEvent(agent.Event{Type: agent.EventMessageEnd, Assistant: bad})
	m.applyAgentEvent(agent.Event{Type: agent.EventMessageEnd, Assistant: &ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: "429 rate limit"}})
	n := 0
	for _, e := range m.transcript {
		if strings.Contains(e.rendered, "/bug writes a report") {
			n++
		}
		if strings.Contains(e.rendered, "429") {
			t.Fatal("retryable error was surfaced as a bug hint")
		}
	}
	if n != 1 {
		t.Fatalf("hints %d", n)
	}
}

func TestSlashBugWritesZip(t *testing.T) {
	dir := t.TempDir()
	sess := session.New(dir, dir)
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	_, _ = sess.AppendMessage("assistant", map[string]any{
		"role": "assistant", "content": "yo", "stopReason": "error", "errorMessage": "boom",
	})
	m := New(config.Config{})
	m.engine = &runtime.Engine{Opts: runtime.Options{Cwd: dir, AgentDir: dir, Session: sess}}

	next, _ := m.handleSlash(slash.Command{Name: "bug", Rest: "--transcript --summary"})
	if !strings.Contains(transcriptText(next.(Model)), "usage: /bug") {
		t.Fatal(transcriptText(next.(Model)))
	}

	next, _ = m.handleSlash(slash.Command{Name: "bug", Rest: "editor froze"})
	text := transcriptText(next.(Model))
	if !strings.Contains(text, "pigo-bug-report-") || !strings.Contains(text, "https://github.com/Lowpower/pigo/issues/new?") {
		t.Fatalf("%s", text)
	}
	found := false
	for _, e := range sess.GetBranch("") {
		if e.CustomType == bugreport.CustomType {
			found = true
		}
	}
	if !found {
		t.Fatal("missing pigo.bug-report entry")
	}
}

func TestStartupCrashNotedOnce(t *testing.T) {
	dir := t.TempDir()
	if _, ok := bugreport.RecordCrash(dir, bugreport.CrashInput{Kind: bugreport.KindFatal, Err: "boom-crash", Cwd: dir}); !ok {
		t.Fatal("record")
	}
	m := New(config.Config{})
	m.engine = &runtime.Engine{Opts: runtime.Options{AgentDir: dir, Cwd: dir}}
	m.noteCrash()
	m.noteCrash()
	n := 0
	for _, e := range m.transcript {
		if strings.Contains(e.rendered, "boom-crash") && strings.Contains(e.rendered, "/bug") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("notices %d\n%s", n, transcriptText(m))
	}
}
