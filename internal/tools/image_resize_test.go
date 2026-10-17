package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/bmp"
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

func TestFitUserImagesAutoResizeOffConvertsBMP(t *testing.T) {
	raw := solidBMP(t, 8, 8)
	img := ai.ImageContent{Type: "image", Data: base64.StdEncoding.EncodeToString(raw), MimeType: "image/bmp"}
	width := 2
	text, images := FitUserImages("look", []ai.ImageContent{img}, false, &models.ImageResize{MaxWidth: &width})
	if len(images) != 1 || images[0].MimeType != "image/png" {
		t.Fatalf("images = %+v", images)
	}
	if strings.Contains(text, "resized from") {
		t.Fatalf("resized with autoResize off: %q", text)
	}
	decoded, err := png.Decode(base64.NewDecoder(base64.StdEncoding, strings.NewReader(images[0].Data)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 8 || decoded.Bounds().Dy() != 8 {
		t.Fatalf("bounds = %v", decoded.Bounds())
	}
}

func solidBMP(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := bmp.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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

func TestProcessImageAppliesEXIFOrientation(t *testing.T) {
	raw := jpegWithAPP1(t, wideSplitJPEG(t), exifAPP1(6))
	assertResizedOrientation(t, raw)
}

func TestProcessImageEXIFAfterNonEXIFAPP1(t *testing.T) {
	raw := jpegWithAPP1(t, wideSplitJPEG(t), xmpAPP1(), exifAPP1(6))
	assertResizedOrientation(t, raw)
}

func TestNormalizeImageKeepsUnderLimitBytes(t *testing.T) {
	samples := [][]byte{
		solidJPEG(t, 32, 24, 90),
		multiFrameGIF(t),
		jpegWithAPP1(t, solidJPEG(t, 32, 16, 90), exifAPP1(6)),
	}
	for _, raw := range samples {
		mime := sniffImageMIME(raw)
		got, ok := processImage(raw, mime, true, nil)
		if !ok {
			t.Fatalf("%s process", mime)
		}
		decoded, err := base64.StdEncoding.DecodeString(got.data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, raw) {
			t.Fatalf("%s bytes changed (%d -> %d)", mime, len(raw), len(decoded))
		}
		if len(got.hints) != 0 {
			t.Fatalf("%s hints = %v", mime, got.hints)
		}
		b64 := base64.StdEncoding.EncodeToString(raw)
		text, images := FitUserImages("look", []ai.ImageContent{{Type: "image", Data: b64, MimeType: mime}}, true, nil)
		if text != "look" || len(images) != 1 || images[0].Data != b64 {
			t.Fatalf("%s attachment changed", mime)
		}
		ext := ".jpg"
		if mime == "image/gif" {
			ext = ".gif"
		}
		dir := t.TempDir()
		file := filepath.Join(dir, "img"+ext)
		if err := os.WriteFile(file, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		out, isErr := run(t, readTool{autoResize: true}, map[string]any{"path": file})
		if isErr {
			t.Fatalf("read %s: %s", mime, out)
		}
		if !strings.Contains(out, b64) {
			t.Fatalf("read re-encoded %s", mime)
		}
	}
}

func TestExifOrientationSkipsNonEXIFAPP1(t *testing.T) {
	body := solidJPEG(t, 8, 4, 80)
	if got := exifOrientation(jpegWithAPP1(t, body, exifAPP1(6))); got != 6 {
		t.Fatalf("exif = %d", got)
	}
	if got := exifOrientation(jpegWithAPP1(t, body, xmpAPP1(), exifAPP1(6))); got != 6 {
		t.Fatalf("xmp then exif = %d", got)
	}
	if got := exifOrientation(jpegWithAPP1(t, body, xmpAPP1())); got != 1 {
		t.Fatalf("xmp only = %d", got)
	}
	if got := exifOrientation(webpWithEXIF(junkChunk(), 8)); got != 8 {
		t.Fatalf("webp after junk = %d", got)
	}
	if got := exifOrientation(webpWithEXIF(nil, 3)); got != 3 {
		t.Fatalf("webp = %d", got)
	}
}

func TestApplyOrientation(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{R: 1, A: 255})
	src.Set(1, 0, color.RGBA{R: 2, A: 255})
	src.Set(0, 1, color.RGBA{R: 3, A: 255})
	src.Set(1, 1, color.RGBA{R: 4, A: 255})
	cases := []struct {
		orient int
		want   [4]uint8
	}{
		{1, [4]uint8{1, 2, 3, 4}},
		{2, [4]uint8{2, 1, 4, 3}},
		{3, [4]uint8{4, 3, 2, 1}},
		{4, [4]uint8{3, 4, 1, 2}},
		{5, [4]uint8{1, 3, 2, 4}},
		{6, [4]uint8{3, 1, 4, 2}},
		{7, [4]uint8{4, 2, 3, 1}},
		{8, [4]uint8{2, 4, 1, 3}},
	}
	for _, tc := range cases {
		got := applyOrientation(src, tc.orient)
		b := got.Bounds()
		if b.Dx() != 2 || b.Dy() != 2 {
			t.Fatalf("orient %d bounds %v", tc.orient, b)
		}
		i := 0
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				r, _, _, _ := got.At(b.Min.X+x, b.Min.Y+y).RGBA()
				if uint8(r>>8) != tc.want[i] {
					t.Fatalf("orient %d pixel %d = %d, want %d", tc.orient, i, r>>8, tc.want[i])
				}
				i++
			}
		}
	}
}

func assertResizedOrientation(t *testing.T, raw []byte) {
	t.Helper()
	maxW, maxH, q := 1000, 100, 95
	profile := &models.ImageResize{MaxWidth: &maxW, MaxHeight: &maxH, JPEGQuality: &q}
	got, ok := processImage(raw, "image/jpeg", true, profile)
	if !ok {
		t.Fatal("process")
	}
	if !strings.Contains(strings.Join(got.hints, "\n"), "resized from 80x200 to 40x100") {
		t.Fatalf("hints = %v", got.hints)
	}
	assertPortraitColors(t, got.data)

	b64 := base64.StdEncoding.EncodeToString(raw)
	text, images := FitUserImages("look", []ai.ImageContent{{Type: "image", Data: b64, MimeType: "image/jpeg"}}, true, profile)
	if len(images) != 1 || images[0].Data != got.data {
		t.Fatal("attachment path diverged")
	}
	if !strings.Contains(text, "resized from 80x200 to 40x100") {
		t.Fatalf("text = %q", text)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "phone.jpg")
	if err := os.WriteFile(file, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	out, isErr := run(t, readTool{
		autoResize: true,
		resize: func() *models.ImageResize {
			return profile
		},
	}, map[string]any{"path": file})
	if isErr {
		t.Fatalf("read: %s", out)
	}
	if !strings.Contains(out, got.data) {
		t.Fatal("read path diverged")
	}
}

func assertPortraitColors(t *testing.T, b64 string) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != 40 || b.Dy() != 100 {
		t.Fatalf("bounds = %v", b)
	}
	if !mostlyRed(img.At(20, 15)) {
		r, g, b := channel8(img.At(20, 15))
		t.Fatalf("top = %d,%d,%d", r, g, b)
	}
	if !mostlyBlue(img.At(20, 85)) {
		r, g, b := channel8(img.At(20, 85))
		t.Fatalf("bottom = %d,%d,%d", r, g, b)
	}
}

func mostlyRed(c color.Color) bool {
	r, _, b := channel8(c)
	return r > 200 && b < 80
}

func mostlyBlue(c color.Color) bool {
	r, _, b := channel8(c)
	return b > 200 && r < 80
}

func channel8(c color.Color) (r, g, b int) {
	rr, gg, bb, _ := c.RGBA()
	return int(rr >> 8), int(gg >> 8), int(bb >> 8)
}

func wideSplitJPEG(t *testing.T) []byte {
	t.Helper()
	const w, h = 200, 80
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func multiFrameGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White, color.RGBA{R: 255, A: 255}}
	a := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	b := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	b.SetColorIndex(1, 1, 2)
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{a, b}, Delay: []int{5, 5}}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegWithAPP1(t *testing.T, jpeg []byte, segs ...[]byte) []byte {
	t.Helper()
	if len(jpeg) < 2 || jpeg[0] != 0xff || jpeg[1] != 0xd8 {
		t.Fatal("not a jpeg")
	}
	var buf bytes.Buffer
	buf.Write(jpeg[:2])
	for _, seg := range segs {
		buf.Write(seg)
	}
	buf.Write(jpeg[2:])
	return buf.Bytes()
}

