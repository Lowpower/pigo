package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strconv"
	"strings"

	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

const imageMaxEdge = 2000

// SniffImageMIME reports a supported image type from magic bytes.
// PNG, JPEG, GIF, WebP, and BMP are recognized. Other input returns "".
func SniffImageMIME(data []byte) string {
	return sniffImageMIME(data)
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
	case bytes.HasPrefix(data, []byte("BM")):
		return "image/bmp"
	default:
		return ""
	}
}

func decodeImage(data []byte, mime string) (image.Image, error) {
	r := bytes.NewReader(data)
	switch mime {
	case "image/png":
		return png.Decode(r)
	case "image/jpeg":
		return jpeg.Decode(r)
	case "image/gif":
		return gif.Decode(r)
	case "image/webp":
		return webp.Decode(r)
	case "image/bmp":
		return bmp.Decode(r)
	default:
		img, _, err := image.Decode(r)
		return img, err
	}
}

type processedImage struct {
	data     string
	mimeType string
	hints    []string
}

func processImage(data []byte, mime string, autoResize bool, profile *models.ImageResize) (processedImage, bool) {
	img, err := decodeImage(data, mime)
	if err != nil {
		return processedImage{}, false
	}
	outMIME := mime
	var hints []string
	if mime == "image/bmp" {
		outMIME = "image/png"
		hints = append(hints, "[Image converted from image/bmp to image/png.]")
	}

	maxW, maxH, quality, maxBytes := resizeLimits(profile)
	b := img.Bounds()
	origW, origH := b.Dx(), b.Dy()
	w, h := origW, origH
	if autoResize && (w > maxW || h > maxH) {
		img, w, h = scaleImage(img, maxW, maxH)
		if outMIME != "image/jpeg" {
			outMIME = "image/png"
		}
	}

	if autoResize && maxBytes > 0 {
		fitted, mimeOut, fw, fh, ok := fitMaxBytes(img, outMIME, quality, maxBytes)
		if !ok {
			return processedImage{}, false
		}
		if fw != origW || fh != origH {
			hints = append(hints, formatResizeHint(origW, origH, fw, fh))
		}
		return processedImage{
			data:     base64.StdEncoding.EncodeToString(fitted),
			mimeType: mimeOut,
			hints:    hints,
		}, true
	}

	if w != origW || h != origH {
		hints = append(hints, formatResizeHint(origW, origH, w, h))
	}
	encoded, mimeOut, err := encodeImage(img, outMIME, quality)
	if err != nil {
		return processedImage{}, false
	}
	return processedImage{
		data:     base64.StdEncoding.EncodeToString(encoded),
		mimeType: mimeOut,
		hints:    hints,
	}, true
}

func resizeLimits(profile *models.ImageResize) (maxW, maxH, quality, maxBytes int) {
	maxW, maxH, quality = imageMaxEdge, imageMaxEdge, 80
	if profile == nil {
		return maxW, maxH, quality, 0
	}
	if profile.MaxWidth != nil && *profile.MaxWidth > 0 {
		maxW = *profile.MaxWidth
	}
	if profile.MaxHeight != nil && *profile.MaxHeight > 0 {
		maxH = *profile.MaxHeight
	}
	if profile.JPEGQuality != nil && *profile.JPEGQuality > 0 && *profile.JPEGQuality <= 100 {
		quality = *profile.JPEGQuality
	}
	if profile.MaxBytes != nil && *profile.MaxBytes > 0 {
		maxBytes = *profile.MaxBytes
	}
	return maxW, maxH, quality, maxBytes
}

func scaleImage(img image.Image, maxW, maxH int) (image.Image, int, int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := float64(maxW) / float64(w)
	if s := float64(maxH) / float64(h); s < scale {
		scale = s
	}
	nw := max(1, int(float64(w)*scale))
	nh := max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst, nw, nh
}

