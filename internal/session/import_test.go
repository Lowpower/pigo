package session

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestImportCopiesIntoSessionDir(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(outside, "chat.jsonl")
	body := importFixture("keep-id", "/original/place", "stay")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}

	opened, err := Import(src, cwd, agent, "")
	if err != nil {
		t.Fatal(err)
	}
	wantDir := StorageDir(cwd, agent, "")
	if filepath.Dir(opened.File()) != wantDir {
		t.Fatalf("dir = %s, want %s", filepath.Dir(opened.File()), wantDir)
	}
	if filepath.Base(opened.File()) != "chat.jsonl" {
		t.Fatalf("base = %s", filepath.Base(opened.File()))
	}
	if opened.File() == src {
		t.Fatal("opened the source file")
	}
	if opened.ID() != "keep-id" {
		t.Fatalf("id = %s", opened.ID())
	}
	if opened.Header().Cwd != "/original/place" {
		t.Fatalf("cwd = %s", opened.Header().Cwd)
	}
	got, err := os.ReadFile(opened.File())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("copy = %s\nwant %s", got, body)
	}
	if _, err := opened.AppendMessage("user", map[string]any{"role": "user", "content": "next"}); err != nil {
		t.Fatal(err)
	}
	srcAfter, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(srcAfter, body) {
		t.Fatal("source bytes changed")
	}
	after, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("source mode = %o, want %o", after.Mode().Perm(), before.Mode().Perm())
	}
}

func TestImportAvoidsNameCollision(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	dir := StorageDir(cwd, agent, "")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(dir, "collision.jsonl")
	storedBody := importFixture("stored", "/stored/cwd", "old")
	if err := os.WriteFile(stored, storedBody, 0o600); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "collision.jsonl")
	imported := importFixture("imported", "/imported/cwd", "new")
	if err := os.WriteFile(src, imported, 0o644); err != nil {
		t.Fatal(err)
	}

	opened, err := Import(src, cwd, agent, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(opened.File()) != "collision-1.jsonl" {
		t.Fatalf("base = %s", filepath.Base(opened.File()))
	}
	if opened.ID() != "imported" {
		t.Fatalf("id = %s", opened.ID())
	}
	gotStored, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotStored, storedBody) {
		t.Fatal("existing session was overwritten")
	}
	gotCopy, err := os.ReadFile(opened.File())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotCopy, imported) {
		t.Fatal("imported copy does not match source")
	}
}

func TestImportUsesSessionDirOverride(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	custom := t.TempDir()
	src := filepath.Join(t.TempDir(), "moved.jsonl")
	body := importFixture("moved-id", "/other/cwd", "keep")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := Import(src, cwd, agent, custom)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(opened.File()) != custom {
		t.Fatalf("dir = %s, want %s", filepath.Dir(opened.File()), custom)
	}
}

func TestImportRemovesCopyWhenOpenFails(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	src := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(src, []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(src, cwd, agent, ""); err == nil {
		t.Fatal("expected error")
	}
	dir := StorageDir(cwd, agent, "")
	ents, readErr := os.ReadDir(dir)
	if os.IsNotExist(readErr) {
		return
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(ents) != 0 {
		t.Fatalf("failed import left %d files", len(ents))
	}
}

func TestImportMissingSource(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	_, err := Import(filepath.Join(t.TempDir(), "missing.jsonl"), cwd, agent, "")
	if err == nil {
		t.Fatal("expected error")
	}
	dir := StorageDir(cwd, agent, "")
	ents, readErr := os.ReadDir(dir)
	if os.IsNotExist(readErr) {
		return
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(ents) != 0 {
		t.Fatalf("session dir has %d entries", len(ents))
	}
}

func TestImportAlreadyStoredDoesNotCopy(t *testing.T) {
	agent := t.TempDir()
	cwd := t.TempDir()
	dir := StorageDir(cwd, agent, "")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "already.jsonl")
	body := importFixture("already-id", "/keep/cwd", "same")
	if err := os.WriteFile(src, body, 0o600); err != nil {
		t.Fatal(err)
	}

	opened, err := Import(src, cwd, agent, "")
	if err != nil {
		t.Fatal(err)
	}
	if opened.File() != src {
		t.Fatalf("file = %s, want %s", opened.File(), src)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Fatalf("session dir has %d files", len(ents))
	}
	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("stored file bytes changed")
	}
}

func importFixture(id, cwd, custom string) []byte {
	return []byte(`{"type":"session","version":3,"id":"` + id + `","timestamp":"2020-01-01T00:00:00.000Z","cwd":"` + cwd + `","customField":"` + custom + `"}` + "\n" +
		`{"type":"message","id":"m1","parentId":null,"timestamp":"2020-01-01T00:00:01.000Z","message":{"role":"user","content":"hi"}}` + "\n")
}
