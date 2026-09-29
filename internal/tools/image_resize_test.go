package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func noisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	n := 1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n = (n*1103515245 + 12345) & 0x7fffffff
			img.Set(x, y, color.RGBA{R: uint8(n), G: uint8(n >> 8), B: uint8(n >> 16), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func solidJPEG(t *testing.T, w, h, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 5), B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProcessImageProfileShrinksWidth(t *testing.T) {
	width := 100
	got, ok := processImage(solidPNG(t, 200, 10), "image/png", true, &models.ImageResize{MaxWidth: &width})
	if !ok {
		t.Fatal("process")
	}
	if !strings.Contains(strings.Join(got.hints, "\n"), "resized from 200x10 to 100x5") {
		t.Fatalf("hints = %v", got.hints)
	}
}

func TestProcessImageNoProfileUses2000(t *testing.T) {
	got, ok := processImage(solidPNG(t, 2001, 10), "image/png", true, nil)
	if !ok {
		t.Fatal("process")
	}
	joined := strings.Join(got.hints, "\n")
	if !strings.Contains(joined, "resized from 2001x10 to 2000x") {
		t.Fatalf("hints = %v", got.hints)
	}
}

func TestProcessImageAutoResizeOffIgnoresProfile(t *testing.T) {
	width := 50
	got, ok := processImage(solidPNG(t, 200, 10), "image/png", false, &models.ImageResize{MaxWidth: &width})
	if !ok {
		t.Fatal("process")
	}
	if strings.Contains(strings.Join(got.hints, "\n"), "resized from") {
		t.Fatalf("resized with autoResize off: %v", got.hints)
	}
}

func TestProcessImageProfileCanRaiseEdge(t *testing.T) {
	width := 3000
	got, ok := processImage(solidPNG(t, 2500, 10), "image/png", true, &models.ImageResize{MaxWidth: &width})
	if !ok {
		t.Fatal("process")
	}
	if strings.Contains(strings.Join(got.hints, "\n"), "resized from") {
		t.Fatalf("width under profile max should not resize: %v", got.hints)
	}
}

func TestProcessImageJPEGQuality(t *testing.T) {
	src := solidJPEG(t, 400, 200, 90)
	lowQ, highQ := 10, 95
	edge := 200
	low, ok := processImage(src, "image/jpeg", true, &models.ImageResize{MaxWidth: &edge, JPEGQuality: &lowQ})
	if !ok {
		t.Fatal("low")
	}
	high, ok := processImage(src, "image/jpeg", true, &models.ImageResize{MaxWidth: &edge, JPEGQuality: &highQ})
	if !ok {
		t.Fatal("high")
	}
	if low.mimeType != "image/jpeg" || high.mimeType != "image/jpeg" {
		t.Fatalf("mime low=%s high=%s", low.mimeType, high.mimeType)
	}
	if len(low.data) >= len(high.data) {
		t.Fatalf("q10 base64 len %d, q95 %d", len(low.data), len(high.data))
	}
}

func TestProcessImageMaxBytesShrinks(t *testing.T) {
	src := noisyPNG(t, 64, 64)
	srcB64 := base64.StdEncoding.EncodedLen(len(src))
	limit := 2000
	if srcB64 <= limit {
		t.Fatalf("fixture base64 %d is not over the limit", srcB64)
	}
	got, ok := processImage(src, "image/png", true, &models.ImageResize{MaxBytes: &limit})
	if !ok {
		t.Fatal("expected a fit under maxBytes")
	}
	if len(got.data) >= limit {
		t.Fatalf("base64 len %d, limit %d", len(got.data), limit)
	}
}

func TestReadUsesModelResizeProfile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "wide.png")
	if err := os.WriteFile(file, solidPNG(t, 200, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	width := 100
	out, isErr := run(t, readTool{
		autoResize: true,
		resize: func() *models.ImageResize {
			return &models.ImageResize{MaxWidth: &width}
		},
	}, map[string]any{"path": file})
	if isErr {
		t.Fatalf("read: %s", out)
	}
	if !strings.Contains(out, "resized from 200x10 to 100x5") {
		t.Fatalf("profile not applied: %q", out)
	}
}

func TestFitUserImagesUsesProfileAndIsIdempotent(t *testing.T) {
	width := 20
	img := ai.ImageContent{Type: "image", Data: base64.StdEncoding.EncodeToString(solidPNG(t, 80, 8)), MimeType: "image/png"}
	text, images := FitUserImages("look", []ai.ImageContent{img}, true, &models.ImageResize{MaxWidth: &width})
	if len(images) != 1 || images[0].Data == img.Data {
		t.Fatal("expected a resized attachment")
	}
	if !strings.Contains(text, "resized from 80x8 to 20x2") {
		t.Fatalf("text = %q", text)
	}
	againText, again := FitUserImages(text, images, true, &models.ImageResize{MaxWidth: &width})
	if againText != text || len(again) != 1 || again[0].Data != images[0].Data {
		t.Fatalf("second pass changed bytes or text")
	}
}

func TestFitUserImagesNoProfileUses2000(t *testing.T) {
	img := ai.ImageContent{Type: "image", Data: base64.StdEncoding.EncodeToString(solidPNG(t, 2001, 10)), MimeType: "image/png"}
	text, images := FitUserImages("look", []ai.ImageContent{img}, true, nil)
	if len(images) != 1 {
		t.Fatal("dropped image")
	}
	if !strings.Contains(text, "resized from 2001x10 to 2000x") {
		t.Fatalf("text = %q", text)
	}
}

func TestFitUserImagesAutoResizeOff(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString(solidPNG(t, 2001, 10))
	img := ai.ImageContent{Type: "image", Data: raw, MimeType: "image/png"}
	width := 10
	text, images := FitUserImages("look", []ai.ImageContent{img}, false, &models.ImageResize{MaxWidth: &width})
	if text != "look" || len(images) != 1 || images[0].Data != raw {
		t.Fatalf("autoResize off changed attachment: %q %+v", text, images)
	}
}

func TestNormalizeToolResultUsesProfile(t *testing.T) {
	raw := toolImageResult(t, solidPNG(t, 40, 4))
	width := 10
	got := NormalizeToolResultImages(raw, true, &models.ImageResize{MaxWidth: &width})
	if !strings.Contains(got, "resized from 40x4 to 10x1") {
		t.Fatalf("tool result = %s", got)
	}
	again := NormalizeToolResultImages(got, true, &models.ImageResize{MaxWidth: &width})
	if again != got {
		t.Fatal("second pass rewrote an in-limit tool result")
	}
}

func TestNormalizeToolResultNoProfileUses2000(t *testing.T) {
	raw := toolImageResult(t, solidPNG(t, 2001, 10))
	got := NormalizeToolResultImages(raw, true, nil)
	if !strings.Contains(got, "resized from 2001x10 to 2000x") {
		t.Fatalf("tool result = %s", got)
	}
}

func TestNormalizeToolResultKeepsOriginalWhenMaxBytesImpossible(t *testing.T) {
	raw := toolImageResult(t, solidPNG(t, 8, 8))
	limit := 1
	got := NormalizeToolResultImages(raw, true, &models.ImageResize{MaxBytes: &limit})
	if got != raw {
		t.Fatalf("tool result changed on failure:\n%s", got)
	}
}

func toolImageResult(t *testing.T, data []byte) string {
	t.Helper()
	payload := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "shot"},
			{"type": "image", "data": base64.StdEncoding.EncodeToString(data), "mimeType": "image/png"},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProcessImageMaxBytesTooSmallFails(t *testing.T) {
	limit := 1
	_, ok := processImage(solidPNG(t, 8, 8), "image/png", true, &models.ImageResize{MaxBytes: &limit})
	if ok {
		t.Fatal("expected failure under 1-byte limit")
	}
}
