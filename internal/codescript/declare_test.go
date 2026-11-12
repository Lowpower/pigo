package codescript

import (
	"strings"
	"testing"
)

func TestDeclarationTagsAndMCPResult(t *testing.T) {
	text := Declaration(Tool{
		Name:      "mcp__dev-radius__search",
		Namespace: "dev-radius",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{
					"type":    "integer",
					"minimum": 1,
					"maximum": 20,
					"pattern": "^[0-9]+$",
					"default": 5,
				},
			},
			"required": []any{"limit"},
		},
	})
	for _, want := range []string{"@integer", "@minimum 1", "@maximum 20", "@pattern ^[0-9]+$", "@default 5", "structuredContent: unknown", "isError: boolean", "content: unknown"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "CallToolResult") {
		t.Fatalf("declaration names CallToolResult:\n%s", text)
	}
	if strings.Contains(text, "fails") {
		t.Fatal("mcp tool without an output schema should not claim failure still resolves")
	}
}

func TestDeclarationSchemaResolvesOnFailure(t *testing.T) {
	text := Declaration(Tool{
		Name:         "bash",
		OutputSchema: map[string]any{"type": "object"},
	})
	if !strings.Contains(text, "including when the call fails") {
		t.Fatal(text)
	}
	if strings.Contains(text, "CallToolResult") || strings.Contains(strings.ToLower(text), "always reject") {
		t.Fatal(text)
	}
}

func TestSuggestCase(t *testing.T) {
	msg := suggest("Bash", []Tool{{Name: "bash"}, {Name: "read"}})
	if !strings.Contains(msg, "tools.bash") || !strings.Contains(msg, "tools.Bash") {
		t.Fatal(msg)
	}
}

func TestCallableFiltersExposure(t *testing.T) {
	env := Env{
		ActiveSet: true,
		Active:    []string{"bash"},
		Tools: []Tool{
			{Name: "bash", Exposure: "direct"},
			{Name: "grep", Exposure: "direct"},
			{Name: "secret", Exposure: "hidden"},
			{Name: "onlyModel", Exposure: "model-only"},
			{Name: "later", Exposure: "deferred"},
			{Name: "code", Exposure: "codemode"},
			{Name: "codemode", Exposure: "direct"},
			{Name: "mcp__dev-radius__search", Namespace: "dev-radius", Exposure: "codemode"},
		},
	}
	var names []string
	for _, t := range callable(env) {
		names = append(names, t.Name)
	}
	got := strings.Join(names, ",")
	want := "bash,later,code,mcp__dev-radius__search"
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestNamespaceDashAndAlias(t *testing.T) {
	groups := collectNamespaces(Env{Namespaces: []Namespace{{
		Name:    "dev-radius",
		Aliases: []string{"radius"},
	}}}, []Tool{{Name: "search", Namespace: "dev-radius"}})
	for _, q := range []string{"mcp__dev-radius", "dev_radius", "radius", "MCP__Dev-Radius"} {
		if findNamespace(groups, q) == nil {
			t.Fatalf("missing %s", q)
		}
	}
	if findNamespace(groups, "other") != nil {
		t.Fatal("matched an unknown namespace")
	}
}

func TestToolDescriptionStaysSpecific(t *testing.T) {
	if !strings.Contains(ToolDescription, "await searchTools()") ||
		!strings.Contains(ToolDescription, "await describeTool()") ||
		!strings.Contains(ToolDescription, "await describeNamespace()") {
		t.Fatal(ToolDescription)
	}
	if strings.Contains(strings.ToLower(ToolDescription), "always reject") || strings.Contains(ToolDescription, "pi.dev") {
		t.Fatal(ToolDescription)
	}
}
