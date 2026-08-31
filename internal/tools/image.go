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
)

const imageMaxEdge = 2000

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

func processImage(data []byte, mime string, autoResize bool) (processedImage, bool) {
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

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if autoResize && (w > imageMaxEdge || h > imageMaxEdge) {
		scale := float64(imageMaxEdge) / float64(w)
		if s := float64(imageMaxEdge) / float64(h); s < scale {
			scale = s
		}
		nw := max(1, int(float64(w)*scale))
		nh := max(1, int(float64(h)*scale))
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
		img = dst
		hints = append(hints, formatResizeHint(w, h, nw, nh))
		if outMIME != "image/jpeg" {
			outMIME = "image/png"
		}
	}

	var buf bytes.Buffer
	switch outMIME {
	case "image/jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
			return processedImage{}, false
		}
	case "image/gif":
		if err := gif.Encode(&buf, img, nil); err != nil {
			return processedImage{}, false
		}
	default:
		outMIME = "image/png"
		if err := png.Encode(&buf, img); err != nil {
			return processedImage{}, false
		}
	}
	return processedImage{
		data:     base64.StdEncoding.EncodeToString(buf.Bytes()),
		mimeType: outMIME,
		hints:    hints,
	}, true
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
