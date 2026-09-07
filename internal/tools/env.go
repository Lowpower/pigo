package tools

import (
	"os"
	"os/exec"
	"strings"
)

func applyExtraEnv(cmd *exec.Cmd, extra map[string]string) {
	if cmd == nil || len(extra) == 0 {
		return
	}
	base := cmd.Env
	if base == nil {
		base = os.Environ()
	}
	skip := map[string]bool{}
	for k := range extra {
		skip[k] = true
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		name, _, ok := strings.Cut(kv, "=")
		if ok && skip[name] {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	cmd.Env = out
}
