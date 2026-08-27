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
	// UI handles extension UI methods (select/confirm/...) in RPC mode.
	UI func(method string, args map[string]any, timeout time.Duration) map[string]any
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
	ui          func(method string, args map[string]any, timeout time.Duration) map[string]any

	writeMu sync.Mutex

	mu       sync.Mutex
	tools    []registeredTool
	pending  map[string]chan protocol.Message
	closed   bool
	waitErr  error
	waitDone chan struct{}
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
		name:        name,
		cmd:         cmd,
		stdin:       stdin,
		callTimeout: callTimeout,
		notify:      opts.Notify,
		ui:          opts.UI,
		pending:     make(map[string]chan protocol.Message),
		waitDone:    make(chan struct{}),
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
			h.mu.Unlock()
			signalReady(fmt.Errorf("ext %q exited before initialization", h.name))
			return
		}

		switch m.Type {
		case protocol.TypeHello:
			_ = h.send(protocol.Message{Type: protocol.TypeReady})
		case protocol.TypeRegisterTool:
			h.mu.Lock()
			h.tools = append(h.tools, registeredTool{name: m.Name, description: m.Description, schema: m.Schema})
			h.mu.Unlock()
		case protocol.TypeInitialized:
			signalReady(nil)
		case protocol.TypeToolResult:
			h.mu.Lock()
			ch := h.pending[m.ID]
			delete(h.pending, m.ID)
			h.mu.Unlock()
			if ch != nil {
				ch <- m
				close(ch)
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

// SetUI installs the RPC (or TUI) handler for extension UI requests.
func (h *Host) SetUI(ui func(method string, args map[string]any, timeout time.Duration) map[string]any) {
	h.mu.Lock()
	h.ui = ui
	h.mu.Unlock()
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
