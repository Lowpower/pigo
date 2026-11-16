package codescript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Lowpower/pigo/internal/codemode"
	"github.com/Lowpower/pigo/internal/tools"
)

const (
	maxOutputItems = 100000
	maxOutputChars = 16 * 1024 * 1024
)

var (
	errTooManyItems = errors.New("script output exceeded 100000 items")
	errTooManyChars = errors.New("script output exceeded 16777216 characters")
)

// Image is one accepted image() value. Data is standard base64. Path is the
// user-only temp file written for that image.
type Image struct {
	Data     string
	MimeType string
	Path     string
}

func (r *runner) addItem(ctx context.Context, chars int) error {
	if r.stopped {
		if r.limitErr == nil {
			return errTooManyItems
		}
		return r.limitErr
	}
	if r.items >= maxOutputItems {
		return r.rejectLimit(ctx, errTooManyItems)
	}
	if chars > 0 && r.chars+chars > maxOutputChars {
		return r.rejectLimit(ctx, errTooManyChars)
	}
	r.items++
	r.chars += chars
	return nil
}

func (r *runner) addChars(ctx context.Context, chars int) error {
	if r.stopped {
		if r.limitErr == nil {
			return errTooManyChars
		}
		return r.limitErr
	}
	if chars > 0 && r.chars+chars > maxOutputChars {
		return r.rejectLimit(ctx, errTooManyChars)
	}
	r.chars += chars
	return nil
}

func (r *runner) rejectLimit(ctx context.Context, err error) error {
	r.stopped = true
	if r.limitErr == nil {
		r.limitErr = err
	}
	codemode.RequestStop(ctx, r.limitErr.Error())
	return r.limitErr
}

func charCount(s string) int {
	return utf8.RuneCountInString(s)
}

func (r *runner) addText(ctx context.Context, text string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.addItem(ctx, charCount(text)); err != nil {
		return "", err
	}
	r.result.Texts = append(r.result.Texts, text)
	return "null", nil
}

func (r *runner) addConsole(ctx context.Context, line string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.addItem(ctx, charCount(line)); err != nil {
		return "", err
	}
	r.result.Console = append(r.result.Console, line)
	return "null", nil
}

func (r *runner) addImage(ctx context.Context, v any) (string, error) {
	data, mime, err := parseImage(v)
	if err != nil {
		return "", err
	}
	path, err := writeImageFile(data, mime)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.addItem(ctx, charCount(data)); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	r.result.Images = append(r.result.Images, Image{Data: data, MimeType: mime, Path: path})
	return "null", nil
}

// Render joins the return value with collected text, console lines, and images.
// Text items are each text() call, then a non-empty return value. More than
// one text item starts each item with "==> text N/M <==". Console calls sit
// in one block. Images are content blocks, and each path is named in the text.
func (r Result) Render() string {
	return r.withTook("")
}

// WithTook renders the result and appends a Took line to the text.
func (r Result) WithTook(d time.Duration) string {
	return r.withSuffix("", "Took "+tools.FormatDuration(d))
}

// WithErrorTook renders the result, then the error, then the Took line.
// The duration stays on the last line so a collapsed view can keep it.
func (r Result) WithErrorTook(d time.Duration, err error) string {
	extra := ""
	if err != nil {
		extra = err.Error()
	}
	return r.withSuffix(extra, "Took "+tools.FormatDuration(d))
}

func (r Result) withTook(took string) string {
	return r.withSuffix("", took)
}

func (r Result) withSuffix(extra, took string) string {
	texts := make([]string, 0, len(r.Texts)+1)
	texts = append(texts, r.Texts...)
	if r.Value != "" {
		texts = append(texts, r.Value)
	}
	var b strings.Builder
	switch len(texts) {
	case 0:
	case 1:
		b.WriteString(texts[0])
	default:
		for i, text := range texts {
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "==> text %d/%d <==\n%s", i+1, len(texts), text)
		}
	}
	if len(r.Console) > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("<console_output>\n")
		for _, line := range r.Console {
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteString("</console_output>")
	}
	for _, img := range r.Images {
		if img.Path == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(img.Path)
	}
	text := b.String()
	if extra != "" && !strings.Contains(text, extra) {
		if text != "" {
			text += "\n"
		}
		text += extra
	}
	if took != "" {
		if text != "" {
			text += "\n"
		}
		text += took
	}
	if len(r.Images) == 0 {
		return text
	}
	blocks := make([]any, 0, len(r.Images)+1)
	if text != "" {
		blocks = append(blocks, map[string]string{"type": "text", "text": text})
	}
	for _, img := range r.Images {
		blocks = append(blocks, map[string]string{
			"type":     "image",
			"data":     img.Data,
			"mimeType": img.MimeType,
		})
	}
	raw, err := json.Marshal(map[string]any{"content": blocks})
	if err != nil {
		return text
	}
	return string(raw)
}
