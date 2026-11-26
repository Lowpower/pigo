package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/models"
	"github.com/Lowpower/pigo/internal/session"
)

func TestVirtualWindowUsesLastPhysicalReply(t *testing.T) {
	registerWindowModels(t)
	e := virtualWindowEngine()
	msgs := longEnoughToCompact(physReply("win386", "small", "medium", 9000))
	if got := e.ContextWindowFor(msgs); got != 8000 {
		t.Fatalf("window = %d, want physical 8000", got)
	}
	_, summary, err := e.MaybeCompact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if summary == "" {
		t.Fatal("expected compaction against the smaller physical window")
	}

	bare := longEnoughToCompact(ai.Message{
		Role: ai.RoleAssistant,
		Assistant: &ai.AssistantMessage{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopStop,
			Usage:      ai.Usage{TotalTokens: 9000},
			Content:    []*ai.Content{{Type: ai.KindText, Text: "ok"}},
		},
	})
	if got := e.ContextWindowFor(bare); got != 100000 {
		t.Fatalf("window before a route = %d, want virtual 100000", got)
	}
	_, summary, err = e.MaybeCompact(context.Background(), bare)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "" {
		t.Fatalf("compacted before any physical reply: %q", summary)
	}
}

func TestExplicitContextWindowOverridesPhysical(t *testing.T) {
	registerWindowModels(t)
	e := virtualWindowEngine()
	e.Opts.ContextWindow = 100000
	msgs := longEnoughToCompact(physReply("win386", "small", "medium", 9000))
	if got := e.ContextWindowFor(msgs); got != 100000 {
		t.Fatalf("window = %d, want explicit 100000", got)
	}
	_, summary, err := e.MaybeCompact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "" {
		t.Fatalf("explicit window compacted: %q", summary)
	}
}

func TestContextWindowFollowsBranchDispatch(t *testing.T) {
	registerWindowModels(t)
	e := virtualWindowEngine()
	sess := session.New(t.TempDir(), t.TempDir())
	e.Opts.Session = sess
	user, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	small, err := sess.AppendMessage("assistant", physReply("win386", "small", "medium", 10).Assistant)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Branch(user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("assistant", physReply("win386", "large", "low", 10).Assistant); err != nil {
		t.Fatal(err)
	}
	if got := e.ContextWindow(); got != 50000 {
		t.Fatalf("current branch window = %d, want large 50000", got)
	}
	if err := sess.Branch(small.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.ContextWindow(); got != 8000 {
		t.Fatalf("other branch window = %d, want small 8000", got)
	}
	id, thinking, ok := e.LastDispatch(nil)
	if !ok || id != "small" || thinking != "medium" {
		t.Fatalf("dispatch = %s %s ok=%v", id, thinking, ok)
	}
}

func TestUnknownReplyDoesNotUseOlderPhysicalWindow(t *testing.T) {
	registerWindowModels(t)
	e := virtualWindowEngine()
	msgs := []ai.Message{
		physReply("win386", "small", "medium", 10),
		physReply("missing", "nope", "low", 10),
	}
	if got := e.ContextWindowFor(msgs); got != 100000 {
		t.Fatalf("window = %d, want virtual 100000", got)
	}
}

func TestPhysicalSelectionKeepsItsOwnWindow(t *testing.T) {
	registerWindowModels(t)
	e := virtualWindowEngine()
	e.Provider = "win386"
	e.Opts.Config.Provider = "win386"
	e.Opts.Config.Model = "large"
	msgs := []ai.Message{physReply("win386", "small", "medium", 10)}
	if got := e.ContextWindowFor(msgs); got != 50000 {
		t.Fatalf("window = %d, want selected large 50000", got)
	}
	if _, _, ok := e.LastDispatch(msgs); ok {
		t.Fatal("physical selection reported a route")
	}
}

func TestInMemoryDispatchBeatsSession(t *testing.T) {
	registerWindowModels(t)
	e := virtualWindowEngine()
	sess := session.New(t.TempDir(), t.TempDir())
	e.Opts.Session = sess
	if _, err := sess.AppendMessage("assistant", physReply("win386", "large", "low", 10).Assistant); err != nil {
		t.Fatal(err)
	}
	msgs := []ai.Message{physReply("win386", "small", "medium", 10)}
	if got := e.ContextWindowFor(msgs); got != 8000 {
		t.Fatalf("window = %d, want in-memory small 8000", got)
	}
	failed := append([]ai.Message{}, msgs...)
	failed = append(failed, ai.Message{
		Role: ai.RoleAssistant,
		Assistant: &ai.AssistantMessage{
			Role: ai.RoleAssistant, Provider: "win386", Model: "large",
			StopReason: ai.StopError, ThinkingLevel: "low",
		},
	})
	if got := e.ContextWindowFor(failed); got != 8000 {
		t.Fatalf("error reply changed window to %d", got)
	}
}

func registerWindowModels(t *testing.T) {
	t.Helper()
	models.RegisterProvider(models.ProviderSpec{
		ID: "win386", DefaultAPI: "openai-responses",
		Models: []models.Model{
			{ID: "small", ContextWindow: 8000},
			{ID: "large", ContextWindow: 50000},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("win386") })
}

func virtualWindowEngine() *Engine {
	e := &Engine{
		Provider: "router",
		Opts: Options{Config: config.Config{
			Provider: "router", Model: "auto", Thinking: "high",
			KeepRecentTokens: 20,
		}},
		Stream: ai.ScriptedStreamFn("## Goal\nDone.", 0),
	}
	e.virtuals = map[string]virtualModel{
		"router\x00auto": {Provider: "router", ID: "auto", ContextWindow: 100000},
	}
	return e
}

func physReply(provider, id, thinking string, tokens int) ai.Message {
	return ai.Message{
		Role: ai.RoleAssistant,
		Assistant: &ai.AssistantMessage{
			Role:          ai.RoleAssistant,
			Provider:      provider,
			Model:         id,
			ThinkingLevel: thinking,
			StopReason:    ai.StopStop,
			Usage:         ai.Usage{TotalTokens: tokens},
			Content:       []*ai.Content{{Type: ai.KindText, Text: "ok"}},
		},
	}
}

func longEnoughToCompact(last ai.Message) []ai.Message {
	body := strings.Repeat("a", 800)
	return []ai.Message{
		{Role: ai.RoleUser, Content: body},
		{Role: ai.RoleUser, Content: body},
		last,
	}
}
