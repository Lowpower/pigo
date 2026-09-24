package ext

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/Lowpower/pigo/internal/protocol"
)

// replyDuringWrite delivers host_result from inside the body Write, before
// WriteMessage returns. A reply registered only after writeOut misses it.
type replyDuringWrite struct {
	buf bytes.Buffer
}

func (w *replyDuringWrite) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	if err != nil || w.buf.Len() < 4 {
		return n, err
	}
	size := int(binary.BigEndian.Uint32(w.buf.Bytes()[:4]))
	if w.buf.Len() < 4+size {
		return n, nil
	}
	var req protocol.Message
	if err := json.Unmarshal(w.buf.Bytes()[4:4+size], &req); err != nil {
		return n, err
	}
	deliverReply(protocol.Message{
		Type: protocol.TypeHostResult,
		ID:   req.ID,
		Args: map[string]any{"ok": true},
	})
	return n, nil
}

func TestHostCallKeepsReplyThatArrivesDuringWrite(t *testing.T) {
	resetRuntime()
	t.Cleanup(resetRuntime)

	w := &replyDuringWrite{}
	serveMu.Lock()
	old := serveOut
	serveOut = w
	serveMu.Unlock()
	t.Cleanup(func() {
		serveMu.Lock()
		serveOut = old
		serveMu.Unlock()
	})

	got, err := HostCall("session.info", nil)
	if err != nil {
		t.Fatal(err)
	}
	ok, _ := got["ok"].(bool)
	if !ok {
		t.Fatalf("args = %#v", got)
	}
}