func app1Segment(payload []byte) []byte {
	length := len(payload) + 2
	out := []byte{0xff, 0xe1, byte(length >> 8), byte(length)}
	return append(out, payload...)
}

func exifTIFF(orient int) []byte {
	return []byte{
		'I', 'I', 0x2a, 0x00,
		0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01,
		0x03, 0x00,
		0x01, 0x00, 0x00, 0x00,
		byte(orient), 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
}

func exifAPP1(orient int) []byte {
	return app1Segment(append([]byte("Exif\x00\x00"), exifTIFF(orient)...))
}

func xmpAPP1() []byte {
	payload := append([]byte("http://ns.adobe.com/xap/1.0/\x00"), []byte("<x:xmpmeta/>")...)
	return app1Segment(payload)
}

func junkChunk() []byte {
	return []byte{'J', 'U', 'N', 'K', 4, 0, 0, 0, 'x', 'x', 'x', 'x'}
}

func webpWithEXIF(before []byte, orient int) []byte {
	payload := append([]byte("Exif\x00\x00"), exifTIFF(orient)...)
	chunk := []byte{'E', 'X', 'I', 'F', byte(len(payload)), byte(len(payload) >> 8), byte(len(payload) >> 16), byte(len(payload) >> 24)}
	chunk = append(chunk, payload...)
	if len(payload)%2 == 1 {
		chunk = append(chunk, 0)
	}
	body := append(append([]byte{}, before...), chunk...)
	riffSize := 4 + len(body)
	out := []byte{'R', 'I', 'F', 'F', byte(riffSize), byte(riffSize >> 8), byte(riffSize >> 16), byte(riffSize >> 24), 'W', 'E', 'B', 'P'}
	return append(out, body...)
}
