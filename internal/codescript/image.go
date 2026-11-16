package codescript

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

var errBadImage = errors.New("image data is not valid base64")

func parseImage(v any) (data, mime string, err error) {
	switch t := v.(type) {
	case string:
		return parseDataURL(t)
	case map[string]any:
		if typ, _ := t["type"].(string); typ == "image" {
			raw, _ := t["data"].(string)
			kind, _ := t["mimeType"].(string)
			if kind == "" {
				kind, _ = t["mime"].(string)
			}
			return checkImage(raw, kind)
		}
		if u, ok := t["image_url"]; ok {
			switch url := u.(type) {
			case string:
				return parseDataURL(url)
			case map[string]any:
				s, _ := url["url"].(string)
				return parseDataURL(s)
			}
		}
	}
	return "", "", errBadImage
}

func parseDataURL(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "data:") {
		return "", "", errBadImage
	}
	header, payload, ok := strings.Cut(strings.TrimPrefix(s, "data:"), ",")
	if !ok || !strings.Contains(strings.ToLower(header), "base64") {
		return "", "", errBadImage
	}
	mime, _, _ := strings.Cut(header, ";")
	return checkImage(payload, mime)
}

func checkImage(data, mime string) (string, string, error) {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if mime == "image/jpg" {
		mime = "image/jpeg"
	}
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "", "", errBadImage
	}
	raw, err := decodeBase64(data)
	if err != nil || len(raw) == 0 {
		return "", "", errBadImage
	}
	return base64.StdEncoding.EncodeToString(raw), mime, nil
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func writeImageFile(data, mime string) (string, error) {
	raw, err := decodeBase64(data)
	if err != nil {
		return "", err
	}
	ext := map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	}[mime]
	f, err := os.CreateTemp("", "pigo-codemode-*"+ext)
	if err != nil {
		return "", err
	}
	name := f.Name()
	remove := func() {
		_ = os.Remove(name)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		remove()
		return "", err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		remove()
		return "", err
	}
	if err := f.Close(); err != nil {
		remove()
		return "", err
	}
	return name, nil
}
