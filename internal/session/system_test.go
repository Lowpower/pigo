package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/prompt"
)

func TestReplaySystemAppliesPatchesAndReplace(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	declare := DeclareSystem(map[string]string{
		prompt.SectionPreamble: "base",
		prompt.SectionCwd:      "cwd",
		"skills":               "skill-a",
	}, []ai.Tool{{Name: "read", Description: "read a file"}, {Name: "write", Description: "write a file"}})
	if _, err := m.AppendMessage("system", declare); err != nil {
		t.Fatal(err)
	}
	patch := DiffSystem(ReplaySystem(m.GetBranch("")), map[string]string{
		prompt.SectionPreamble: "base",
		prompt.SectionCwd:      "cwd",
	}, []ai.Tool{{Name: "read", Description: "read a file"}})
	if patch == nil {
		t.Fatal("expected a patch")
	}
	if _, err := m.AppendMessage("system", *patch); err != nil {
		t.Fatal(err)
	}
	got := ReplaySystem(m.GetBranch(""))
	if !got.Declared {
		t.Fatal("expected a declaration")
	}
	if got.Sections[prompt.SectionPreamble] != "base" || got.Sections["skills"] != "" {
		t.Fatalf("sections=%v", got.Sections)
	}
	if !strings.Contains(got.Prompt, "base") || strings.Contains(got.Prompt, "skill-a") {
		t.Fatalf("prompt=%s", got.Prompt)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "read" {
		t.Fatalf("tools=%v", got.Tools)
	}

	repl := SystemMessage{Role: "system", Content: "only", Replace: true, Timestamp: 1}
	if _, err := m.AppendMessage("system", repl); err != nil {
		t.Fatal(err)
	}
	got = ReplaySystem(m.GetBranch(""))
	if got.Prompt != "only" || len(got.Tools) != 0 || len(got.Sections) != 0 {
		t.Fatalf("replace result prompt=%q sections=%v tools=%v", got.Prompt, got.Sections, got.Tools)
	}
}

func TestReplaySystemCheckpointDropsKeptSystemMessages(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendMessage("system", DeclareSystem(map[string]string{prompt.SectionPreamble: "old"}, nil)); err != nil {
		t.Fatal(err)
	}
	keep, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("system", PreamblePatch("kept")); err != nil {
		t.Fatal(err)
	}
	check := CheckpointSystem("", map[string]string{prompt.SectionPreamble: "check", prompt.SectionCwd: "dir"}, []ai.Tool{{Name: "read", Description: "r"}})
	if _, err := m.AppendCompaction("sum", keep.ID, 3, CompactionMeta{SystemMessage: &check}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("system", PreamblePatch("after")); err != nil {
		t.Fatal(err)
	}
	got := ReplaySystem(m.GetBranch(""))
	if got.Sections[prompt.SectionPreamble] != "after" || got.Sections[prompt.SectionCwd] != "dir" {
		t.Fatalf("sections=%v", got.Sections)
	}
	if strings.Contains(got.Prompt, "old") || strings.Contains(got.Prompt, "kept") {
		t.Fatalf("pre-checkpoint system leaked: %s", got.Prompt)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "read" {
		t.Fatalf("tools=%v", got.Tools)
	}
	msgs := RestoreAIMessages(ContextEntries(m))
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "old") || strings.Contains(msg.Content, "after") || msg.Role == "system" {
			t.Fatalf("system message entered model context: %+v", msg)
		}
	}
}

func TestReplaySystemWithoutCheckpointKeepsTailSystemMessages(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendMessage("system", DeclareSystem(map[string]string{prompt.SectionPreamble: "old"}, nil)); err != nil {
		t.Fatal(err)
	}
	keep, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("system", PreamblePatch("kept")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendCompaction("sum", keep.ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("system", PreamblePatch("after")); err != nil {
		t.Fatal(err)
	}
	got := ReplaySystem(m.GetBranch(""))
	if got.Sections[prompt.SectionPreamble] != "after" {
		t.Fatalf("preamble=%q", got.Sections[prompt.SectionPreamble])
	}
	if strings.Contains(got.Prompt, "old") {
		t.Fatalf("summarized system leaked: %s", got.Prompt)
	}
}

func TestSystemMessageJSONShape(t *testing.T) {
	msg := DeclareSystem(map[string]string{
		prompt.SectionPreamble: "base",
		prompt.SectionCwd:      "/tmp",
	}, []ai.Tool{{Name: "read", Description: "read a file", Parameters: map[string]any{"type": "object"}}})
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["role"] != "system" || got["content"] != "" {
		t.Fatalf("declare=%v", got)
	}
	if _, ok := got["replace"]; ok {
		t.Fatalf("replace should be omitted: %v", got)
	}
	sections, _ := got["sections"].(map[string]any)
	if sections[prompt.SectionPreamble] != "base" || sections[prompt.SectionCwd] != "/tmp" {
		t.Fatalf("sections=%v", sections)
	}
	added, _ := got["toolsAdded"].([]any)
	if len(added) != 1 {
		t.Fatalf("toolsAdded=%v", got["toolsAdded"])
	}

	patch := DiffSystem(ReplayResult{Declared: true, Sections: map[string]string{
		prompt.SectionPreamble: "base",
		"extra":                "x",
	}, Tools: []ai.Tool{{Name: "write", Description: "w"}}}, map[string]string{
		prompt.SectionPreamble: "base",
	}, nil)
	if patch == nil {
		t.Fatal("expected patch")
	}
	raw, err = json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]any{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	sections, _ = got["sections"].(map[string]any)
	if _, ok := sections["extra"]; !ok || sections["extra"] != nil {
		t.Fatalf("extra should be null: %v", sections)
	}
	if _, ok := got["toolsAdded"]; ok {
		t.Fatalf("toolsAdded=%v", got["toolsAdded"])
	}
	removed, _ := got["toolsRemoved"].([]any)
	if len(removed) != 1 {
		t.Fatalf("toolsRemoved=%v", got["toolsRemoved"])
	}
}

func TestUnknownSectionOrderIsPreserved(t *testing.T) {
	raw := []byte(`{"role":"system","content":"","sections":{"zeta":"z-body","alpha":"a-body"},"timestamp":1}`)
	var msg SystemMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendMessage("system", msg); err != nil {
		t.Fatal(err)
	}
	got := ReplaySystem(m.GetBranch(""))
	z := strings.Index(got.Prompt, "z-body")
	a := strings.Index(got.Prompt, "a-body")
	if z < 0 || a < 0 || z > a {
		t.Fatalf("prompt=%s", got.Prompt)
	}
}

func TestCompactionSystemMessageRoundTrip(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	u, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "yo"}); err != nil {
		t.Fatal(err)
	}
	check := CheckpointSystem("", map[string]string{prompt.SectionPreamble: "snap"}, []ai.Tool{{Name: "read", Description: "r"}})
	if _, err := m.AppendCompaction("sum", u.ID, 7, CompactionMeta{SystemMessage: &check}); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(m.File())
	if err != nil {
		t.Fatal(err)
	}
	got := ReplaySystem(opened.GetBranch(""))
	if got.Sections[prompt.SectionPreamble] != "snap" || len(got.Tools) != 1 {
		t.Fatalf("replay=%+v", got)
	}
}
