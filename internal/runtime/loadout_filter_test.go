package runtime

import (
	"fmt"
	"testing"
)

func TestSelectModelToolsKeepsDirectModelOnlyAndLoadedDeferred(t *testing.T) {
	order := []string{"direct", "model", "code", "later", "secret", "plain"}
	exp := map[string]string{
		"direct": "direct",
		"model":  "model-only",
		"code":   "codemode",
		"later":  "deferred",
		"secret": "hidden",
	}
	got := selectModelTools(order, exp, nil, nil, false, nil, false)
	if fmt.Sprint(got) != "[direct model plain]" {
		t.Fatalf("visible = %v", got)
	}

	got = selectModelTools(order, exp, map[string]bool{"later": true}, nil, false, nil, false)
	if fmt.Sprint(got) != "[direct model later plain]" {
		t.Fatalf("loaded = %v", got)
	}
}

func TestSelectModelToolsIntersectsActiveAndTurn(t *testing.T) {
	order := []string{"direct", "model", "later", "secret"}
	exp := map[string]string{
		"direct": "direct",
		"model":  "model-only",
		"later":  "deferred",
		"secret": "hidden",
	}
	loaded := map[string]bool{"later": true}
	got := selectModelTools(order, exp, loaded, []string{"secret", "direct", "later"}, true, nil, false)
	if fmt.Sprint(got) != "[direct later]" {
		t.Fatalf("active = %v", got)
	}
	got = selectModelTools(order, exp, loaded, nil, false, []string{"model"}, true)
	if fmt.Sprint(got) != "[model]" {
		t.Fatalf("turn = %v", got)
	}
	got = selectModelTools(order, exp, loaded, nil, false, []string{}, true)
	if len(got) != 0 {
		t.Fatalf("empty turn = %v", got)
	}
}
