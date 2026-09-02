package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	codexWSCacheTTL  = 5 * time.Minute
	codexWSMaxAge    = 55 * time.Minute
	codexWSLimitCode = "websocket_connection_limit_reached"
	codexWSPrevCode  = "previous_response_not_found"
)

type cachedWSContinuation struct {
	lastRequestBody   map[string]any
	lastResponseID    string
	lastResponseItems []any
}

type cachedWSConn struct {
	conn         *websocket.Conn
	busy         bool
	createdAt    time.Time
	idleTimer    *time.Timer
	continuation *cachedWSContinuation
}

type wsAcquire struct {
	conn    *websocket.Conn
	entry   *cachedWSConn
	release func(keep bool)
}

var (
	codexWSMu        sync.Mutex
	codexWSCache     = map[string]map[string]*cachedWSConn{}
	codexSSEFallback = map[string]struct{}{}
)

func resetCodexWebSocketCache() {
	codexWSMu.Lock()
	defer codexWSMu.Unlock()
	for _, accounts := range codexWSCache {
		for _, e := range accounts {
			if e.idleTimer != nil {
				e.idleTimer.Stop()
			}
			_ = e.conn.Close()
		}
	}
	codexWSCache = map[string]map[string]*cachedWSConn{}
	codexSSEFallback = map[string]struct{}{}
}

func codexSessionStickySSE(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	codexWSMu.Lock()
	defer codexWSMu.Unlock()
	_, ok := codexSSEFallback[sessionID]
	return ok
}

func markCodexSessionStickySSE(sessionID string) {
	if sessionID == "" {
		return
	}
	codexWSMu.Lock()
	defer codexWSMu.Unlock()
	codexSSEFallback[sessionID] = struct{}{}
}

func wsInputDelta(current, baseline []any) ([]any, bool) {
	if len(current) < len(baseline) {
		return nil, false
	}
	for i := range baseline {
		a, err1 := json.Marshal(current[i])
		b, err2 := json.Marshal(baseline[i])
		if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
			return nil, false
		}
	}
	return current[len(baseline):], true
}

func jsonCloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

func anySlice(v any) []any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out []any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

func bodiesMatchExceptInput(a, b map[string]any) bool {
	ca, cb := jsonCloneMap(a), jsonCloneMap(b)
	for _, m := range []*map[string]any{&ca, &cb} {
		if *m == nil {
			*m = map[string]any{}
		}
		delete(*m, "input")
		delete(*m, "previous_response_id")
		delete(*m, "type")
	}
	ba, err1 := json.Marshal(ca)
	bb, err2 := json.Marshal(cb)
	return err1 == nil && err2 == nil && bytes.Equal(ba, bb)
}

func parseCodexWSError(data []byte) (code, message string) {
	var ev struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Response struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &ev) != nil {
		return "", ""
	}
	if ev.Type != "error" && ev.Type != "response.failed" {
		return "", ""
	}
	code = ev.Code
	if code == "" {
		code = ev.Error.Code
	}
	if code == "" {
		code = ev.Response.Error.Code
	}
	message = ev.Message
	if message == "" {
		message = ev.Error.Message
	}
	if message == "" {
		message = ev.Response.Error.Message
	}
	return code, message
}

func (c *OpenAICodexClient) accountID() string {
	if c.Headers != nil {
		if id := c.Headers["chatgpt-account-id"]; id != "" {
			return id
		}
	}
	return extractCodexAccountID(c.APIKey)
}

func (c *OpenAICodexClient) acquireWS(ctx context.Context, sessionID string) (wsAcquire, error) {
	if sessionID == "" {
		conn, err := c.dialWebSocket(ctx, sessionID)
		if err != nil {
			return wsAcquire{}, err
		}
		return wsAcquire{
			conn: conn,
			release: func(bool) {
				_ = conn.Close()
			},
		}, nil
	}
	account := c.accountID()
	codexWSMu.Lock()
	accounts := codexWSCache[sessionID]
	cached := accounts[account]
	if cached != nil {
		if cached.idleTimer != nil {
			cached.idleTimer.Stop()
			cached.idleTimer = nil
		}
		expired := time.Since(cached.createdAt) >= codexWSMaxAge
		if !cached.busy && !expired {
			cached.busy = true
			conn := cached.conn
			codexWSMu.Unlock()
			return wsAcquire{
				conn:  conn,
				entry: cached,
				release: func(keep bool) {
					c.releaseCachedWS(sessionID, account, cached, keep)
				},
			}, nil
		}
		if cached.busy {
			codexWSMu.Unlock()
			conn, err := c.dialWebSocket(ctx, sessionID)
			if err != nil {
				return wsAcquire{}, err
			}
			return wsAcquire{
				conn: conn,
				release: func(bool) {
					_ = conn.Close()
				},
			}, nil
		}
		_ = cached.conn.Close()
		delete(accounts, account)
		if len(accounts) == 0 {
			delete(codexWSCache, sessionID)
		}
	}
	codexWSMu.Unlock()

	conn, err := c.dialWebSocket(ctx, sessionID)
	if err != nil {
		return wsAcquire{}, err
	}
	entry := &cachedWSConn{conn: conn, busy: true, createdAt: time.Now()}
	codexWSMu.Lock()
	accounts = codexWSCache[sessionID]
	if accounts == nil {
		accounts = map[string]*cachedWSConn{}
		codexWSCache[sessionID] = accounts
	}
	accounts[account] = entry
	codexWSMu.Unlock()
	return wsAcquire{
		conn:  conn,
		entry: entry,
		release: func(keep bool) {
			c.releaseCachedWS(sessionID, account, entry, keep)
		},
	}, nil
}

