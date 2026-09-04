package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/models"
)

const (
	defaultShareViewerURL = "https://pi.dev/session/"
	defaultRadiusGateway  = "https://radius.pi.dev"
	errGHMissing          = "GitHub CLI (gh) is not installed. Install it from https://cli.github.com/"
	errGHAuth             = "GitHub CLI is not logged in. Run 'gh auth login' first."
)

type shareError string

func (e shareError) Error() string { return string(e) }

func shareErrorf(format string, args ...any) error {
	return shareError(fmt.Sprintf(format, args...))
}

// ShareResult is the outcome of /share.
type ShareResult struct {
	ViewerURL string
	GistURL   string
	RadiusURL string
}

// String is the TUI/status line.
func (r ShareResult) String() string {
	if r.RadiusURL != "" {
		return "Share URL: " + r.RadiusURL
	}
	var b strings.Builder
	b.WriteString("Share URL: ")
	b.WriteString(r.ViewerURL)
	if r.GistURL != "" {
		b.WriteString("\nGist: ")
		b.WriteString(r.GistURL)
	}
	return b.String()
}

// ShareOptions is /share input.
type ShareOptions struct {
	Session      *Manager
	ThemeName    string
	Cwd          string
	AgentDir     string
	SystemPrompt string
	Tools        []ai.Tool
}

// Test hooks (nil = real implementations).
var (
	shareLookPath     = exec.LookPath
	shareGHAuthStatus = ghAuthStatus
	shareGistCreate   = gistCreate
	shareRadius       = tryShareViaRadius
	shareRadiusToken  = radiusToken
	shareHTTPClient   = http.DefaultClient
)

// Share exports the session and uploads it (Radius, else private gist).
func Share(opts ShareOptions) (ShareResult, error) {
	if opts.Session == nil {
		return ShareResult{}, errors.New("no session to share")
	}
	dir, err := os.MkdirTemp("", "pigo-share-*")
	if err != nil {
		return ShareResult{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	jsonlPath := filepath.Join(dir, "session.jsonl")
	if err := WriteShareJSONL(opts.Session, jsonlPath, opts.SystemPrompt, opts.Tools); err != nil {
		return ShareResult{}, fmt.Errorf("failed to export session: %w", err)
	}
	if url, ok, err := shareRadius(jsonlPath, opts.AgentDir); ok {
		if err != nil {
			return ShareResult{}, err
		}
		return ShareResult{RadiusURL: url}, nil
	}

	if _, err := shareLookPath("gh"); err != nil {
		return ShareResult{}, shareError(errGHMissing)
	}
	if err := shareGHAuthStatus(); err != nil {
		return ShareResult{}, err
	}

	htmlPath := filepath.Join(dir, "session.html")
	if _, err := ExportHTMLWith(opts.Session, WithBuiltinToolRenderer(HTMLOptions{
		OutputPath:   htmlPath,
		ThemeName:    opts.ThemeName,
		Cwd:          opts.Cwd,
		AgentDir:     opts.AgentDir,
		SystemPrompt: opts.SystemPrompt,
		Tools:        opts.Tools,
	})); err != nil {
		return ShareResult{}, fmt.Errorf("failed to export session: %w", err)
	}
	gistURL, err := shareGistCreate(htmlPath)
	if err != nil {
		return ShareResult{}, err
	}
	gistID := gistURL[strings.LastIndex(strings.TrimRight(gistURL, "/"), "/")+1:]
	if gistID == "" || gistID == gistURL {
		return ShareResult{}, shareError("Failed to parse gist ID from gh output")
	}
	return ShareResult{ViewerURL: ShareViewerURL(gistID), GistURL: gistURL}, nil
}

// ShareViewerURL builds the gist viewer URL (PIGO_SHARE_VIEWER_URL overrides).
func ShareViewerURL(gistID string) string {
	base := os.Getenv("PIGO_SHARE_VIEWER_URL")
	if base == "" {
		base = defaultShareViewerURL
	}
	return base + "#" + gistID
}

// WriteShareJSONL writes the current branch plus a pi.share custom entry.
func WriteShareJSONL(m *Manager, path string, systemPrompt string, tools []ai.Tool) error {
	if m == nil {
		return os.ErrInvalid
	}
	ts := isoNow()
	header := Header{
		Type:      "session",
		Version:   CurrentVersion,
		ID:        m.id,
		Timestamp: ts,
		Cwd:       m.cwd,
	}
	var lines []string
	b, err := json.Marshal(header)
	if err != nil {
		return err
	}
	lines = append(lines, string(b))
	var parent *string
	for _, e := range m.GetBranch("") {
		e.ParentID = parent
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		lines = append(lines, string(raw))
		id := e.ID
		parent = &id
	}
	toolSpecs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		toolSpecs = append(toolSpecs, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.Parameters,
		})
	}
	custom := map[string]any{
		"type":       "custom",
		"customType": "pi.share",
		"id":         newUUID()[:8],
		"parentId":   parent,
		"timestamp":  ts,
		"data": map[string]any{
			"systemPrompt": systemPrompt,
			"tools":        toolSpecs,
		},
	}
	raw, err := json.Marshal(custom)
	if err != nil {
		return err
	}
	lines = append(lines, string(raw))
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func ghAuthStatus() error {
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return shareError(errGHAuth)
	}
	return nil
}

