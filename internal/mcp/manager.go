package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	errNotConnected = errors.New("mcp server is not connected")
	errDisabled     = errors.New("mcp server is disabled")
)

// Status is one server after load, for events and tests.
type Status struct {
	Name         string
	Exposure     string
	Description  string
	Status       string
	HasResources bool
	Err          string
}

// Manager connects configured servers in the background.
type Manager struct {
	client   *sdkmcp.Client
	servers  []*running
	workDir  string
	mu       sync.Mutex
	onChange func()
	ctx      context.Context
	cancel   context.CancelFunc
	started  bool
}

type running struct {
	owner        *Manager
	name         string
	canon        string
	spec         Server
	done         chan struct{}
	settled      chan struct{}
	settleOnce   sync.Once
	mu           sync.Mutex
	err          error
	session      *sdkmcp.ClientSession
	capture      *bodyCapture
	callMu       sync.Mutex
	tools        []ToolSpec
	status       string
	hasResources bool
	instructions string
	cancel       context.CancelFunc
}

// New builds a manager. Call Start to connect. Disabled servers are not connected.
func New(servers []Server, workDir string) *Manager {
	m := &Manager{client: newClient(), workDir: workDir}
	for _, srv := range servers {
		status := "connecting"
		if !srv.Config.Enabled {
			status = "disabled"
		}
		m.servers = append(m.servers, &running{
			owner:   m,
			name:    srv.Name,
			canon:   Canon(srv.Name),
			spec:    srv,
			done:    make(chan struct{}),
			settled: make(chan struct{}),
			status:  status,
		})
	}
	return m
}

// SetOnChange registers a callback for connection and tool-list changes.
// It is invoked without the manager lock held.
func (m *Manager) SetOnChange(fn func()) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

// Start connects enabled servers in the background. parent cancellation and
// Close both stop in-flight handshakes.
func (m *Manager) Start(parent context.Context) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	m.ctx = ctx
	m.cancel = cancel
	servers := append([]*running(nil), m.servers...)
	m.mu.Unlock()
	for _, srv := range servers {
		m.launch(srv)
	}
}

// Close cancels connections and closes sessions that are already up.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, srv := range m.snapshot() {
		srv.closeSession()
	}
}

// Add connects a server registered after startup. An existing name is ignored
// by the caller. The new server follows the same lazy connect rules.
func (m *Manager) Add(srv Server) {
	if m == nil || srv.Name == "" {
		return
	}
	status := "connecting"
	if !srv.Config.Enabled {
		status = "disabled"
	}
	r := &running{
		owner:   m,
		name:    srv.Name,
		canon:   Canon(srv.Name),
		spec:    srv,
		done:    make(chan struct{}),
		settled: make(chan struct{}),
		status:  status,
	}
	m.mu.Lock()
	m.servers = append(m.servers, r)
	m.mu.Unlock()
	m.launch(r)
}

// SetEnabled turns one server on or off without touching the others.
// The list updates as soon as this returns; a disabled server does not connect.
func (m *Manager) SetEnabled(name string, enabled bool) error {
	return m.swap(name, &enabled)
}

// Reconnect drops one server's session and connects it again.
// Other servers keep the connections they already have.
func (m *Manager) Reconnect(name string) error {
	return m.swap(name, nil)
}

// Server returns the configured entry for name.
func (m *Manager) Server(name string) (Server, bool) {
	srv := m.serverByName(name)
	if srv == nil {
		return Server{}, false
	}
	return srv.spec, true
}

