package tui

import (
	"reflect"
	"testing"
)

func TestToggleFromAllBecomesSingleton(t *testing.T) {
	got := toggleID(enabledIDs{all: true}, "a/1")
	if got.all || !reflect.DeepEqual(got.ids, []string{"a/1"}) {
		t.Fatalf("%+v", got)
	}
}

func TestToggleRemovesAndAppends(t *testing.T) {
	e := enabledIDs{ids: []string{"a/1", "b/2"}}
	got := toggleID(e, "a/1")
	if !reflect.DeepEqual(got.ids, []string{"b/2"}) {
		t.Fatalf("remove: %+v", got)
	}
	got = toggleID(got, "c/3")
	if !reflect.DeepEqual(got.ids, []string{"b/2", "c/3"}) {
		t.Fatalf("append: %+v", got)
	}
}

func TestEnableAllCoversAllReturnsAll(t *testing.T) {
	all := []string{"a/1", "b/2"}
	got := enableAllIDs(enabledIDs{ids: []string{"a/1"}}, all, nil)
	if !got.all {
		t.Fatalf("%+v", got)
	}
	got = enableAllIDs(enabledIDs{all: true}, all, nil)
	if !got.all {
		t.Fatal("all stays all")
	}
}

func TestEnableAllWithTargets(t *testing.T) {
	all := []string{"a/1", "b/2", "c/3"}
	got := enableAllIDs(enabledIDs{ids: []string{"a/1"}}, all, []string{"b/2"})
	if got.all || !reflect.DeepEqual(got.ids, []string{"a/1", "b/2"}) {
		t.Fatalf("%+v", got)
	}
}

func TestClearAllFromAll(t *testing.T) {
	all := []string{"a/1", "b/2", "c/3"}
	got := clearAllIDs(enabledIDs{all: true}, all, nil)
	if got.all || len(got.ids) != 0 {
		t.Fatalf("clear all: %+v", got)
	}
	got = clearAllIDs(enabledIDs{all: true}, all, []string{"b/2"})
	if got.all || !reflect.DeepEqual(got.ids, []string{"a/1", "c/3"}) {
		t.Fatalf("clear targets from all: %+v", got)
	}
}

func TestMoveNoopWhenAll(t *testing.T) {
	got := moveID(enabledIDs{all: true}, "a/1", 1)
	if !got.all {
		t.Fatalf("%+v", got)
	}
}

func TestMoveSwapsEnabled(t *testing.T) {
	e := enabledIDs{ids: []string{"a/1", "b/2", "c/3"}}
	got := moveID(e, "a/1", 1)
	if !reflect.DeepEqual(got.ids, []string{"b/2", "a/1", "c/3"}) {
		t.Fatalf("%+v", got)
	}
	got = moveID(e, "a/1", -1)
	if !reflect.DeepEqual(got.ids, e.ids) {
		t.Fatalf("oob: %+v", got)
	}
}

func TestSortedIDsEnabledFirst(t *testing.T) {
	all := []string{"a/1", "b/2", "c/3"}
	got := sortedIDs(enabledIDs{ids: []string{"c/3", "a/1"}}, all)
	if !reflect.DeepEqual(got, []string{"c/3", "a/1", "b/2"}) {
		t.Fatalf("%v", got)
	}
	got = sortedIDs(enabledIDs{all: true}, all)
	if !reflect.DeepEqual(got, all) {
		t.Fatalf("all order: %v", got)
	}
}

func TestSortedIDsKeepsUnavailable(t *testing.T) {
	all := []string{"a/1"}
	got := sortedIDs(enabledIDs{ids: []string{"gone/x", "a/1"}}, all)
	if !reflect.DeepEqual(got, []string{"gone/x", "a/1"}) {
		t.Fatalf("%v", got)
	}
}

func TestSessionScopeIDs(t *testing.T) {
	avail := []string{"a/1", "b/2"}
	ids, implicit := sessionScopeIDs(enabledIDs{all: true}, avail)
	if !implicit || ids != nil {
		t.Fatalf("all: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{"a/1"}}, avail)
	if implicit || !reflect.DeepEqual(ids, []string{"a/1"}) {
		t.Fatalf("subset: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{"a/1", "b/2"}}, avail)
	if !implicit {
		t.Fatalf("fully checked: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{"a/1", "b/2", "gone/x"}}, avail)
	if !implicit {
		t.Fatalf("superset: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{"gone/x"}}, avail)
	if !implicit {
		t.Fatalf("only unavailable: %v %v", ids, implicit)
	}
	ids, implicit = sessionScopeIDs(enabledIDs{ids: []string{}}, avail)
	if !implicit {
		t.Fatalf("empty explicit: %v %v", ids, implicit)
	}
}
