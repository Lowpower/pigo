package auth

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	commandCache   = map[string]*string{}
	commandCacheMu sync.Mutex
	envVarNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type templatePart struct {
	literal bool
	value   string
}

// IsCommandConfigValue reports whether config starts with "!".
func IsCommandConfigValue(config string) bool {
	return strings.HasPrefix(config, "!")
}

// ResolveConfigValue interpolates $ENV / ${ENV} or runs a !command.
// Ported from packages/coding-agent/src/core/resolve-config-value.ts.
func ResolveConfigValue(config string, env map[string]string) string {
	if strings.HasPrefix(config, "!") {
		return executeCommand(config)
	}
	return resolveTemplate(parseTemplate(config), env)
}

// ClearConfigValueCache is for tests.
func ClearConfigValueCache() {
	commandCacheMu.Lock()
	defer commandCacheMu.Unlock()
	commandCache = map[string]*string{}
}

func executeCommand(commandConfig string) string {
	commandCacheMu.Lock()
	if v, ok := commandCache[commandConfig]; ok {
		commandCacheMu.Unlock()
		if v == nil {
			return ""
		}
		return *v
	}
	commandCacheMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sh", "-c", commandConfig[1:]).Output()
	val := strings.TrimSpace(string(out))
	commandCacheMu.Lock()
	defer commandCacheMu.Unlock()
	if err != nil || val == "" {
		commandCache[commandConfig] = nil
		return ""
	}
	commandCache[commandConfig] = &val
	return val
}

func parseTemplate(config string) []templatePart {
	var parts []templatePart
	appendLit := func(s string) {
		if s == "" {
			return
		}
		if n := len(parts); n > 0 && parts[n-1].literal {
			parts[n-1].value += s
			return
		}
		parts = append(parts, templatePart{literal: true, value: s})
	}
	i := 0
	for i < len(config) {
		dollar := strings.IndexByte(config[i:], '$')
		if dollar < 0 {
			appendLit(config[i:])
			break
		}
		dollar += i
		appendLit(config[i:dollar])
		if dollar+1 >= len(config) {
			appendLit("$")
			break
		}
		next := config[dollar+1]
		if next == '$' || next == '!' {
			appendLit(string(next))
			i = dollar + 2
			continue
		}
		if next == '{' {
			end := strings.IndexByte(config[dollar+2:], '}')
			if end < 0 {
				appendLit("$")
				i = dollar + 1
				continue
			}
			end += dollar + 2
			name := config[dollar+2 : end]
			if envVarNameRe.MatchString(name) {
				parts = append(parts, templatePart{value: name})
			} else {
				appendLit(config[dollar : end+1])
			}
			i = end + 1
			continue
		}
		name, n := scanEnvName(config[dollar+1:])
		if n > 0 {
			parts = append(parts, templatePart{value: name})
			i = dollar + 1 + n
			continue
		}
		appendLit("$")
		i = dollar + 1
	}
	return parts
}

func scanEnvName(s string) (string, int) {
	if s == "" || (!unicode.IsLetter(rune(s[0])) && s[0] != '_') {
		return "", 0
	}
	i := 1
	for i < len(s) {
		r := rune(s[i])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		i++
	}
	return s[:i], i
}

func resolveTemplate(parts []templatePart, env map[string]string) string {
	var b strings.Builder
	for _, p := range parts {
		if p.literal {
			b.WriteString(p.value)
			continue
		}
		v, ok := lookupEnv(p.value, env)
		if !ok {
			return ""
		}
		b.WriteString(v)
	}
	return b.String()
}

func lookupEnv(name string, env map[string]string) (string, bool) {
	if env != nil {
		if v, ok := env[name]; ok {
			return v, true
		}
	}
	v, ok := os.LookupEnv(name)
	return v, ok
}
