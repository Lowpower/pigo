package ext

import (
	"bufio"
	"io"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/protocol"
)

func TestServeRegistersMCPServer(t *testing.T) {
	msgs, err := driveServe(t, Handler{
		Name: "demo",
		MCPServers: []MCPServerDef{{
			Name:   "demo",
			Config: map[string]any{"url": "http://127.0.0.1/mcp"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Type == protocol.TypeRegisterMCPServer && m.Name == "demo" {
			if m.Args["url"] != "http://127.0.0.1/mcp" {
				t.Fatalf("args=%v", m.Args)
			}
			return
		}
	}
	t.Fatalf("missing register_mcp_server in %v", msgs)
}

func TestHostRecordsMCPServer(t *testing.T) {
	pr, pw := io.Pipe()
	h := &Host{
		pending:  map[string]chan protocol.Message{},
		waitDone: make(chan struct{}),
	}
	ready := make(chan error, 1)
	go h.readLoop(bufio.NewReader(pr), func(err error) { ready <- err })
	if err := protocol.WriteMessage(pw, protocol.Message{
		Type: protocol.TypeRegisterMCPServer,
		Name: "demo",
		Args: map[string]any{"command": "demo-server"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteMessage(pw, protocol.Message{Type: protocol.TypeInitialized}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("handshake timed out")
	}
	got := h.MCPServers()
	if len(got) != 1 || got[0].Name != "demo" || got[0].Args["command"] != "demo-server" {
		t.Fatalf("%+v", got)
	}
	_ = pw.Close()
}
