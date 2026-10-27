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
var (
	serveMu  sync.Mutex
	serveOut io.Writer
)

// SetActiveTools asks the host to replace the active tool set. Unknown names
// are ignored. Call from a tool or a session_start handler.
func SetActiveTools(names []string) error {
	if names == nil {
		names = []string{}
	}
	serveMu.Lock()
	out := serveOut
	serveMu.Unlock()
	if out == nil {
		return errors.New("ext: SetActiveTools called outside Serve")
	}
	serveMu.Lock()
	defer serveMu.Unlock()
	return protocol.WriteMessage(out, protocol.Message{
		Type:    protocol.TypeSetActiveTools,
		Payload: map[string]any{"names": names},
	})
}

// Flag returns a CLI flag value this process claimed during Serve handshake.
func Flag(name string) (any, bool) {
	v, ok := flagStore.Load(name)
	return v, ok
}

// ToolDef is a tool an extension exposes.
// Fn is the text result. Run, when set, is used instead and may also return
// structured content and usage.
type ToolDef struct {
	Name         string
	Description  string
	Schema       map[string]any
	Exposure     string
	Namespace    string
	Aliases      []string
	Annotations  map[string]any
	OutputSchema map[string]any
	ReachesTools []string
	Fn           func(ctx context.Context, args map[string]any) (string, bool)
	Run          func(ctx context.Context, args map[string]any) ToolResult
}

