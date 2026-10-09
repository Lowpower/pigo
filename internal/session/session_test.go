package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	header, entries, _, err := Load(m.File())
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
	_, entries, _, err = Load(m.File())
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

func flushedSession(t *testing.T) *Manager {
	t.Helper()
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLoadSkipsTornTailAndAppendTruncatesIt(t *testing.T) {
	m := flushedSession(t)
	before, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(before, []byte("\n")) {
		t.Fatal("setup file should end with a newline")
	}
	tornTail := []byte(`{"type":"message","id":"zz","timest`)
	if err := os.WriteFile(m.File(), append(append([]byte{}, before...), tornTail...), 0o600); err != nil {
		t.Fatal(err)
	}

	header, entries, skips, err := Load(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if header.ID != m.ID() {
		t.Fatalf("header id=%s", header.ID)
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%d", len(entries))
	}
	if len(skips) != 1 || skips[0].Line != 4 {
		t.Fatalf("skips=%+v", skips)
	}
	onDisk, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, append(append([]byte{}, before...), tornTail...)) {
		t.Fatal("load rewrote the file")
	}

	opened, err := Open(m.File())
	if err != nil {
		t.Fatal(err)
	}
	warn := opened.LoadWarningText()
	if !strings.Contains(warn, "skipped 1 unreadable line") || !strings.Contains(warn, "4 (") {
		t.Fatalf("warning=%q", warn)
	}
	if _, err := opened.AppendMessage("user", map[string]any{"role": "user", "content": "next"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(opened.File())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) {
		t.Fatal("complete lines changed")
	}
	if bytes.Contains(after, tornTail) {
		t.Fatal("torn tail still in file")
	}
	rest := after[len(before):]
	if !bytes.HasPrefix(rest, []byte("{")) || !bytes.HasSuffix(after, []byte("\n")) {
		t.Fatalf("new record is not its own line: %q", rest)
	}
	if bytes.Count(rest, []byte("\n")) != 1 {
		t.Fatalf("new record lines=%d", bytes.Count(rest, []byte("\n")))
	}
	st, err := os.Stat(opened.File())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", st.Mode().Perm())
	}
	_, entries, skips, err = Load(opened.File())
	if err != nil {
		t.Fatal(err)
	}
	if len(skips) != 0 || len(entries) != 3 {
		t.Fatalf("entries=%d skips=%+v", len(entries), skips)
	}
}

func TestAppendRepairsMissingNewline(t *testing.T) {
	m := flushedSession(t)
	before, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	trimmed := bytes.TrimSuffix(before, []byte("\n"))
	if err := os.WriteFile(m.File(), trimmed, 0o600); err != nil {
		t.Fatal(err)
	}
	_, entries, skips, err := Load(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || len(skips) != 0 {
		t.Fatalf("entries=%d skips=%+v", len(entries), skips)
	}
	opened, err := Open(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.AppendMessage("user", map[string]any{"role": "user", "content": "next"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(opened.File())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, trimmed) {
		t.Fatal("original record bytes changed")
	}
	rest := after[len(trimmed):]
	if !bytes.HasPrefix(rest, []byte("\n{")) || !bytes.HasSuffix(after, []byte("\n")) {
		t.Fatalf("separator=%q", rest)
	}
	_, entries, skips, err = Load(opened.File())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || len(skips) != 0 {
		t.Fatalf("entries=%d skips=%+v", len(entries), skips)
	}
}

func TestAppendKeepsGluedLine(t *testing.T) {
	m := flushedSession(t)
	base, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	glued := []byte(`{"type":"message","id":"a"}{"type":"message","id":"b"}`)
	if err := os.WriteFile(m.File(), append(append([]byte{}, base...), glued...), 0o600); err != nil {
		t.Fatal(err)
	}
	_, entries, skips, err := Load(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || len(skips) != 1 || skips[0].Line != 4 {
		t.Fatalf("entries=%d skips=%+v", len(entries), skips)
	}
	opened, err := Open(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.AppendMessage("user", map[string]any{"role": "user", "content": "next"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(opened.File())
	if err != nil {
		t.Fatal(err)
	}
	idx := bytes.Index(after, glued)
	if idx < 0 {
		t.Fatal("glued line was removed")
	}
	rest := after[idx+len(glued):]
	if !bytes.HasPrefix(rest, []byte("\n{")) {
		t.Fatalf("after glued=%q", rest)
	}
	_, entries, skips, err = Load(opened.File())
	if err != nil {
		t.Fatal(err)
	}
	if len(skips) != 1 || len(entries) != 3 {
		t.Fatalf("entries=%d skips=%+v", len(entries), skips)
	}
}

func TestAppendHealthyFileIsSuffix(t *testing.T) {
	m := flushedSession(t)
	before, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "more"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) || bytes.Equal(after, before) {
		t.Fatal("append rewrote or skipped the healthy file")
	}
}

func TestLoadSkipsMiddleBadLine(t *testing.T) {
	m := flushedSession(t)
	base, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	bad := append(append([]byte{}, base...), []byte("not-json\n")...)
	good := []byte(`{"type":"message","id":"later","parentId":null,"timestamp":"2020-01-01T00:00:00.000Z"}` + "\n")
	if err := os.WriteFile(m.File(), append(bad, good...), 0o600); err != nil {
		t.Fatal(err)
	}
	_, entries, skips, err := Load(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if len(skips) != 1 || skips[0].Line != 4 {
		t.Fatalf("skips=%+v", skips)
	}
	if entries[len(entries)-1].ID != "later" {
		t.Fatalf("last=%+v", entries[len(entries)-1])
	}
}

func TestLoadRejectsBadHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(path, []byte("{nope\n{\"type\":\"message\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Load(path); err == nil {
		t.Fatal("corrupt header returned nil error")
	}
}

func TestLoadRejectsOversizedHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.jsonl")
	body := append(bytes.Repeat([]byte("x"), maxHeaderBytes+1), '\n')
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "session header exceeds") {
		t.Fatalf("err=%v", err)
	}
}

func TestAppendRefusesPartialHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "torn.jsonl")
	body := []byte(`{"type":"session","id":"x"`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{file: path, flushed: true, persist: true}
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "nope"}); err == nil {
		t.Fatal("expected error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("file changed: %s", got)
	}
}

func TestLoadAcceptsLineOverEightMiB(t *testing.T) {
	m := flushedSession(t)
	payload := strings.Repeat("a", 8*1024*1024+1)
	raw, err := json.Marshal(map[string]any{
		"type":      "message",
		"id":        "big",
		"parentId":  nil,
		"timestamp": "2020-01-01T00:00:00.000Z",
		"message":   map[string]any{"role": "user", "content": payload},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= 8*1024*1024 || len(raw) >= maxEntryLineBytes {
		t.Fatalf("line size=%d", len(raw))
	}
	f, err := os.OpenFile(m.File(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, entries, skips, err := Load(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if len(skips) != 0 {
		t.Fatalf("skips=%+v", skips)
	}
	if entries[len(entries)-1].ID != "big" {
		t.Fatalf("last=%s", entries[len(entries)-1].ID)
	}
}

func TestLoadSkipsLineOverLimit(t *testing.T) {
	m := flushedSession(t)
	f, err := os.OpenFile(m.File(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	written := 0
	for written <= maxEntryLineBytes {
		n := maxEntryLineBytes + 1 - written
		if n > len(chunk) {
			n = len(chunk)
		}
		if _, err := f.Write(chunk[:n]); err != nil {
			t.Fatal(err)
		}
		written += n
	}
	good := []byte("\n" + `{"type":"message","id":"after","parentId":null,"timestamp":"2020-01-01T00:00:00.000Z","message":{"role":"user","content":"kept"}}` + "\n")
	if _, err := f.Write(good); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, entries, skips, err := Load(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if len(skips) != 1 || skips[0].Line != 4 || !strings.Contains(skips[0].Reason, "64 MiB") {
		t.Fatalf("skips=%+v", skips)
	}
	if len(entries) != 3 || entries[len(entries)-1].ID != "after" {
		t.Fatalf("entries=%d last=%+v", len(entries), entries[len(entries)-1])
	}
}

func TestSummariesKeepTornSession(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	m := New(cwd, agent)
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(m.File())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.File(), append(before, []byte(`{"type":"message"`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	sums, err := Summaries(cwd, agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || sums[0].ID != m.ID() {
		t.Fatalf("summaries=%+v", sums)
	}
}

func TestReadLimitedLine(t *testing.T) {
	raw := "abcd\nabcde\nok\n"
	r := bufio.NewReaderSize(strings.NewReader(raw), 2)
	line, tooLong, err := readLimitedLine(r, 4)
	if err != nil || tooLong || line != "abcd" {
		t.Fatalf("line=%q tooLong=%v err=%v", line, tooLong, err)
	}
	line, tooLong, err = readLimitedLine(r, 4)
	if err != nil || !tooLong || line != "" {
		t.Fatalf("line=%q tooLong=%v err=%v", line, tooLong, err)
	}
	line, tooLong, err = readLimitedLine(r, 4)
	if err != nil || tooLong || line != "ok" {
		t.Fatalf("line=%q tooLong=%v err=%v", line, tooLong, err)
	}
	_, _, err = readLimitedLine(r, 4)
	if err == nil {
		t.Fatal("expected EOF")
	}
}
