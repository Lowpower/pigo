package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
)

func TestBranchSelectionPrefersVirtualUntilUnregistered(t *testing.T) {
	virtual := func(provider, id string) bool { return provider == "router" && id == "auto" }
	change := Entry{Type: "model_change", Provider: "router", ModelID: "auto"}
	entries := []Entry{
		change,
		assistantEntry(t, "vmphys", "small", "openai-responses", "stop"),
		assistantEntry(t, "router", "auto", ai.VirtualModelAPI, "error"),
	}
	provider, id, ok := BranchSelection(entries, virtual)
	if !ok || provider != "router" || id != "auto" {
		t.Fatalf("registered = %s/%s ok=%v", provider, id, ok)
	}
	provider, id, ok = BranchSelection(entries, func(string, string) bool { return false })
	if !ok || provider != "vmphys" || id != "small" {
		t.Fatalf("unregistered = %s/%s ok=%v", provider, id, ok)
	}
	later := append(entries, Entry{Type: "model_change", Provider: "openai", ModelID: "gpt-4o"})
	provider, id, ok = BranchSelection(later, func(string, string) bool { return false })
	if !ok || provider != "openai" || id != "gpt-4o" {
		t.Fatalf("later change = %s/%s ok=%v", provider, id, ok)
	}
}

func TestBranchSelectionIgnoresFailedPhysicalReplies(t *testing.T) {
	entries := []Entry{
		{Type: "model_change", Provider: "anthropic", ModelID: "claude"},
		assistantEntry(t, "anthropic", "claude", "", "stop"),
		assistantEntry(t, "anthropic", "other", "", "error"),
		assistantEntry(t, "anthropic", "aborted", "", "aborted"),
	}
	provider, id, ok := BranchSelection(entries, nil)
	if !ok || provider != "anthropic" || id != "claude" {
		t.Fatalf("selection = %s/%s ok=%v", provider, id, ok)
	}
	if _, _, ok := BranchSelection(nil, nil); ok {
		t.Fatal("empty branch reported a selection")
	}
}

func TestBranchThinkingAndVirtualState(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendThinkingLevelChange("low"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendThinkingLevelChange("high"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendCustomEntry(VirtualModelStateType, map[string]any{
		"provider": "router", "modelId": "auto", "state": map[string]any{"phase": "user"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendCustomEntry(VirtualModelStateType, map[string]any{
		"provider": "router", "modelId": "other", "state": map[string]any{"phase": "no"},
	}); err != nil {
		t.Fatal(err)
	}
	level, ok := BranchThinkingLevel(m.GetBranch(""))
	if !ok || level != "high" {
		t.Fatalf("thinking = %q ok=%v", level, ok)
	}
	state, ok := VirtualModelState(m.GetBranch(""), "router", "auto")
	if !ok || state["phase"] != "user" {
		t.Fatalf("state = %#v ok=%v", state, ok)
	}
	if _, ok := VirtualModelState(m.GetBranch(""), "router", "missing"); ok {
		t.Fatal("missing model had state")
	}
}

func TestForkKeepsVirtualStateApart(t *testing.T) {
	cwd := t.TempDir()
	agent := t.TempDir()
	m := New(cwd, agent)
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendCustomEntry(VirtualModelStateType, map[string]any{
		"provider": "router", "modelId": "auto", "state": map[string]any{"phase": "user"},
	}); err != nil {
		t.Fatal(err)
	}
	child, err := m.Fork(cwd, agent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendCustomEntry(VirtualModelStateType, map[string]any{
		"provider": "router", "modelId": "auto", "state": map[string]any{"phase": "next"},
	}); err != nil {
		t.Fatal(err)
	}
	parent, ok := VirtualModelState(m.GetBranch(""), "router", "auto")
	if !ok || parent["phase"] != "next" {
		t.Fatalf("parent = %#v", parent)
	}
	forked, ok := VirtualModelState(child.GetBranch(""), "router", "auto")
	if !ok || forked["phase"] != "user" {
		t.Fatalf("fork = %#v", forked)
	}
}

func TestLoadVersion1WithoutSelection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.jsonl")
	body := "{\"type\":\"session\",\"version\":1,\"id\":\"old\",\"timestamp\":\"2020-01-01T00:00:00Z\",\"cwd\":\"/tmp\"}\n" +
		"{\"type\":\"message\",\"id\":\"m1\",\"parentId\":null,\"timestamp\":\"2020-01-01T00:00:01Z\",\"message\":{\"role\":\"user\",\"content\":\"hi\"}}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	header, entries, skips, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if header.Version != 1 || len(entries) != 1 || len(skips) != 0 {
		t.Fatalf("header=%+v entries=%d skips=%d", header, len(entries), len(skips))
	}
	if _, _, ok := BranchSelection(entries, nil); ok {
		t.Fatal("v1 file restored a model")
	}
	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Header().Version != 1 {
		t.Fatalf("opened version = %d", opened.Header().Version)
	}
}

func assistantEntry(t *testing.T, provider, model, api, stop string) Entry {
	t.Helper()
	raw, err := json.Marshal(&ai.AssistantMessage{
		Role: ai.RoleAssistant, Provider: provider, Model: model, API: api, StopReason: ai.StopReason(stop),
	})
	if err != nil {
		t.Fatal(err)
	}
	return Entry{Type: "message", Message: raw}
}
