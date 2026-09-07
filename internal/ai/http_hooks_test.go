package ai

import (
	"net/http"
	"testing"
)

func TestApplyExtraHeadersAndTransformBody(t *testing.T) {
	h := make(http.Header)
	h.Set("X-Keep", "1")
	applyExtraHeaders(h, map[string]string{"X-Add": "2", "X-Keep": ""})
	if h.Get("X-Add") != "2" {
		t.Fatalf("X-Add=%q", h.Get("X-Add"))
	}
	if h.Get("X-Keep") != "" {
		t.Fatalf("X-Keep should be deleted, got %q", h.Get("X-Keep"))
	}

	opts := Options{TransformBody: func(body []byte) []byte {
		return append(body, '!')
	}}
	got := transformRequestBody(opts, []byte("hi"))
	if string(got) != "hi!" {
		t.Fatalf("body=%q", got)
	}
	if string(transformRequestBody(Options{}, []byte("hi"))) != "hi" {
		t.Fatal("nil transform should be identity")
	}
}
