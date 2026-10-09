package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// emitProviderStreamEvent delivers one parsed JSON event. A nil fn returns
// without inspecting data.
func emitProviderStreamEvent(fn func([]byte), data []byte) {
	if fn == nil {
		return
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return
	}
	buf := make([]byte, len(data))
	copy(buf, data)
	fn(buf)
}

// observeSDKEvent JSON-encodes a parsed SDK chunk. Marshal failure skips the
// event and does not fail the stream.
func observeSDKEvent(fn func([]byte), v any) {
	if fn == nil || v == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return
	}
	fn(raw)
}

// frameSSEClient wraps a successful event-stream body so the last event is
// still delivered when the server closes without a trailing blank line.
// observe receives each complete JSON event, including that last one.
func frameSSEClient(base *http.Client, observe func([]byte)) *http.Client {
	c := &http.Client{}
	if base != nil {
		*c = *base
	}
	c.Transport = sseFrameTransport{base: c.Transport, observe: observe}
	return c
}

func frameSSEBody(rc io.ReadCloser, observe func([]byte)) io.ReadCloser {
	if rc == nil {
		return nil
	}
	rc = &sseFlushReadCloser{rc: rc}
	if observe != nil {
		rc = &sseObserveReadCloser{rc: rc, observe: observe}
	}
	return rc
}

type sseFrameTransport struct {
	base    http.RoundTripper
	observe func([]byte)
}

func (t sseFrameTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, err
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return resp, err
	}
	resp.Body = frameSSEBody(resp.Body, t.observe)
	return resp, err
}

// sseFlushReadCloser finishes a residual SSE frame at EOF. openai-go only
// dispatches an event when it sees a blank line, so a terminal event that is
// not followed by one would otherwise be dropped with a nil stream error.
type sseFlushReadCloser struct {
	rc    io.ReadCloser
	tail  []byte
	extra []byte
	eof   bool
}

func (b *sseFlushReadCloser) Read(p []byte) (int, error) {
	if len(b.extra) > 0 {
		n := copy(p, b.extra)
		b.extra = b.extra[n:]
		return n, nil
	}
	if b.eof {
		return 0, io.EOF
	}
	n, err := b.rc.Read(p)
	if n > 0 {
		b.note(p[:n])
	}
	if err != io.EOF {
		return n, err
	}
	b.eof = true
	pad := sseFlushPad(b.tail)
	if len(pad) == 0 {
		return n, io.EOF
	}
	if n > 0 {
		b.extra = pad
		return n, nil
	}
	n = copy(p, pad)
	b.extra = pad[n:]
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func (b *sseFlushReadCloser) Close() error { return b.rc.Close() }

func (b *sseFlushReadCloser) note(chunk []byte) {
	if len(chunk) >= 4 {
		b.tail = append([]byte(nil), chunk[len(chunk)-4:]...)
		return
	}
	b.tail = append(b.tail, chunk...)
	if len(b.tail) > 4 {
		b.tail = append([]byte(nil), b.tail[len(b.tail)-4:]...)
	}
}

func sseFlushPad(tail []byte) []byte {
	if len(tail) == 0 {
		return nil
	}
	if bytes.HasSuffix(tail, []byte("\n\n")) || bytes.HasSuffix(tail, []byte("\r\n\r\n")) {
		return nil
	}
	if bytes.HasSuffix(tail, []byte("\n")) {
		return []byte("\n")
	}
	return []byte("\n\n")
}

type sseObserveReadCloser struct {
	rc      io.ReadCloser
	observe func([]byte)
	carry   []byte
}

func (b *sseObserveReadCloser) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 && b.observe != nil {
		b.carry = append(b.carry, p[:n]...)
		b.flush()
	}
	return n, err
}

func (b *sseObserveReadCloser) Close() error { return b.rc.Close() }

func (b *sseObserveReadCloser) flush() {
	for {
		idx, sep := sseEventEnd(b.carry)
		if idx < 0 {
			return
		}
		block := b.carry[:idx]
		b.carry = append([]byte(nil), b.carry[idx+sep:]...)
		payload := sseDataPayload(block)
		if payload == "" {
			continue
		}
		raw := []byte(payload)
		if !json.Valid(raw) {
			continue
		}
		emitProviderStreamEvent(b.observe, raw)
	}
}

func sseEventEnd(buf []byte) (idx, sep int) {
	idx = bytes.Index(buf, []byte("\n\n"))
	sep = 2
	if i := bytes.Index(buf, []byte("\r\n\r\n")); i >= 0 && (idx < 0 || i < idx) {
		return i, 4
	}
	if idx < 0 {
		return -1, 0
	}
	return idx, sep
}

func sseDataPayload(block []byte) string {
	var data bytes.Buffer
	for _, line := range bytes.Split(block, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			continue
		}
		name, value, ok := bytes.Cut(line, []byte(":"))
		if !ok {
			continue
		}
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		if string(name) != "data" {
			continue
		}
		if data.Len() > 0 {
			data.WriteByte('\n')
		}
		data.Write(value)
	}
	return data.String()
}
