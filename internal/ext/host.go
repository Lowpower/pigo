package ext

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/protocol"
	"github.com/Lowpower/pigo/internal/shell"
)

// APIVersion is the extension RPC version the host speaks.
const APIVersion = 1

// Options configures Spawn.
type Options struct {
	// Env is appended to the host environment for the child process.
	Env []string
	// InitTimeout bounds the handshake+registration phase (default 10s).
	InitTimeout time.Duration
	// CallTimeout bounds a single tool call (default 60s). ctx can shorten it.
	CallTimeout time.Duration
	// Notify receives the extension's notify messages (level, text). Optional.
	Notify func(level, text string)
	// Status receives keyed status_line_item updates (empty text clears).
	Status func(key, text string)
	// UI handles extension UI methods (select/confirm/...) in RPC mode.
	UI func(method string, args map[string]any, timeout time.Duration) map[string]any
	// UnknownFlags are leftover CLI flags for register_flag to claim.
	UnknownFlags []UnknownFlag
}

type registeredTool struct {
	name        string
	description string
	schema      map[string]any
}

// Host is the host side of one extension subprocess.
type Host struct {
	name        string
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	callTimeout time.Duration
	notify      func(level, text string)
	status      func(key, text string)
	ui          func(method string, args map[string]any, timeout time.Duration) map[string]any
	hostCall    func(name string, args map[string]any) map[string]any
	shutdownReq func()

	writeMu sync.Mutex

	mu             sync.Mutex
	tools          []registeredTool
	commands       []RegisteredCommand
	shortcuts      []RegisteredShortcut
	flags          map[string]registeredFlag
	unknown        []UnknownFlag
	claimedUnknown map[string]bool
	subscribed     map[string]bool
	providers      []registeredProvider
	pending        map[string]chan protocol.Message
	streamCh       map[string]chan protocol.Message
	closed         bool
	waitErr        error
	waitDone       chan struct{}

	providerHook    func(id string, args map[string]any, drop bool)
	activeToolsHook func([]string)
	busSubs         map[string]bool
}

