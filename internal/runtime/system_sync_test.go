package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/session"
	"github.com/Lowpower/pigo/internal/tools"
)

func countSystemMessages(m *session.Manager) int {
	n := 0
	for _, e := range m.GetBranch("") {
		if e.Type != "message" {
			continue
		}
		var role struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(e.Message, &role) == nil && role.Role == "system" {
			n++
		}
	}
	return n
}

func TestSyncSystemDeclaresOnceThenPatchesTools(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	e := &Engine{
		Tools: tools.NewRegistry(
			fnTool{name: "search", desc: "find", schema: objectSchema()},
			fnTool{name: "lookup", desc: "look", schema: objectSchema()},
		),
		Opts: Options{Session: sess, Cwd: cwd, Config: config.Config{Model: "x"}},
	}
	e.System = "built"
	first := e.syncSystem("")
	if !strings.Contains(first, cwd) || !strings.Contains(first, "You are an expert coding assistant") {
		t.Fatalf("prompt=%s", first)
	}
	if countSystemMessages(sess) != 1 {
		t.Fatalf("declarations=%d", countSystemMessages(sess))
	}
	if again := e.syncSystem(""); again != first || countSystemMessages(sess) != 1 {
		t.Fatalf("second sync wrote a patch or changed the prompt: %q count=%d", again, countSystemMessages(sess))
	}
	e.SetActiveTools([]string{"search"})
	if countSystemMessages(sess) != 2 {
		t.Fatalf("tool change should patch once, count=%d", countSystemMessages(sess))
	}
	got := session.ReplaySystem(sess.GetBranch(""))
	if len(got.Tools) != 1 || got.Tools[0].Name != "search" {
		t.Fatalf("tools=%v", got.Tools)
	}
	if !strings.Contains(got.Prompt, cwd) {
		t.Fatalf("cwd dropped: %s", got.Prompt)
	}
}

func TestForceSystemPromptOverridesPreambleOnly(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	e := &Engine{
		Tools: tools.NewRegistry(fnTool{name: "search", desc: "find", schema: objectSchema()}),
		Opts:  Options{Session: sess, Cwd: cwd, Config: config.Config{Model: "x"}},
	}
	e.System = "built-prompt"
	_ = e.syncSystem("")
	forced := e.syncSystem("LEAD")
	if e.System != "built-prompt" {
		t.Fatalf("force replaced Engine.System: %q", e.System)
	}
	if !strings.Contains(forced, "LEAD") || !strings.Contains(forced, cwd) {
		t.Fatalf("forced prompt=%s", forced)
	}
	if strings.Contains(forced, "You are an expert coding assistant") {
		t.Fatalf("default preamble still present: %s", forced)
	}
	msgs := systemPayloads(t, sess)
	last := msgs[len(msgs)-1]
	sections, _ := last["sections"].(map[string]any)
	if len(sections) != 1 || sections[prompt.SectionPreamble] != "LEAD" {
		t.Fatalf("preamble patch=%v", last)
	}
	if _, ok := last["replace"]; ok {
		t.Fatalf("replace=%v", last["replace"])
	}
	if _, ok := last["toolsRemoved"]; ok {
		t.Fatalf("toolsRemoved=%v", last["toolsRemoved"])
	}
	held := e.syncSystem("")
	if !strings.Contains(held, "LEAD") || countSystemMessages(sess) != 2 {
		t.Fatalf("preamble hold failed prompt=%s count=%d", held, countSystemMessages(sess))
	}
	e.Opts.SystemPrompt = "from-file"
	changed := e.syncSystem("")
	if !strings.Contains(changed, "from-file") || strings.Contains(changed, "LEAD") {
		t.Fatalf("built preamble change should win: %s", changed)
	}
}

func TestRunPromptPersistsSystemAndUsesReplayTools(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	var seenSystem string
	var seenTools []string
	e := &Engine{
		Tools: tools.NewRegistry(fnTool{name: "search", desc: "find", schema: objectSchema()}),
		Opts:  Options{Session: sess, Cwd: cwd, Config: config.Config{Model: "x"}},
		Stream: func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
			seenSystem = req.System
			for _, tool := range req.Tools {
				seenTools = append(seenTools, tool.Name)
			}
			return textReply("ok")(ctx, req, ai.Options{})
		},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if !strings.Contains(seenSystem, cwd) {
		t.Fatalf("request system=%s", seenSystem)
	}
	if len(seenTools) != 1 || seenTools[0] != "search" {
		t.Fatalf("tools=%v", seenTools)
	}
	if countSystemMessages(sess) != 1 {
		t.Fatalf("count=%d", countSystemMessages(sess))
	}
	if e.System == "" || !strings.Contains(e.System, cwd) {
		t.Fatalf("Engine.System=%q", e.System)
	}
}

