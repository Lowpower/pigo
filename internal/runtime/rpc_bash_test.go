package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestRPCBashReturnsPiResultShape(t *testing.T) {
	e := &Engine{Opts: Options{Cwd: t.TempDir(), Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	in := strings.NewReader(`{"id":"b1","type":"bash","command":"printf hello"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	var resp map[string]any
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "bash" {
			resp = r
			break
		}
	}
	if resp == nil || resp["success"] != true || resp["id"] != "b1" {
		t.Fatalf("bash response = %#v in %s", resp, out.String())
	}
	data, _ := resp["data"].(map[string]any)
	if data == nil {
		t.Fatalf("missing data in %#v", resp)
	}
	if !strings.Contains(data["output"].(string), "hello") {
		t.Fatalf("output = %#v", data["output"])
	}
	if data["cancelled"] != false {
		t.Fatalf("cancelled = %#v", data["cancelled"])
	}
	if data["truncated"] != false {
		t.Fatalf("truncated = %#v", data["truncated"])
	}
	if data["exitCode"] != float64(0) {
		t.Fatalf("exitCode = %#v", data["exitCode"])
	}
}

func TestRPCBashStreamsExecutionUpdateWithCommandID(t *testing.T) {
	e := &Engine{Opts: Options{Cwd: t.TempDir(), Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	in := strings.NewReader(`{"id":"req-1","type":"bash","command":"printf hello"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := e.ServeRPC(context.Background(), in, &out); err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	updates := rpcRowsOfType(rows, "bash_execution_update")
	if len(updates) == 0 {
		t.Fatalf("missing bash_execution_update in %s", out.String())
	}
	var saw bool
	for _, u := range updates {
		if u["id"] == "req-1" && strings.Contains(fmtString(u["delta"]), "hello") {
			saw = true
			break
		}
	}
	if !saw {
		t.Fatalf("updates = %#v", updates)
	}
}

func TestRPCAbortBashCancelsRunningCommand(t *testing.T) {
	e := &Engine{Opts: Options{Cwd: t.TempDir(), Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	pr, pw := io.Pipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- e.ServeRPC(context.Background(), pr, &out) }()

	enc := json.NewEncoder(pw)
	if err := enc.Encode(map[string]any{"id": "b1", "type": "bash", "command": "sleep 30"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := enc.Encode(map[string]any{"id": "a1", "type": "abort_bash"}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{"type": "quit"}); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("rpc did not return; bash was not aborted. out=%s", out.String())
	}

	rows := decodeRPCRows(t, out.String())
	var abort, bash map[string]any
	for _, r := range rows {
		if r["type"] != "response" {
			continue
		}
		switch r["command"] {
		case "abort_bash":
			abort = r
		case "bash":
			bash = r
		}
	}
	if abort == nil || abort["success"] != true || abort["id"] != "a1" {
		t.Fatalf("abort_bash = %#v in %s", abort, out.String())
	}
	if bash == nil || bash["success"] != true {
		t.Fatalf("bash = %#v in %s", bash, out.String())
	}
	data, _ := bash["data"].(map[string]any)
	if data["cancelled"] != true {
		t.Fatalf("bash data = %#v, want cancelled true", data)
	}
	if _, ok := data["exitCode"]; ok && data["exitCode"] != nil {
		t.Fatalf("cancelled bash should omit exitCode, got %#v", data["exitCode"])
	}
}

func TestRPCBashAddsOutputToNextPromptContext(t *testing.T) {
	e := &Engine{Opts: Options{Cwd: t.TempDir(), Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	pr, pw := io.Pipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- e.ServeRPC(context.Background(), pr, &out) }()
	enc := json.NewEncoder(pw)
	if err := enc.Encode(map[string]any{"type": "bash", "command": "printf hello"}); err != nil {
		t.Fatal(err)
	}
	waitRPCCommand(t, &out, "bash", 3*time.Second)
	if err := enc.Encode(map[string]any{"type": "get_messages"}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{"type": "quit"}); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	var msgs []any
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "get_messages" {
			data, _ := r["data"].(map[string]any)
			msgs, _ = data["messages"].([]any)
		}
	}
	if len(msgs) != 1 {
		t.Fatalf("messages = %#v in %s", msgs, out.String())
	}
	raw, _ := json.Marshal(msgs[0])
	var m ai.Message
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Role != ai.RoleUser || !strings.Contains(m.Content, "Ran `printf hello`") || !strings.Contains(m.Content, "hello") {
		t.Fatalf("message = %+v", m)
	}
}

func TestRPCBashExcludeFromContextSkipsLLMHistory(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	e := &Engine{Opts: Options{Cwd: cwd, AgentDir: dir, Session: sess, Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	pr, pw := io.Pipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- e.ServeRPC(context.Background(), pr, &out) }()
	enc := json.NewEncoder(pw)
	if err := enc.Encode(map[string]any{"type": "bash", "command": "printf secret", "excludeFromContext": true}); err != nil {
		t.Fatal(err)
	}
	waitRPCCommand(t, &out, "bash", 3*time.Second)
	if err := enc.Encode(map[string]any{"type": "get_messages"}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{"type": "quit"}); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rows := decodeRPCRows(t, out.String())
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "get_messages" {
			data, _ := r["data"].(map[string]any)
			msgs, _ := data["messages"].([]any)
			if len(msgs) != 0 {
				t.Fatalf("excluded bash still in get_messages: %#v", msgs)
			}
		}
	}
	entries := sess.Entries()
	if len(entries) == 0 {
		t.Fatal("excluded bash was not persisted to session")
	}
	var payload map[string]any
	if err := json.Unmarshal(entries[len(entries)-1].Message, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["role"] != "bashExecution" || payload["excludeFromContext"] != true {
		t.Fatalf("session payload = %#v", payload)
	}
}

func waitRPCCommand(t *testing.T, out *syncBuffer, command string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		rows := decodeRPCRows(t, out.String())
		for _, r := range rows {
			if r["type"] == "response" && r["command"] == command {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s response in %s", command, out.String())
}

func fmtString(v any) string {
	s, _ := v.(string)
	return s
}
