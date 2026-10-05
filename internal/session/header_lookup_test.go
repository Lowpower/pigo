package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadHeaderSkipsBlankLinesAndBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	body := []byte("\n\n" + `{"type":"session","version":3,"id":"abc","timestamp":"2020-01-01T00:00:00.000Z","cwd":"/tmp"}` + "\nNOT JSON\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := readHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != "abc" || h.Cwd != "/tmp" {
		t.Fatalf("header = %+v", h)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o, header read changed permissions", info.Mode().Perm())
	}
}

func TestReadHeaderRejectsCorruptAndOversized(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(corrupt, []byte("{nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readHeader(corrupt); err == nil {
		t.Fatal("corrupt header returned nil error")
	}

	big := filepath.Join(dir, "big.jsonl")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), (1<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readHeader(big); err == nil {
		t.Fatal("oversized header returned nil error")
	}
}

func TestFindIDUsesHeaderAndSkipsBlockedBody(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	sessionDir := t.TempDir()

	target := NewWithID(cwd, agentDir, "target-session", sessionDir)
	if _, err := target.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := target.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "ok"}); err != nil {
		t.Fatal(err)
	}

	dir := StorageDir(cwd, agentDir, sessionDir)
	line, err := json.Marshal(Header{
		Type: "session", Version: CurrentVersion, ID: "other-session",
		Timestamp: "2020-01-01T00:00:00.000Z", Cwd: target.Header().Cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	serveFIFO(t, filepath.Join(dir, "blocked-body.jsonl"), line, 16)

	corrupt := filepath.Join(dir, "zz-corrupt.jsonl")
	if err := os.WriteFile(corrupt, []byte("not-json\n{\"type\":\"message\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nameMatch := filepath.Join(dir, "session-by-name.jsonl")
	nameHeader, err := json.Marshal(Header{
		Type: "session", Version: CurrentVersion, ID: "unrelated-id",
		Timestamp: "2020-01-01T00:00:00.000Z", Cwd: target.Header().Cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nameMatch, append(nameHeader, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	// Newest first, so lookup inspects the blocked file before the real hit.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "blocked-body.jsonl"), future, future); err != nil {
		t.Fatal(err)
	}

	got, err := lookupWithin(t, 2*time.Second, func() (*Manager, error) {
		return FindExactIDAt(cwd, agentDir, "target-session", sessionDir)
	})
	if err != nil || got.ID() != "target-session" {
		t.Fatalf("exact = %v %v", got, err)
	}
	if _, err := lookupWithin(t, 2*time.Second, func() (*Manager, error) {
		return FindExactIDAt(cwd, agentDir, "target", sessionDir)
	}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exact prefix err = %v", err)
	}
	got, err = lookupWithin(t, 2*time.Second, func() (*Manager, error) {
		return FindByIDAt(cwd, agentDir, "target-ses", sessionDir)
	})
	if err != nil || got.ID() != "target-session" {
		t.Fatalf("prefix = %v %v", got, err)
	}
	got, err = lookupWithin(t, 2*time.Second, func() (*Manager, error) {
		return FindByIDAt(cwd, agentDir, "by-name", sessionDir)
	})
	if err != nil || got.ID() != "unrelated-id" {
		t.Fatalf("filename = %v %v", got, err)
	}
	if _, err := lookupWithin(t, 2*time.Second, func() (*Manager, error) {
		return FindByIDAt(cwd, agentDir, "zz-corrupt", sessionDir)
	}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt filename err = %v", err)
	}
}

func serveFIFO(t *testing.T, path string, line []byte, n int) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	go func() {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		payload := append(append([]byte{}, line...), '\n')
		for i := 0; i < n; i++ {
			if _, err := f.Write(payload); err != nil {
				return
			}
		}
		<-release
	}()
}

func lookupWithin(t *testing.T, d time.Duration, fn func() (*Manager, error)) (*Manager, error) {
	t.Helper()
	type result struct {
		m   *Manager
		err error
	}
	ch := make(chan result, 1)
	go func() {
		m, err := fn()
		ch <- result{m, err}
	}()
	select {
	case r := <-ch:
		return r.m, r.err
	case <-time.After(d):
		t.Fatal("lookup blocked while reading a session file")
		return nil, nil
	}
}