// Spawn starts an extension process (argv) and completes the handshake: it waits
// for the extension to announce itself, register its tools, and signal that it
// finished initializing. The returned Host is ready to serve tool calls.
func Spawn(ctx context.Context, name string, argv []string, opts Options) (*Host, error) {
	if len(argv) == 0 {
		return nil, errors.New("ext: empty argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), opts.Env...)
	cmd.Stderr = os.Stderr
	shell.Prepare(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	callTimeout := opts.CallTimeout
	if callTimeout <= 0 {
		callTimeout = 60 * time.Second
	}
	h := &Host{
		name:           name,
		cmd:            cmd,
		stdin:          stdin,
		callTimeout:    callTimeout,
		notify:         opts.Notify,
		status:         opts.Status,
		ui:             opts.UI,
		unknown:        append([]UnknownFlag(nil), opts.UnknownFlags...),
		claimedUnknown: map[string]bool{},
		flags:          map[string]registeredFlag{},
		subscribed:     map[string]bool{},
		pending:        make(map[string]chan protocol.Message),
		streamCh:       map[string]chan protocol.Message{},
		waitDone:       make(chan struct{}),
		busSubs:        map[string]bool{},
	}

	ready := make(chan error, 1)
	var readyOnce sync.Once
	signal := func(err error) { readyOnce.Do(func() { ready <- err }) }
	go h.readLoop(bufio.NewReader(stdout), signal)

	initTimeout := opts.InitTimeout
	if initTimeout <= 0 {
		initTimeout = 10 * time.Second
	}
	select {
	case err := <-ready:
		if err != nil {
			_ = h.Close()
			return nil, err
		}
	case <-time.After(initTimeout):
		_ = h.Close()
		return nil, fmt.Errorf("ext %q: initialization timed out", name)
	case <-ctx.Done():
		_ = h.Close()
		return nil, ctx.Err()
	}
	return h, nil
}

func (h *Host) readLoop(r *bufio.Reader, signalReady func(error)) {
	defer close(h.waitDone)
	for {
		m, err := protocol.ReadMessage(r)
		if err != nil {
			h.mu.Lock()
			h.closed = true
			if h.waitErr == nil {
				h.waitErr = err
			}
			for id, ch := range h.pending {
				close(ch)
				delete(h.pending, id)
			}
			for id, ch := range h.streamCh {
				close(ch)
				delete(h.streamCh, id)
			}
			h.mu.Unlock()
			signalReady(fmt.Errorf("ext %q exited before initialization", h.name))
			return
		}

		switch m.Type {
		case protocol.TypeHello:
			_ = h.send(protocol.Message{Type: protocol.TypeReady})
		case protocol.TypeRegisterTool:
			if err := checkToolSchema(m.Name, m.Schema); err != nil {
				if h.notify != nil {
					h.notify("error", err.Error())
				}
				break
			}
			h.mu.Lock()
			h.tools = append(h.tools, registeredTool{name: m.Name, description: m.Description, schema: m.Schema})
			h.mu.Unlock()
		case protocol.TypeRegisterCommand:
			h.mu.Lock()
			h.commands = append(h.commands, RegisteredCommand{Name: m.Name, Description: m.Description})
			h.mu.Unlock()
		case protocol.TypeRegisterShortcut:
			h.mu.Lock()
			h.shortcuts = append(h.shortcuts, RegisteredShortcut{Name: m.Name, Description: m.Description})
			h.mu.Unlock()
		case protocol.TypeRegisterFlag:
			typ := ""
			var def any
			if m.Args != nil {
				typ, _ = m.Args["type"].(string)
				def = m.Args["default"]
			}
			h.claimFlag(m.Name, typ, def)
		case protocol.TypeSubscribe:
			h.mu.Lock()
			if h.subscribed == nil {
				h.subscribed = map[string]bool{}
			}
			for _, ev := range m.Events {
				h.subscribed[ev] = true
			}
			h.mu.Unlock()
		case protocol.TypeRegisterProvider:
			h.mu.Lock()
			h.providers = append(h.providers, registeredProvider{id: m.Name, args: m.Args})
			hook := h.providerHook
			h.mu.Unlock()
			if hook != nil {
				hook(m.Name, m.Args, false)
			}
		case protocol.TypeUnregisterProvider:
			h.mu.Lock()
			filtered := h.providers[:0]
			for _, p := range h.providers {
				if p.id != m.Name {
					filtered = append(filtered, p)
				}
			}
			h.providers = filtered
			hook := h.providerHook
			h.mu.Unlock()
			if hook != nil {
				hook(m.Name, nil, true)
			}
		case protocol.TypeGetFlag:
			h.replyFlag(m.ID, m.Name)
		case protocol.TypeInitialized:
			signalReady(nil)
		case protocol.TypeToolResult, protocol.TypeEventResult, protocol.TypeOAuthResult, protocol.TypeRefreshModelsResult, protocol.TypeHostEventResult:
			h.deliverPending(m)
		case protocol.TypeStreamEvent:
			h.deliverStream(m)
		case protocol.TypeStatusItem:
			if h.status != nil {
				h.status(m.Name, m.Text)
			}
		case protocol.TypeNotify:
			if h.notify != nil {
				h.notify(m.Level, m.Text)
			}
		case protocol.TypeUIRequest:
			h.mu.Lock()
			ui := h.ui
			h.mu.Unlock()
			if ui == nil {
				continue
			}
			go func(m protocol.Message) {
				var timeout time.Duration
				if m.Args != nil {
					switch v := m.Args["timeout"].(type) {
					case float64:
						timeout = time.Duration(v) * time.Millisecond
					case int:
						timeout = time.Duration(v) * time.Millisecond
					}
				}
				result := ui(m.Name, m.Args, timeout)
				_ = h.send(protocol.Message{Type: protocol.TypeUIResult, ID: m.ID, Args: result})
			}(m)
		case protocol.TypeSetActiveTools:
			names := payloadStringSlice(m.Payload, "names")
			h.mu.Lock()
			hook := h.activeToolsHook
			h.mu.Unlock()
			if hook != nil {
				hook(names)
			}
		case protocol.TypeHostRequest:
			h.mu.Lock()
			call := h.hostCall
			ui := h.ui
			h.mu.Unlock()
			go func(m protocol.Message) {
				var result map[string]any
				isErr := false
				if isUIHostMethod(m.Name) && ui != nil {
					result = ui(m.Name, m.Args, hostCallTimeout(m.Args))
				} else if call != nil {
					result = call(m.Name, m.Args)
				} else {
					result = map[string]any{"error": "no host call handler"}
					isErr = true
				}
				if result == nil {
					result = map[string]any{}
				}
				if _, ok := result["error"]; ok {
					isErr = true
				}
				_ = h.send(protocol.Message{Type: protocol.TypeHostResult, ID: m.ID, Args: result, IsError: isErr})
			}(m)
		case protocol.TypeShutdownRequest:
			h.mu.Lock()
			fn := h.shutdownReq
			h.mu.Unlock()
			if fn != nil {
				fn()
			}
		}
	}
}

func (h *Host) deliverPending(m protocol.Message) {
	h.mu.Lock()
	ch := h.pending[m.ID]
	delete(h.pending, m.ID)
	h.mu.Unlock()
	if ch != nil {
		ch <- m
		close(ch)
	}
}

func (h *Host) deliverStream(m protocol.Message) {
	h.mu.Lock()
	ch := h.streamCh[m.ID]
	h.mu.Unlock()
	if ch != nil {
		select {
		case ch <- m:
		default:
			ch <- m
		}
	}
}

func (h *Host) send(m protocol.Message) error {
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed {
		return errors.New("ext: extension closed")
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return protocol.WriteMessage(h.stdin, m)
}

// Tools returns the extension's registered tools as ai.Tool definitions.
func (h *Host) Tools() []ai.Tool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]ai.Tool, 0, len(h.tools))
	for _, t := range h.tools {
		out = append(out, ai.Tool{Name: t.name, Description: t.description, Parameters: t.schema})
	}
	return out
}

// Name is the spawn label (usually argv0).
func (h *Host) Name() string { return h.name }

// SetUI installs the RPC (or TUI) handler for extension UI requests.
func (h *Host) SetUI(ui func(method string, args map[string]any, timeout time.Duration) map[string]any) {
	h.mu.Lock()
	h.ui = ui
	h.mu.Unlock()
}

// SetNotify replaces the notify callback after spawn.
func (h *Host) SetNotify(fn func(level, text string)) {
	h.mu.Lock()
	h.notify = fn
	h.mu.Unlock()
}

// SetStatus replaces the status_line_item callback after spawn.
func (h *Host) SetStatus(fn func(key, text string)) {
	h.mu.Lock()
	h.status = fn
	h.mu.Unlock()
}

// SetProviderHook is called for register/unregister after the handshake.
func (h *Host) SetProviderHook(fn func(id string, args map[string]any, drop bool)) {
	h.mu.Lock()
	h.providerHook = fn
	h.mu.Unlock()
}

// SetActiveToolsHook is called when the extension sends set_active_tools.
func (h *Host) SetActiveToolsHook(fn func([]string)) {
	h.mu.Lock()
	h.activeToolsHook = fn
	h.mu.Unlock()
}

// SetHostCall handles session/runtime host_request methods.
func (h *Host) SetHostCall(fn func(name string, args map[string]any) map[string]any) {
	h.mu.Lock()
	h.hostCall = fn
	h.mu.Unlock()
}

// SetShutdownRequest is called when the extension asks the host to exit.
func (h *Host) SetShutdownRequest(fn func()) {
	h.mu.Lock()
	h.shutdownReq = fn
	h.mu.Unlock()
}

// SubscribeBus records interest in an extension-bus event name.
func (h *Host) SubscribeBus(event string) {
	h.mu.Lock()
	if h.busSubs == nil {
		h.busSubs = map[string]bool{}
	}
	h.busSubs[event] = true
	h.mu.Unlock()
}

// WantsBus reports whether this extension subscribed to an event-bus name.
func (h *Host) WantsBus(event string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.busSubs[event]
}

// SendHostEvent pushes a host_event. If wait is true, it waits for host_event_result.
func (h *Host) SendHostEvent(ctx context.Context, name string, args map[string]any, wait bool) map[string]any {
	id := ""
	var ch chan protocol.Message
	if wait {
		id = newID()
		ch = make(chan protocol.Message, 1)
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return map[string]any{"cancelled": true}
		}
		h.pending[id] = ch
		h.mu.Unlock()
	}
	if err := h.send(protocol.Message{Type: protocol.TypeHostEvent, ID: id, Name: name, Args: args}); err != nil {
		if wait {
			h.mu.Lock()
			delete(h.pending, id)
			h.mu.Unlock()
		}
		return map[string]any{"error": err.Error()}
	}
	if !wait {
		return map[string]any{}
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return map[string]any{"cancelled": true}
		}
		if m.Args == nil {
			return map[string]any{}
		}
		return m.Args
	case <-ctx.Done():
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return map[string]any{"cancelled": true}
	}
}