func (c *OpenAICodexClient) releaseCachedWS(sessionID, account string, entry *cachedWSConn, keep bool) {
	codexWSMu.Lock()
	defer codexWSMu.Unlock()
	if !keep {
		if entry.idleTimer != nil {
			entry.idleTimer.Stop()
			entry.idleTimer = nil
		}
		_ = entry.conn.Close()
		if accounts := codexWSCache[sessionID]; accounts != nil && accounts[account] == entry {
			delete(accounts, account)
			if len(accounts) == 0 {
				delete(codexWSCache, sessionID)
			}
		}
		return
	}
	entry.busy = false
	if entry.idleTimer != nil {
		entry.idleTimer.Stop()
	}
	entry.idleTimer = time.AfterFunc(codexWSCacheTTL, func() {
		codexWSMu.Lock()
		defer codexWSMu.Unlock()
		if entry.busy {
			return
		}
		_ = entry.conn.Close()
		if accounts := codexWSCache[sessionID]; accounts != nil && accounts[account] == entry {
			delete(accounts, account)
			if len(accounts) == 0 {
				delete(codexWSCache, sessionID)
			}
		}
	})
}

func applyWSContinuation(entry *cachedWSConn, bodyMap map[string]any) map[string]any {
	payload := jsonCloneMap(bodyMap)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["type"] = "response.create"
	if entry == nil || entry.continuation == nil {
		return payload
	}
	cont := entry.continuation
	if !bodiesMatchExceptInput(bodyMap, cont.lastRequestBody) || cont.lastResponseID == "" {
		entry.continuation = nil
		return payload
	}
	current := anySlice(bodyMap["input"])
	baseline := append(anySlice(cont.lastRequestBody["input"]), cont.lastResponseItems...)
	delta, ok := wsInputDelta(current, baseline)
	if !ok {
		entry.continuation = nil
		return payload
	}
	payload["input"] = delta
	payload["previous_response_id"] = cont.lastResponseID
	return payload
}

func (c *OpenAICodexClient) streamWebSocket(ctx context.Context, bodyMap map[string]any, opts Options) (*EventStream, error) {
	retriedLimit := false
	retriedPrev := false
	for {
		acq, err := c.acquireWS(ctx, opts.SessionID)
		if err != nil {
			return nil, err
		}
		payload := applyWSContinuation(acq.entry, bodyMap)
		if err := acq.conn.WriteJSON(payload); err != nil {
			acq.release(false)
			return nil, err
		}
		_, data, err := acq.conn.ReadMessage()
		if err != nil {
			acq.release(false)
			return nil, err
		}
		code, msg := parseCodexWSError(data)
		if code == codexWSLimitCode && !retriedLimit {
			retriedLimit = true
			acq.release(false)
			continue
		}
		if code == codexWSPrevCode && !retriedPrev {
			retriedPrev = true
			if acq.entry != nil {
				acq.entry.continuation = nil
			}
			acq.release(false)
			continue
		}
		if code != "" {
			acq.release(false)
			if msg == "" {
				msg = code
			}
			return errorStreamProvider(opts.Model, "openai-codex-responses", fmt.Sprintf("Codex error: %s", msg)), nil
		}
		return c.wsEventStream(ctx, acq, data, bodyMap, opts), nil
	}
}

func continuationItems(out *AssistantMessage) []any {
	if out == nil {
		return nil
	}
	return anySlice(buildResponsesInput(Context{Messages: []Message{{Role: RoleAssistant, Assistant: out}}}))
}
