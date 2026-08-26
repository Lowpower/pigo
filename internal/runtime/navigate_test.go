package runtime

import (
	"context"
	"testing"

	"github.com/Lowpower/pigo/internal/session"
)

func TestNavigateTreeBeforeHookCanCancel(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "a"})
	a, _ := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "b"})
	_, _ = sess.AppendMessage("user", map[string]any{"role": "user", "content": "c"})
	leaf := sess.LeafID()
	e := &Engine{Opts: Options{Session: sess}}
	e.BeforeTree = func(session.TreePrep) session.TreeHookResult {
		return session.TreeHookResult{Cancel: true}
	}
	res, err := e.NavigateTree(context.Background(), a.ID, session.NavigateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Cancelled || sess.LeafID() != leaf {
		t.Fatalf("cancelled=%v leaf=%s want %s", res.Cancelled, sess.LeafID(), leaf)
	}
}
