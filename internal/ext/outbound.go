package ext

import (
	"errors"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/protocol"
)

type serveRuntime struct {
	mu      sync.Mutex
	pending map[string]chan protocol.Message
	bus     map[string][]func(map[string]any)
	onHost  func(name string, args map[string]any) map[string]any
}

var runtime = &serveRuntime{pending: map[string]chan protocol.Message{}, bus: map[string][]func(map[string]any){}}

func resetRuntime() {
	runtime.mu.Lock()
	runtime.pending = map[string]chan protocol.Message{}
	runtime.bus = map[string][]func(map[string]any){}
	runtime.onHost = nil
	runtime.mu.Unlock()
}

func writeOut(m protocol.Message) error {
	serveMu.Lock()
	out := serveOut
	serveMu.Unlock()
	if out == nil {
		return errors.New("ext: not serving")
	}
	serveMu.Lock()
	defer serveMu.Unlock()
	return protocol.WriteMessage(out, m)
}

func waitReply(id string, timeout time.Duration) (protocol.Message, error) {
	ch := make(chan protocol.Message, 1)
	runtime.mu.Lock()
	runtime.pending[id] = ch
	runtime.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case m := <-ch:
		return m, nil
	case <-timer.C:
		runtime.mu.Lock()
		delete(runtime.pending, id)
		runtime.mu.Unlock()
		return protocol.Message{}, errors.New("ext: host call timed out")
	}
}

func deliverReply(m protocol.Message) {
	runtime.mu.Lock()
	ch := runtime.pending[m.ID]
	delete(runtime.pending, m.ID)
	runtime.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

// Notify shows a message to the user.
func Notify(text, level string) error {
	if level == "" {
		level = "info"
	}
	return writeOut(protocol.Message{Type: protocol.TypeNotify, Text: text, Level: level})
}

// Status sets or clears a footer status_line_item. Empty text clears.
func Status(key, text string) error {
	return writeOut(protocol.Message{Type: protocol.TypeStatusItem, Name: key, Text: text})
}

// UI sends a ui_request and waits for ui_result.
func UI(method string, args map[string]any) (map[string]any, error) {
	id := newID()
	if args == nil {
		args = map[string]any{}
	}
	if err := writeOut(protocol.Message{Type: protocol.TypeUIRequest, ID: id, Name: method, Args: args}); err != nil {
		return nil, err
	}
	m, err := waitReply(id, 70*time.Second)
	if err != nil {
		return nil, err
	}
	if m.Args == nil {
		return map[string]any{}, nil
	}
	return m.Args, nil
}

// HostCall invokes a host method (session, runtime, or extra UI).
func HostCall(name string, args map[string]any) (map[string]any, error) {
	id := newID()
	if args == nil {
		args = map[string]any{}
	}
	if err := writeOut(protocol.Message{Type: protocol.TypeHostRequest, ID: id, Name: name, Args: args}); err != nil {
		return nil, err
	}
	m, err := waitReply(id, 70*time.Second)
	if err != nil {
		return nil, err
	}
	if m.IsError {
		msg, _ := m.Args["error"].(string)
		if msg == "" {
			msg = "host call failed"
		}
		return m.Args, errors.New(msg)
	}
	if m.Args == nil {
		return map[string]any{}, nil
	}
	return m.Args, nil
}

// EventsOn subscribes to the extension event bus.
func EventsOn(event string, fn func(map[string]any)) error {
	if event == "" || fn == nil {
		return errors.New("ext: events.on requires event and handler")
	}
	runtime.mu.Lock()
	runtime.bus[event] = append(runtime.bus[event], fn)
	runtime.mu.Unlock()
	_, err := HostCall("events.on", map[string]any{"event": event})
	return err
}

// EventsEmit publishes on the extension event bus.
func EventsEmit(event string, data map[string]any) error {
	_, err := HostCall("events.emit", map[string]any{"event": event, "data": data})
	return err
}

// RequestShutdown asks the host to exit when idle.
func RequestShutdown() error {
	return writeOut(protocol.Message{Type: protocol.TypeShutdownRequest})
}

func dispatchBus(event string, data map[string]any) {
	runtime.mu.Lock()
	fns := append([]func(map[string]any){}, runtime.bus[event]...)
	runtime.mu.Unlock()
	for _, fn := range fns {
		fn(data)
	}
}
