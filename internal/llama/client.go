// Package llama is a llama.cpp router HTTP client used by /llama and the catalog.
package llama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
	Meta   *ModelMeta  `json:"meta,omitempty"`
}

// ModelMeta is optional llama.cpp model metadata.
type ModelMeta struct {
	NCtx      int64 `json:"n_ctx"`
	NCtxTrain int64 `json:"n_ctx_train"`
}

// FileProgress is one download file's bytes.
type FileProgress struct {
	Done  float64 `json:"done"`
	Total float64 `json:"total"`
}

// ModelStatus is llama.cpp router status.
type ModelStatus struct {
	Value    string                  `json:"value"`
	Failed   bool                    `json:"failed,omitempty"`
	ExitCode *int                    `json:"exit_code,omitempty"`
	Args     []string                `json:"args,omitempty"`
	Progress map[string]FileProgress `json:"progress,omitempty"`
}

// Progress is a load/download status update for the manager UI.
type Progress struct {
	Message string
	Ratio   float64 // -1 when unknown
	Detail  string
}

// ServerProps is GET /props.
type ServerProps struct {
	ModelsAutoload bool `json:"models_autoload"`
}

// Client talks to a llama.cpp router.
type Client struct {
	ServerURL    string
	APIKey       string
	HTTP         *http.Client
	PollInterval time.Duration
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
	return c.requestContext(context.Background(), method, path, body)
}

func (c *Client) requestContext(ctx context.Context, method, path string, body any) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.ServerURL+path, rdr)
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

// Download POSTs /models to fetch a GGUF into the router.
func (c *Client) Download(model string) error {
	_, err := c.request(http.MethodPost, "/models", map[string]string{"model": model})
	return err
}

func (c *Client) pollDelay(d time.Duration) time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return d
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) findModel(reload bool, id string) (ModelInfo, bool, error) {
	list, err := c.List(reload)
	if err != nil {
		return ModelInfo{}, false, err
	}
	for _, m := range list {
		if m.ID == id {
			return m, true, nil
		}
	}
	return ModelInfo{}, false, nil
}

// LoadAndWait POSTs /models/load and polls until loaded or failed.
func (c *Client) LoadAndWait(ctx context.Context, model string) (ModelInfo, error) {
	return c.LoadAndWaitProgress(ctx, model, nil)
}

// LoadAndWaitProgress is LoadAndWait with optional progress callbacks.
func (c *Client) LoadAndWaitProgress(ctx context.Context, model string, onProgress func(Progress)) (ModelInfo, error) {
	if err := c.Load(model); err != nil {
		return ModelInfo{}, err
	}
	if onProgress != nil {
		onProgress(Progress{Message: "Loading model", Ratio: -1})
	}
	for {
		entry, ok, err := c.findModel(false, model)
		if err != nil {
			return ModelInfo{}, err
		}
		if ok && entry.Status.Value == "loaded" {
			return entry, nil
		}
		if ok && entry.Status.Failed {
			if entry.Status.ExitCode != nil {
				return ModelInfo{}, fmt.Errorf("model exited with code %d", *entry.Status.ExitCode)
			}
			return ModelInfo{}, fmt.Errorf("model failed to load")
		}
		if err := c.sleep(ctx, c.pollDelay(250*time.Millisecond)); err != nil {
			return ModelInfo{}, err
		}
	}
}

// UnloadAndWait POSTs /models/unload and polls until unloaded.
func (c *Client) UnloadAndWait(ctx context.Context, model string) error {
	if err := c.Unload(model); err != nil {
		return err
	}
	for {
		entry, ok, err := c.findModel(false, model)
		if err != nil {
			return err
		}
		if !ok || entry.Status.Value == "unloaded" {
			return nil
		}
		if err := c.sleep(ctx, c.pollDelay(100*time.Millisecond)); err != nil {
			return err
		}
	}
}

// DownloadAndWait POSTs /models and polls until the model leaves the downloading state.
func (c *Client) DownloadAndWait(ctx context.Context, model string) ([]ModelInfo, error) {
	return c.DownloadAndWaitProgress(ctx, model, nil)
}

// DownloadAndWaitProgress is DownloadAndWait with optional progress callbacks.
func (c *Client) DownloadAndWaitProgress(ctx context.Context, model string, onProgress func(Progress)) ([]ModelInfo, error) {
	if err := c.Download(model); err != nil {
		return nil, err
	}
	if onProgress != nil {
		onProgress(Progress{Message: "Downloading model", Ratio: -1})
	}
	sawDownloading := false
	polls := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		list, err := c.List(false)
		if err != nil {
			return nil, err
		}
		polls++
		var entry *ModelInfo
		for i := range list {
			if list[i].ID == model {
				entry = &list[i]
				break
			}
		}
		if entry != nil && entry.Status.Value == "downloading" {
			sawDownloading = true
			if onProgress != nil {
				onProgress(downloadProgress(*entry))
			}
		} else if entry != nil && (sawDownloading || polls >= 2) {
			return c.List(true)
		}
		if err := c.sleep(ctx, c.pollDelay(500*time.Millisecond)); err != nil {
			return nil, err
		}
	}
}

func downloadProgress(entry ModelInfo) Progress {
	var done, total float64
	for _, p := range entry.Status.Progress {
		done += p.Done
		total += p.Total
	}
	if total <= 0 {
		return Progress{Message: "Downloading model", Ratio: -1}
	}
	return Progress{
		Message: "Downloading model",
		Ratio:   done / total,
		Detail:  FormatBytes(done) + " / " + FormatBytes(total),
	}
}

// ModelIsLoaded reports loaded or sleeping.
func ModelIsLoaded(m ModelInfo) bool {
	return m.Status.Value == "loaded" || m.Status.Value == "sleeping"
}

// ContextLabel is a short n_ctx / --ctx-size label.
func ContextLabel(m ModelInfo) string {
	ctx := int64(0)
	if m.Meta != nil {
		ctx = m.Meta.NCtx
		if ctx == 0 {
			ctx = m.Meta.NCtxTrain
		}
	}
	if ctx == 0 {
		args := m.Status.Args
		for i := 0; i < len(args)-1; i++ {
			switch args[i] {
			case "--ctx-size", "-c", "-ctx":
				n, err := strconv.ParseInt(args[i+1], 10, 64)
				if err == nil && n > 0 {
					ctx = n
				}
			}
		}
	}
	if ctx <= 0 {
		return ""
	}
	if ctx >= 1000 {
		return strconv.FormatInt((ctx+500)/1000, 10) + "k"
	}
	return strconv.FormatInt(ctx, 10)
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
