package ext

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/protocol"
)

func TestServeRejectsInvalidCommand(t *testing.T) {
	cases := []struct {
		name string
		cmd  CommandDef
		want string
	}{
		{
			name: "empty name",
			cmd:  CommandDef{Name: "  ", Description: "blank", Fn: func(string) {}},
			want: "non-empty string name",
		},
		{
			name: "missing handler",
			cmd:  CommandDef{Name: "review", Description: "review"},
			want: "handler()",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgs, err := driveServe(t, Handler{
				Name:     "bad-ext",
				Commands: []CommandDef{tc.cmd},
				Tools: []ToolDef{{
					Name: "should-not-register", Description: "nope",
					Schema: map[string]any{"type": "object"},
				}},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "bad-ext") {
				t.Fatalf("err=%v", err)
			}
			if tc.name == "missing handler" && !strings.Contains(err.Error(), "review") {
				t.Fatalf("err=%v", err)
			}
			for _, m := range msgs {
				if m.Type == protocol.TypeInitialized || m.Type == protocol.TypeRegisterCommand || m.Type == protocol.TypeRegisterTool {
					t.Fatalf("registered after rejection: %+v", msgs)
				}
			}
			var notified string
			for _, m := range msgs {
				if m.Type == protocol.TypeNotify {
					notified = m.Text
				}
			}
			if !strings.Contains(notified, tc.want) {
				t.Fatalf("notify=%q msgs=%+v", notified, msgs)
			}
		})
	}
}

func TestHostRejectsNamelessCommand(t *testing.T) {
	_, err := Spawn(context.Background(), "nameless-ext",
		[]string{os.Args[0], "-test.run=^TestExtNamelessCommandProcess$"},
		Options{Env: []string{"PIGO_EXT_HELPER=nameless-cmd"}})
	if err == nil {
		t.Fatal("expected spawn error")
	}
	if !strings.Contains(err.Error(), "nameless-ext") || !strings.Contains(err.Error(), "non-empty string name") {
		t.Fatalf("err=%v", err)
	}
}

func TestExtNamelessCommandProcess(_ *testing.T) {
	if os.Getenv("PIGO_EXT_HELPER") != "nameless-cmd" {
		return
	}
	_ = protocol.WriteMessage(os.Stdout, protocol.Message{Type: protocol.TypeHello, ExtName: "nameless", APIVersion: APIVersion})
	r := bufio.NewReader(os.Stdin)
	for {
		m, err := protocol.ReadMessage(r)
		if err != nil {
			os.Exit(1)
		}
		if m.Type == protocol.TypeReady {
			break
		}
	}
	_ = protocol.WriteMessage(os.Stdout, protocol.Message{Type: protocol.TypeRegisterCommand, Description: "blank"})
	_ = protocol.WriteMessage(os.Stdout, protocol.Message{Type: protocol.TypeInitialized})
	os.Exit(0)
}

type readResult struct {
	m   protocol.Message
	err error
}

func driveServe(t *testing.T, h Handler) ([]protocol.Message, error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveRW(h, inR, outW)
		_ = outW.Close()
	}()
	reads := make(chan readResult, 8)
	go func() {
		br := bufio.NewReader(outR)
		for {
			m, err := protocol.ReadMessage(br)
			reads <- readResult{m, err}
			if err != nil {
				return
			}
		}
	}()
	var msgs []protocol.Message
	readySent := false
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case r := <-reads:
			if r.err != nil {
				_ = inW.Close()
				return msgs, <-errCh
			}
			msgs = append(msgs, r.m)
			if !readySent && r.m.Type == protocol.TypeHello {
				readySent = true
				if err := protocol.WriteMessage(inW, protocol.Message{Type: protocol.TypeReady}); err != nil {
					t.Fatal(err)
				}
			}
			if r.m.Type == protocol.TypeInitialized {
				_ = inW.Close()
				_ = outR.Close()
				return msgs, <-errCh
			}
		case err := <-errCh:
			_ = inW.Close()
			return drainServe(msgs, reads, err)
		case <-timer.C:
			_ = inW.Close()
			_ = outR.Close()
			return msgs, <-errCh
		}
	}
}

func drainServe(msgs []protocol.Message, reads <-chan readResult, err error) ([]protocol.Message, error) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case r := <-reads:
			if r.err != nil {
				return msgs, err
			}
			msgs = append(msgs, r.m)
		case <-timer.C:
			return msgs, err
		}
	}
}
