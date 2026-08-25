package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

// MaxFrameLen is the maximum size of one framed payload.
const MaxFrameLen = 16 * 1024 * 1024

// Message type tags for the extension RPC (handshake, registration, tool
// invocation, events, shallow UI, lifecycle).
const (
	TypeHello           = "hello"            // ext -> host: announce { ExtName, APIVersion }
	TypeReady           = "ready"            // host -> ext: handshake accepted
	TypeInitialized     = "initialized"      // ext -> host: finished registering
	TypeRegisterTool    = "register_tool"    // ext -> host: { Name, Description, Schema }
	TypeRegisterCommand = "register_command" // ext -> host: { Name, Description }
	TypeSubscribe       = "subscribe"        // ext -> host: { Events }
	TypeToolCall        = "tool_call"        // host -> ext: { ID, Name, Args }
	TypeToolResult      = "tool_result"      // ext -> host: { ID, Result, IsError }
	TypeCommand         = "command"          // host -> ext: { Name, Args }
	TypeEvent           = "event"            // host -> ext: { Event, Payload }
	TypeNotify          = "notify"           // ext -> host: { Text, Level }
	TypeStatusItem      = "status_line_item" // ext -> host: { Text }
	TypeUIRequest       = "ui_request"       // ext -> host: { ID, Name=method, Args }
	TypeUIResult        = "ui_result"        // host -> ext: { ID, Args }
	TypePing            = "ping"             // host -> ext
	TypePong            = "pong"             // ext -> host
	TypeShutdown        = "shutdown"         // host -> ext: terminate gracefully
)

// Message is the RPC envelope. Type selects which fields are meaningful.
type Message struct {
	Type string `json:"type"`

	ExtName    string `json:"extName,omitempty"`
	APIVersion int    `json:"apiVersion,omitempty"`

	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`

	ID      string         `json:"id,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Result  string         `json:"result,omitempty"`
	IsError bool           `json:"isError,omitempty"`

	Events  []string       `json:"events,omitempty"`
	Event   string         `json:"event,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`

	Text  string `json:"text,omitempty"`
	Level string `json:"level,omitempty"`
}

// WriteMessage encodes m as a length-prefixed JSON frame.
func WriteMessage(w io.Writer, m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(b) > MaxFrameLen {
		return errors.New("protocol: frame exceeds max length")
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// ReadMessage reads one length-prefixed JSON frame.
func ReadMessage(r io.Reader) (Message, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Message{}, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxFrameLen {
		return Message{}, errors.New("protocol: frame exceeds max length")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Message{}, err
	}
	var m Message
	if err := json.Unmarshal(buf, &m); err != nil {
		return Message{}, err
	}
	return m, nil
}
