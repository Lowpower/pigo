package runtime

import (
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestToolTimelineJoinsAnthropicRequest(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	e := &Engine{
		Provider: "anthropic",
		Tools:    tools.NewRegistry(fnTool{name: "read", desc: "read a file", schema: objectSchema()}),
		Opts: Options{
			Session: sess,
			Cwd:     cwd,
			Config:  config.Config{Model: "claude-opus-4-8"},
		},
	}
	if prompt := e.syncSystem(""); prompt == "" {
		t.Fatal("empty prompt")
	}
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	opts := ai.Options{Provider: "anthropic", Model: "claude-opus-4-8"}
	live := []ai.Message{{Role: ai.RoleUser, Content: "hi"}}
	got := e.withToolTimeline(ai.Context{Messages: live}, opts).Messages
	if len(got) != 2 || got[0].Role != ai.RoleSystem || len(got[0].ToolsAdded) != 1 || got[0].ToolsAdded[0].Name != "read" {
		t.Fatalf("messages = %+v", got)
	}
	if got[1].Content != "hi" {
		t.Fatalf("user = %+v", got[1])
	}
	again := e.withToolTimeline(ai.Context{Messages: got}, opts).Messages
	if len(again) != 2 {
		t.Fatalf("second join = %+v", again)
	}
	other := e.withToolTimeline(ai.Context{Messages: live}, ai.Options{Provider: "openai", Model: "gpt-4o"}).Messages
	if len(other) != 1 || other[0].Role != ai.RoleUser {
		t.Fatalf("openai saw tool timeline: %+v", other)
	}
}
