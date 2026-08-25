package runtime

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

type uiHandlerFunc func(req map[string]any, timeout time.Duration) map[string]any

func isDialogUIMethod(method string) bool {
	switch method {
	case "select", "confirm", "input", "editor":
		return true
	default:
		return false
	}
}

func rpcUIID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (e *Engine) setUIHandler(fn uiHandlerFunc) {
	e.mu.Lock()
	e.uiHandler = fn
	e.mu.Unlock()
}

// RequestExtensionUI emits an RPC extension_ui_request. Dialog methods block
// until extension_ui_response or timeout (pi rpc-mode.ts createDialogPromise).
func (e *Engine) RequestExtensionUI(method string, fields map[string]any, timeout time.Duration) map[string]any {
	e.mu.Lock()
	fn := e.uiHandler
	e.mu.Unlock()
	if fn == nil {
		return map[string]any{"cancelled": true}
	}
	req := map[string]any{"method": method}
	for k, v := range fields {
		req[k] = v
	}
	return fn(req, timeout)
}

// uiPending is an in-flight dialog waiting on stdin extension_ui_response.
type uiPending struct {
	ch chan map[string]any
}

func newUIBridge(emit func(any), done <-chan struct{}) (setHandler func(e *Engine), onResponse func(raw map[string]any), closeAll func()) {
	var mu sync.Mutex
	pending := map[string]chan map[string]any{}

	onResponse = func(raw map[string]any) {
		id, _ := raw["id"].(string)
		if id == "" {
			if n, ok := raw["id"].(float64); ok {
				id = fmt.Sprint(n)
			}
		}
		mu.Lock()
		ch := pending[id]
		delete(pending, id)
		mu.Unlock()
		if ch != nil {
			ch <- raw
		}
	}

	closeAll = func() {
		mu.Lock()
		for id, ch := range pending {
			delete(pending, id)
			close(ch)
		}
		mu.Unlock()
	}

	setHandler = func(e *Engine) {
		e.setUIHandler(func(req map[string]any, timeout time.Duration) map[string]any {
			id := rpcUIID()
			req["type"] = "extension_ui_request"
			req["id"] = id
			method, _ := req["method"].(string)
			if timeout > 0 {
				req["timeout"] = timeout.Milliseconds()
			}
			if !isDialogUIMethod(method) {
				emit(req)
				return nil
			}
			ch := make(chan map[string]any, 1)
			mu.Lock()
			pending[id] = ch
			mu.Unlock()
			emit(req)
			timer := (<-chan time.Time)(nil)
			if timeout > 0 {
				t := time.NewTimer(timeout)
				defer t.Stop()
				timer = t.C
			}
			select {
			case resp, ok := <-ch:
				if !ok {
					return map[string]any{"cancelled": true}
				}
				return resp
			case <-timer:
				mu.Lock()
				delete(pending, id)
				mu.Unlock()
				return map[string]any{}
			case <-done:
				return map[string]any{"cancelled": true}
			}
		})
	}
	return setHandler, onResponse, closeAll
}