func TestOldSessionDeclaresOnFirstRequest(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		Opts:   Options{Session: sess, Cwd: cwd, Config: config.Config{Model: "x"}},
		Stream: textReply("ok"),
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	e.AdoptSession(sess)
	if session.ReplaySystem(sess.GetBranch("")).Declared {
		t.Fatal("old session should not look declared")
	}
	_ = e.RunPrompt(context.Background(), e.History(), "next", nil).Collect()
	got := session.ReplaySystem(sess.GetBranch(""))
	if !got.Declared || !strings.Contains(got.Prompt, cwd) {
		t.Fatalf("replay=%+v", got)
	}
	msgs := session.RestoreAIMessages(session.ContextEntries(sess))
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "You are an expert") {
			t.Fatalf("system text became a user message: %+v", msg)
		}
	}
}

func TestCompactionWritesSystemCheckpoint(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	e := &Engine{
		Tools:  tools.NewRegistry(fnTool{name: "search", desc: "find", schema: objectSchema()}),
		Opts:   Options{Session: sess, Cwd: cwd, Config: config.Config{Model: "x"}},
		Stream: textReply("summary text"),
	}
	_ = e.syncSystem("")
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: strings.Repeat("a", 80)},
		{Role: ai.RoleUser, Content: "tail"},
	}
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 1}, false); err != nil {
		t.Fatal(err)
	}
	var check *session.SystemMessage
	for _, entry := range sess.GetBranch("") {
		if entry.Type == "compaction" {
			check = entry.SystemMessage
		}
	}
	if check == nil || check.Sections == nil {
		t.Fatal("compaction missing systemMessage")
	}
	got := session.ReplaySystem(sess.GetBranch(""))
	if !strings.Contains(got.Prompt, cwd) || len(got.Tools) != 1 || got.Tools[0].Name != "search" {
		t.Fatalf("replay prompt=%s tools=%v", got.Prompt, got.Tools)
	}
}

func TestBeforeAgentStartForceAndTurnPrompt(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	h := spawnRuntimeExt(t, "forcesys", nil)
	var seen string
	e := &Engine{
		Hosts: []*ext.Host{h},
		Opts:  Options{Session: sess, Cwd: cwd, Config: config.Config{Model: "x"}},
		Stream: func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
			seen = req.System
			return textReply("ok")(ctx, req, ai.Options{})
		},
	}
	e.System = "built-prompt"
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	_ = e.RunPrompt(context.Background(), nil, "hi", nil).Collect()
	if e.System != "built-prompt" {
		t.Fatalf("force replaced Engine.System: %q", e.System)
	}
	if !strings.Contains(seen, "LEAD") || !strings.Contains(seen, cwd) {
		t.Fatalf("request system=%s", seen)
	}
	msgs := systemPayloads(t, sess)
	if len(msgs) != 1 {
		t.Fatalf("first force should be the declaration, payloads=%d", len(msgs))
	}
	sections, _ := msgs[0]["sections"].(map[string]any)
	if sections[prompt.SectionPreamble] != "LEAD" || sections[prompt.SectionCwd] == "" {
		t.Fatalf("declaration=%v", msgs[0]["sections"])
	}

	h2 := spawnRuntimeExt(t, "forcesys", nil, "PIGO_FORCE_SYS=turn")
	e.Hosts = []*ext.Host{h2}
	seen = ""
	_ = e.RunPrompt(context.Background(), nil, "again", nil).Collect()
	if seen != "TURN-ONLY" {
		t.Fatalf("turn system=%q", seen)
	}
	if strings.Contains(session.ReplaySystem(sess.GetBranch("")).Prompt, "TURN-ONLY") {
		t.Fatal("turn systemPrompt was persisted")
	}
}

func TestAdoptSessionRestoresSystemAndTools(t *testing.T) {
	cwd := t.TempDir()
	sess := session.New(cwd, t.TempDir())
	src := &Engine{
		Tools: tools.NewRegistry(fnTool{name: "search", desc: "find", schema: objectSchema()}),
		Opts:  Options{Session: sess, Cwd: cwd},
	}
	_ = src.syncSystem("LEAD")
	e := &Engine{Tools: tools.NewRegistry(
		fnTool{name: "search", desc: "find", schema: objectSchema()},
		fnTool{name: "lookup", desc: "look", schema: objectSchema()},
	), Opts: Options{Cwd: cwd}}
	e.AdoptSession(sess)
	if !strings.Contains(e.System, "LEAD") || !strings.Contains(e.System, cwd) {
		t.Fatalf("restored system=%s", e.System)
	}
	names := namesOf(e.providerTools())
	if len(names) != 1 || names[0] != "search" {
		t.Fatalf("restored tools=%v", names)
	}
}

func systemPayloads(t *testing.T, m *session.Manager) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, e := range m.GetBranch("") {
		if e.Type != "message" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(e.Message, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["role"] == "system" {
			out = append(out, payload)
		}
	}
	return out
}
