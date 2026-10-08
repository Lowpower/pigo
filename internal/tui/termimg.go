package tui

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
)

const (
	protoKitty        = "kitty"
	protoITerm        = "iterm2"
	kittyChunk        = 4096
	kittyCellWidthPx  = 9
	kittyCellHeightPx = 18
)

var dataURLRe = regexp.MustCompile(`data:(image/[A-Za-z0-9.+-]+);base64,([A-Za-z0-9+/=]+)`)

type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

func detectImageProtocol(getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	term := strings.ToLower(getenv("TERM"))
	if getenv("TMUX") != "" || strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "screen") {
		return ""
	}
	program := strings.ToLower(getenv("TERM_PROGRAM"))
	if getenv("KITTY_WINDOW_ID") != "" || program == "kitty" {
		return protoKitty
	}
	if program == "ghostty" || strings.Contains(term, "ghostty") || getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return protoKitty
	}
	if getenv("WEZTERM_PANE") != "" || program == "wezterm" {
		return protoKitty
	}
	if program == "warpterminal" || getenv("WARP_SESSION_ID") != "" || getenv("WARP_TERMINAL_SESSION_UUID") != "" {
		return protoKitty
	}
	if getenv("ITERM_SESSION_ID") != "" || program == "iterm.app" {
		return protoITerm
	}
	return ""
}

// chooseLessDistortedCellCount picks ceil or ceil-1, whichever changes the
// image aspect ratio less. Equal distortion keeps the ceiling.
func chooseLessDistortedCellCount(upper int, ideal float64) int {
	if upper <= 1 || ideal <= 0 {
		return upper
	}
	lower := upper - 1
	upperDist := math.Max(float64(upper)/ideal, ideal/float64(upper))
	lowerDist := math.Max(float64(lower)/ideal, ideal/float64(lower))
	if lowerDist < upperDist {
		return lower
	}
	return upper
}

// kittyPlacement sizes a Kitty image in cells. Columns stay within maxCols.
// Rows are chosen against a 9×18 px cell so extreme aspects are not squared.
func kittyPlacement(imgW, imgH, maxCols int) (int, int) {
	maxWidth := maxCols
	if maxWidth < 1 {
		maxWidth = 1
	}
	if imgW < 1 {
		imgW = 1
	}
	if imgH < 1 {
		imgH = 1
	}
	cellW := float64(kittyCellWidthPx)
	cellH := float64(kittyCellHeightPx)
	widthScale := (float64(maxWidth) * cellW) / float64(imgW)
	scaledWidthPx := float64(imgW) * widthScale
	scaledHeightPx := float64(imgH) * widthScale
	columns := int(math.Ceil(scaledWidthPx / cellW))
	if columns < 1 {
		columns = 1
	}
	if columns > maxWidth {
		columns = maxWidth
	}
	rows := int(math.Ceil(scaledHeightPx / cellH))
	if rows < 1 {
		rows = 1
	}
	idealRows := (float64(columns) * cellW * float64(imgH)) / (float64(imgW) * cellH)
	return columns, chooseLessDistortedCellCount(rows, idealRows)
}

func encodeKitty(b64 string, cells, rows int) string {
	const prefix = "\x1b_G"
	const suffix = "\x1b\\"
	if cells < 1 {
		cells = 60
	}
	params := fmt.Sprintf("a=T,f=100,q=2,c=%d", cells)
	if rows >= 1 {
		params += fmt.Sprintf(",r=%d", rows)
	}
	if len(b64) <= kittyChunk {
		return prefix + params + ";" + b64 + suffix
	}
	var b strings.Builder
	first := true
	for offset := 0; offset < len(b64); offset += kittyChunk {
		end := min(offset+kittyChunk, len(b64))
		chunk := b64[offset:end]
		last := end >= len(b64)
		switch {
		case first:
			b.WriteString(prefix)
			b.WriteString(params)
			b.WriteString(",m=1;")
			b.WriteString(chunk)
			b.WriteString(suffix)
			first = false
		case last:
			b.WriteString(prefix)
			b.WriteString("m=0;")
			b.WriteString(chunk)
			b.WriteString(suffix)
		default:
			b.WriteString(prefix)
			b.WriteString("m=1;")
			b.WriteString(chunk)
			b.WriteString(suffix)
		}
	}
	return b.String()
}

