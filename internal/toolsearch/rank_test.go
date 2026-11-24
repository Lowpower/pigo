package toolsearch

import (
	"fmt"
	"testing"
)

func TestTokenizeMatchesPi(t *testing.T) {
	if got := fmt.Sprint(Tokenize("listIssues for the GitHub_repo")); got != "[list issue git hub repo]" {
		t.Fatalf("issues = %s", got)
	}
	if got := fmt.Sprint(Tokenize("searches queries HTTPServer")); got != "[search query http server]" {
		t.Fatalf("server = %s", got)
	}
}

func sampleDocs() []Doc {
	tools := []Meta{
		{
			Name:        "mcp__github__list_issues",
			Description: "List issues in a repository.",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"state": map[string]any{"type": "string", "description": "open or closed"},
				},
			},
		},
		{Name: "mcp__github__create_pull_request", Description: "Open a pull request."},
		{Name: "mcp__linear__search_issues", Description: "Search Linear issues by text."},
		{Name: "mcp__docs__search", Description: "Search the documentation."},
	}
	docs := make([]Doc, len(tools))
	for i, tool := range tools {
		docs[i] = Document(tool)
	}
	return docs
}

func names(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, hit := range hits {
		out[i] = hit.Name
	}
	return out
}

func TestRankMatchesPiExamples(t *testing.T) {
	docs := sampleDocs()
	if got := fmt.Sprint(names(Rank("issue", docs, 8))); got != "[mcp__linear__search_issues mcp__github__list_issues]" {
		t.Fatalf("issue = %s", got)
	}
	pull := Rank("pull requests", docs, 8)
	if len(pull) == 0 || pull[0].Name != "mcp__github__create_pull_request" {
		t.Fatalf("pull = %v", names(pull))
	}
	if got := Rank("search", docs, 1); len(got) != 1 {
		t.Fatalf("limit = %v", names(got))
	}
	if got := fmt.Sprint(names(Rank("closed", docs, 8))); got != "[mcp__github__list_issues]" {
		t.Fatalf("closed = %s", got)
	}
	if Rank("kubernetes", docs, 8) != nil || Rank("the", docs, 8) != nil || Rank("tickets", docs, 8) != nil {
		t.Fatal("empty queries returned hits")
	}
}

func TestRankIncludesNamespace(t *testing.T) {
	doc := Document(Meta{
		Name:                 "mcp__x__run",
		Description:          "Run it.",
		NamespaceName:        "mcp__x",
		NamespaceDescription: "Kubernetes cluster tools",
	})
	hits := Rank("kubernetes", []Doc{doc}, 8)
	if len(hits) != 1 || hits[0].Name != "mcp__x__run" || hits[0].Score <= 0 {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestParseLimit(t *testing.T) {
	n, err := ParseLimit(nil)
	if err != nil || n != DefaultLimit {
		t.Fatalf("default = %d %v", n, err)
	}
	n, err = ParseLimit(float64(3))
	if err != nil || n != 3 {
		t.Fatalf("float = %d %v", n, err)
	}
	if _, err := ParseLimit(float64(1.5)); err == nil || err.Error() != ErrLimit.Error() {
		t.Fatalf("fraction err = %v", err)
	}
	if _, err := ParseLimit(0); err == nil {
		t.Fatal("zero was accepted")
	}
}

func TestFormatResult(t *testing.T) {
	if FormatResult(nil, nil) != "No matching tools found." {
		t.Fatal(FormatResult(nil, nil))
	}
	got := FormatResult([]string{"lookup"}, map[string]string{"lookup": "First line.\nSecond"})
	want := "Loaded 1 tool. They are available from your next call:\n- lookup: First line."
	if got != want {
		t.Fatalf("got %q", got)
	}
	got = FormatResult([]string{"a", "b"}, map[string]string{"a": "A", "b": "B"})
	if got != "Loaded 2 tools. They are available from your next call:\n- a: A\n- b: B" {
		t.Fatalf("got %q", got)
	}
}