func isUIHostMethod(name string) bool {
	switch name {
	case "select", "confirm", "input", "editor",
		"setWidget", "setTitle", "set_editor_text",
		"getEditorText", "pasteToEditor",
		"setFooter", "setHeader", "getFooterData",
		"setWorkingMessage", "setWorkingVisible", "setWorkingIndicator",
		"setHiddenThinkingLabel",
		"setToolsExpanded", "getToolsExpanded",
		"getAllThemes", "getTheme", "setTheme", "getCurrentTheme",
		"custom.open", "custom.update", "custom.close",
		"custom.focus", "custom.unfocus", "custom.hide", "custom.setHidden",
		"terminal_input.subscribe":
		return true
	default:
		return false
	}
}

func hostCallTimeout(args map[string]any) time.Duration {
	if args == nil {
		return 0
	}
	switch v := args["timeout"].(type) {
	case float64:
		return time.Duration(v) * time.Millisecond
	case int:
		return time.Duration(v) * time.Millisecond
	}
	return 0
}

// HasTool reports whether the extension registered a tool with this name.
func (h *Host) HasTool(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range h.tools {
		if t.name == name {
			return true
		}
	}
	return false
}

// CallTool sends a tool call to the extension and waits for its result. It is
// safe for the agent's ToolExecutor: CallTool(ctx, name, args) -> (result, isError).
func (h *Host) CallTool(ctx context.Context, name string, args map[string]any) (string, bool) {
	id := newID()
	ch := make(chan protocol.Message, 1)

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return "extension is not running", true
	}
	h.pending[id] = ch
	h.mu.Unlock()

	if err := h.send(protocol.Message{Type: protocol.TypeToolCall, ID: id, Name: name, Args: args}); err != nil {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return err.Error(), true
	}

	callCtx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	select {
	case m, ok := <-ch:
		if !ok {
			return "extension exited during tool call", true
		}
		return m.Result, m.IsError
	case <-callCtx.Done():
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return "extension tool call timed out: " + callCtx.Err().Error(), true
	}
}

// EmitEvent sends a subscribed lifecycle event to the extension (fire-and-forget).
func (h *Host) EmitEvent(event string, payload map[string]any) {
	_ = h.send(protocol.Message{Type: protocol.TypeEvent, Event: event, Payload: payload})
}

// Close asks the extension to shut down, then terminates its process group.
func (h *Host) Close() error {
	h.mu.Lock()
	already := h.closed
	h.mu.Unlock()
	if !already {
		_ = h.send(protocol.Message{Type: protocol.TypeShutdown})
	}
	_ = h.stdin.Close()

	done := make(chan struct{})
	go func() { _ = h.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		if h.cmd.Process != nil {
			_ = shell.KillTree(h.cmd.Process.Pid)
		}
		<-done
	}

	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	return nil
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}

func payloadStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	switch t := payload[key].(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			s, ok := x.(string)
			if ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
