package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSessionFileSyncFailureKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	original := []byte("original\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	prev := syncFile
	syncFile = func(*os.File) error { return errors.New("sync failed") }
	t.Cleanup(func() { syncFile = prev })

	err := writeSessionFile(path, 0o644, false, func(f *os.File) error {
		_, werr := f.Write([]byte("replacement\n"))
		return werr
	})
	if err == nil {
		t.Fatal("expected sync error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("content = %q", got)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", st.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "s.jsonl" {
		t.Fatalf("dir = %v, want only s.jsonl", names(entries))
	}
}

func TestWriteSessionFileReplaceKeepsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const updated = "updated\n"
	if err := writeSessionFile(path, 0o644, false, func(f *os.File) error {
		_, werr := f.Write([]byte(updated))
		return werr
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != updated {
		t.Fatalf("content = %q", got)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", st.Mode().Perm())
	}
}

func TestWriteSessionFileExclusiveLeavesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	original := []byte("keep\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeSessionFile(path, 0o644, true, func(f *os.File) error {
		_, werr := f.Write([]byte("new\n"))
		return werr
	})
	if !os.IsExist(err) {
		t.Fatalf("err = %v, want exist", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("content = %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir = %v, want only the original file", names(entries))
	}
}

func TestWriteSessionFileExclusiveCreatesCompleteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	body := []byte("{\"type\":\"session\"}\n{\"type\":\"message\"}\n")
	if err := writeSessionFile(path, 0o644, true, func(f *os.File) error {
		_, werr := f.Write(body)
		return werr
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("content = %q", got)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o", st.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "s.jsonl" {
		t.Fatalf("dir = %v, want one complete jsonl", names(entries))
	}
}

func TestUpdateHeaderPreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	body := []byte("{\"type\":\"session\",\"version\":3,\"id\":\"abc\",\"timestamp\":\"2020-01-01T00:00:00.000Z\",\"cwd\":\"/tmp\",\"name\":\"old\"}\n{\"type\":\"message\"}\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateHeader(path, func(h *Header) { h.Name = "new" }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`"name":"new"`)) || !bytes.Contains(got, []byte("{\"type\":\"message\"}\n")) {
		t.Fatalf("content = %s", got)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", st.Mode().Perm())
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}
