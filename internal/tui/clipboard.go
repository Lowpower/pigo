package tui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
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
	if !ok {
		return nil
	}
	mime := baseMIME(selectImageMIME(strings.Split(string(targets), "\n")))
	if mime == "" {
		return nil
	}
	data, ok := runClip("xclip", clipReadTimeout, "-selection", "clipboard", "-t", mime, "-o")
	if !ok {
		return nil
	}
	return supportedClipImage(data, mime)
}

// darwinFileURLScript prints a JSON array of POSIX paths for Finder file URLs.
// An empty array, or a failing osascript, means there is nothing to prefer over images.
const darwinFileURLScript = `use framework "Foundation"
use framework "AppKit"
set pb to current application's NSPasteboard's generalPasteboard()
set opts to current application's NSDictionary's dictionaryWithObject:(current application's NSNumber's numberWithBool:true) forKey:(current application's NSPasteboardURLReadingFileURLsOnlyKey)
set urls to pb's readObjectsForClasses:{current application's NSURL} options:opts
set paths to current application's NSMutableArray's array()
if urls is not missing value then
	repeat with u in urls
		set p to (u's |path|())
		if p is not missing value and (p as text) is not "" then
			(paths's addObject:(p as text))
		end if
	end repeat
end if
set json to current application's NSJSONSerialization's dataWithJSONObject:paths options:0 |error|:(missing value)
if json is missing value then return "[]"
set s to current application's NSString's alloc()'s initWithData:json encoding:(current application's NSUTF8StringEncoding)
if s is missing value then return "[]"
return s as text`

func readClipboardFilePaths() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return readDarwinClipboardFilePaths()
}

func readDarwinClipboardFilePaths() []string {
	out, ok := runClip("osascript", clipReadTimeout, "-e", darwinFileURLScript)
	if !ok {
		return nil
	}
	return parseClipboardFilePaths(out)
}

func parseClipboardFilePaths(data []byte) []string {
	var paths []string
	if err := json.Unmarshal(bytes.TrimSpace(data), &paths); err != nil {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func formatClipboardFilePaths(paths []string, bash bool) (string, bool) {
	if len(paths) == 0 || clipboardPathsHaveControl(paths) {
		return "", false
	}
	if !bash {
		return strings.Join(paths, "\n"), true
	}
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = quotePathIfNeeded(p)
	}
	return strings.Join(quoted, " "), true
}

func clipboardPathsHaveControl(paths []string) bool {
	for _, p := range paths {
		for _, r := range p {
			if unicode.IsControl(r) {
				return true
			}
		}
	}
	return false
}

func quotePathIfNeeded(value string) string {
	if value != "" && shellSafePath(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellSafePath(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == '/' || r == '~' || r == ':' || r == '@':
		default:
			return false
		}
	}
	return true
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

// runClip runs a clipboard helper. Tests replace it.
var runClip = func(name string, timeout time.Duration, args ...string) ([]byte, bool) {
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

// runClipCmd is the WSL clipboard writer. Tests replace it.
var runClipCmd = func(name string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// copyText returns an OSC 52 sequence and, on WSL, also writes the Windows clipboard.
func copyText(text string) string {
	_ = copyWindowsClipboard(text, os.Getenv, runtime.GOOS)
	return osc52(text)
}

func copyWindowsClipboard(text string, getenv func(string) string, goos string) bool {
	if goos != "linux" || !isWSL(getenv) {
		return false
	}
	f, err := os.CreateTemp("", "pigo-wsl-clip-*.txt")
	if err != nil {
		return false
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return false
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return false
	}
	if err := f.Close(); err != nil {
		return false
	}
	out, err := runClipCmd("wslpath", time.Second, "-w", path)
	if err != nil {
		return false
	}
	winPath := strings.TrimSpace(string(out))
	if winPath == "" {
		return false
	}
	script := "Set-Clipboard -Value ([System.IO.File]::ReadAllText('" + strings.ReplaceAll(winPath, "'", "''") + "', [System.Text.Encoding]::UTF8))"
	_, err = runClipCmd("powershell.exe", 5*time.Second, "-NoProfile", "-Command", script)
	return err == nil
}