func encodeITerm2(b64 string, cells int) string {
	if cells < 1 {
		cells = 60
	}
	return fmt.Sprintf("\x1b]1337;File=inline=1;width=%d;size=%d:%s\x07", cells, decodedBase64Len(b64), b64)
}

func decodedBase64Len(s string) int {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' {
			return -1
		}
		return r
	}, s)
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		b, err = base64.RawStdEncoding.DecodeString(s)
		if err != nil {
			return 0
		}
	}
	return len(b)
}

func decodeImageData(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "data:"); ok {
		if i := strings.Index(rest, ";base64,"); i >= 0 {
			s = rest[i+len(";base64,"):]
		}
	}
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' {
			return -1
		}
		return r
	}, s)
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return base64.RawStdEncoding.DecodeString(s)
	}
	return b, nil
}

func imageFallback(img ai.ImageContent) string {
	mime := img.MimeType
	if mime == "" {
		mime = "image/unknown"
	}
	parts := []string{"[" + mime + "]"}
	if w, h, ok := imageDimensions(img); ok {
		parts = append(parts, fmt.Sprintf("%dx%d", w, h))
	}
	return "[Image: " + strings.Join(parts, " ") + "]"
}

func imageDimensions(img ai.ImageContent) (int, int, bool) {
	b, err := decodeImageData(img.Data)
	if err != nil || len(b) == 0 {
		return 0, 0, false
	}
	mime := baseMIME(img.MimeType)
	if mime == "" {
		mime = sniffImageMIME(b)
	}
	switch mime {
	case "image/png":
		return pngSize(b)
	case "image/jpeg":
		return jpegSize(b)
	case "image/gif":
		return gifSize(b)
	case "image/webp":
		return webpSize(b)
	default:
		if w, h, ok := pngSize(b); ok {
			return w, h, true
		}
		return jpegSize(b)
	}
}

func pngSize(b []byte) (int, int, bool) {
	if len(b) < 24 || sniffImageMIME(b) != "image/png" {
		return 0, 0, false
	}
	w := binary.BigEndian.Uint32(b[16:20])
	h := binary.BigEndian.Uint32(b[20:24])
	if w == 0 || h == 0 {
		return 0, 0, false
	}
	return int(w), int(h), true
}

func jpegSize(b []byte) (int, int, bool) {
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		return 0, 0, false
	}
	i := 2
	for i < len(b)-8 {
		if b[i] != 0xff {
			i++
			continue
		}
		marker := b[i+1]
		if marker >= 0xc0 && marker <= 0xc2 {
			h := int(binary.BigEndian.Uint16(b[i+5 : i+7]))
			w := int(binary.BigEndian.Uint16(b[i+7 : i+9]))
			if w == 0 || h == 0 {
				return 0, 0, false
			}
			return w, h, true
		}
		if i+3 >= len(b) {
			return 0, 0, false
		}
		n := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if n < 2 {
			return 0, 0, false
		}
		i += 2 + n
	}
	return 0, 0, false
}

func gifSize(b []byte) (int, int, bool) {
	if len(b) < 10 || sniffImageMIME(b) != "image/gif" {
		return 0, 0, false
	}
	w := int(binary.LittleEndian.Uint16(b[6:8]))
	h := int(binary.LittleEndian.Uint16(b[8:10]))
	if w == 0 || h == 0 {
		return 0, 0, false
	}
	return w, h, true
}

