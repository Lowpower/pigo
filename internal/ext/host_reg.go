package ext

import (
	"context"
	"errors"

	"github.com/Lowpower/pigo/internal/protocol"
)

var errClosed = errors.New("ext: extension closed")

// CommandDef is a slash command the extension registers.
type CommandDef struct {
	Name        string
	Description string
	Fn          func(args string)
}

// ShortcutDef is a keybinding the extension registers.
type ShortcutDef struct {
	Name        string
	Description string
	Fn          func()
}

// FlagDef is a CLI flag the extension registers.
type FlagDef struct {
	Name        string
	Description string
	Type        string // "boolean" or "string"
	Default     any
}

// ProviderDef is a JSON provider registration.
type ProviderDef struct {
	ID   string
	Args map[string]any
}

// UnknownFlag is one leftover CLI flag collected before spawn.
type UnknownFlag struct {
	Name     string
	Value    string
	HasValue bool
	Present  bool
}

// RegisteredCommand is a command collected from the child.
type RegisteredCommand struct {
	Name        string
	Description string
}

// RegisteredShortcut is a shortcut collected from the child.
type RegisteredShortcut struct {
	Name        string
	Description string
}

type registeredFlag struct {
	def     FlagDef
	value   any
	claimed bool
}

type registeredProvider struct {
	id   string
	args map[string]any
}

func (h *Host) Commands() []RegisteredCommand {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := append([]RegisteredCommand(nil), h.commands...)
	return out
}

func (h *Host) HasShortcut(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.shortcuts {
		if s.Name == name {
			return true
		}
	}
	return false
}

func (h *Host) Shortcuts() []RegisteredShortcut {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]RegisteredShortcut(nil), h.shortcuts...)
}

func (h *Host) FlagValue(name string) (any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, ok := h.flags[name]
	if !ok {
		return nil, false
	}
	if f.value != nil {
		return f.value, true
	}
	if f.def.Default != nil {
		return f.def.Default, true
	}
	return nil, false
}

func (h *Host) UnclaimedFlags() []UnknownFlag {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []UnknownFlag
	for _, u := range h.unknown {
		if !h.claimedUnknown[u.Name] {
			out = append(out, u)
		}
	}
	return out
}

func (h *Host) Subscribed(event string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subscribed[event]
}

func (h *Host) Providers() []ProviderDef {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]ProviderDef, 0, len(h.providers))
	for _, p := range h.providers {
		out = append(out, ProviderDef{ID: p.id, Args: p.args})
	}
	return out
}

func (h *Host) SendCommand(name, args string) error {
	return h.send(protocol.Message{Type: protocol.TypeCommand, Name: name, Text: args})
}

func (h *Host) SendShortcut(name string) error {
	return h.send(protocol.Message{Type: protocol.TypeShortcut, Name: name})
}

// QueryEvent sends a subscribed event and waits for event_result.
// Unsubscribed events return an empty payload without sending.
func (h *Host) QueryEvent(ctx context.Context, event string, payload map[string]any) (map[string]any, error) {
	if !h.Subscribed(event) {
		return map[string]any{}, nil
	}
	m, err := h.roundTrip(ctx, protocol.Message{
		Type: protocol.TypeEvent, Event: event, Payload: payload,
	}, protocol.TypeEventResult)
	if err != nil {
		return map[string]any{}, nil
	}
	if m.Payload == nil {
		return map[string]any{}, nil
	}
	return m.Payload, nil
}

func (h *Host) roundTrip(ctx context.Context, m protocol.Message, wantType string) (protocol.Message, error) {
	id := newID()
	m.ID = id
	ch := make(chan protocol.Message, 1)

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return protocol.Message{}, errClosed
	}
	h.pending[id] = ch
	h.mu.Unlock()

	if err := h.send(m); err != nil {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return protocol.Message{}, err
	}

	callCtx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	select {
	case got, ok := <-ch:
		if !ok {
			return protocol.Message{}, errClosed
		}
		if wantType != "" && got.Type != wantType {
			return got, nil
		}
		return got, nil
	case <-callCtx.Done():
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return protocol.Message{}, callCtx.Err()
	}
}

func (h *Host) claimFlag(name, typ string, def any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.flags == nil {
		h.flags = map[string]registeredFlag{}
	}
	rec := registeredFlag{def: FlagDef{Name: name, Type: typ, Default: def}}
	for _, u := range h.unknown {
		if u.Name != name {
			continue
		}
		h.claimedUnknown[u.Name] = true
		rec.claimed = true
		switch typ {
		case "string":
			if u.HasValue {
				rec.value = u.Value
			} else {
				rec.value = ""
			}
		default:
			rec.value = true
		}
		break
	}
	if rec.value == nil && def != nil {
		rec.value = def
	}
	h.flags[name] = rec
}

func (h *Host) replyFlag(id, name string) {
	h.mu.Lock()
	f, ok := h.flags[name]
	h.mu.Unlock()
	payload := map[string]any{}
	if ok && f.value != nil {
		payload["value"] = f.value
	}
	_ = h.send(protocol.Message{Type: protocol.TypeFlagValue, ID: id, Payload: payload})
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	default:
		return false
	}
}
