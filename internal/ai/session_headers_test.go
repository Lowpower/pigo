package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func captureStreamHeader(t *testing.T, fixture string, call func(base string, client *http.Client)) http.Header {
	t.Helper()
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(fixture))
	}))
	t.Cleanup(srv.Close)
	call(srv.URL, srv.Client())
	if got == nil {
		t.Fatal("server received no request")
	}
	return got
}

func hiContext() Context {
	return Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
}

func TestOpenCodeSessionHeader(t *testing.T) {
	const googleFixture = "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hi\"}]},\"finishReason\":\"STOP\"}]}\n\n"

	apis := []struct {
		name    string
		fixture string
		stream  func(base string, client *http.Client, headers map[string]string, opts Options)
	}{
		{
			name: "anthropic", fixture: anthropicFixture,
			stream: func(base string, client *http.Client, headers map[string]string, opts Options) {
				c := &AnthropicClient{BaseURL: base, APIKey: "k", Headers: headers, HTTPClient: client}
				stream, err := c.StreamFn()(context.Background(), hiContext(), opts)
				if err != nil {
					t.Fatal(err)
				}
				stream.Collect()
			},
		},
		{
			name: "completions", fixture: openAIFixture,
			stream: func(base string, client *http.Client, headers map[string]string, opts Options) {
				c := &OpenAICompletionsClient{BaseURL: base, APIKey: "k", Headers: headers, HTTPClient: client}
				stream, err := c.StreamFn()(context.Background(), hiContext(), opts)
				if err != nil {
					t.Fatal(err)
				}
				stream.Collect()
			},
		},
		{
			name: "responses", fixture: responsesFixture,
			stream: func(base string, client *http.Client, headers map[string]string, opts Options) {
				c := &OpenAIResponsesClient{BaseURL: base, APIKey: "k", Headers: headers, HTTPClient: client}
				stream, err := c.StreamFn()(context.Background(), hiContext(), opts)
				if err != nil {
					t.Fatal(err)
				}
				stream.Collect()
			},
		},
		{
			name: "google", fixture: googleFixture,
			stream: func(base string, client *http.Client, headers map[string]string, opts Options) {
				c := &GoogleClient{BaseURL: base, APIKey: "k", Headers: headers, HTTPClient: client}
				stream, err := c.StreamFn()(context.Background(), hiContext(), opts)
				if err != nil {
					t.Fatal(err)
				}
				stream.Collect()
			},
		},
	}

	for _, api := range apis {
		t.Run(api.name, func(t *testing.T) {
			t.Run("opencode cache none", func(t *testing.T) {
				got := captureStreamHeader(t, api.fixture, func(base string, client *http.Client) {
					api.stream(base, client, nil, Options{
						Provider: "opencode", Model: "m", SessionID: "sess-1", CacheRetention: "none",
					})
				})
				if got.Get("x-opencode-session") != "sess-1" {
					t.Fatalf("x-opencode-session = %q", got.Get("x-opencode-session"))
				}
			})
			t.Run("opencode-go", func(t *testing.T) {
				got := captureStreamHeader(t, api.fixture, func(base string, client *http.Client) {
					api.stream(base, client, nil, Options{Provider: "opencode-go", Model: "m", SessionID: "sess-2"})
				})
				if got.Get("x-opencode-session") != "sess-2" {
					t.Fatalf("x-opencode-session = %q", got.Get("x-opencode-session"))
				}
			})
			t.Run("no session id", func(t *testing.T) {
				got := captureStreamHeader(t, api.fixture, func(base string, client *http.Client) {
					api.stream(base, client, nil, Options{Provider: "opencode", Model: "m"})
				})
				if got.Get("x-opencode-session") != "" {
					t.Fatalf("x-opencode-session = %q", got.Get("x-opencode-session"))
				}
			})
			t.Run("other provider", func(t *testing.T) {
				got := captureStreamHeader(t, api.fixture, func(base string, client *http.Client) {
					api.stream(base, client, nil, Options{Provider: "anthropic", Model: "m", SessionID: "sess-1"})
				})
				if got.Get("x-opencode-session") != "" {
					t.Fatalf("x-opencode-session = %q", got.Get("x-opencode-session"))
				}
			})
			t.Run("client header wins", func(t *testing.T) {
				got := captureStreamHeader(t, api.fixture, func(base string, client *http.Client) {
					api.stream(base, client, map[string]string{"X-OpenCode-Session": "caller"}, Options{
						Provider: "opencode", Model: "m", SessionID: "generated",
					})
				})
				if got.Get("x-opencode-session") != "caller" {
					t.Fatalf("x-opencode-session = %q", got.Get("x-opencode-session"))
				}
			})
			t.Run("extra header wins", func(t *testing.T) {
				got := captureStreamHeader(t, api.fixture, func(base string, client *http.Client) {
					api.stream(base, client, map[string]string{"x-opencode-session": "from-client"}, Options{
						Provider: "opencode", Model: "m", SessionID: "generated",
						ExtraHeaders: map[string]string{"X-Opencode-Session": "from-request"},
					})
				})
				if got.Get("x-opencode-session") != "from-request" {
					t.Fatalf("x-opencode-session = %q", got.Get("x-opencode-session"))
				}
			})
			t.Run("empty extra deletes", func(t *testing.T) {
				got := captureStreamHeader(t, api.fixture, func(base string, client *http.Client) {
					api.stream(base, client, map[string]string{"x-opencode-session": "from-client"}, Options{
						Provider: "opencode", Model: "m", SessionID: "generated",
						ExtraHeaders: map[string]string{"x-opencode-session": ""},
					})
				})
				if got.Get("x-opencode-session") != "" {
					t.Fatalf("x-opencode-session = %q", got.Get("x-opencode-session"))
				}
			})
		})
	}
}

