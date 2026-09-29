package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/version"
)

func TestFormatProviderUserAgent(t *testing.T) {
	got := formatProviderUserAgent("1.2.3", "linux", "6.8.0", "amd64")
	if got != "pigo/1.2.3 (linux 6.8.0; amd64)" {
		t.Fatalf("ua = %q", got)
	}
	got = formatProviderUserAgent("1.2.3", "linux", "", "amd64")
	if got != "pigo/1.2.3 (linux unknown; amd64)" {
		t.Fatalf("empty release = %q", got)
	}
}

func TestProviderUserAgentUsesRuntime(t *testing.T) {
	got := providerUserAgent()
	prefix := "pigo/" + version.Version + " (" + runtime.GOOS + " "
	suffix := "; " + runtime.GOARCH + ")"
	if !strings.HasPrefix(got, prefix) || !strings.HasSuffix(got, suffix) {
		t.Fatalf("ua = %q", got)
	}
	release := strings.TrimSuffix(strings.TrimPrefix(got, prefix), suffix)
	if release == "" || strings.ContainsAny(release, "\r\n") {
		t.Fatalf("release = %q", release)
	}
	if runtime.GOOS == "linux" {
		out, err := exec.Command("uname", "-r").Output()
		if err != nil {
			t.Fatal(err)
		}
		if release != strings.TrimSpace(string(out)) {
			t.Fatalf("release = %q, uname -r = %q", release, strings.TrimSpace(string(out)))
		}
	}
}

func TestSetDefaultUserAgent(t *testing.T) {
	h := make(http.Header)
	setDefaultUserAgent(h)
	if h.Get("User-Agent") != providerUserAgent() {
		t.Fatalf("default = %q", h.Get("User-Agent"))
	}
	h.Set("User-Agent", "custom")
	setDefaultUserAgent(h)
	if h.Get("User-Agent") != "custom" {
		t.Fatalf("kept = %q", h.Get("User-Agent"))
	}
}

func TestDoHTTPUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	do := func(preset string, opts Options) {
		t.Helper()
		got = ""
		req, err := http.NewRequest(http.MethodPost, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if preset != "" {
			req.Header.Set("User-Agent", preset)
		}
		resp, err := doHTTP(srv.Client(), req, opts)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	do("", Options{})
	if got != providerUserAgent() {
		t.Fatalf("default = %q", got)
	}
	do("from-models", Options{})
	if got != "from-models" {
		t.Fatalf("models = %q", got)
	}
	do("from-models", Options{ExtraHeaders: map[string]string{"User-Agent": "from-request"}})
	if got != "from-request" {
		t.Fatalf("request = %q", got)
	}
}

func TestOpenAIClientUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(openAIFixture))
	}))
	t.Cleanup(srv.Close)

	send := func(headers map[string]string, extra map[string]string) {
		t.Helper()
		got = ""
		client := &OpenAICompletionsClient{BaseURL: srv.URL, APIKey: "k", Headers: headers, HTTPClient: srv.Client()}
		stream, err := client.StreamFn()(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt-test", ExtraHeaders: extra})
		if err != nil {
			t.Fatal(err)
		}
		stream.Collect()
	}

	send(nil, nil)
	if got != providerUserAgent() {
		t.Fatalf("default = %q", got)
	}
	send(map[string]string{"user-agent": "from-models"}, nil)
	if got != "from-models" {
		t.Fatalf("models = %q", got)
	}
	send(map[string]string{"User-Agent": "from-models"}, map[string]string{"User-Agent": "from-request"})
	if got != "from-request" {
		t.Fatalf("request = %q", got)
	}
}

func TestGoogleUserAgentHeaders(t *testing.T) {
	c := &GoogleClient{APIKey: "k"}
	if got := c.outboundHeaders(Options{}).Get("User-Agent"); got != providerUserAgent() {
		t.Fatalf("default = %q", got)
	}
	c.Headers = map[string]string{"User-Agent": "from-models"}
	if got := c.outboundHeaders(Options{}).Get("User-Agent"); got != "from-models" {
		t.Fatalf("models = %q", got)
	}
	got := c.outboundHeaders(Options{ExtraHeaders: map[string]string{"user-agent": "from-request"}}).Get("User-Agent")
	if got != "from-request" {
		t.Fatalf("request = %q", got)
	}
}

func TestCodexWebSocketUserAgent(t *testing.T) {
	c := &OpenAICodexClient{APIKey: "k"}
	if got := c.websocketHeaders("").Get("User-Agent"); got != providerUserAgent() {
		t.Fatalf("default = %q", got)
	}
	c.Headers = map[string]string{"User-Agent": "from-models"}
	if got := c.websocketHeaders("sess").Get("User-Agent"); got != "from-models" {
		t.Fatalf("models = %q", got)
	}
}

func TestBedrockUserAgentTransport(t *testing.T) {
	var gotUA, gotAuth string
	base := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotUA = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}

	do := func(apiKey string, configured, extra map[string]string) {
		t.Helper()
		gotUA, gotAuth = "", ""
		client := withBedrockHTTP(base, apiKey, configured, extra)
		req, err := http.NewRequest(http.MethodPost, "https://bedrock.example/model", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", "aws-sdk-go-v2/1.0")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	do("token", nil, nil)
	if gotUA != providerUserAgent() {
		t.Fatalf("default = %q", gotUA)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	do("", map[string]string{"User-Agent": "from-models"}, nil)
	if gotUA != "from-models" {
		t.Fatalf("models = %q", gotUA)
	}
	do("", map[string]string{"User-Agent": "from-models"}, map[string]string{"User-Agent": "from-request"})
	if gotUA != "from-request" {
		t.Fatalf("request = %q", gotUA)
	}
}

func TestCopilotKeepsProviderUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)

	stream, err := StreamFor("github-copilot", ClientConfig{
		APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(),
	})(context.Background(), Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, Options{Model: "gpt-4.1"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if got != "GitHubCopilotChat/0.35.0" {
		t.Fatalf("copilot ua = %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
