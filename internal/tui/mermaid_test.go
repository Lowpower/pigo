package tui

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
)

const flowLR = "```mermaid\nflowchart LR\n  A[Start] --> B[Done]\n```"

func TestTransformMermaidReplacesFlowchart(t *testing.T) {
	md := "Before\n\n" + flowLR + "\nAfter"
	got := transformMermaid(md, mermaidOpts{mode: "streaming", width: 100})
	if !strings.Contains(got, "Before") || !strings.Contains(got, "After") {
		t.Fatalf("lost surrounding text:\n%s", got)
	}
	if strings.Contains(got, "```mermaid") {
		t.Fatalf("fence should be replaced:\n%s", got)
	}
	if !strings.Contains(got, "Start") || !strings.Contains(got, "Done") {
		t.Fatalf("missing labels:\n%s", got)
	}
	if !strings.Contains(got, "┌") || !strings.Contains(got, "▶") {
		t.Fatalf("missing box art:\n%s", got)
	}
}

func TestTransformMermaidModes(t *testing.T) {
	if got := transformMermaid(flowLR, mermaidOpts{mode: "off", width: 100}); got != flowLR {
		t.Fatalf("off should keep source:\n%s", got)
	}
	if got := transformMermaid(flowLR, mermaidOpts{mode: "final", width: 100, streaming: true}); got != flowLR {
		t.Fatalf("final+streaming should keep source:\n%s", got)
	}
	if got := transformMermaid(flowLR, mermaidOpts{mode: "final", width: 100}); strings.Contains(got, "```mermaid") {
		t.Fatalf("final should render:\n%s", got)
	}
	if got := transformMermaid(flowLR, mermaidOpts{mode: "streaming", width: 100, thinking: true}); got != flowLR {
		t.Fatalf("thinking should skip:\n%s", got)
	}
}

func TestTransformMermaidKeepsUnsupportedAndOversized(t *testing.T) {
	pie := "```mermaid\npie\n  title Pets\n  \"Dogs\" : 4\n```"
	if got := transformMermaid(pie, mermaidOpts{mode: "streaming", width: 100}); got != pie {
		t.Fatalf("pie should stay:\n%s", got)
	}
	if got := transformMermaid(flowLR, mermaidOpts{mode: "streaming", width: 10}); got != flowLR {
		t.Fatalf("narrow width should keep fence:\n%s", got)
	}
}

func TestTransformMermaidStreamingPartialFence(t *testing.T) {
	partial := "```mermaid\nflowchart LR\n  A --> B"
	got := transformMermaid(partial, mermaidOpts{mode: "streaming", width: 100, streaming: true})
	if strings.Contains(got, "```mermaid") {
		t.Fatalf("partial fence should render while streaming:\n%s", got)
	}
	if !strings.Contains(got, "▶") {
		t.Fatalf("missing arrow:\n%s", got)
	}
}

func TestViewRendersMermaidOnFinalize(t *testing.T) {
	m := New(testCfg())
	m.width = 80
	md := "see\n\n" + flowLR
	m = send(m, agentEventMsg{agent.Event{Type: agent.EventMessageEnd, Assistant: &ai.AssistantMessage{
		Content: []*ai.Content{{Type: ai.KindText, Text: md}},
	}}})
	view := m.View()
	if strings.Contains(view, "```mermaid") {
		t.Fatalf("final view still has fence:\n%s", view)
	}
	if !strings.Contains(view, "Start") || !strings.Contains(view, "Done") {
		t.Fatalf("missing diagram labels:\n%s", view)
	}
}

func TestViewStreamingMermaidRespectsMode(t *testing.T) {
	m := New(testCfg())
	m.width = 80
	m.streamingActive = true
	m.streaming = flowLR
	if !strings.Contains(m.View(), "Start") || strings.Contains(m.View(), "```mermaid") {
		t.Fatalf("default streaming should render:\n%s", m.View())
	}

	m.cfg.Markdown.Mermaid = "final"
	if !strings.Contains(m.View(), "```mermaid") {
		t.Fatalf("final mode should keep fence while streaming:\n%s", m.View())
	}

	m.cfg.Markdown.Mermaid = "off"
	if !strings.Contains(m.View(), "```mermaid") {
		t.Fatalf("off should keep fence:\n%s", m.View())
	}
}