func webpSize(b []byte) (int, int, bool) {
	if len(b) < 30 || sniffImageMIME(b) != "image/webp" {
		return 0, 0, false
	}
	chunk := string(b[12:16])
	switch chunk {
	case "VP8 ":
		w := int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff)
		h := int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff)
		return w, h, w > 0 && h > 0
	case "VP8L":
		bits := binary.LittleEndian.Uint32(b[21:25])
		w := int(bits&0x3fff) + 1
		h := int((bits>>14)&0x3fff) + 1
		return w, h, w > 0 && h > 0
	case "VP8X":
		w := int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w + 1, h + 1, true
	default:
		return 0, 0, false
	}
}

func splitToolImages(raw string) (string, []ai.ImageContent) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	var obj struct {
		Content []contentBlock `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &obj) == nil && len(obj.Content) > 0 {
		if text, imgs, ok := splitBlocks(obj.Content); ok {
			return text, imgs
		}
	}
	var arr []contentBlock
	if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
		if text, imgs, ok := splitBlocks(arr); ok {
			return text, imgs
		}
	}
	var one contentBlock
	if json.Unmarshal([]byte(raw), &one) == nil && one.Type == "image" && one.Data != "" {
		return "", []ai.ImageContent{normalizeImage(ai.ImageContent{Type: "image", Data: one.Data, MimeType: one.MimeType})}
	}
	return splitDataURLs(raw)
}

func splitBlocks(blocks []contentBlock) (string, []ai.ImageContent, bool) {
	var texts []string
	var imgs []ai.ImageContent
	found := false
	for _, b := range blocks {
		switch b.Type {
		case "text":
			found = true
			if b.Text != "" {
				texts = append(texts, b.Text)
			}
		case "image":
			found = true
			if b.Data != "" {
				imgs = append(imgs, normalizeImage(ai.ImageContent{Type: "image", Data: b.Data, MimeType: b.MimeType}))
			}
		}
	}
	if !found {
		return "", nil, false
	}
	return strings.Join(texts, "\n"), imgs, true
}

func splitDataURLs(raw string) (string, []ai.ImageContent) {
	matches := dataURLRe.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return raw, nil
	}
	var imgs []ai.ImageContent
	for _, m := range matches {
		imgs = append(imgs, normalizeImage(ai.ImageContent{Type: "image", Data: m[2], MimeType: m[1]}))
	}
	text := strings.TrimSpace(dataURLRe.ReplaceAllString(raw, ""))
	return text, imgs
}

func normalizeImage(img ai.ImageContent) ai.ImageContent {
	if img.Type == "" {
		img.Type = "image"
	}
	if rest, ok := strings.CutPrefix(strings.TrimSpace(img.Data), "data:"); ok {
		if i := strings.Index(rest, ";base64,"); i >= 0 {
			img.Data = rest[i+len(";base64,"):]
			if img.MimeType == "" {
				img.MimeType = rest[:i]
			}
		}
	}
	if img.MimeType == "" {
		if b, err := decodeImageData(img.Data); err == nil {
			img.MimeType = sniffImageMIME(b)
		}
	}
	return img
}

func kittyCanInline(img ai.ImageContent) bool {
	if baseMIME(img.MimeType) == "image/png" {
		return true
	}
	b, err := decodeImageData(img.Data)
	return err == nil && sniffImageMIME(b) == "image/png"
}

func (m Model) renderInlineImage(img ai.ImageContent) string {
	if !m.cfg.ShowImages() || m.imgProto == "" {
		return imageFallback(img)
	}
	cells := m.cfg.ImageWidthCells()
	switch m.imgProto {
	case protoKitty:
		if !kittyCanInline(img) {
			return imageFallback(img)
		}
		rows := 0
		if b, err := decodeImageData(img.Data); err == nil {
			if w, h, ok := pngSize(b); ok {
				cells, rows = kittyPlacement(w, h, cells)
			}
		}
		return encodeKitty(img.Data, cells, rows)
	case protoITerm:
		return encodeITerm2(img.Data, cells)
	default:
		return imageFallback(img)
	}
}
