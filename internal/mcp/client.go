package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Lowpower/pigo/internal/version"
)

const terminateGrace = 200 * time.Millisecond

// ToolSpec is one remote tool registered under a pigo name.
type ToolSpec struct {
	Server       string
	Remote       string
	Name         string
	Description  string
	Schema       map[string]any
	OutputSchema map[string]any
	Exposure     string
	Namespace    string
	Aliases      []string
}

// CallResult is one tools/call outcome.
type CallResult struct {
	Text       string
	Structured json.RawMessage
	IsError    bool
}

func dial(ctx context.Context, client *sdkmcp.Client, srv Server, workDir string) (*sdkmcp.ClientSession, *bodyCapture, error) {
	cfg := srv.Config
	if cfg.URL != "" {
		capture := &bodyCapture{base: http.DefaultTransport, header: requestHeaders(cfg)}
		transport := &sdkmcp.StreamableClientTransport{
			Endpoint:             cfg.URL,
			HTTPClient:           &http.Client{Transport: capture},
			MaxRetries:           -1,
			DisableStandaloneSSE: true,
		}
		session, err := client.Connect(ctx, transport, nil)
		return session, capture, err
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = mergeEnv(cfg.Env)
	cmd.Dir = resolveDir(workDir, cfg.Cwd)
	transport := &sdkmcp.CommandTransport{Command: cmd, TerminateDuration: terminateGrace}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, nil, err
	}
	return session, nil, nil
}

// bodyCapture records structuredContent from HTTP responses, including JSON null,
// which the SDK decodes as a nil interface.
type bodyCapture struct {
	base   http.RoundTripper
	header map[string]string
	mu     sync.Mutex
	raw    json.RawMessage
	saw    bool
}

func (c *bodyCapture) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.raw = nil
	c.saw = false
	c.mu.Unlock()
}

func (c *bodyCapture) take() (json.RawMessage, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, saw := c.raw, c.saw
	c.raw = nil
	c.saw = false
	return raw, saw
}

func (c *bodyCapture) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for key, val := range c.header {
		req.Header.Set(key, val)
	}
	base := c.base
	if base == nil {
		base = http.DefaultTransport
	}
	res, err := base.RoundTrip(req)
	if err != nil || res == nil || res.Body == nil {
		return res, err
	}
	// The standalone SSE stream is a GET that stays open. Buffering it would
	// stall Connect. Tool results arrive on POST, which finishes.
	if req.Method == http.MethodGet {
		return res, nil
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return nil, err
	}
	res.Body = io.NopCloser(bytes.NewReader(body))
	if raw, ok := findStructured(body); ok {
		c.mu.Lock()
		c.raw = raw
		c.saw = true
		c.mu.Unlock()
	}
	return res, nil
}

func findStructured(body []byte) (json.RawMessage, bool) {
	if raw, ok := structuredFromJSON(body); ok {
		return raw, true
	}
	var found json.RawMessage
	ok := false
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if raw, yes := structuredFromJSON(data); yes {
			found, ok = raw, true
		}
	}
	return found, ok
}

func structuredFromJSON(body []byte) (json.RawMessage, bool) {
	var msg struct {
		Result struct {
			Structured json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if len(body) == 0 || body[0] != '{' {
		return nil, false
	}
	if json.Unmarshal(body, &msg) != nil || len(msg.Result.Structured) == 0 {
		return nil, false
	}
	return append(json.RawMessage(nil), msg.Result.Structured...), true
}

func requestHeaders(cfg Config) map[string]string {
	out := map[string]string{}
	for key, val := range cfg.Headers {
		out[key] = val
	}
	name := strings.TrimSpace(cfg.BearerTokenEnvVar)
	if name == "" {
		if len(out) == 0 {
			return nil
		}
		return out
	}
	if _, ok := out["Authorization"]; !ok {
		if tok := os.Getenv(name); tok != "" {
			out["Authorization"] = "Bearer " + tok
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return nil
	}
	skip := map[string]bool{}
	for key := range extra {
		skip[key] = true
	}
	var out []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if skip[key] {
			continue
		}
		out = append(out, entry)
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+extra[key])
	}
	return out
}

func resolveDir(base, dir string) string {
	if dir == "" {
		return base
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	if base == "" {
		return dir
	}
	return filepath.Join(base, dir)
}

func newClient() *sdkmcp.Client {
	return sdkmcp.NewClient(&sdkmcp.Implementation{Name: "pigo", Version: version.Version}, nil)
}

func listTools(ctx context.Context, session *sdkmcp.ClientSession) ([]*sdkmcp.Tool, error) {
	var cursor string
	var all []*sdkmcp.Tool
	for {
		res, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			return all, nil
		}
		cursor = res.NextCursor
	}
}

func specsFromTools(srv Server, tools []*sdkmcp.Tool) []ToolSpec {
	remotes := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool == nil || tool.Name == "" {
			continue
		}
		remotes = append(remotes, tool.Name)
	}
	names := RegisteredToolNames(srv.Name, remotes)
	out := make([]ToolSpec, 0, len(remotes))
	for _, tool := range tools {
		if tool == nil || tool.Name == "" {
			continue
		}
		registered := names[tool.Name]
		var aliases []string
		if alias := ToolAlias(srv.Name, tool.Name, registered); alias != "" {
			aliases = append(aliases, alias)
		}
		out = append(out, ToolSpec{
			Server:       srv.Name,
			Remote:       tool.Name,
			Name:         registered,
			Description:  tool.Description,
			Schema:       inputSchema(tool.InputSchema),
			OutputSchema: schemaMap(tool.OutputSchema),
			Exposure:     srv.Config.ExposureFor(tool.Name),
			Namespace:    Namespace(srv.Name),
			Aliases:      aliases,
		})
	}
	return out
}

func inputSchema(v any) map[string]any {
	if m := schemaMap(v); m != nil {
		return m
	}
	return map[string]any{"type": "object"}
}

func schemaMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		if len(m) == 0 {
			return nil
		}
		return m
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || len(m) == 0 {
		return nil
	}
	return m
}

func callResult(res *sdkmcp.CallToolResult) CallResult {
	if res == nil {
		return CallResult{IsError: true}
	}
	return CallResult{
		Text:       contentText(res.Content),
		Structured: structuredRaw(res.StructuredContent),
		IsError:    res.IsError,
	}
}

func contentText(content []sdkmcp.Content) string {
	var b strings.Builder
	for _, item := range content {
		text, ok := item.(*sdkmcp.TextContent)
		if !ok {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(text.Text)
	}
	return b.String()
}

func structuredRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	switch raw := v.(type) {
	case json.RawMessage:
		if len(raw) == 0 {
			return nil
		}
		return append(json.RawMessage(nil), raw...)
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

func hasResources(res *sdkmcp.InitializeResult) bool {
	return res != nil && res.Capabilities != nil && res.Capabilities.Resources != nil
}

func instructionsOf(res *sdkmcp.InitializeResult) string {
	if res == nil {
		return ""
	}
	return res.Instructions
}
