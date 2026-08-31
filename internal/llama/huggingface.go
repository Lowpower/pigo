package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const huggingFaceAPI = "https://huggingface.co"

// HFModel is one Hugging Face GGUF search hit.
type HFModel struct {
	ID        string `json:"id"`
	Downloads int    `json:"downloads"`
}

// FindHuggingFaceToken returns HF_TOKEN or a cached huggingface token file.
func FindHuggingFaceToken() string {
	if v := strings.TrimSpace(os.Getenv("HF_TOKEN")); v != "" {
		return v
	}
	var paths []string
	if p := strings.TrimSpace(os.Getenv("HF_TOKEN_PATH")); p != "" {
		paths = append(paths, p)
	}
	if home := strings.TrimSpace(os.Getenv("HF_HOME")); home != "" {
		paths = append(paths, filepath.Join(home, "token"))
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "huggingface", "token"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".cache", "huggingface", "token"))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	return ""
}

// SearchHuggingFace lists GGUF models matching query, sorted by downloads.
func SearchHuggingFace(ctx context.Context, query, token, baseURL string) ([]HFModel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = huggingFaceAPI
	}
	params := url.Values{
		"search":    {query},
		"filter":    {"gguf"},
		"sort":      {"downloads"},
		"direction": {"-1"},
		"limit":     {"20"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/models?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("Hugging Face returned HTTP %d", resp.StatusCode)
		if resp.StatusCode == 429 {
			msg = "Hugging Face rate limit reached"
		}
		var wrap struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &wrap) == nil && wrap.Error != "" {
			msg = wrap.Error
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var raw []struct {
		ID        string `json:"id"`
		Downloads int    `json:"downloads"`
	}
	if json.Unmarshal(payload, &raw) != nil {
		return nil, fmt.Errorf("Hugging Face returned invalid search results")
	}
	out := make([]HFModel, 0, len(raw))
	for _, m := range raw {
		if m.ID == "" {
			continue
		}
		out = append(out, HFModel{ID: m.ID, Downloads: m.Downloads})
	}
	return out, nil
}

// FormatSearch is the /llama search listing.
func FormatSearch(models []HFModel) string {
	var b strings.Builder
	b.WriteString("Hugging Face GGUF models\n")
	if len(models) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}
	for _, m := range models {
		fmt.Fprintf(&b, "  %s  %d downloads\n", m.ID, m.Downloads)
	}
	b.WriteString("Download with /llama download owner/repo:QUANT\n")
	return b.String()
}
