package ai

import (
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestSessionAffinityHeaders(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "aff-test", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{
			{Provider: "aff-test", ID: "openai", Compat: &models.Compat{SessionAffinityFormat: "openai"}},
			{Provider: "aff-test", ID: "nosession", Compat: &models.Compat{SessionAffinityFormat: "openai-nosession"}},
			{Provider: "aff-test", ID: "openrouter", Compat: &models.Compat{SessionAffinityFormat: "openrouter"}},
			{Provider: "aff-test", ID: "unknown", Compat: &models.Compat{SessionAffinityFormat: "other"}},
			{Provider: "aff-test", ID: "none"},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("aff-test") })

	opts := func(model, session string) Options {
		return Options{Provider: "aff-test", Model: model, SessionID: session}
	}

	cases := []struct {
		name, model, kind, session string
		want                       map[string]string
	}{
		{
			name: "completions openai", model: "openai", kind: affinityCompletions, session: "sess-1",
			want: map[string]string{
				"session_id":          "sess-1",
				"x-client-request-id": "sess-1",
				"x-session-affinity":  "sess-1",
			},
		},
		{
			name: "completions nosession", model: "nosession", kind: affinityCompletions, session: "sess-1",
			want: map[string]string{
				"x-client-request-id": "sess-1",
				"x-session-affinity":  "sess-1",
			},
		},
		{
			name: "completions openrouter", model: "openrouter", kind: affinityCompletions, session: "sess-1",
			want: map[string]string{"x-session-id": "sess-1"},
		},
		{
			name: "responses openai", model: "openai", kind: affinityResponses, session: "sess-1",
			want: map[string]string{
				"session_id":          "sess-1",
				"x-client-request-id": "sess-1",
			},
		},
		{
			name: "responses nosession", model: "nosession", kind: affinityResponses, session: "sess-1",
			want: map[string]string{"x-client-request-id": "sess-1"},
		},
		{
			name: "responses openrouter", model: "openrouter", kind: affinityResponses, session: "sess-1",
			want: map[string]string{"x-session-id": "sess-1"},
		},
		{name: "no session id", model: "openai", kind: affinityCompletions, session: ""},
		{name: "no format", model: "none", kind: affinityCompletions, session: "sess-1"},
		{name: "unknown format", model: "unknown", kind: affinityCompletions, session: "sess-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionAffinityHeaders(opts(tc.model, tc.session), tc.kind)
			if len(got) != len(tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("got %#v, want %#v", got, tc.want)
				}
			}
			if _, ok := got["session_id"]; ok && tc.want["session_id"] == "" {
				t.Fatalf("unexpected session_id in %#v", got)
			}
		})
	}
}
