package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

const responsesToolFixture = `event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":""}}

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_tool","status":"completed","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}

`

func registerMidConvo(t *testing.T, compat *models.Compat) {
	t.Helper()
	models.RegisterProvider(models.ProviderSpec{
		ID: "mid-effort", DefaultAPI: "openai-responses", DefaultID: "gpt-6-luna",
		Models: []models.Model{{
			Provider: "mid-effort", ID: "gpt-6-luna", API: "openai-responses",
			Reasoning: boolPtrAI(true),
			ThinkingLevelMap: map[string]*string{
				"off":     strPtrAI("none"),
				"minimal": nil,
				"low":     strPtrAI("low"),
				"medium":  strPtrAI("medium"),
				"high":    strPtrAI("high"),
				"xhigh":   strPtrAI("xhigh"),
				"max":     strPtrAI("max"),
			},
			Compat: compat,
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("mid-effort") })
}

type capturedResponses struct {
	Input     json.RawMessage `json:"input"`
	Reasoning struct {
		Effort string `json:"effort"`
	} `json:"reasoning"`
	Include []string `json:"include"`
}

func captureResponses(t *testing.T, body string) (*httptest.Server, *[]capturedResponses) {
	t.Helper()
	got := &[]capturedResponses{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload capturedResponses
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("body: %v", err)
		}
		*got = append(*got, payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	return srv, got
}

func runResponsesTurn(t *testing.T, srv *httptest.Server, baseURL, apiKey, provider, model, thinking string, messages []Message) *AssistantMessage {
	t.Helper()
	if baseURL == "" {
		baseURL = srv.URL
	}
	httpClient := srv.Client()
	if baseURL != srv.URL {
		httpClient = rewriteResponsesClient(t, srv)
	}
	client := &OpenAIResponsesClient{BaseURL: baseURL, APIKey: apiKey, HTTPClient: httpClient}
	stream, err := client.StreamFn()(context.Background(), Context{Messages: messages}, Options{
		Provider: provider, Model: model, Thinking: thinking,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason == StopError {
		t.Fatalf("stop = %v err = %s", final, errorText(final))
	}
	return final
}

func errorText(msg *AssistantMessage) string {
	if msg == nil {
		return ""
	}
	return msg.ErrorMessage
}

func rewriteResponsesClient(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	dest, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := srv.Client()
	client.Transport = rewriteTransport{base: client.Transport, dest: dest}
	return client
}

type rewriteTransport struct {
	base http.RoundTripper
	dest *url.URL
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.dest.Scheme
	clone.URL.Host = t.dest.Host
	clone.Host = t.dest.Host
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func decodeInput(t *testing.T, raw json.RawMessage) []json.RawMessage {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("input: %v", err)
	}
	return items
}

func assertInputPrefix(t *testing.T, later, earlier json.RawMessage) {
	t.Helper()
	got := decodeInput(t, later)
	want := decodeInput(t, earlier)
	if len(got) < len(want) {
		t.Fatalf("input shorter than prefix: %d < %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != string(want[i]) {
			t.Fatalf("input[%d]\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

func configurationEfforts(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var efforts []string
	for _, item := range decodeInput(t, raw) {
		var decoded struct {
			Type      string `json:"type"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		if err := json.Unmarshal(item, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Type != "configuration_update" {
			continue
		}
		efforts = append(efforts, decoded.Reasoning.Effort)
	}
	return efforts
}

func inputKinds(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var kinds []string
	for _, item := range decodeInput(t, raw) {
		var decoded struct {
			Type string `json:"type"`
			Role string `json:"role"`
		}
		if err := json.Unmarshal(item, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Type != "" {
			kinds = append(kinds, decoded.Type)
			continue
		}
		kinds = append(kinds, decoded.Role)
	}
	return kinds
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestOpenAIResponsesConfigurationUpdateKeepsEffort(t *testing.T) {
	registerMidConvo(t, &models.Compat{SupportsMidConvoEffort: true})
	srv, got := captureResponses(t, responsesFixture)
	defer srv.Close()

	history := []Message{{Role: RoleUser, Content: "one"}}
	first := runResponsesTurn(t, srv, "", "sk-test", "mid-effort", "gpt-6-luna", "high", history)
	history = append(history, Message{Assistant: first}, Message{Role: RoleUser, Content: "two"})
	second := runResponsesTurn(t, srv, "", "sk-test", "mid-effort", "gpt-6-luna", "high", history)
	history = append(history, Message{Assistant: second}, Message{Role: RoleUser, Content: "three"})
	third := runResponsesTurn(t, srv, "", "sk-test", "mid-effort", "gpt-6-luna", "low", history)

	if len(*got) != 3 {
		t.Fatalf("requests = %d", len(*got))
	}
	for i, payload := range *got {
		if payload.Reasoning.Effort != "medium" {
			t.Fatalf("request %d effort = %q", i, payload.Reasoning.Effort)
		}
		if len(payload.Include) == 0 || payload.Include[0] != "reasoning.encrypted_content" {
			t.Fatalf("request %d include = %#v", i, payload.Include)
		}
	}
	assertInputPrefix(t, (*got)[1].Input, (*got)[0].Input)
	assertInputPrefix(t, (*got)[2].Input, (*got)[1].Input)
	if efforts := configurationEfforts(t, (*got)[2].Input); !equalStrings(efforts, []string{"high", "high", "low"}) {
		t.Fatalf("efforts = %#v", efforts)
	}
	kinds := inputKinds(t, (*got)[2].Input)
	if len(kinds) == 0 || kinds[len(kinds)-1] != "configuration_update" {
		t.Fatalf("kinds = %#v", kinds)
	}
	if first.ProviderThinkingLevel != "high" || second.ProviderThinkingLevel != "high" || third.ProviderThinkingLevel != "low" {
		t.Fatalf("levels = %q %q %q", first.ProviderThinkingLevel, second.ProviderThinkingLevel, third.ProviderThinkingLevel)
	}
}

func TestOpenAIResponsesConfigurationUpdateAfterTool(t *testing.T) {
	registerMidConvo(t, &models.Compat{SupportsMidConvoEffort: true})
	srv, got := captureResponses(t, responsesToolFixture)
	defer srv.Close()

	history := []Message{{Role: RoleUser, Content: "one"}}
	toolCall := runResponsesTurn(t, srv, "", "sk-test", "mid-effort", "gpt-6-luna", "high", history)
	if len(toolCall.ToolCalls()) != 1 {
		t.Fatalf("tool calls = %#v", toolCall.Content)
	}
	history = append(history, Message{Assistant: toolCall}, Message{
		Role: RoleToolResult, ToolCallID: "call_1", ToolName: "lookup", Content: "result",
	})
	after := runResponsesTurn(t, srv, "", "sk-test", "mid-effort", "gpt-6-luna", "low", history)
	if len(*got) != 2 {
		t.Fatalf("requests = %d", len(*got))
	}
	assertInputPrefix(t, (*got)[1].Input, (*got)[0].Input)
	kinds := inputKinds(t, (*got)[1].Input)
	if len(kinds) < 2 || kinds[len(kinds)-2] != "function_call_output" || kinds[len(kinds)-1] != "configuration_update" {
		t.Fatalf("kinds = %#v", kinds)
	}
	efforts := configurationEfforts(t, (*got)[1].Input)
	if !equalStrings(efforts, []string{"high", "low"}) {
		t.Fatalf("efforts = %#v", efforts)
	}
	if after.ProviderThinkingLevel != "low" {
		t.Fatalf("level = %q", after.ProviderThinkingLevel)
	}
}

func TestOpenAIResponsesConfigurationUpdateSameModelOnly(t *testing.T) {
	registerMidConvo(t, &models.Compat{SupportsMidConvoEffort: true})
	srv, got := captureResponses(t, responsesFixture)
	defer srv.Close()

	unstamped := &AssistantMessage{
		Role: RoleAssistant, Provider: "mid-effort", API: "openai-responses", Model: "gpt-6-luna",
		Content: []*Content{{Type: KindText, Text: "plain"}},
	}
	otherProvider := &AssistantMessage{
		Role: RoleAssistant, Provider: "opencode", API: "openai-responses", Model: "gpt-6-luna",
		ProviderThinkingLevel: "high",
		Content:               []*Content{{Type: KindText, Text: "other provider"}},
	}
	otherModel := &AssistantMessage{
		Role: RoleAssistant, Provider: "mid-effort", API: "openai-responses", Model: "gpt-6-sol",
		ProviderThinkingLevel: "high",
		Content:               []*Content{{Type: KindText, Text: "other model"}},
	}
	recorded := &AssistantMessage{
		Role: RoleAssistant, Provider: "mid-effort", API: "openai-responses", Model: "gpt-6-luna",
		ProviderThinkingLevel: "high",
		Content:               []*Content{{Type: KindText, Text: "recorded"}},
	}
	runResponsesTurn(t, srv, "", "sk-test", "mid-effort", "gpt-6-luna", "low", []Message{
		{Role: RoleUser, Content: "one"},
		{Assistant: unstamped},
		{Role: RoleUser, Content: "two"},
		{Assistant: otherProvider},
		{Role: RoleUser, Content: "three"},
		{Assistant: otherModel},
		{Role: RoleUser, Content: "four"},
		{Assistant: recorded},
		{Role: RoleUser, Content: "five"},
	})
	if len(*got) != 1 {
		t.Fatalf("requests = %d", len(*got))
	}
	if kinds := inputKinds(t, (*got)[0].Input); !equalStrings(kinds, []string{
		"user", "assistant", "user", "assistant", "user", "assistant", "user",
		"configuration_update", "assistant", "user", "configuration_update",
	}) {
		t.Fatalf("kinds = %#v", kinds)
	}
	if efforts := configurationEfforts(t, (*got)[0].Input); !equalStrings(efforts, []string{"high", "low"}) {
		t.Fatalf("efforts = %#v", efforts)
	}
}

func TestOpenAIResponsesConfigurationUpdateDisabled(t *testing.T) {
	tests := []struct {
		name     string
		compat   *models.Compat
		apiKey   string
		baseURL  string
		provider string
		model    string
		level    string
		effort   string
	}{
		{
			name: "compat off", compat: &models.Compat{SupportsMidConvoEffort: false},
			apiKey: "sk-test", provider: "mid-effort", model: "gpt-6-luna", level: "low", effort: "low",
		},
		{
			name: "thinking off", compat: &models.Compat{SupportsMidConvoEffort: true},
			apiKey: "sk-test", provider: "mid-effort", model: "gpt-6-luna", level: "off", effort: "none",
		},
		{
			name: "sign in with chatgpt", compat: &models.Compat{SupportsMidConvoEffort: true},
			apiKey: "chatgpt-access-token", baseURL: "https://api.openai.com/v1",
			provider: "openai", model: "gpt-6-luna", level: "low", effort: "low",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.provider == "mid-effort" {
				registerMidConvo(t, tc.compat)
			}
			srv, got := captureResponses(t, responsesFixture)
			defer srv.Close()
			first := runResponsesTurn(t, srv, tc.baseURL, tc.apiKey, tc.provider, tc.model, "high", []Message{
				{Role: RoleUser, Content: "one"},
			})
			second := runResponsesTurn(t, srv, tc.baseURL, tc.apiKey, tc.provider, tc.model, tc.level, []Message{
				{Role: RoleUser, Content: "one"},
				{Assistant: first},
				{Role: RoleUser, Content: "two"},
			})
			if len(*got) != 2 {
				t.Fatalf("requests = %d", len(*got))
			}
			payload := (*got)[1]
			if payload.Reasoning.Effort != tc.effort {
				t.Fatalf("effort = %q", payload.Reasoning.Effort)
			}
			if efforts := configurationEfforts(t, payload.Input); len(efforts) != 0 {
				t.Fatalf("configuration_update = %#v", efforts)
			}
			wantFirst := ""
			if tc.level == "off" {
				wantFirst = "high"
			}
			if first.ProviderThinkingLevel != wantFirst || second.ProviderThinkingLevel != "" {
				t.Fatalf("levels = %q %q", first.ProviderThinkingLevel, second.ProviderThinkingLevel)
			}
		})
	}
}

func TestOpenAIResponsesConfigurationUpdateMinimalAndAPIKey(t *testing.T) {
	registerMidConvo(t, &models.Compat{SupportsMidConvoEffort: true})
	srv, got := captureResponses(t, responsesFixture)
	defer srv.Close()

	minimal := runResponsesTurn(t, srv, "", "sk-test", "mid-effort", "gpt-6-luna", "minimal", []Message{
		{Role: RoleUser, Content: "one"},
	})
	if (*got)[0].Reasoning.Effort != "medium" {
		t.Fatalf("minimal top-level = %q", (*got)[0].Reasoning.Effort)
	}
	if efforts := configurationEfforts(t, (*got)[0].Input); !equalStrings(efforts, []string{"minimal"}) {
		t.Fatalf("minimal efforts = %#v", efforts)
	}
	if minimal.ProviderThinkingLevel != "minimal" {
		t.Fatalf("level = %q", minimal.ProviderThinkingLevel)
	}

	official, officialGot := captureResponses(t, responsesFixture)
	defer official.Close()
	keyed := runResponsesTurn(t, official, "https://api.openai.com/v1", "sk-test", "openai", "gpt-6-luna", "high", []Message{
		{Role: RoleUser, Content: "one"},
	})
	if (*officialGot)[0].Reasoning.Effort != "medium" {
		t.Fatalf("sk- effort = %q", (*officialGot)[0].Reasoning.Effort)
	}
	if efforts := configurationEfforts(t, (*officialGot)[0].Input); !equalStrings(efforts, []string{"high"}) {
		t.Fatalf("sk- efforts = %#v", efforts)
	}
	if keyed.ProviderThinkingLevel != "high" {
		t.Fatalf("sk- level = %q", keyed.ProviderThinkingLevel)
	}
}

func TestBuildResponsesInputSkipsEmptyAssistantUpdate(t *testing.T) {
	items := buildResponsesInput(Context{Messages: []Message{
		{Role: RoleUser, Content: "one"},
		{Assistant: &AssistantMessage{
			Provider: "mid-effort", API: "openai-responses", Model: "gpt-6-luna",
			ProviderThinkingLevel: "high",
			Content:               []*Content{{Type: KindText, Text: "   "}},
		}},
	}}, replayTarget{Provider: "mid-effort", API: "openai-responses", Model: "gpt-6-luna"}, deferredAnchor{}, "low")
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"effort":"high"`) {
		t.Fatalf("empty assistant inserted an update: %s", raw)
	}
	if !strings.Contains(string(raw), `"effort":"low"`) {
		t.Fatalf("trailing update missing: %s", raw)
	}
}
