package main

import (
	"strings"

	"github.com/Lowpower/pigo/internal/ext"
)

var extraFlags []ext.UnknownFlag

var rootSubcommands = map[string]bool{
	"auth": true, "config": true, "install": true, "remove": true,
	"list": true, "update": true, "help": true, "completion": true,
}

var knownBoolFlags = map[string]bool{
	"print": true, "continue": true, "resume": true, "no-session": true,
	"no-context-files": true, "no-skills": true, "no-tools": true,
	"no-extensions": true, "no-themes": true, "list-models": true,
	"offline": true, "no-builtin-tools": true, "no-prompt-templates": true,
	"no-sandbox": true, "approve": true, "no-approve": true, "verbose": true,
	"version": true, "help": true,
}

var knownValueFlags = map[string]bool{
	"mode": true, "prompt": true, "config-dir": true, "session": true,
	"provider": true, "model": true, "thinking": true, "system-prompt": true,
	"append-system-prompt": true, "skill": true, "tools": true,
	"exclude-tools": true, "extension": true, "use-theme": true, "theme": true,
	"list-models-query": true, "export": true, "fork": true, "session-id": true,
	"name": true, "api-key": true, "models": true, "prompt-template": true,
	"tui-mode": true, "session-dir": true,
}

func isRootSubcommand(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return rootSubcommands[a]
	}
	return false
}

func splitLongFlag(arg string) (name, value string, hasValue bool) {
	body := strings.TrimPrefix(arg, "--")
	if i := strings.IndexByte(body, '='); i >= 0 {
		return body[:i], body[i+1:], true
	}
	return body, "", false
}

// peelUnknownFlags removes unrecognized --flags from a root invocation so cobra
// can parse the rest. Values are taken from the original argv: --foo, --foo=bar,
// and --foo <token> when the token does not start with - or @.
func peelUnknownFlags(args []string) (rest []string, unknown []ext.UnknownFlag) {
	if isRootSubcommand(args) {
		return args, nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			rest = append(rest, a)
			continue
		}
		name, val, hasEq := splitLongFlag(a)
		if knownBoolFlags[name] || knownValueFlags[name] {
			rest = append(rest, a)
			if !hasEq && knownValueFlags[name] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				rest = append(rest, args[i])
			}
			continue
		}
		u := ext.UnknownFlag{Name: name, Present: true}
		if hasEq {
			u.Value = val
			u.HasValue = true
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && !strings.HasPrefix(args[i+1], "@") {
			u.Value = args[i+1]
			u.HasValue = true
			i++
		}
		unknown = append(unknown, u)
	}
	return rest, unknown
}

func formatUnclaimed(flags []ext.UnknownFlag) string {
	parts := make([]string, 0, len(flags))
	seen := map[string]bool{}
	for _, f := range flags {
		if seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		parts = append(parts, "--"+f.Name)
	}
	if len(parts) == 1 {
		return "unknown option: " + parts[0]
	}
	return "unknown options: " + strings.Join(parts, ", ")
}

func inputSource(mode string) string {
	switch mode {
	case "interactive":
		return "tui"
	case "json", "rpc":
		return "rpc"
	default:
		return "cli"
	}
}
