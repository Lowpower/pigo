package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestSessionDirOverride(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	custom := t.TempDir()
	m := NewAt(cwd, agent, custom)
	if filepath.Dir(m.File()) != custom {
		t.Fatalf("dir=%s want %s", filepath.Dir(m.File()), custom)
	}
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	got, err := ContinueRecentAt(cwd, agent, custom)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != m.ID() {
		t.Fatalf("continue id=%s want %s", got.ID(), m.ID())
	}
}

func TestListSessionFilesAtFiltersByCwd(t *testing.T) {
	agent := t.TempDir()
	custom := t.TempDir()
	cwdA := t.TempDir()
	cwdB := t.TempDir()
	a := NewAt(cwdA, agent, custom)
	if _, err := a.AppendMessage("user", map[string]any{"role": "user", "content": "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	b := NewAt(cwdB, agent, custom)
	if _, err := b.AppendMessage("user", map[string]any{"role": "user", "content": "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	got, err := ContinueRecentAt(cwdA, agent, custom)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != a.ID() {
		t.Fatalf("continue cwdA id=%s want %s (B=%s)", got.ID(), a.ID(), b.ID())
	}
}

func TestSessionDirAndFilenameEncoding(t *testing.T) {
	agentDir := t.TempDir()
	m := New("/tmp/proj:x/sub", agentDir)

	wantDir := filepath.Join(agentDir, "sessions", "--tmp-proj-x-sub--")
	if got := filepath.Dir(m.File()); got != wantDir {
		t.Errorf("session dir = %q, want %q", got, wantDir)
	}

	base := filepath.Base(m.File())
	// e.g. 2026-08-24T09-33-00-123Z_<uuid>.jsonl — no ':' or '.' in the timestamp.
	if !regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}-[0-9]{3}Z_[0-9a-f-]{36}\.jsonl$`).MatchString(base) {
		t.Errorf("filename = %q, does not match expected pattern", base)
	}
}

func TestFlushOnFirstUserMessage(t *testing.T) {
	agentDir := t.TempDir()
	m := New("/work/proj", agentDir)

	if _, err := m.AppendModelChange("openai", "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("system", map[string]any{"role": "system", "content": ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.File()); !os.IsNotExist(err) {
		t.Fatal("session file should not exist before the first user message")
	}

	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	header, entries, err := Load(m.File())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if header.Type != "session" || header.Version != CurrentVersion {
		t.Errorf("header = %+v, want type=session version=%d", header, CurrentVersion)
	}
	if header.Cwd != "/work/proj" {
		t.Errorf("header cwd = %q", header.Cwd)
	}
	if len(entries) != 3 {
		t.Fatalf("entries after user = %d, want model_change, system, user", len(entries))
	}
	if entries[0].Type != "model_change" || messageRole(t, entries[1]) != "system" || messageRole(t, entries[2]) != "user" {
		t.Fatalf("types/roles = %s %s %s", entries[0].Type, messageRole(t, entries[1]), messageRole(t, entries[2]))
	}
	if entries[0].ParentID != nil {
		t.Errorf("first entry parentId = %v, want null", *entries[0].ParentID)
	}
	if entries[1].ParentID == nil || *entries[1].ParentID != entries[0].ID {
		t.Errorf("entry[1] parentId = %v, want %q", entries[1].ParentID, entries[0].ID)
	}

	assistant := map[string]any{"role": "assistant", "content": "hello back", "stopReason": "stop"}
	if _, err := m.AppendMessage("assistant", assistant); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("toolResult", map[string]any{"role": "toolResult", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) {
		t.Fatal("later entries rewrote the flushed prefix")
	}
	_, entries, err = Load(m.File())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(entries))
	}
	if messageRole(t, entries[3]) != "assistant" || messageRole(t, entries[4]) != "toolResult" {
		t.Fatalf("appended roles = %s %s", messageRole(t, entries[3]), messageRole(t, entries[4]))
	}
}

func messageRole(t *testing.T, e Entry) string {
	t.Helper()
	var msg struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(e.Message, &msg); err != nil {
		t.Fatal(err)
	}
	return msg.Role
}

func TestSessionFileIsPrivate(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("session mode=%o", st.Mode().Perm())
	}
	dir, err := os.Stat(filepath.Dir(m.File()))
	if err != nil {
		t.Fatal(err)
	}
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("session dir mode=%o", dir.Mode().Perm())
	}
}

func TestAppendModelAndThinkingChange(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendModelChange("openai", "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendThinkingLevelChange("high"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range m.Entries() {
		types = append(types, e.Type)
	}
	if len(types) < 4 || types[0] != "model_change" || types[1] != "thinking_level_change" {
		t.Fatalf("%v", types)
	}
	if m.Entries()[0].Provider != "openai" || m.Entries()[0].ModelID != "gpt-4o" {
		t.Fatalf("%+v", m.Entries()[0])
	}
	if m.Entries()[1].ThinkingLevel != "high" {
		t.Fatalf("%+v", m.Entries()[1])
	}
}
