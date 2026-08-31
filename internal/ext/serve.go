package ext

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/protocol"
)

var flagStore sync.Map

// Flag returns a CLI flag value this process claimed during Serve handshake.
func Flag(name string) (any, bool) {
	v, ok := flagStore.Load(name)
	return v, ok
}

// ToolDef is a tool an extension exposes.
type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
	Fn          func(ctx context.Context, args map[string]any) (string, bool)
}

// Handler describes an extension: tools, commands, flags, events, and optional hooks.
type Handler struct {
	Name            string
	Tools           []ToolDef
	Commands        []CommandDef
	Shortcuts       []ShortcutDef
	Flags           []FlagDef
	Providers       []ProviderDef
	Events          []string
	OnEvent         func(event string, payload map[string]any) map[string]any
	OnOAuth         func(kind string, cred map[string]any) map[string]any
	OnRefreshModels func() []map[string]any
	OnStream        func(req map[string]any, emit func(event string, payload map[string]any), abort <-chan struct{})
}

// Serve runs an extension over stdin/stdout. It is the entrypoint an extension
// binary calls from main(). It blocks until the host disconnects or sends a
// shutdown message.
func Serve(h Handler) error {
	return serveRW(h, os.Stdin, os.Stdout)
}

func serveRW(h Handler, in io.Reader, out io.Writer) error {
	flagStore.Range(func(k, _ any) bool {
		flagStore.Delete(k)
		return true
	})
	r := bufio.NewReader(in)

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

	byTool := make(map[string]ToolDef, len(h.Tools))
	for _, t := range h.Tools {
		byTool[t.Name] = t
		if err := protocol.WriteMessage(out, protocol.Message{
			Type: protocol.TypeRegisterTool, Name: t.Name, Description: t.Description, Schema: t.Schema,
		}); err != nil {
			return err
		}
	}
	byCmd := make(map[string]CommandDef, len(h.Commands))
	for _, c := range h.Commands {
		byCmd[c.Name] = c
		if err := protocol.WriteMessage(out, protocol.Message{
			Type: protocol.TypeRegisterCommand, Name: c.Name, Description: c.Description,
		}); err != nil {
			return err
		}
	}
	byShort := make(map[string]ShortcutDef, len(h.Shortcuts))
	for _, s := range h.Shortcuts {
		byShort[s.Name] = s
		if err := protocol.WriteMessage(out, protocol.Message{
			Type: protocol.TypeRegisterShortcut, Name: s.Name, Description: s.Description,
		}); err != nil {
			return err
		}
	}
	for _, f := range h.Flags {
		args := map[string]any{"type": f.Type}
		if f.Default != nil {
			args["default"] = f.Default
		}
		if err := protocol.WriteMessage(out, protocol.Message{
			Type: protocol.TypeRegisterFlag, Name: f.Name, Description: f.Description, Args: args,
		}); err != nil {
			return err
		}
	}
	for _, f := range h.Flags {
		id := newID()
		if err := protocol.WriteMessage(out, protocol.Message{Type: protocol.TypeGetFlag, ID: id, Name: f.Name}); err != nil {
			return err
		}
		for {
			m, err := protocol.ReadMessage(r)
			if err != nil {
				return ignoreEOF(err)
			}
			if m.Type == protocol.TypeFlagValue && m.ID == id {
				if m.Payload != nil {
					if v, ok := m.Payload["value"]; ok {
						flagStore.Store(f.Name, v)
					}
				}
				break
			}
		}
	}
	for _, p := range h.Providers {
		if err := protocol.WriteMessage(out, protocol.Message{
			Type: protocol.TypeRegisterProvider, Name: p.ID, Args: p.Args,
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

	aborts := map[string]chan struct{}{}

	for {
		m, err := protocol.ReadMessage(r)
		if err != nil {
			return ignoreEOF(err)
		}
		switch m.Type {
		case protocol.TypeToolCall:
			result, isErr := "unknown tool: "+m.Name, true
			if t, ok := byTool[m.Name]; ok {
				result, isErr = t.Fn(context.Background(), m.Args)
			}
			if err := protocol.WriteMessage(out, protocol.Message{
				Type: protocol.TypeToolResult, ID: m.ID, Result: result, IsError: isErr,
			}); err != nil {
				return err
			}
		case protocol.TypeCommand:
			if c, ok := byCmd[m.Name]; ok && c.Fn != nil {
				c.Fn(m.Text)
			}
		case protocol.TypeShortcut:
			if s, ok := byShort[m.Name]; ok && s.Fn != nil {
				s.Fn()
			}
		case protocol.TypeEvent:
			var payload map[string]any
			if h.OnEvent != nil {
				payload = h.OnEvent(m.Event, m.Payload)
			}
			if m.ID != "" {
				if err := protocol.WriteMessage(out, protocol.Message{
					Type: protocol.TypeEventResult, ID: m.ID, Payload: payload,
				}); err != nil {
					return err
				}
			}
		case protocol.TypeOAuthLogin, protocol.TypeOAuthRefresh, protocol.TypeOAuthGetAPIKey:
			kind := "login"
			if m.Type == protocol.TypeOAuthRefresh {
				kind = "refresh"
			}
			if m.Type == protocol.TypeOAuthGetAPIKey {
				kind = "get_api_key"
			}
			var result map[string]any
			if h.OnOAuth != nil {
				result = h.OnOAuth(kind, m.Payload)
			}
			if err := protocol.WriteMessage(out, protocol.Message{
				Type: protocol.TypeOAuthResult, ID: m.ID, Payload: result,
			}); err != nil {
				return err
			}
		case protocol.TypeRefreshModels:
			var models []map[string]any
			if h.OnRefreshModels != nil {
				models = h.OnRefreshModels()
			}
			payload := map[string]any{"models": models}
			if err := protocol.WriteMessage(out, protocol.Message{
				Type: protocol.TypeRefreshModelsResult, ID: m.ID, Payload: payload,
			}); err != nil {
				return err
			}
		case protocol.TypeStreamStart:
			stop := make(chan struct{})
			aborts[m.ID] = stop
			id := m.ID
			req := m.Payload
			emit := func(event string, payload map[string]any) {
				_ = protocol.WriteMessage(out, protocol.Message{
					Type: protocol.TypeStreamEvent, ID: id, Event: event, Payload: payload,
				})
			}
			if h.OnStream != nil {
				go h.OnStream(req, emit, stop)
			} else {
				msg, _ := json.Marshal(ai.AssistantMessage{Role: ai.RoleAssistant, StopReason: ai.StopError, ErrorMessage: "no stream handler"})
				var payload map[string]any
				_ = json.Unmarshal(msg, &payload)
				emit("error", map[string]any{"reason": "error", "message": payload})
			}
		case protocol.TypeStreamAbort:
			if ch := aborts[m.ID]; ch != nil {
				close(ch)
				delete(aborts, m.ID)
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
