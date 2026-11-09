package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/Lowpower/pigo/internal/models"
)

// ClassifyContext is one classifier call: structured state, questions, and
// optional images. Decisions and System One ignore temperature.
// Temperature 0 uses the default of 1. Only llama-cpp-classify applies it.
type ClassifyContext struct {
	State       map[string]any
	Questions   map[string]any
	Images      []ClassifyImage
	Temperature float64
}

// ClassifyImage is one image judged with the state.
type ClassifyImage struct {
	Data string
	MIME string
}

// ClassifyResult is the host-facing classifier response.
// Provider and transport failures set StopReason to "error" and do not panic.
// Usage is kept when the service billed a request whose answers could not be parsed.
type ClassifyResult struct {
	API          string         `json:"api"`
	Provider     string         `json:"provider"`
	Model        string         `json:"model"`
	Answers      map[string]any `json:"answers"`
	Usage        *Usage         `json:"usage,omitempty"`
	StopReason   string         `json:"stopReason"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
}

type classifyQuestion struct {
	name         string
	kind         string
	instructions string
	choices      []classifyChoice
	levels       []string
	boolTrue     string
	boolFalse    string
	criteria     any
}

type classifyChoice struct {
	value       string
	description string
}

// Classify calls a classifier model. api is openai-decisions, typesafe-system-one,
// cloudflare-workers-ai-system-one, or llama-cpp-classify.
func Classify(ctx context.Context, model models.Model, req ClassifyContext, cfg ClientConfig) ClassifyResult {
	out := ClassifyResult{
		API:        model.API,
		Provider:   model.Provider,
		Model:      model.ID,
		Answers:    map[string]any{},
		StopReason: "stop",
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fail := func(err error) ClassifyResult {
		out.Answers = map[string]any{}
		out.StopReason = "error"
		if ctx.Err() != nil {
			out.StopReason = "aborted"
		}
		if err != nil {
			out.ErrorMessage = err.Error()
		}
		return out
	}

	questions, err := parseClassifyQuestions(req.Questions)
	if err != nil {
		return fail(err)
	}
	if err := classifyImagesOK(model, req.Images); err != nil {
		return fail(err)
	}
	if cfg.APIKey == "" && !hasAuthHeader(cfg.Headers) {
		return fail(fmt.Errorf("no API key for %s", model.Provider))
	}
	base := cfg.BaseURL
	if base == "" {
		base = model.BaseURL
	}
	if base == "" {
		return fail(fmt.Errorf("classifier %s/%s has no base URL", model.Provider, model.ID))
	}
	base = expandPlaceholders(base)
	if model.API == "llama-cpp-classify" {
		return classifyLlama(ctx, model, req, questions, cfg, base)
	}

	var endpoint, label string
	var payload any
	switch model.API {
	case "openai-decisions":
		label = "openai decisions"
		endpoint, err = classifierEndpoint(base, "decisions")
		if err != nil {
			return fail(err)
		}
		payload, err = decisionsPayload(model, req, questions)
	case "typesafe-system-one":
		label = "system one"
		if len(req.Images) > 0 {
			return fail(fmt.Errorf("system one does not support image input"))
		}
		endpoint, err = classifierEndpoint(base, "systemone")
		if err != nil {
			return fail(err)
		}
		payload, err = systemOnePayload(model, req, questions)
	case "cloudflare-workers-ai-system-one":
		label = "cloudflare workers ai"
		if len(req.Images) > 0 {
			return fail(fmt.Errorf("system one does not support image input"))
		}
		endpoint, err = classifierEndpoint(base, "run")
		if err != nil {
			return fail(err)
		}
		payload, err = cloudflarePayload(model, req, questions)
	default:
		return fail(fmt.Errorf("unsupported classifier api %q", model.API))
	}
	if err != nil {
		return fail(err)
	}

	body, err := postClassifier(ctx, label, endpoint, payload, cfg)
	if err != nil {
		return fail(err)
	}
	if model.API == "cloudflare-workers-ai-system-one" {
		body, err = unwrapCloudflare(body)
		if err != nil {
			return fail(err)
		}
	}
	if usage := parseClassifierUsage(body["usage"], model.Cost); usage != nil {
		out.Usage = usage
	}
	switch model.API {
	case "openai-decisions":
		out.Answers, err = parseDecisionAnswers(body["answers"], questions)
	default:
		out.Answers, err = parseSystemOneAnswers(body["answers"], questions)
	}
	if err != nil {
		kept := out.Usage
		failed := fail(err)
		failed.Usage = kept
		return failed
	}
	return out
}

func classifyImagesOK(model models.Model, images []ClassifyImage) error {
	if len(images) == 0 {
		return nil
	}
	for _, kind := range model.Input {
		if strings.EqualFold(kind, "image") {
			return nil
		}
	}
	return fmt.Errorf("model %s does not accept images", model.ID)
}

func hasAuthHeader(h map[string]string) bool {
	for k := range h {
		if strings.EqualFold(k, "authorization") {
			return true
		}
	}
	return false
}

func classifierEndpoint(base, leaf string) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(leaf)
	if err != nil {
		return "", err
	}
	return u.ResolveReference(ref).String(), nil
}

func parseClassifyQuestions(raw map[string]any) ([]classifyQuestion, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("classifier questions are required")
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]classifyQuestion, 0, len(names))
	for _, name := range names {
		obj, ok := raw[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("question %s is not an object", name)
		}
		kind, _ := obj["type"].(string)
		instructions, _ := obj["instructions"].(string)
		q := classifyQuestion{name: name, kind: kind, instructions: instructions, criteria: obj["criteria"]}
		switch kind {
		case "choice":
			criteria, _ := obj["criteria"].(map[string]any)
			if len(criteria) == 0 {
				return nil, fmt.Errorf("question %s has no choices", name)
			}
			keys := make([]string, 0, len(criteria))
			for k := range criteria {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				desc, _ := criteria[k].(string)
				q.choices = append(q.choices, classifyChoice{value: k, description: desc})
			}
		case "score":
			switch criteria := obj["criteria"].(type) {
			case []any:
				for _, item := range criteria {
					s, _ := item.(string)
					q.levels = append(q.levels, s)
				}
			case []string:
				q.levels = append(q.levels, criteria...)
			}
			if len(q.levels) == 0 {
				return nil, fmt.Errorf("question %s has no score levels", name)
			}
		case "bool":
			if criteria, ok := obj["criteria"].(map[string]any); ok {
				q.boolTrue, _ = criteria["true"].(string)
				q.boolFalse, _ = criteria["false"].(string)
			}
		default:
			return nil, fmt.Errorf("question %s has unknown type %q", name, kind)
		}
		out = append(out, q)
	}
	return out, nil
}

func decisionsPayload(model models.Model, req ClassifyContext, questions []classifyQuestion) (any, error) {
	input, err := decisionsInput(req)
	if err != nil {
		return nil, err
	}
	wire := make([]any, 0, len(questions))
	for _, q := range questions {
		wire = append(wire, decisionsQuestion(q))
	}
	return map[string]any{"model": model.ID, "input": input, "questions": wire}, nil
}

func decisionsInput(req ClassifyContext) (any, error) {
	state, err := json.Marshal(req.State)
	if err != nil {
		return nil, err
	}
	if len(req.Images) == 0 {
		return string(state), nil
	}
	if len(req.Images) > 128 {
		return nil, fmt.Errorf("openai decisions accepts at most 128 images")
	}
	content := make([]any, 0, 1+len(req.Images))
	content = append(content, map[string]any{"type": "input_text", "text": string(state)})
	for _, image := range req.Images {
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": "data:" + image.MIME + ";base64," + image.Data,
		})
	}
	return []any{map[string]any{"role": "user", "content": content}}, nil
}

func decisionsQuestion(q classifyQuestion) map[string]any {
	switch q.kind {
	case "choice":
		choices := make([]any, 0, len(q.choices))
		for _, c := range q.choices {
			item := map[string]any{"value": c.value}
			if c.description != "" {
				item["description"] = c.description
			}
			choices = append(choices, item)
		}
		return map[string]any{
			"type": "choice", "name": q.name, "instructions": q.instructions, "choices": choices,
		}
	case "score":
		levels := make([]any, 0, len(q.levels))
		for _, label := range q.levels {
			levels = append(levels, map[string]any{"label": label})
		}
		return map[string]any{
			"type": "score", "name": q.name, "instructions": q.instructions, "levels": levels,
		}
	default:
		return map[string]any{
			"type": "predicate", "name": q.name, "instructions": predicateInstructions(q),
		}
	}
}

func predicateInstructions(q classifyQuestion) string {
	var lines []string
	if q.boolTrue != "" {
		lines = append(lines, "True means: "+q.boolTrue)
	}
	if q.boolFalse != "" {
		lines = append(lines, "False means: "+q.boolFalse)
	}
	if len(lines) == 0 {
		return q.instructions
	}
	return q.instructions + "\n\n" + strings.Join(lines, "\n")
}

func cloudflarePayload(model models.Model, req ClassifyContext, questions []classifyQuestion) (any, error) {
	inner, err := systemOnePayload(model, req, questions)
	if err != nil {
		return nil, err
	}
	body, _ := inner.(map[string]any)
	return map[string]any{
		"model": model.ID,
		"input": map[string]any{"state": body["state"], "questions": body["questions"]},
	}, nil
}

func unwrapCloudflare(body map[string]any) (map[string]any, error) {
	if success, ok := body["success"].(bool); ok && !success {
		return nil, fmt.Errorf("cloudflare workers ai error: %s", cloudflareErrorMessage(body["errors"]))
	}
	result, ok := body["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cloudflare workers ai returned an unexpected response")
	}
	if _, ok := result["answers"]; ok {
		return result, nil
	}
	state, _ := result["state"].(string)
	if state != "Completed" {
		if state == "" {
			state = "unknown"
		}
		return nil, fmt.Errorf("cloudflare workers ai run did not complete (state: %s)", state)
	}
	inner, ok := result["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cloudflare workers ai returned an unexpected response")
	}
	return inner, nil
}

func cloudflareErrorMessage(value any) string {
	items, _ := value.([]any)
	var parts []string
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		msg, _ := obj["message"].(string)
		if msg != "" {
			parts = append(parts, msg)
		}
	}
	if len(parts) == 0 {
		return "request failed"
	}
	return strings.Join(parts, "; ")
}

func systemOnePayload(model models.Model, req ClassifyContext, questions []classifyQuestion) (any, error) {
	qs := map[string]any{}
	for _, q := range questions {
		body := map[string]any{"type": q.kind, "instructions": q.instructions}
		if q.kind == "bool" {
			body["type"] = "noul"
		}
		if q.criteria != nil {
			body["criteria"] = q.criteria
		}
		qs[q.name] = body
	}
	state := req.State
	if state == nil {
		state = map[string]any{}
	}
	return map[string]any{"model": model.ID, "state": state, "questions": qs}, nil
}

func postClassifier(ctx context.Context, label, endpoint string, payload any, cfg ClientConfig) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "application/json")
	for k, v := range cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	if cfg.APIKey != "" && httpReq.Header.Get("authorization") == "" {
		httpReq.Header.Set("authorization", "Bearer "+cfg.APIKey)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("%s error (%d): %s", label, resp.StatusCode, msg)
	}
	var decoded map[string]any
	if err := json.Unmarshal(respBody, &decoded); err != nil || decoded == nil {
		return nil, fmt.Errorf("%s returned an unexpected response", label)
	}
	return decoded, nil
}

func parseClassifierUsage(value any, cost *models.Cost) *Usage {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	_, inOK := raw["input_tokens"]
	_, outOK := raw["output_tokens"]
	if !inOK && !outOK {
		return nil
	}
	usage := &Usage{
		Input:  tokenCount(raw["input_tokens"]),
		Output: tokenCount(raw["output_tokens"]),
	}
	usage.TotalTokens = usage.Input + usage.Output
	calculateCost(cost, usage)
	return usage
}

func tokenCount(v any) int {
	switch n := v.(type) {
	case float64:
		if n < 0 {
			return 0
		}
		return int(n)
	case int:
		if n < 0 {
			return 0
		}
		return n
	default:
		return 0
	}
}

func parseDecisionAnswers(value any, questions []classifyQuestion) (map[string]any, error) {
	arr, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("openai decisions returned an unexpected response")
	}
	byName := map[string]map[string]any{}
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := obj["name"].(string)
		if name != "" {
			byName[name] = obj
		}
	}
	out := map[string]any{}
	for _, q := range questions {
		answer, ok := byName[q.name]
		if !ok {
			return nil, fmt.Errorf("openai decisions did not return an answer for %s", q.name)
		}
		parsed, err := parseDecisionAnswer(q, answer)
		if err != nil {
			return nil, err
		}
		out[q.name] = parsed
	}
	return out, nil
}

func parseDecisionAnswer(q classifyQuestion, answer map[string]any) (map[string]any, error) {
	kind, _ := answer["type"].(string)
	if kind == "refusal" {
		return nil, fmt.Errorf("openai decisions refused to answer %s", q.name)
	}
	switch q.kind {
	case "choice":
		choice, _ := answer["choice"].(string)
		if kind != "choice" || choice == "" {
			return nil, fmt.Errorf("openai decisions did not return a choice answer for %s", q.name)
		}
		probs, err := decisionProbabilities(answer["probabilities"], q.name)
		if err != nil {
			return nil, err
		}
		confidence, err := requiredNumber(answer["confidence"], "confidence for "+q.name)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "choice", "choice": choice, "probabilities": probs, "confidence": confidence}, nil
	case "score":
		if kind != "score" {
			return nil, fmt.Errorf("openai decisions did not return a score answer for %s", q.name)
		}
		score, err := requiredNumber(answer["score"], "score for "+q.name)
		if err != nil {
			return nil, err
		}
		confidence, err := requiredNumber(answer["confidence"], "confidence for "+q.name)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "score", "score": score, "confidence": confidence}, nil
	default:
		if kind != "predicate" {
			return nil, fmt.Errorf("openai decisions did not return a predicate answer for %s", q.name)
		}
		probability, err := requiredNumber(answer["probability"], "probability for "+q.name)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "bool", "probability": probability}, nil
	}
}

func decisionProbabilities(value any, id string) (map[string]any, error) {
	arr, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("openai decisions returned invalid probabilities for %s", id)
	}
	out := map[string]any{}
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("openai decisions returned invalid probabilities for %s", id)
		}
		key, _ := obj["value"].(string)
		if key == "" {
			return nil, fmt.Errorf("openai decisions returned invalid probabilities for %s", id)
		}
		n, err := requiredNumber(obj["probability"], "probability for "+id+"."+key)
		if err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, nil
}

func parseSystemOneAnswers(value any, questions []classifyQuestion) (map[string]any, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("system one returned an unexpected response")
	}
	out := map[string]any{}
	for _, q := range questions {
		obj, ok := raw[q.name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("system one did not return an answer for %s", q.name)
		}
		kind, _ := obj["type"].(string)
		switch q.kind {
		case "choice":
			choice, _ := obj["choice"].(string)
			if kind != "choice" || choice == "" {
				return nil, fmt.Errorf("system one did not return a choice answer for %s", q.name)
			}
			probs, ok := obj["probabilities"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("system one returned invalid probabilities for %s", q.name)
			}
			confidence, err := requiredNumber(obj["confidence"], "confidence for "+q.name)
			if err != nil {
				return nil, err
			}
			out[q.name] = map[string]any{"type": "choice", "choice": choice, "probabilities": probs, "confidence": confidence}
		case "score":
			if kind != "score" {
				return nil, fmt.Errorf("system one did not return a score answer for %s", q.name)
			}
			score, err := requiredNumber(obj["score"], "score for "+q.name)
			if err != nil {
				return nil, err
			}
			confidence, err := requiredNumber(obj["confidence"], "confidence for "+q.name)
			if err != nil {
				return nil, err
			}
			out[q.name] = map[string]any{"type": "score", "score": score, "confidence": confidence}
		default:
			if kind != "noul" {
				return nil, fmt.Errorf("system one did not return a bool answer for %s", q.name)
			}
			probability, err := requiredNumber(obj["noul"], "probability for "+q.name)
			if err != nil {
				return nil, err
			}
			out[q.name] = map[string]any{"type": "bool", "probability": probability}
		}
	}
	return out, nil
}

func requiredNumber(v any, what string) (float64, error) {
	n, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("missing %s", what)
	}
	return n, nil
}
