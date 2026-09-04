package sesssrv

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestUnixJSONLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pigo.sock")
	ln, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() { _ = ln.Close() }()
	go func() { _ = Serve(ctx, ln, func() (Session, error) { return newTestEngine(), nil }) }()

	c, err := waitDial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	in := strings.NewReader(`{"type":"prompt","message":"hi"}
{"type":"quit"}
`)
	var out bytes.Buffer
	if err := Bridge(context.Background(), c, in, &out); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&out)
	var sawReady, sawAgent bool
	for {
		var ev map[string]any
		if err := dec.Decode(&ev); err != nil {
			break
		}
		switch ev["type"] {
		case "ready":
			sawReady = true
		case "message_update", "agent_end", "text_delta", "message_end":
			sawAgent = true
		}
	}
	if !sawReady {
		t.Fatalf("missing ready: %s", out.String())
	}
	if !sawAgent {
		t.Fatalf("missing agent events: %s", out.String())
	}
}

func newTestEngine() *runtime.Engine {
	return &runtime.Engine{
		Stream: func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
			return ai.EmitMessage(ctx, &ai.AssistantMessage{
				Role:       ai.RoleAssistant,
				StopReason: ai.StopStop,
				Content:    []*ai.Content{{Type: ai.KindText, Text: "pong"}},
			}), nil
		},
		Provider: "anthropic",
		Tools:    tools.NewRegistry(),
		Opts:     runtime.Options{Config: config.Config{Provider: "anthropic", Model: "m"}},
	}
}

func waitDial(path string) (net.Conn, error) {
	var last error
	for range 50 {
		c, err := DialUnix(path)
		if err == nil {
			return c, nil
		}
		last = err
		time.Sleep(10 * time.Millisecond)
	}
	return nil, last
}