// WaitSettled blocks until every server has finished connecting, failed, or
// been left disabled. It does not require them to succeed.
func (m *Manager) WaitSettled(ctx context.Context) error {
	if m == nil {
		return nil
	}
	for _, srv := range m.snapshot() {
		if err := srv.wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) swap(name string, enabled *bool) error {
	if m == nil {
		return errors.New("mcp manager is nil")
	}
	m.mu.Lock()
	idx := -1
	for i, srv := range m.servers {
		if srv.name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.mu.Unlock()
		return fmt.Errorf("unknown mcp server %q", name)
	}
	old := m.servers[idx]
	spec := old.spec
	if enabled != nil {
		spec.Config.Enabled = *enabled
	}
	status := "connecting"
	if !spec.Config.Enabled {
		status = "disabled"
	}
	next := &running{
		owner:   m,
		name:    spec.Name,
		canon:   Canon(spec.Name),
		spec:    spec,
		done:    make(chan struct{}),
		settled: make(chan struct{}),
		status:  status,
	}
	m.servers[idx] = next
	m.mu.Unlock()
	old.stop()
	m.launch(next)
	m.notify()
	return nil
}

func (m *Manager) launch(s *running) {
	if m == nil || s == nil {
		return
	}
	m.mu.Lock()
	parent := m.ctx
	started := m.started
	m.mu.Unlock()
	if !started || parent == nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	go s.run(ctx)
}

func (s *running) stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) snapshot() []*running {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*running, len(m.servers))
	copy(out, m.servers)
	return out
}

