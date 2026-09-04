package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultImagesBaseURL = "https://openrouter.ai/api/v1"

// DefaultImagesModel is the OpenRouter image model used when none is given.
const DefaultImagesModel = "google/gemini-2.5-flash-image"

// GenerateImagesFunc generates images from a text prompt.
type GenerateImagesFunc func(ctx context.Context, prompt, model string) ([]ImageContent, error)

// GenerateImages calls OpenRouter image generation (overridable in tests).
var GenerateImages GenerateImagesFunc = generateOpenRouterImages

func generateOpenRouterImages(ctx context.Context, prompt, model string) ([]ImageContent, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("image prompt is required")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is not set")
	}
	if model == "" {
		model = DefaultImagesModel
	}
	base := os.Getenv("OPENROUTER_BASE_URL")
	if base == "" {
		base = defaultImagesBaseURL
	}
	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{{"type": "text", "text": prompt}}},
		},
		"stream":     false,
		"modalities": []string{"image"},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL(base), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+key)
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter images: %s: %s", resp.Status, bytes.TrimSpace(raw))
	}
	return parseOpenRouterImages(raw)
}

func parseOpenRouterImages(raw []byte) ([]ImageContent, error) {
	var payload struct {
		Choices []struct {
			Message struct {
				Images []struct {
					ImageURL any `json:"image_url"`
				} `json:"images"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var out []ImageContent
	for _, ch := range payload.Choices {
		for _, img := range ch.Message.Images {
			url := imageURLString(img.ImageURL)
			mime, data, ok := splitDataURL(url)
			if !ok {
				continue
			}
			out = append(out, ImageContent{Type: "image", MimeType: mime, Data: data})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("openrouter images: no image in response")
	}
	return out, nil
}

func imageURLString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		s, _ := t["url"].(string)
		return s
	default:
		return ""
	}
}

func splitDataURL(url string) (mime, data string, ok bool) {
	if !strings.HasPrefix(url, "data:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(url, "data:")
	mime, b64, found := strings.Cut(rest, ";base64,")
	if !found || mime == "" || b64 == "" {
		return "", "", false
	}
	return mime, b64, true
}