func gistCreate(htmlPath string) (string, error) {
	cmd := exec.Command("gh", "gist", "create", "--public=false", htmlPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = "Unknown error"
		}
		return "", shareErrorf("Failed to create gist: %s", msg)
	}
	// gh prints the gist URL on stdout; CombinedOutput mixes stderr notices.
	url := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "gist.github.com") || strings.HasPrefix(line, "https://") {
			url = line
		}
	}
	if url == "" {
		return "", shareError("Failed to parse gist ID from gh output")
	}
	return url, nil
}

func tryShareViaRadius(jsonlPath, agentDir string) (string, bool, error) {
	token, gateway, ok := shareRadiusToken(agentDir)
	if !ok {
		return "", false, nil
	}
	body, err := os.ReadFile(jsonlPath)
	if err != nil {
		return "", true, shareErrorf("Failed to upload Radius artifact: %v", err)
	}
	if gateway == "" {
		gateway = defaultRadiusGateway
	}
	u := strings.TrimRight(gateway, "/") + "/v1/artifacts?visibility=organization&title=Pi%20session"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", true, shareErrorf("Failed to upload Radius artifact: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.ContentLength = int64(len(body))
	resp, err := shareHTTPClient.Do(req)
	if err != nil {
		return "", true, shareErrorf("Failed to upload Radius artifact: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Artifact *struct {
			Canonical string `json:"canonical_url"`
		} `json:"artifact"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(respBody, &parsed)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || parsed.Artifact == nil || parsed.Artifact.Canonical == "" {
		msg := parsed.Error
		if msg == "" {
			msg = strings.TrimSpace(resp.Status)
		}
		if msg == "" {
			msg = fmt.Sprintf("%d", resp.StatusCode)
		}
		return "", true, shareErrorf("Failed to upload Radius artifact: %s", msg)
	}
	return parsed.Artifact.Canonical, true, nil
}

func radiusToken(agentDir string) (token, gateway string, ok bool) {
	p, found := auth.Lookup("radius")
	if !found {
		return "", "", false
	}
	gateway = defaultRadiusGateway
	if spec, okp := models.LookupProvider("radius"); okp && spec.BaseURL != "" {
		gateway = spec.BaseURL
	}
	s := auth.Open(agentDir)
	res, err := auth.Resolve(context.Background(), s, p, auth.ResolveOpts{MinOAuthValidityMs: 5 * 60 * 1000})
	if err != nil || res == nil {
		return "", "", false
	}
	if res.Auth.APIKey != "" {
		return res.Auth.APIKey, firstNonEmpty(res.Auth.BaseURL, gateway), true
	}
	for name, v := range res.Auth.Headers {
		if strings.EqualFold(name, "authorization") {
			if rest, okh := strings.CutPrefix(v, "Bearer "); okh {
				return rest, firstNonEmpty(res.Auth.BaseURL, gateway), true
			}
		}
	}
	return "", "", false
}
