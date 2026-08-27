package tui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type clipImage struct {
	bytes []byte
	mime  string
}

const (
	clipListTimeout = time.Second
	clipReadTimeout = 3 * time.Second
)

var imageMIMEPref = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

func readClipboardImage() *clipImage {
	return readClipboardImageEnv(os.Getenv)
}

func readClipboardImageEnv(getenv func(string) string) *clipImage {
	if getenv("TERMUX_VERSION") != "" {
		return nil
	}
	switch runtime.GOOS {
	case "linux":
		return readLinuxClipboardImage(getenv)
	case "darwin":
		return readDarwinClipboardImage()
	case "windows":
		return readWindowsClipboardImage()
	default:
		return nil
	}
}

func isWayland(getenv func(string) string) bool {
	return getenv("WAYLAND_DISPLAY") != "" || getenv("XDG_SESSION_TYPE") == "wayland"
}

func isWSL(getenv func(string) string) bool {
	return getenv("WSL_DISTRO_NAME") != "" || getenv("WSLENV") != ""
}

func readLinuxClipboardImage(getenv func(string) string) *clipImage {
	wayland := isWayland(getenv)
	wsl := isWSL(getenv)
	if wayland || wsl {
		if img := readWlPasteImage(); img != nil {
			return img
		}
		if img := readXclipImage(); img != nil {
			return img
		}
	}
	if !wayland {
		if img := readXclipImage(); img != nil {
			return img
		}
	}
	return nil
}

func readWlPasteImage() *clipImage {
	list, ok := runClip("wl-paste", clipListTimeout, "--list-types")
	if !ok {
		return nil
	}
	selected := selectImageMIME(strings.Split(string(list), "\n"))
	if selected == "" {
		return nil
	}
	data, ok := runClip("wl-paste", clipReadTimeout, "--type", selected, "--no-newline")
	if !ok {
		return nil
	}
	return supportedClipImage(data, baseMIME(selected))
}

func readXclipImage() *clipImage {
	targets, ok := runClip("xclip", clipListTimeout, "-selection", "clipboard", "-t", "TARGETS", "-o")
	var types []string
	if ok {
		types = strings.Split(string(targets), "\n")
	}
	preferred := selectImageMIME(types)
	try := append([]string(nil), imageMIMEPref...)
	if preferred != "" {
		try = append([]string{preferred}, try...)
	}
	seen := map[string]bool{}
	for _, mime := range try {
		mime = baseMIME(mime)
		if mime == "" || seen[mime] {
			continue
		}
		seen[mime] = true
		data, ok := runClip("xclip", clipReadTimeout, "-selection", "clipboard", "-t", mime, "-o")
		if ok {
			return supportedClipImage(data, mime)
		}
	}
	return nil
}

func readDarwinClipboardImage() *clipImage {
	tmp := filepath.Join(os.TempDir(), "pigo-clip-"+randHex(8)+".png")
	defer func() { _ = os.Remove(tmp) }()
	if !runClipOK("pngpaste", clipReadTimeout, tmp) {
		return nil
	}
	data, err := os.ReadFile(tmp)
	if err != nil || len(data) == 0 {
		return nil
	}
	return supportedClipImage(data, "image/png")
}

func readWindowsClipboardImage() *clipImage {
	tmp := filepath.Join(os.TempDir(), "pigo-clip-"+randHex(8)+".png")
	defer func() { _ = os.Remove(tmp) }()
	script := "Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; " +
		"$img = [System.Windows.Forms.Clipboard]::GetImage(); " +
		"if ($img) { $img.Save('" + strings.ReplaceAll(tmp, "'", "''") + "', [System.Drawing.Imaging.ImageFormat]::Png); Write-Output 'ok' }"
	out, ok := runClip("powershell.exe", 5*time.Second, "-NoProfile", "-Command", script)
	if !ok || !strings.Contains(string(out), "ok") {
		return nil
	}
	data, err := os.ReadFile(tmp)
	if err != nil || len(data) == 0 {
		return nil
	}
	return supportedClipImage(data, "image/png")
}

func readClipboardText() string {
	return readClipboardTextEnv(os.Getenv)
}

func readClipboardTextEnv(getenv func(string) string) string {
	switch runtime.GOOS {
	case "linux":
		if isWayland(getenv) {
			if b, ok := runClip("wl-paste", clipReadTimeout, "--no-newline", "--type", "text"); ok {
				return string(b)
			}
		}
		if b, ok := runClip("xclip", clipReadTimeout, "-selection", "clipboard", "-o"); ok {
			return string(b)
		}
		if b, ok := runClip("xsel", clipReadTimeout, "--clipboard", "--output"); ok {
			return string(b)
		}
	case "darwin":
		if b, ok := runClip("pbpaste", clipReadTimeout); ok {
			return string(b)
		}
	case "windows":
		if b, ok := runClip("powershell.exe", clipReadTimeout, "-NoProfile", "-Command", "Get-Clipboard"); ok {
			return strings.TrimRight(string(b), "\r\n")
		}
	}
	return ""
}

func writeClipboardImage(img *clipImage) (string, error) {
	ext := extForImageMIME(img.mime)
	if ext == "" {
		ext = "png"
	}
	name := "pigo-clipboard-" + randHex(16) + "." + ext
	path := filepath.Join(os.TempDir(), name)
	return path, os.WriteFile(path, img.bytes, 0o600)
}

func selectImageMIME(types []string) string {
	normalized := make([]string, 0, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t != "" {
			normalized = append(normalized, t)
		}
	}
	for _, want := range imageMIMEPref {
		for _, t := range normalized {
			if baseMIME(t) == want {
				return t
			}
		}
	}
	for _, t := range normalized {
		if strings.HasPrefix(baseMIME(t), "image/") {
			return t
		}
	}
	return ""
}

func baseMIME(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func extForImageMIME(mime string) string {
	switch baseMIME(mime) {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	default:
		return ""
	}
}

func supportedClipImage(data []byte, mime string) *clipImage {
	if len(data) == 0 {
		return nil
	}
	if sniffed := sniffImageMIME(data); sniffed != "" {
		mime = sniffed
	}
	if extForImageMIME(mime) == "" {
		return nil
	}
	return &clipImage{bytes: data, mime: mime}
}

func sniffImageMIME(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp"
	default:
		return ""
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
	}
	return hex.EncodeToString(b)
}

func runClip(name string, timeout time.Duration, args ...string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil, false
	}
	return out, true
}

func runClipOK(name string, timeout time.Duration, args ...string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run() == nil
}