// Handler describes an extension: tools, commands, flags, events, and optional hooks.
type Handler struct {
	Name            string
	Tools           []ToolDef
	Commands        []CommandDef
	Shortcuts       []ShortcutDef
	Flags           []FlagDef
	Providers       []ProviderDef
	VirtualModels   []VirtualModelDef
	Events          []string
	OnEvent         func(event string, payload map[string]any) map[string]any
	OnOAuth         func(kind string, cred map[string]any) map[string]any
	OnRefreshModels func() []map[string]any
	OnStream        func(req map[string]any, emit func(event string, payload map[string]any), abort <-chan struct{})
	OnHostEvent     func(name string, args map[string]any) map[string]any
	// PrepareLoadout narrows the model-visible names for one turn.
	// A nil names slice leaves the set unchanged. An empty slice clears it.
	PrepareLoadout func(visible []string) (names []string, guidelines string)
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
	resetRuntime()
	serveMu.Lock()
	serveOut = out
	serveMu.Unlock()
	defer func() {
		serveMu.Lock()
		if serveOut == out {
			serveOut = nil
		}
		serveMu.Unlock()
	}()
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
		if err := checkToolSchema(t.Name, t.Schema); err != nil {
			return err
		}
		byTool[t.Name] = t
		if err := protocol.WriteMessage(out, protocol.Message{
			Type: protocol.TypeRegisterTool, Name: t.Name, Description: t.Description, Schema: t.Schema,
			Exposure: t.Exposure, Namespace: t.Namespace, Aliases: t.Aliases,
			Annotations: t.Annotations, OutputSchema: t.OutputSchema, ReachesTools: t.ReachesTools,
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
	routes := map[string]func(context.Context, RouteRequest) (RouteResult, error){}
	for _, vm := range h.VirtualModels {
		if err := protocol.WriteMessage(out, protocol.Message{
			Type: protocol.TypeRegisterVirtualModel, Args: virtualModelArgs(vm),
		}); err != nil {
			return err
		}
		if vm.Route != nil {
			routes[vm.Provider+"\x00"+vm.ID] = vm.Route
		}
	}
	if len(h.Events) > 0 {
		if err := protocol.WriteMessage(out, protocol.Message{Type: protocol.TypeSubscribe, Events: h.Events}); err != nil {
			return err
		}
	}
	if h.PrepareLoadout != nil {
		if err := protocol.WriteMessage(out, protocol.Message{Type: protocol.TypeRegisterLoadout}); err != nil {
			return err
		}
	}
	if err := protocol.WriteMessage(out, protocol.Message{Type: protocol.TypeInitialized}); err != nil {
		return err
	}

	aborts := map[string]chan struct{}{}
	var routeMu sync.Mutex
	routeCancel := map[string]context.CancelFunc{}
	runtime.mu.Lock()
	runtime.onHost = h.OnHostEvent
	runtime.mu.Unlock()

	for {
		m, err := protocol.ReadMessage(r)
		if err != nil {
			return ignoreEOF(err)
		}
		switch m.Type {
		case protocol.TypeToolCall:
			id := m.ID
			name := m.Name
			args := m.Args
			callID := m.ToolCallID
			t, ok := byTool[name]
			go func() {
				result := ToolResult{Text: "unknown tool: " + name, IsError: true}
				if ok {
					ctx := WithToolCallID(context.Background(), callID)
					if t.Run != nil {
						result = t.Run(ctx, args)
					} else if t.Fn != nil {
						text, isErr := t.Fn(ctx, args)
						result = ToolResult{Text: text, IsError: isErr}
					}
				}
				msg := protocol.Message{
					Type: protocol.TypeToolResult, ID: id, Result: result.Text, IsError: result.IsError,
					StructuredContent: append(json.RawMessage(nil), result.Structured...),
					Usage:             usageToMap(result.Usage),
				}
				_ = writeOut(msg)
			}()
		case protocol.TypePrepareLoadout:
			id := m.ID
			visible := payloadStringSlice(m.Payload, "names")
			go func() {
				var names []string
				guidelines := ""
				if h.PrepareLoadout != nil {
					names, guidelines = h.PrepareLoadout(visible)
				}
				payload := map[string]any{}
				if names != nil {
					listed := make([]any, len(names))
					for i, name := range names {
						listed[i] = name
					}
					payload["names"] = listed
				}
				if guidelines != "" {
					payload["guidelines"] = guidelines
				}
				_ = writeOut(protocol.Message{Type: protocol.TypePrepareLoadoutResult, ID: id, Payload: payload})
			}()
		case protocol.TypeCommand:
			if c, ok := byCmd[m.Name]; ok && c.Fn != nil {
				text := m.Text
				fn := c.Fn
				go fn(text)
			}
		case protocol.TypeShortcut:
			if s, ok := byShort[m.Name]; ok && s.Fn != nil {
				fn := s.Fn
				go fn()
			}
		case protocol.TypeEvent:
			id := m.ID
			ev := m.Event
			payload := m.Payload
			go func() {
				var outPayload map[string]any
				if h.OnEvent != nil {
					outPayload = h.OnEvent(ev, payload)
				}
				if id != "" {
					_ = writeOut(protocol.Message{Type: protocol.TypeEventResult, ID: id, Payload: outPayload})
				}
			}()
		case protocol.TypeUIResult, protocol.TypeHostResult:
			deliverReply(m)
		case protocol.TypeHostEvent:
			id := m.ID
			name := m.Name
			args := m.Args
			go func() {
				if name == "bus" {
					ev, _ := args["event"].(string)
					data, _ := args["data"].(map[string]any)
					dispatchBus(ev, data)
					if id != "" {
						_ = writeOut(protocol.Message{Type: protocol.TypeHostEventResult, ID: id, Args: map[string]any{}})
					}
					return
				}
				var result map[string]any
				runtime.mu.Lock()
				fn := runtime.onHost
				runtime.mu.Unlock()
				if fn != nil {
					result = fn(name, args)
				} else if h.OnHostEvent != nil {
					result = h.OnHostEvent(name, args)
				}
				if id != "" {
					_ = writeOut(protocol.Message{Type: protocol.TypeHostEventResult, ID: id, Args: result})
				}
			}()
		case protocol.TypeOAuthLogin, protocol.TypeOAuthRefresh, protocol.TypeOAuthGetAPIKey:
			kind := "login"
			if m.Type == protocol.TypeOAuthRefresh {
				kind = "refresh"
			}
			if m.Type == protocol.TypeOAuthGetAPIKey {
				kind = "get_api_key"
			}
			id := m.ID
			cred := m.Payload
			go func() {
				var result map[string]any
				if h.OnOAuth != nil {
					result = h.OnOAuth(kind, cred)
				}
				_ = writeOut(protocol.Message{Type: protocol.TypeOAuthResult, ID: id, Payload: result})
			}()
		case protocol.TypeRefreshModels:
			id := m.ID
			go func() {
				var models []map[string]any
				if h.OnRefreshModels != nil {
					models = h.OnRefreshModels()
				}
				_ = writeOut(protocol.Message{Type: protocol.TypeRefreshModelsResult, ID: id, Payload: map[string]any{"models": models}})
			}()
		case protocol.TypeStreamStart:
			stop := make(chan struct{})
			aborts[m.ID] = stop
			id := m.ID
			req := m.Payload
			emit := func(event string, payload map[string]any) {
				_ = writeOut(protocol.Message{Type: protocol.TypeStreamEvent, ID: id, Event: event, Payload: payload})
			}
			if h.OnStream != nil {
				go h.OnStream(req, emit, stop)
			} else {
				msg, _ := json.Marshal(ai.AssistantMessage{Role: ai.RoleAssistant, StopReason: ai.StopError, ErrorMessage: "no stream handler"})
				var payload map[string]any
				_ = json.Unmarshal(msg, &payload)
				emit("error", map[string]any{"reason": "error", "message": payload})
			}
		case protocol.TypeRoute:
			id := m.ID
			payload := m.Payload
			req := routeRequestFromPayload(payload)
			fn := routes[req.Model.Provider+"\x00"+req.Model.ID]
			ctx, cancel := context.WithCancel(context.Background())
			routeMu.Lock()
			routeCancel[id] = cancel
			routeMu.Unlock()
			go func() {
				defer func() {
					cancel()
					routeMu.Lock()
					delete(routeCancel, id)
					routeMu.Unlock()
				}()
				res, err := callRoute(ctx, fn, req)
				if err != nil {
					_ = writeOut(protocol.Message{
						Type: protocol.TypeRouteResult, ID: id, IsError: true, Result: err.Error(),
					})
					return
				}
				_ = writeOut(protocol.Message{
					Type: protocol.TypeRouteResult, ID: id, Payload: routeResultPayload(req, res),
				})
			}()
		case protocol.TypeRouteAbort:
			routeMu.Lock()
			cancel := routeCancel[m.ID]
			routeMu.Unlock()
			if cancel != nil {
				cancel()
			}
		case protocol.TypeStreamAbort:
			if ch := aborts[m.ID]; ch != nil {
				close(ch)
				delete(aborts, m.ID)
			}
		case protocol.TypePing:
			if err := writeOut(protocol.Message{Type: protocol.TypePong}); err != nil {
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
