package ext

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"

	"github.com/Lowpower/pigo/internal/protocol"
)

// ToolDef is a tool an extension exposes.
type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
	// Fn runs the tool and returns its textual result and whether it errored.
	Fn func(ctx context.Context, args map[string]any) (string, bool)
}

// Handler describes an extension: its name, the tools it provides, the lifecycle
// events it wants, and an optional event callback.
type Handler struct {
	Name    string
	Tools   []ToolDef
	Events  []string
	OnEvent func(event string, payload map[string]any)
}

// Serve runs an extension over stdin/stdout. It is the entrypoint an extension
// binary calls from main(). It blocks until the host disconnects or sends a
// shutdown message.
func Serve(h Handler) error {
	return serveRW(h, os.Stdin, os.Stdout)
}

func serveRW(h Handler, in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)

	// Handshake: announce, then wait for the host's ready.
	if err := protocol.WriteMessage(out, protocol.Message{Type: protocol.TypeHello, ExtName: h.Name, APIVersion: APIVersion}); err != nil {
		return err
	}
	for {
		m, err := protocol.ReadMessage(r)
		if err != nil {
			return ignoreEOF(err)
		}
		if m.Type == protocol.TypeReady {
			break
		}
	}

	// Register tools (+ subscribe), then signal initialization complete.
	byName := make(map[string]ToolDef, len(h.Tools))
	for _, t := range h.Tools {
		byName[t.Name] = t
		if err := protocol.WriteMessage(out, protocol.Message{
			Type: protocol.TypeRegisterTool, Name: t.Name, Description: t.Description, Schema: t.Schema,
		}); err != nil {
			return err
		}
	}
	if len(h.Events) > 0 {
		if err := protocol.WriteMessage(out, protocol.Message{Type: protocol.TypeSubscribe, Events: h.Events}); err != nil {
			return err
		}
	}
	if err := protocol.WriteMessage(out, protocol.Message{Type: protocol.TypeInitialized}); err != nil {
		return err
	}

	// Serve loop.
	for {
		m, err := protocol.ReadMessage(r)
		if err != nil {
			return ignoreEOF(err)
		}
		switch m.Type {
		case protocol.TypeToolCall:
			result, isErr := "unknown tool: "+m.Name, true
			if t, ok := byName[m.Name]; ok {
				result, isErr = t.Fn(context.Background(), m.Args)
			}
			if err := protocol.WriteMessage(out, protocol.Message{
				Type: protocol.TypeToolResult, ID: m.ID, Result: result, IsError: isErr,
			}); err != nil {
				return err
			}
		case protocol.TypeEvent:
			if h.OnEvent != nil {
				h.OnEvent(m.Event, m.Payload)
			}
		case protocol.TypePing:
			if err := protocol.WriteMessage(out, protocol.Message{Type: protocol.TypePong}); err != nil {
				return err
			}
		case protocol.TypeShutdown:
			return nil
		}
	}
}

func ignoreEOF(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return err
}