func TestOpenCodeMuxDefaultsProvider(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-opencode-session")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicFixture))
	}))
	defer srv.Close()

	sf := openCodeMux(ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	stream, err := sf(context.Background(), hiContext(), Options{
		Model: "claude-sonnet-4", SessionID: "mux-1", CacheRetention: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if got != "mux-1" {
		t.Fatalf("x-opencode-session = %q", got)
	}
}

func TestBasetenSessionAffinityHeaders(t *testing.T) {
	stream := func(t *testing.T, headers map[string]string, opts Options) http.Header {
		t.Helper()
		return captureStreamHeader(t, openAIFixture, func(base string, client *http.Client) {
			c := &OpenAICompletionsClient{BaseURL: base, APIKey: "k", Headers: headers, HTTPClient: client}
			s, err := c.StreamFn()(context.Background(), hiContext(), opts)
			if err != nil {
				t.Fatal(err)
			}
			s.Collect()
		})
	}
	wantTrio := func(t *testing.T, got http.Header, sid string) {
		t.Helper()
		for _, k := range []string{"session_id", "x-client-request-id", "x-session-affinity"} {
			if got.Get(k) != sid {
				t.Errorf("%s = %q, want %q", k, got.Get(k), sid)
			}
		}
	}
	absent := func(t *testing.T, got http.Header) {
		t.Helper()
		for _, k := range []string{"session_id", "x-client-request-id", "x-session-affinity"} {
			if got.Get(k) != "" {
				t.Errorf("unexpected %s = %q", k, got.Get(k))
			}
		}
	}

	t.Run("sends fireworks trio", func(t *testing.T) {
		got := stream(t, nil, Options{Provider: "baseten", Model: "zai-org/GLM-5.2", SessionID: "sess-1"})
		wantTrio(t, got, "sess-1")
	})
	t.Run("cache none", func(t *testing.T) {
		got := stream(t, nil, Options{Provider: "baseten", Model: "m", SessionID: "sess-1", CacheRetention: "none"})
		absent(t, got)
	})
	t.Run("no session id", func(t *testing.T) {
		got := stream(t, nil, Options{Provider: "baseten", Model: "m", CacheRetention: "short"})
		absent(t, got)
	})
	t.Run("other provider", func(t *testing.T) {
		got := stream(t, nil, Options{Provider: "openai", Model: "m", SessionID: "sess-1"})
		absent(t, got)
	})
	t.Run("client header wins", func(t *testing.T) {
		got := stream(t, map[string]string{"X-Session-Affinity": "caller"}, Options{
			Provider: "baseten", Model: "m", SessionID: "sess-1",
		})
		if got.Get("x-session-affinity") != "caller" {
			t.Fatalf("x-session-affinity = %q", got.Get("x-session-affinity"))
		}
		if got.Get("session_id") != "sess-1" || got.Get("x-client-request-id") != "sess-1" {
			t.Fatalf("headers = %v", got)
		}
	})
	t.Run("extra header wins", func(t *testing.T) {
		got := stream(t, nil, Options{
			Provider: "baseten", Model: "m", SessionID: "sess-1",
			ExtraHeaders: map[string]string{"session_id": "from-request"},
		})
		if got.Get("session_id") != "from-request" {
			t.Fatalf("session_id = %q", got.Get("session_id"))
		}
		if got.Get("x-session-affinity") != "sess-1" {
			t.Fatalf("x-session-affinity = %q", got.Get("x-session-affinity"))
		}
	})
	t.Run("empty extra deletes", func(t *testing.T) {
		got := stream(t, map[string]string{"x-client-request-id": "from-client"}, Options{
			Provider: "baseten", Model: "m", SessionID: "sess-1",
			ExtraHeaders: map[string]string{"x-client-request-id": ""},
		})
		if got.Get("x-client-request-id") != "" {
			t.Fatalf("x-client-request-id = %q", got.Get("x-client-request-id"))
		}
		if got.Get("session_id") != "sess-1" {
			t.Fatalf("session_id = %q", got.Get("session_id"))
		}
	})
}
