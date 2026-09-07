package tools

import "path/filepath"

func resolvePath(cwd, p string) string {
	if p == "" {
		if cwd != "" {
			return cwd
		}
		return "."
	}
	if filepath.IsAbs(p) || cwd == "" {
		return p
	}
	return filepath.Join(cwd, p)
}
