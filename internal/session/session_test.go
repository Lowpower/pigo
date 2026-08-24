package session

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"testing"
)

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

func TestBufferUntilAssistantThenFlush(t *testing.T) {
	agentDir := t.TempDir()
	m := New("/work/proj", agentDir)

	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	// No assistant yet -> nothing on disk.
	if _, _, err := Load(m.File()); err == nil {
		t.Fatal("session file should not exist before the first assistant message")
	}

	assistant := map[string]any{"role": "assistant", "content": "hello back", "stopReason": "stop"}
	if _, err := m.AppendMessage("assistant", assistant); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("toolResult", map[string]any{"role": "toolResult", "content": "ok"}); err != nil {
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
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if entries[0].ParentID != nil {
		t.Errorf("first entry parentId = %v, want null", *entries[0].ParentID)
	}
	if entries[1].ParentID == nil || *entries[1].ParentID != entries[0].ID {
		t.Errorf("entry[1] parentId = %v, want %q", entries[1].ParentID, entries[0].ID)
	}

	// roles round-trip via the message payload
	roles := make([]string, len(entries))
	for i, e := range entries {
		var msg struct {
			Role string `json:"role"`
		}
		_ = json.Unmarshal(e.Message, &msg)
		roles[i] = msg.Role
	}
	if roles[0] != "user" || roles[1] != "assistant" || roles[2] != "toolResult" {
		t.Errorf("roles = %v, want [user assistant toolResult]", roles)
	}
}
