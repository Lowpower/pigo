// Package llama is a llama.cpp router HTTP client used by /llama and the catalog.
package llama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// ProviderID is the catalog/auth id.
	ProviderID = "llama.cpp"
	// DefaultServerURL is the local llama.cpp router.
	DefaultServerURL = "http://127.0.0.1:8080"
)

// ModelInfo is one llama.cpp router catalog entry.
type ModelInfo struct {
	ID     string      `json:"id"`
	Source string      `json:"source,omitempty"`
	Status ModelStatus `json:"status"`
}

// ModelStatus is llama.cpp router status.
type ModelStatus struct {
	Value    string `json:"value"`
	Failed   bool   `json:"failed,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// ServerProps is GET /props.
type ServerProps struct {
	ModelsAutoload bool `json:"models_autoload"`
}

// Client talks to a llama.cpp router.
type Client struct {
	ServerURL string
	APIKey    string
	HTTP      *http.Client
}

// NormalizeServerURL strips trailing slashes and a /v1 suffix.
func NormalizeServerURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultServerURL, nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("server URL must use http or https")
	}
	u.Fragment = ""
	u.RawQuery = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.Path = strings.TrimSuffix(u.Path, "/v1")
	if u.Path == "" {
		u.Path = ""
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// InferenceURL is the OpenAI-compatible base URL.
func InferenceURL(serverURL string) string {
	return strings.TrimRight(serverURL, "/") + "/v1"
}

// NewClient builds a llama.cpp client.
func NewClient(serverURL, apiKey string) (*Client, error) {
	norm, err := NormalizeServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		ServerURL: norm,
		APIKey:    apiKey,
		HTTP:      &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (c *Client) request(method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ServerURL+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := errorMessage(payload, fmt.Sprintf("llama.cpp returned HTTP %d", resp.StatusCode))
		return nil, fmt.Errorf("%s", msg)
	}
	return payload, nil
}

func errorMessage(payload []byte, fallback string) string {
	var wrap struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &wrap) == nil && wrap.Error.Message != "" {
		return wrap.Error.Message
	}
	return fallback
}

// List returns the router catalog.
func (c *Client) List(reload bool) ([]ModelInfo, error) {
	path := "/models"
	if reload {
		path += "?reload=1"
	}
	payload, err := c.request(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Data []ModelInfo `json:"data"`
	}
	if json.Unmarshal(payload, &wrap) != nil || wrap.Data == nil {
		return nil, fmt.Errorf("llama.cpp returned an invalid model catalog")
	}
	for _, m := range wrap.Data {
		if m.ID == "" || m.Status.Value == "" {
			return nil, fmt.Errorf("server is not running in llama.cpp router mode")
		}
	}
	return wrap.Data, nil
}

// Props returns GET /props.
func (c *Client) Props() (ServerProps, error) {
	payload, err := c.request(http.MethodGet, "/props", nil)
	if err != nil {
		return ServerProps{}, err
	}
	var raw map[string]any
	if json.Unmarshal(payload, &raw) != nil {
		return ServerProps{}, nil
	}
	autoload, _ := raw["models_autoload"].(bool)
	return ServerProps{ModelsAutoload: autoload}, nil
}

// Load POSTs /models/load.
func (c *Client) Load(model string) error {
	_, err := c.request(http.MethodPost, "/models/load", map[string]string{"model": model})
	return err
}

// Unload POSTs /models/unload.
func (c *Client) Unload(model string) error {
	_, err := c.request(http.MethodPost, "/models/unload", map[string]string{"model": model})
	return err
}

// Selectable reports whether a router model can be used for inference.
func Selectable(m ModelInfo, autoload bool) bool {
	switch m.Status.Value {
	case "loaded", "sleeping":
		return true
	case "unloaded":
		return autoload && !m.Status.Failed && m.Source == "preset"
	default:
		return false
	}
}

// FormatCatalog is the /llama text listing.
func FormatCatalog(models []ModelInfo, autoload bool) string {
	var b strings.Builder
	b.WriteString("llama.cpp models\n")
	if len(models) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}
	for _, m := range models {
		mark := " "
		if m.Status.Value == "loaded" || m.Status.Value == "sleeping" {
			mark = "*"
		}
		fmt.Fprintf(&b, "%s %s  %s", mark, m.ID, m.Status.Value)
		if Selectable(m, autoload) {
			b.WriteString("  (selectable)")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