// WaitDirect waits until every server with a direct exposure has finished
// connecting. Other servers keep running in the background.
func (m *Manager) WaitDirect(ctx context.Context) error {
	if m == nil {
		return nil
	}
	for _, srv := range m.snapshot() {
		if !srv.spec.Config.Enabled || !srv.spec.Config.BlocksFirstPrompt() {
			continue
		}
		if err := srv.wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

// WaitFor waits for servers named by a tool call, by tool_search arguments,
// or by reachesTools. A server that is not direct stays untouched until then.
func (m *Manager) WaitFor(ctx context.Context, toolName string, args map[string]any, reaches []string) error {
	if m == nil {
		return nil
	}
	want := map[*running]bool{}
	if srv := m.serverForRef(toolName); srv != nil {
		want[srv] = true
	}
	if isToolSearch(toolName) {
		walkStrings(args, func(s string) {
			if srv := m.serverForRef(s); srv != nil {
				want[srv] = true
			}
		})
	}
	for _, name := range reaches {
		if srv := m.serverForRef(name); srv != nil {
			want[srv] = true
		}
	}
	for srv := range want {
		if err := srv.wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Call invokes a remote tool. It waits for that server first.
func (m *Manager) Call(ctx context.Context, server, remote string, args map[string]any) (CallResult, error) {
	if m == nil {
		return CallResult{Text: "mcp is not configured", IsError: true}, nil
	}
	srv := m.serverByName(server)
	if srv == nil {
		return CallResult{Text: "unknown mcp server " + server, IsError: true}, nil
	}
	if err := srv.wait(ctx); err != nil {
		return CallResult{}, err
	}
	session, capture, callErr := srv.readySession()
	if callErr != nil {
		return CallResult{Text: callErr.Error(), IsError: true}, nil
	}
	if capture != nil {
		srv.callMu.Lock()
		defer srv.callMu.Unlock()
		capture.reset()
	}
	cctx, cancel := context.WithTimeout(ctx, srv.spec.Config.Timeout())
	defer cancel()
	if args == nil {
		args = map[string]any{}
	}
	res, err := session.CallTool(cctx, &sdkmcp.CallToolParams{Name: remote, Arguments: args})
	if err != nil {
		return CallResult{Text: err.Error(), IsError: true}, nil
	}
	out := callResult(res)
	if raw, ok := capture.take(); ok {
		out.Structured = raw
	}
	return out, nil
}

// Tools returns the tools known from servers that have connected.
func (m *Manager) Tools() []ToolSpec {
	if m == nil {
		return nil
	}
	var out []ToolSpec
	for _, srv := range m.snapshot() {
		srv.mu.Lock()
		out = append(out, srv.tools...)
		srv.mu.Unlock()
	}
	return out
}

// Section is the mcp_servers prompt text, one enabled server per line.
func (m *Manager) Section() string {
	if m == nil {
		return ""
	}
	var lines []string
	for _, srv := range m.snapshot() {
		if !srv.spec.Config.Enabled {
			continue
		}
		srv.mu.Lock()
		instructions := srv.instructions
		srv.mu.Unlock()
		lines = append(lines, SectionLine(srv.name, srv.spec.Config, instructions))
	}
	return strings.Join(lines, "\n")
}

// Statuses snapshots every configured server.
func (m *Manager) Statuses() []Status {
	if m == nil {
		return nil
	}
	servers := m.snapshot()
	out := make([]Status, 0, len(servers))
	for _, srv := range servers {
		srv.mu.Lock()
		st := Status{
			Name:         srv.name,
			Exposure:     srv.spec.Config.ExposureValue(),
			Description:  Summary(srv.spec.Config.Description, srv.instructions),
			Status:       srv.status,
			HasResources: srv.hasResources,
		}
		if srv.err != nil {
			st.Err = srv.err.Error()
		}
		srv.mu.Unlock()
		out = append(out, st)
	}
	return out
}

func (m *Manager) notify() {
	m.mu.Lock()
	fn := m.onChange
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (m *Manager) serverByName(name string) *running {
	for _, srv := range m.snapshot() {
		if srv.name == name {
			return srv
		}
	}
	return nil
}

func (m *Manager) serverForRef(ref string) *running {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	canon := Canon(ref)
	var best *running
	for _, srv := range m.snapshot() {
		if ref == srv.name || canon == srv.canon || ref == "mcp__"+srv.name || canon == "mcp__"+srv.canon {
			return srv
		}
		body := strings.TrimPrefix(canon, "mcp__")
		if strings.HasPrefix(body, srv.canon+"__") {
			if best == nil || len(srv.canon) > len(best.canon) {
				best = srv
			}
		}
	}
	return best
}

func (s *running) run(ctx context.Context) {
	defer close(s.done)
	defer s.settle()
	if !s.spec.Config.Enabled {
		s.mu.Lock()
		s.err = errDisabled
		s.mu.Unlock()
		return
	}
	hctx, hcancel := context.WithCancel(ctx)
	defer hcancel()
	go s.cancelAfter(hcancel, s.spec.Config.Timeout())
	session, capture, err := dial(hctx, s.owner.client, s.spec, s.owner.workDir)
	if err != nil {
		s.fail(err)
		return
	}
	s.mu.Lock()
	s.session = session
	s.capture = capture
	s.mu.Unlock()
	defer s.closeSession()

	initRes := session.InitializeResult()
	tools, err := listTools(hctx, session)
	if err != nil {
		s.fail(err)
		return
	}
	if ctx.Err() != nil {
		s.fail(ctx.Err())
		return
	}
	specs := specsFromTools(s.spec, tools)
	s.mu.Lock()
	s.tools = specs
	s.instructions = instructionsOf(initRes)
	s.hasResources = hasResources(initRes)
	s.status = "ready"
	s.err = nil
	s.mu.Unlock()
	s.settle()
	s.owner.notify()
	<-ctx.Done()
}

func (s *running) settle() {
	s.settleOnce.Do(func() { close(s.settled) })
}

func (s *running) cancelAfter(cancel context.CancelFunc, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		cancel()
	case <-s.settled:
	}
}

func (s *running) fail(err error) {
	s.mu.Lock()
	s.err = err
	s.status = "failed"
	s.mu.Unlock()
	s.settle()
	s.owner.notify()
}

func (s *running) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.settled:
		return nil
	}
}

func (s *running) readySession() (*sdkmcp.ClientSession, *bodyCapture, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		if s.err != nil {
			return nil, nil, s.err
		}
		return nil, nil, errNotConnected
	}
	return s.session, s.capture, nil
}

func (s *running) closeSession() {
	s.mu.Lock()
	session := s.session
	s.session = nil
	s.mu.Unlock()
	if session != nil {
		_ = session.Close()
	}
}

func isToolSearch(name string) bool {
	switch name {
	case "tool_search", "ToolSearch":
		return true
	default:
		return false
	}
}

func walkStrings(v any, fn func(string)) {
	switch t := v.(type) {
	case string:
		fn(t)
	case map[string]any:
		for _, child := range t {
			walkStrings(child, fn)
		}
	case []any:
		for _, child := range t {
			walkStrings(child, fn)
		}
	}
}
