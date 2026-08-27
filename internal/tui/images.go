package tui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
)

const maxImageBytes = 50 * 1024 * 1024

func extractImages(text string) []ai.ImageContent {
	var out []ai.ImageContent
	seen := map[string]bool{}
	for _, tok := range strings.Fields(text) {
		tok = strings.Trim(tok, "`'\"<>,;:()[]")
		if tok == "" || !looksLikeImagePath(tok) {
			continue
		}
		path := expandHome(tok)
		if seen[path] {
			continue
		}
		img, ok := loadImageFile(path)
		if !ok {
			continue
		}
		seen[path] = true
		out = append(out, img)
	}
	return out
}

func looksLikeImagePath(p string) bool {
	base := strings.ToLower(filepath.Base(p))
	if strings.HasPrefix(base, "pigo-clipboard-") {
		return true
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func loadImageFile(path string) (ai.ImageContent, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > maxImageBytes {
		return ai.ImageContent{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ai.ImageContent{}, false
	}
	mime := sniffImageMIME(data)
	if extForImageMIME(mime) == "" {
		return ai.ImageContent{}, false
	}
	return ai.ImageContent{
		Type:     "image",
		Data:     base64.StdEncoding.EncodeToString(data),
		MimeType: mime,
	}, true
}
