package codescript

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/dop251/goja/parser"

	"github.com/Lowpower/pigo/internal/codemode"
)

// ToolDescription is the short model-facing summary. The long form lives in
// docs/codemode.md. Search helpers are named with await so a script does not
// stringify the promise.
const ToolDescription = "Run one JavaScript script (top-level await and return). " +
	"Call a tool with await tools.<name>(args). " +
	"await searchTools(), await describeTool(), and await describeNamespace() find tools that are not listed on this tool. " +
	"text(), image(), console, store(), and load() keep output and small JSON values. " +
	"models.getAvailableOfType(type) lists usable models. " +
	"models.classify and models.generateImages resolve with stopReason \"error\" and errorMessage \"not implemented\". " +
	"A tool that declares an output schema resolves with its value on failure too."

// Env is the session data one script runs against.
type Env struct {
	Tools      []Tool
	Namespaces []Namespace
	// Active is the session tool set. ActiveSet false means every direct tool
	// in Tools is in the session.
	Active    []string
	ActiveSet bool
	Exec      func(ctx context.Context, name string, args map[string]any) (value string, isError bool)
	Models    func(typ, provider string) ([]ModelInfo, error)
	Store     *Store
}

// Result is what the script produced. Value is the return value.
type Result struct {
	Value   string
	Texts   []string
	Images  []json.RawMessage
	Console []string
}

// Render joins the return value with collected text, console lines, and images.
func (r Result) Render() string {
	var parts []string
	if r.Value != "" {
		parts = append(parts, r.Value)
	}
	parts = append(parts, r.Texts...)
	parts = append(parts, r.Console...)
	for _, img := range r.Images {
		parts = append(parts, string(img))
	}
	return strings.Join(parts, "\n")
}

// Run executes source with the script globals installed.
func Run(ctx context.Context, source string, env Env) (Result, error) {
	code, opt, err := codemode.Parse(source)
	if err != nil {
		return Result{}, err
	}
	state := &runner{env: env, visible: callable(env)}
	state.index()
	program := "(async () => {\n" + state.prelude() + "\nreturn await " + wrapUser(code) + ";\n})()"
	got, err := codemode.Run(ctx, withOptions(program, opt), map[string]codemode.HostFunc{
		"bridge": state.bridge,
	})
	state.mu.Lock()
	res := state.result
	state.mu.Unlock()
	res.Value = got
	if err != nil {
		return res, err
	}
	return res, nil
}

func withOptions(code string, opt codemode.Options) string {
	if opt.TimeoutMS == nil && opt.MaxOutputTokens == nil {
		return code
	}
	fields := map[string]int{}
	if opt.TimeoutMS != nil {
		fields["timeout_ms"] = *opt.TimeoutMS
	}
	if opt.MaxOutputTokens != nil {
		fields["max_output_tokens"] = *opt.MaxOutputTokens
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return code
	}
	return "// @options: " + string(raw) + "\n" + code
}

func wrapUser(code string) string {
	expr := "(async () => {\nreturn (\n" + code + "\n);\n})()"
	if _, err := parser.ParseFile(nil, "", expr, 0); err == nil {
		return expr
	}
	return "(async () => {\n" + code + "\n})()"
}

type runner struct {
	mu      sync.Mutex
	env     Env
	visible []Tool
	byName  map[string]Tool
	byJS    map[string]Tool
	groups  []nsGroup
	result  Result
}

func (r *runner) index() {
	r.byName = map[string]Tool{}
	r.byJS = map[string]Tool{}
	for _, t := range r.visible {
		r.byName[t.Name] = t
		r.byJS[jsName(t.Name)] = t
		for _, alias := range t.Aliases {
			if _, ok := r.byName[alias]; !ok {
				r.byName[alias] = t
			}
		}
	}
	r.groups = collectNamespaces(r.env, r.visible)
}