func fitMaxBytes(img image.Image, mime string, quality, maxBytes int) ([]byte, string, int, int, bool) {
	qualities := jpegQualities(quality)
	cur := img
	for {
		if raw, outMIME, ok := encodeUnder(cur, mime, qualities, maxBytes); ok {
			b := cur.Bounds()
			return raw, outMIME, b.Dx(), b.Dy(), true
		}
		b := cur.Bounds()
		cw, ch := b.Dx(), b.Dy()
		if cw == 1 && ch == 1 {
			return nil, "", 0, 0, false
		}
		nw := max(1, int(float64(cw)*0.75))
		nh := max(1, int(float64(ch)*0.75))
		if nw == cw && nh == ch {
			return nil, "", 0, 0, false
		}
		cur, _, _ = scaleImage(cur, nw, nh)
		mime = "image/jpeg"
	}
}

func encodeUnder(img image.Image, mime string, qualities []int, maxBytes int) ([]byte, string, bool) {
	quality := 80
	if len(qualities) > 0 {
		quality = qualities[0]
	}
	if raw, outMIME, err := encodeImage(img, mime, quality); err == nil && base64.StdEncoding.EncodedLen(len(raw)) < maxBytes {
		return raw, outMIME, true
	}
	for _, q := range qualities {
		raw, err := jpegBytes(img, q)
		if err != nil {
			continue
		}
		if base64.StdEncoding.EncodedLen(len(raw)) < maxBytes {
			return raw, "image/jpeg", true
		}
	}
	return nil, "", false
}

func jpegQualities(first int) []int {
	if first <= 0 || first > 100 {
		first = 80
	}
	out := []int{first}
	seen := map[int]bool{first: true}
	for _, q := range []int{85, 70, 55, 40} {
		if !seen[q] {
			seen[q] = true
			out = append(out, q)
		}
	}
	return out
}

func jpegBytes(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeImage(img image.Image, mime string, quality int) ([]byte, string, error) {
	if quality <= 0 || quality > 100 {
		quality = 80
	}
	var buf bytes.Buffer
	switch mime {
	case "image/jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/jpeg", nil
	case "image/gif":
		if err := gif.Encode(&buf, img, nil); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/gif", nil
	default:
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	}
}

// FitUserImages resizes user-message attachments before they enter history.
// Images already inside the limits keep their original bytes. An image that
// cannot be fit under maxBytes is omitted. BMP attachments are converted to
// PNG even when auto-resize is off, and are not scaled in that case.
func FitUserImages(text string, images []ai.ImageContent, autoResize bool, profile *models.ImageResize) (string, []ai.ImageContent) {
	if len(images) == 0 {
		return text, images
	}
	if !autoResize {
		return convertBMPAttachments(text, images)
	}
	out := make([]ai.ImageContent, 0, len(images))
	var hints []string
	for _, img := range images {
		fitted, h, ok := FitImageContent(img, profile)
		if !ok {
			continue
		}
		out = append(out, fitted)
		hints = append(hints, h...)
	}
	if len(hints) == 0 {
		return text, out
	}
	note := strings.Join(hints, "\n")
	if strings.TrimSpace(text) == "" {
		return note, out
	}
	return text + "\n" + note, out
}

// convertBMPAttachments turns BMP attachments into PNG without scaling.
// Other images keep their original bytes when auto-resize is off.
func convertBMPAttachments(text string, images []ai.ImageContent) (string, []ai.ImageContent) {
	out := make([]ai.ImageContent, len(images))
	copy(out, images)
	var hints []string
	for i, img := range images {
		raw, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			continue
		}
		mime := img.MimeType
		if mime == "" {
			mime = sniffImageMIME(raw)
		}
		if mime != "image/bmp" {
			continue
		}
		processed, ok := processImage(raw, mime, false, nil)
		if !ok {
			continue
		}
		out[i] = ai.ImageContent{Type: "image", Data: processed.data, MimeType: processed.mimeType}
		hints = append(hints, processed.hints...)
	}
	if len(hints) == 0 {
		return text, out
	}
	note := strings.Join(hints, "\n")
	if strings.TrimSpace(text) == "" {
		return note, out
	}
	return text + "\n" + note, out
}

