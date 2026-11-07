package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
)

func TestVirtualSessionRecordsDispatch(t *testing.T) {
	e, sess := virtualSession(t)
	selectVirtual(e)
	e.PersistTurn(turnMessages(t, e.RunPrompt(context.Background(), nil, "hi", nil).Collect()), 0)

	var change, assistant, state int
	for _, en := range sess.Entries() {
		switch en.Type {
		case "model_change":
			change++
			if en.Provider != "router" || en.ModelID != "auto" {
				t.Fatalf("model_change = %s/%s", en.Provider, en.ModelID)
			}
		case "custom":
			if en.CustomType != session.VirtualModelStateType {
				continue
			}
			state++
			var data struct {
				State map[string]any `json:"state"`
			}
			if json.Unmarshal(en.Data, &data) != nil || data.State["phase"] != "user" {
				t.Fatalf("state = %s", en.Data)
			}
		case "message":
			var msg ai.AssistantMessage
			if json.Unmarshal(en.Message, &msg) != nil || msg.Role != ai.RoleAssistant {
				continue
			}
			assistant++
			if msg.Provider != "vmphys384" || msg.Model != "small" || msg.API != "openai-responses" || msg.ThinkingLevel != "low" {
				t.Fatalf("assistant = %s/%s api %s thinking %s", msg.Provider, msg.Model, msg.API, msg.ThinkingLevel)
			}
			if msg.ProviderThinkingLevel != "" {
				t.Fatalf("provider thinking = %s", msg.ProviderThinkingLevel)
			}
		}
	}
	if change != 1 || assistant != 1 || state != 1 {
		t.Fatalf("change=%d assistant=%d state=%d", change, assistant, state)
	}
}

func TestVirtualSessionForkAndTree(t *testing.T) {
	e, sess := virtualSession(t)
	selectVirtual(e)
	e.PersistTurn(turnMessages(t, e.RunPrompt(context.Background(), nil, "hi", nil).Collect()), 0)

	var firstState string
	for _, en := range sess.Entries() {
		if en.Type == "custom" && en.CustomType == session.VirtualModelStateType {
			firstState = en.ID
		}
	}
	if firstState == "" {
		t.Fatal("missing route state")
	}
	child, err := sess.Fork(e.Opts.Cwd, e.Opts.AgentDir)
	if err != nil {
		t.Fatal(err)
	}
	other, err := sess.AppendCustomEntry(session.VirtualModelStateType, map[string]any{
		"provider": "router", "modelId": "auto", "state": map[string]any{"phase": "side"},
	})
	if err != nil {
		t.Fatal(err)
	}
	forked, ok := session.VirtualModelState(child.GetBranch(""), "router", "auto")
	if !ok || forked["phase"] != "user" {
		t.Fatalf("fork state = %#v", forked)
	}

	if _, err := e.NavigateTree(context.Background(), firstState, session.NavigateOpts{}); err != nil {
		t.Fatal(err)
	}
	landed, ok := session.VirtualModelState(sess.GetBranch(""), "router", "auto")
	if !ok || landed["phase"] != "user" {
		t.Fatalf("landed state = %#v", landed)
	}
	var got []ai.Options
	e.extStreams["vmphys384"] = func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		got = append(got, opts)
		return textReply("ok")(ctx, req, opts)
	}
	cont := ai.Context{Messages: []ai.Message{
		{Role: ai.RoleUser, Content: "hi"},
		{Role: ai.RoleAssistant, Assistant: &ai.AssistantMessage{
			Role: ai.RoleAssistant, Provider: "vmphys384", Model: "small",
			ThinkingLevel: "low", StopReason: ai.StopStop,
		}},
		{Role: ai.RoleToolResult, Content: "done", ToolCallID: "1"},
	}}
	if _, err := e.Stream(context.Background(), cont, ai.Options{Provider: "router", Model: "auto", Thinking: "high"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Model != "saw-user" {
		t.Fatalf("tree route = %+v", got)
	}
	side, ok := session.VirtualModelState(sess.GetBranch(other.ID), "router", "auto")
	if !ok || side["phase"] != "side" {
		t.Fatalf("other branch = %#v", side)
	}
}

func TestVirtualSessionFallsBackWhenUnregistered(t *testing.T) {
	e, sess := virtualSession(t)
	selectVirtual(e)
	e.PersistTurn(turnMessages(t, e.RunPrompt(context.Background(), nil, "hi", nil).Collect()), 0)
	before := len(sess.Entries())
	e.dropVirtual(e.Hosts[0], "router", "auto")
	e.Provider = "anthropic"
	e.Opts.Config.Provider = "anthropic"
	e.Opts.Config.Model = "claude-sonnet-4"
	e.Opts.Config.Thinking = "off"
	e.restoreSessionSelection()
	if e.Provider != "vmphys384" || e.Opts.Config.Model != "small" {
		t.Fatalf("fallback = %s/%s", e.Provider, e.Opts.Config.Model)
	}
	if e.Opts.Config.Thinking != "high" {
		t.Fatalf("thinking = %s", e.Opts.Config.Thinking)
	}
	if len(sess.Entries()) != before {
		t.Fatalf("restore appended entries: %d -> %d", before, len(sess.Entries()))
	}
}

func TestAdoptSessionKeepsCurrentModelWithoutSelection(t *testing.T) {
	dir := t.TempDir()
	sess := session.New(t.TempDir(), dir)
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Provider: "anthropic", Opts: Options{Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4", Thinking: "medium"}}}
	e.AdoptSession(sess)
	if e.Provider != "anthropic" || e.Opts.Config.Model != "claude-sonnet-4" || e.Opts.Config.Thinking != "medium" {
		t.Fatalf("model = %s/%s thinking %s", e.Provider, e.Opts.Config.Model, e.Opts.Config.Thinking)
	}

	selected := session.New(t.TempDir(), dir)
	if _, err := selected.AppendModelChange("openai", "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	if _, err := selected.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	e.Opts.CLIModel = "flagged"
	e.Opts.Config.Model = "flagged"
	before := len(selected.Entries())
	e.AdoptSession(selected)
	if e.Opts.Config.Model != "flagged" || len(selected.Entries()) != before {
		t.Fatalf("cli model = %s entries %d -> %d", e.Opts.Config.Model, before, len(selected.Entries()))
	}
}

func virtualSession(t *testing.T) (*Engine, *session.Manager) {
	t.Helper()
	low := "low"
	registerPhys(t, map[string]*string{
		"off": nil, "minimal": nil, "low": &low, "medium": nil, "high": nil, "xhigh": nil, "max": nil,
	})
	h := spawnVirtualExt(t)
	t.Cleanup(func() { _ = h.Close() })
	e := newVirtualEngine(h, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		return textReply("ok")(ctx, req, opts)
	})
	cwd := t.TempDir()
	dir := t.TempDir()
	sess := session.New(cwd, dir)
	e.Opts.Session = sess
	e.Opts.Cwd = cwd
	e.Opts.AgentDir = dir
	return e, sess
}

func selectVirtual(e *Engine) {
	e.Provider = "anthropic"
	e.Opts.Config.Provider = "anthropic"
	e.Opts.Config.Model = "claude-sonnet-4"
	e.Opts.Config.Thinking = "off"
	e.ApplyModel("router", "auto", "high")
}

func turnMessages(t *testing.T, events []agent.Event) []agent.Msg {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == agent.EventAgentEnd {
			return events[i].Messages
		}
	}
	t.Fatal("no agent_end")
	return nil
}