func (r *runner) prelude() string {
	var b strings.Builder
	b.WriteString("async function __pigo(op, payload) {\n")
	b.WriteString("  const raw = await host.bridge(op, JSON.stringify(payload === undefined ? null : payload));\n")
	b.WriteString("  return JSON.parse(raw);\n")
	b.WriteString("}\n")
	b.WriteString("const __tools = {\n")
	for _, t := range r.visible {
		name, _ := json.Marshal(t.Name)
		b.WriteString("  ")
		b.WriteString(jsKey(jsName(t.Name)))
		b.WriteString(": (args) => __pigo(\"tool\", {name: ")
		b.Write(name)
		b.WriteString(", args: args == null ? {} : args}),\n")
	}
	b.WriteString("};\n")
	b.WriteString("Object.freeze(__tools);\n")
	b.WriteString("const tools = new Proxy(__tools, {\n")
	b.WriteString("  get(target, prop) {\n")
	b.WriteString("    if (typeof prop !== \"string\" && typeof prop !== \"number\") return target[prop];\n")
	b.WriteString("    const key = String(prop);\n")
	b.WriteString("    if (Object.prototype.hasOwnProperty.call(target, key)) return target[key];\n")
	b.WriteString("    return () => __pigo(\"missing\", {name: key});\n")
	b.WriteString("  }\n")
	b.WriteString("});\n")
	b.WriteString("async function text(value) { return __pigo(\"text\", {value}); }\n")
	b.WriteString("async function image(value) { return __pigo(\"image\", {value}); }\n")
	b.WriteString("async function store(key, value) { return __pigo(\"store\", {key, value}); }\n")
	b.WriteString("async function load(key) { return __pigo(\"load\", {key}); }\n")
	b.WriteString("async function searchTools(query, opts) {\n")
	b.WriteString("  const o = opts || {};\n")
	b.WriteString("  return __pigo(\"searchTools\", {query, limit: o.limit, namespace: o.namespace});\n")
	b.WriteString("}\n")
	b.WriteString("async function describeTool(name) { return __pigo(\"describeTool\", {name}); }\n")
	b.WriteString("async function describeNamespace(name) { return __pigo(\"describeNamespace\", {name}); }\n")
	b.WriteString("function __console(level) { return (...args) => __pigo(\"console\", {level, args}); }\n")
	b.WriteString("const console = Object.freeze({ log: __console(\"log\"), info: __console(\"info\"), warn: __console(\"warn\"), error: __console(\"error\"), debug: __console(\"debug\") });\n")
	b.WriteString("const models = Object.freeze({\n")
	b.WriteString("  getAvailableOfType: (type, provider) => __pigo(\"models\", {method: \"getAvailableOfType\", type, provider}),\n")
	b.WriteString("  classify: (model, context) => __pigo(\"models\", {method: \"classify\", model, context}),\n")
	b.WriteString("  generateImages: (model, context) => __pigo(\"models\", {method: \"generateImages\", model, context})\n")
	b.WriteString("});\n")
	return b.String()
}

func jsKey(name string) string {
	raw, _ := json.Marshal(name)
	return string(raw)
}

func (r *runner) bridge(ctx context.Context, args []any) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("missing script operation")
	}
	op, _ := args[0].(string)
	body := map[string]any{}
	if len(args) > 1 && args[1] != nil {
		raw, _ := args[1].(string)
		if raw != "" && raw != "null" {
			if err := json.Unmarshal([]byte(raw), &body); err != nil {
				return "", err
			}
		}
	}
	switch op {
	case "tool":
		return r.callTool(ctx, body)
	case "missing":
		name, _ := body["name"].(string)
		return "", fmt.Errorf("%s", suggest(name, r.visible))
	case "text":
		r.mu.Lock()
		r.result.Texts = append(r.result.Texts, display(body["value"]))
		r.mu.Unlock()
		return "null", nil
	case "image":
		raw, err := json.Marshal(body["value"])
		if err != nil {
			return "", err
		}
		r.mu.Lock()
		r.result.Images = append(r.result.Images, raw)
		r.mu.Unlock()
		return "null", nil
	case "console":
		r.mu.Lock()
		r.result.Console = append(r.result.Console, consoleLine(body))
		r.mu.Unlock()
		return "null", nil
	case "store":
		return r.store(body)
	case "load":
		return r.load(body)
	case "searchTools":
		return okJSON(r.search(body))
	case "describeTool":
		return r.describeTool(body)
	case "describeNamespace":
		return r.describeNamespace(body)
	case "models":
		return r.models(body)
	default:
		return "", fmt.Errorf("unknown script operation %q", op)
	}
}

func (r *runner) callTool(ctx context.Context, body map[string]any) (string, error) {
	name, _ := body["name"].(string)
	tool, ok := r.lookup(name)
	if !ok {
		return "", fmt.Errorf("%s", suggest(name, r.visible))
	}
	args, _ := body["args"].(map[string]any)
	if args == nil {
		args = map[string]any{}
	}
	if r.env.Exec == nil {
		return "", fmt.Errorf("tool executor is not configured")
	}
	val, isErr := r.env.Exec(ctx, tool.Name, args)
	if isErr && tool.OutputSchema == nil {
		if val == "" {
			val = "tool failed"
		}
		return "", fmt.Errorf("%s", val)
	}
	return okJSON(val)
}

func (r *runner) lookup(name string) (Tool, bool) {
	if t, ok := r.byName[name]; ok {
		return t, true
	}
	if t, ok := r.byJS[name]; ok {
		return t, true
	}
	if t, ok := r.byJS[jsName(name)]; ok {
		return t, true
	}
	return Tool{}, false
}