// FitImageContent resizes one base64 image. ok is false only when the image
// decoded but could not be fit under maxBytes. Undecodable payloads are kept.
func FitImageContent(img ai.ImageContent, profile *models.ImageResize) (ai.ImageContent, []string, bool) {
	raw, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return img, nil, true
	}
	mime := img.MimeType
	if mime == "" {
		mime = sniffImageMIME(raw)
	}
	if mime != "image/bmp" {
		within, decoded := imageWithinLimits(raw, mime, profile)
		if !decoded || within {
			return img, nil, true
		}
	}
	processed, ok := processImage(raw, mime, true, profile)
	if !ok {
		return img, nil, false
	}
	return ai.ImageContent{Type: "image", Data: processed.data, MimeType: processed.mimeType}, processed.hints, true
}

func imageWithinLimits(data []byte, mime string, profile *models.ImageResize) (within, decoded bool) {
	img, err := decodeImage(data, mime)
	if err != nil {
		return false, false
	}
	maxW, maxH, _, maxBytes := resizeLimits(profile)
	b := img.Bounds()
	if b.Dx() > maxW || b.Dy() > maxH {
		return false, true
	}
	if maxBytes > 0 && base64.StdEncoding.EncodedLen(len(data)) >= maxBytes {
		return false, true
	}
	return true, true
}

// NormalizeToolResultImages resizes image blocks in a tool result before the
// result enters history. Images that cannot be fit are left unchanged.
func NormalizeToolResultImages(raw string, autoResize bool, profile *models.ImageResize) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !autoResize || !strings.Contains(trimmed, `"image"`) {
		return raw
	}
	var obj struct {
		Content []map[string]any `json:"content"`
	}
	if json.Unmarshal([]byte(trimmed), &obj) == nil && len(obj.Content) > 0 {
		next, changed := normalizeImageBlocks(obj.Content, profile)
		if !changed {
			return raw
		}
		b, err := json.Marshal(map[string]any{"content": next})
		if err != nil {
			return raw
		}
		return string(b)
	}
	var arr []map[string]any
	if json.Unmarshal([]byte(trimmed), &arr) == nil && len(arr) > 0 {
		next, changed := normalizeImageBlocks(arr, profile)
		if !changed {
			return raw
		}
		b, err := json.Marshal(next)
		if err != nil {
			return raw
		}
		return string(b)
	}
	return raw
}

func normalizeImageBlocks(blocks []map[string]any, profile *models.ImageResize) ([]map[string]any, bool) {
	out := make([]map[string]any, 0, len(blocks))
	changed := false
	for _, b := range blocks {
		typ, _ := b["type"].(string)
		if typ != "image" {
			out = append(out, b)
			continue
		}
		data, _ := b["data"].(string)
		mime, _ := b["mimeType"].(string)
		fitted, hints, ok := FitImageContent(ai.ImageContent{Type: "image", Data: data, MimeType: mime}, profile)
		if !ok || (fitted.Data == data && fitted.MimeType == mime && len(hints) == 0) {
			out = append(out, b)
			continue
		}
		changed = true
		out = append(out, map[string]any{"type": "image", "data": fitted.Data, "mimeType": fitted.MimeType})
		if len(hints) > 0 {
			out = append(out, map[string]any{"type": "text", "text": strings.Join(hints, "\n")})
		}
	}
	return out, changed
}

func formatResizeHint(ow, oh, nw, nh int) string {
	return "[Image resized from " + strconv.Itoa(ow) + "x" + strconv.Itoa(oh) + " to " + strconv.Itoa(nw) + "x" + strconv.Itoa(nh) + ".]"
}

func encodeImageResult(note string, img processedImage) string {
	text := note
	for _, h := range img.hints {
		text += "\n" + h
	}
	payload := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
			{"type": "image", "data": img.data, "mimeType": img.mimeType},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return text
	}
	return string(b)
}

func imageReadNote(mime string) string {
	return "Read image file [" + strings.TrimSpace(mime) + "]"
}
