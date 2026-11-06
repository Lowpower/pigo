package pkgmgr

import (
	"fmt"
	"strings"
)

// BuiltinPrefix marks an in-process extension. The path is the name
// "builtin:<name>", not a filesystem location.
const BuiltinPrefix = "builtin:"

// BuiltinNames is the pi v1.3.0 built-in set, in load order.
// llama.cpp is the provider; the other three are named here so they can be
// switched before their own issues land.
var BuiltinNames = []string{"llama.cpp", "codemode", "tool-search", "mcp"}

// BuiltinPath is the settings and report id for a built-in extension.
func BuiltinPath(name string) string {
	return BuiltinPrefix + name
}

// IsBuiltinPath reports whether spec is a builtin: extension id.
func IsBuiltinPath(spec string) bool {
	return strings.HasPrefix(strings.TrimSpace(spec), BuiltinPrefix)
}

// BuiltinName strips the builtin: prefix. The caller checks KnownBuiltin.
func BuiltinName(spec string) string {
	return strings.TrimPrefix(strings.TrimSpace(spec), BuiltinPrefix)
}

// KnownBuiltin reports whether name is one of BuiltinNames.
func KnownBuiltin(name string) bool {
	for _, n := range BuiltinNames {
		if n == name {
			return true
		}
	}
	return false
}

func appendBuiltinResources(rs []Resource, userExt, projectExt []string, trusted bool) []Resource {
	for _, name := range BuiltinNames {
		path := BuiltinPath(name)
		scope := "user"
		enabled := builtinEnabled(userExt, path)
		if trusted {
			if hit, on := builtinProjectOverride(projectExt, path); hit {
				scope = "project"
				enabled = on
			}
		}
		rs = append(rs, Resource{
			Path:    path,
			Enabled: enabled,
			Scope:   scope,
			Origin:  "top-level",
			Source:  "builtin",
			Type:    KindExtensions,
		})
	}
	return rs
}

// builtinEnabled applies ! then + then - to one builtin: path.
// No matching pattern leaves the extension enabled.
func builtinEnabled(entries []string, path string) bool {
	enabled := true
	var includes, excludes []string
	for _, p := range entries {
		if p == "" || !isOverridePattern(p) {
			continue
		}
		body := stripOverridePrefix(p)
		switch p[0] {
		case '!':
			if builtinMatch(path, body, false) {
				enabled = false
			}
		case '+':
			includes = append(includes, body)
		case '-':
			excludes = append(excludes, body)
		}
	}
	if builtinMatchAny(path, includes, true) {
		enabled = true
	}
	if builtinMatchAny(path, excludes, true) {
		enabled = false
	}
	return enabled
}

// builtinProjectOverride reports whether project settings name this path.
// A matching + / - / ! replaces the user result. Other entries do not.
func builtinProjectOverride(entries []string, path string) (hit bool, enabled bool) {
	enabled = true
	var includes, excludes []string
	for _, p := range entries {
		if p == "" || !isOverridePattern(p) {
			continue
		}
		body := stripOverridePrefix(p)
		switch p[0] {
		case '!':
			if builtinMatch(path, body, false) {
				hit = true
				enabled = false
			}
		case '+':
			includes = append(includes, body)
		case '-':
			excludes = append(excludes, body)
		}
	}
	if builtinMatchAny(path, includes, true) {
		hit = true
		enabled = true
	}
	if builtinMatchAny(path, excludes, true) {
		hit = true
		enabled = false
	}
	return hit, enabled
}

func builtinMatchAny(path string, patterns []string, exact bool) bool {
	for _, p := range patterns {
		if builtinMatch(path, p, exact) {
			return true
		}
	}
	return false
}

func builtinMatch(path, pattern string, exact bool) bool {
	if exact {
		return normalizeExactPattern(pattern) == path
	}
	return globMatch(toPosix(pattern), path)
}

// ApplyBuiltinFlags selects the built-ins that actually load.
// Settings discovered in resolved are the default. --no-extensions drops them.
// --no-tools drops them. -e builtin:<name> adds one back, including over a
// settings disable. An unknown builtin: name is an error.
func ApplyBuiltinFlags(resolved []Resource, noExtensions, noTools bool, cli []string) ([]Resource, error) {
	for _, spec := range cli {
		spec = strings.TrimSpace(spec)
		if !IsBuiltinPath(spec) {
			continue
		}
		if !KnownBuiltin(BuiltinName(spec)) {
			return nil, fmt.Errorf("unknown built-in extension: %s", spec)
		}
	}
	if noTools {
		return nil, nil
	}
	var base []Resource
	found := false
	for _, name := range BuiltinNames {
		path := BuiltinPath(name)
		for _, r := range resolved {
			if r.Source != "builtin" || r.Path != path {
				continue
			}
			found = true
			if !noExtensions && r.Enabled {
				base = append(base, r)
			}
		}
	}
	if !found && !noExtensions {
		for _, name := range BuiltinNames {
			base = append(base, Resource{
				Path:    BuiltinPath(name),
				Enabled: true,
				Scope:   "user",
				Origin:  "top-level",
				Source:  "builtin",
				Type:    KindExtensions,
			})
		}
	}
	seen := map[string]bool{}
	out := make([]Resource, 0, len(base)+len(cli))
	for _, r := range base {
		seen[r.Path] = true
		out = append(out, r)
	}
	for _, spec := range cli {
		spec = strings.TrimSpace(spec)
		if !IsBuiltinPath(spec) || seen[spec] {
			continue
		}
		seen[spec] = true
		out = append(out, Resource{
			Path:    spec,
			Enabled: true,
			Scope:   "temporary",
			Origin:  "top-level",
			Source:  "builtin",
			Type:    KindExtensions,
		})
	}
	return out, nil
}