func (r *runner) store(body map[string]any) (string, error) {
	key, _ := body["key"].(string)
	if key == "" {
		return "", fmt.Errorf("store key must be a non-empty string")
	}
	if r.env.Store == nil {
		return "", fmt.Errorf("store is not available")
	}
	raw, err := json.Marshal(body["value"])
	if err != nil {
		return "", err
	}
	r.env.Store.Set(key, raw)
	return "null", nil
}

func (r *runner) load(body map[string]any) (string, error) {
	key, _ := body["key"].(string)
	raw, ok := r.env.Store.Get(key)
	if !ok || len(raw) == 0 {
		return "null", nil
	}
	return string(raw), nil
}

type listedTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (r *runner) search(body map[string]any) []listedTool {
	limit := 8
	if n, ok := numText(body["limit"]); ok {
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	query := ""
	if body["query"] != nil {
		query = strings.ToLower(strings.TrimSpace(display(body["query"])))
	}
	nsQuery, _ := body["namespace"].(string)
	var groups []nsGroup
	if nsQuery != "" {
		if g := findNamespace(r.groups, nsQuery); g != nil {
			groups = []nsGroup{*g}
		}
	}
	allowed := map[string]bool{}
	if nsQuery != "" {
		for _, g := range groups {
			for _, t := range g.tools {
				allowed[t.Name] = true
			}
		}
	}
	type hit struct {
		tool  Tool
		score int
		idx   int
	}
	var hits []hit
	for i, t := range r.visible {
		if nsQuery != "" && !allowed[t.Name] {
			continue
		}
		name := strings.ToLower(t.Name)
		js := strings.ToLower(jsName(t.Name))
		desc := strings.ToLower(t.Description)
		score := 3
		switch {
		case query == "":
		case name == query || js == query:
			score = 0
		case strings.Contains(name, query) || strings.Contains(js, query):
			score = 1
		case strings.Contains(desc, query):
			score = 2
		default:
			continue
		}
		hits = append(hits, hit{tool: t, score: score, idx: i})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score < hits[j].score
		}
		return hits[i].idx < hits[j].idx
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]listedTool, 0, len(hits))
	for _, h := range hits {
		out = append(out, listedTool{Name: h.tool.Name, Description: h.tool.Description})
	}
	return out
}

type described struct {
	Description string `json:"description"`
	Declaration string `json:"declaration"`
}

func (r *runner) describeTool(body map[string]any) (string, error) {
	name, _ := body["name"].(string)
	tool, ok := r.lookup(name)
	if !ok {
		fold := strings.ToLower(name)
		for _, t := range r.visible {
			if strings.EqualFold(t.Name, name) || strings.EqualFold(jsName(t.Name), fold) {
				tool, ok = t, true
				break
			}
			for _, alias := range t.Aliases {
				if strings.EqualFold(alias, name) {
					tool, ok = t, true
					break
				}
			}
			if ok {
				break
			}
		}
	}
	if !ok {
		return "null", nil
	}
	return okJSON(described{Description: tool.Description, Declaration: Declaration(tool)})
}

type nsView struct {
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	Instructions string       `json:"instructions,omitempty"`
	Tools        []listedTool `json:"tools"`
}

func (r *runner) describeNamespace(body map[string]any) (string, error) {
	name, _ := body["name"].(string)
	g := findNamespace(r.groups, name)
	if g == nil {
		return "null", nil
	}
	view := nsView{Name: g.Name, Description: g.Description, Instructions: g.Instructions, Tools: []listedTool{}}
	for _, t := range g.tools {
		view.Tools = append(view.Tools, listedTool{Name: t.Name, Description: t.Description})
	}
	return okJSON(view)
}

func (r *runner) models(body map[string]any) (string, error) {
	method, _ := body["method"].(string)
	switch method {
	case "classify", "generateImages":
		return okJSON(map[string]string{
			"stopReason":   "error",
			"errorMessage": "not implemented",
		})
	case "getAvailableOfType":
		typ, _ := body["type"].(string)
		provider, _ := body["provider"].(string)
		if r.env.Models == nil {
			switch typ {
			case "chat", "classifier", "image":
				return okJSON([]ModelInfo{})
			default:
				return "", fmt.Errorf("unknown model type %q", typ)
			}
		}
		list, err := r.env.Models(typ, provider)
		if err != nil {
			return "", err
		}
		if list == nil {
			list = []ModelInfo{}
		}
		return okJSON(list)
	default:
		return "", fmt.Errorf("unknown models method %q", method)
	}
}

func consoleLine(body map[string]any) string {
	level, _ := body["level"].(string)
	if level == "" {
		level = "log"
	}
	var parts []string
	switch args := body["args"].(type) {
	case []any:
		for _, arg := range args {
			parts = append(parts, display(arg))
		}
	}
	return level + " " + strings.Join(parts, " ")
}

func display(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

func okJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
