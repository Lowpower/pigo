package runtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/session"
)

func TestRPCBashReturnsPiResultShape(t *testing.T) {
	e := &Engine{Opts: Options{Cwd: t.TempDir(), Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	out := serveRPC(t, e, `{"id":"b1","type":"bash","command":"printf hello"}
{"type":"quit"}
`)
	rows := decodeRPCRows(t, out)
	var resp map[string]any
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "bash" {
			resp = r
			break
		}
	}
	if resp == nil || resp["success"] != true || resp["id"] != "b1" {
		t.Fatalf("bash response = %#v in %s", resp, out)
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
	var sawUpdate bool
	for _, u := range rpcRowsOfType(rows, "bash_execution_update") {
		if u["id"] == "b1" && strings.Contains(fmtString(u["delta"]), "hello") {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Fatalf("missing bash_execution_update for b1 in %s", out)
	}
}

func TestRPCAbortBashCancelsRunningCommand(t *testing.T) {
	e := &Engine{Opts: Options{Cwd: t.TempDir(), Config: config.Config{Provider: "anthropic", Model: "claude-sonnet-4"}}}
	c := startRPC(t, e)
	c.send(map[string]any{"id": "b1", "type": "bash", "command": "sleep 30"})
	time.Sleep(150 * time.Millisecond)
	c.send(map[string]any{"id": "a1", "type": "abort_bash"})
	out := c.close()

	rows := decodeRPCRows(t, out)
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
		t.Fatalf("abort_bash = %#v in %s", abort, out)
	}
	if bash == nil || bash["success"] != true {
		t.Fatalf("bash = %#v in %s", bash, out)
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
	c := startRPC(t, e)
	c.send(map[string]any{"type": "bash", "command": "printf hello"})
	c.waitCommand("bash", 3*time.Second)
	c.send(map[string]any{"type": "get_messages"})
	out := c.close()
	rows := decodeRPCRows(t, out)
	var msgs []any
	for _, r := range rows {
		if r["type"] == "response" && r["command"] == "get_messages" {
			data, _ := r["data"].(map[string]any)
			msgs, _ = data["messages"].([]any)
		}
	}
	if len(msgs) != 1 {
		t.Fatalf("messages = %#v in %s", msgs, out)
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
	c := startRPC(t, e)
	c.send(map[string]any{"type": "bash", "command": "printf secret", "excludeFromContext": true})
	c.waitCommand("bash", 3*time.Second)
	c.send(map[string]any{"type": "get_messages"})
	out := c.close()
	rows := decodeRPCRows(t, out)
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

func fmtString(v any) string {
	s, _ := v.(string)
	return s
}
