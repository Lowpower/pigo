package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Canon folds '-' to '_' so server and tool names share one spelling.
func Canon(s string) string {
	return strings.ReplaceAll(s, "-", "_")
}

// Namespace is the tool namespace for a server, with '-' folded to '_'.
func Namespace(server string) string {
	return "mcp__" + Canon(server)
}

// CanonNamespace folds '-' to '_' and adds the mcp__ prefix when missing.
func CanonNamespace(ns string) string {
	ns = Canon(strings.TrimSpace(ns))
	if ns == "" {
		return ""
	}
	if !strings.HasPrefix(ns, "mcp__") {
		return "mcp__" + ns
	}
	return ns
}

// RegisteredToolNames maps each remote tool name to its pigo tool name.
// Names that collide after folding '-' to '_' gain a stable hash of the
// original name.
func RegisteredToolNames(server string, tools []string) map[string]string {
	groups := map[string][]string{}
	var order []string
	for _, name := range tools {
		key := Canon(name)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], name)
	}
	prefix := Namespace(server) + "__"
	out := make(map[string]string, len(tools))
	for _, key := range order {
		originals := groups[key]
		for _, original := range originals {
			local := key
			if len(originals) > 1 {
				local = key + "__" + shortHash(original)
			}
			out[original] = prefix + local
		}
	}
	return out
}

// DisplayTitle is the call title for a registered tool.
// mcp__server__tool becomes server/tool. Other names are unchanged.
func DisplayTitle(name string) string {
	if !strings.HasPrefix(name, "mcp__") {
		return name
	}
	rest := strings.TrimPrefix(name, "mcp__")
	server, tool, ok := strings.Cut(rest, "__")
	if !ok || server == "" || tool == "" {
		return name
	}
	return server + "/" + tool
}

// ToolAlias is the dashed mcp__server__tool form when it differs from the
// registered name.
func ToolAlias(server, remote, registered string) string {
	alias := "mcp__" + server + "__" + remote
	if alias == registered {
		return ""
	}
	return alias
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}
