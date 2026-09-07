package runtime

import (
	"os"
	"testing"
)

// TestMain points HOME at an empty temp dir so a developer machine's
// ~/.agents/skills cannot leak into Engine.New skill-list assertions.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pigo-runtime-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", dir)
	_ = os.Setenv("USERPROFILE", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}
