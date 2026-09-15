package ext

import (
	"bufio"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Lowpower/pigo/internal/protocol"
)

func TestObjectParameterSchema(t *testing.T) {
	if ObjectParameterSchema(nil) || ObjectParameterSchema(map[string]any{"type": "string"}) {
		t.Fatal("nil and non-object must fail")
	}
	if !ObjectParameterSchema(map[string]any{"type": "object"}) {
		t.Fatal("type object should pass")
	}
	if checkToolSchema("x", nil) == nil {
		t.Fatal("expected error")
	}
	var te ToolSchemaError
	if !errors.As(checkToolSchema("x", nil), &te) || te.Name != "x" {
		t.Fatalf("want ToolSchemaError, got %v", checkToolSchema("x", nil))
	}
}

func TestExtBadSchemaProcess(_ *testing.T) {
	if os.Getenv("PIGO_EXT_HELPER") != "bad-schema" {
		return
	}
	_ = protocol.WriteMessage(os.Stdout, protocol.Message{Type: protocol.TypeHello, ExtName: "bad", APIVersion: APIVersion})
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
	_ = protocol.WriteMessage(os.Stdout, protocol.Message{Type: protocol.TypeRegisterTool, Name: "broken"})
	_ = protocol.WriteMessage(os.Stdout, protocol.Message{Type: protocol.TypeInitialized})
	select {}
}

func TestHostRejectsNonObjectToolSchema(t *testing.T) {
	var noticed string
	h, err := Spawn(context.Background(), "bad",
		[]string{os.Args[0], "-test.run=^TestExtBadSchemaProcess$"},
		Options{
			Env:    []string{"PIGO_EXT_HELPER=bad-schema"},
			Notify: func(_, text string) { noticed = text },
		})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = h.Close() }()
	if n := len(h.Tools()); n != 0 {
		t.Fatalf("tools=%d, want 0", n)
	}
	if noticed == "" {
		t.Fatal("expected notify error")
	}
}

func TestServeCheckToolSchemaBeforeRegister(t *testing.T) {
	if err := checkToolSchema("hello", map[string]any{"type": "object"}); err != nil {
		t.Fatal(err)
	}
	if err := checkToolSchema("hello", map[string]any{"type": "array"}); err == nil {
		t.Fatal("array schema should fail")
	}
}
