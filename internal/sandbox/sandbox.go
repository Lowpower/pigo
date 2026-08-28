// Package sandbox wraps bash commands when enabled via sandbox.json.
//
// Config files (project overrides global):
//   - ~/.pigo/agent/extensions/sandbox.json
//   - <cwd>/.pigo/sandbox.json
//
// Off by default. --no-sandbox and Windows never wrap, so bash is unchanged
// until a config sets enabled: true on linux/darwin.
package sandbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Config is the JSON shape of sandbox.json (project overrides global).
type Config struct {
	Enabled    *bool      `json:"enabled"`
	Network    Network    `json:"network"`
	Filesystem Filesystem `json:"filesystem"`
}

// Network is the optional domain allow/deny lists.
type Network struct {
	AllowedDomains []string `json:"allowedDomains"`
	DeniedDomains  []string `json:"deniedDomains"`
}

// Filesystem is the optional path allow/deny lists.
type Filesystem struct {
	DenyRead   []string `json:"denyRead"`
	AllowWrite []string `json:"allowWrite"`
	DenyWrite  []string `json:"denyWrite"`
}

var cliNoSandbox bool
var processAgentDir string
var projectTrusted = true

// SetNoSandbox records the --no-sandbox CLI flag for this process.
func SetNoSandbox(v bool) { cliNoSandbox = v }

// SetAgentDir records the agent dir used to find extensions/sandbox.json.
func SetAgentDir(dir string) { processAgentDir = dir }

// SetProjectTrusted gates cwd/.pigo/sandbox.json (untrusted projects skip it).
func SetProjectTrusted(v bool) { projectTrusted = v }

// NoSandbox reports the --no-sandbox CLI flag.
func NoSandbox() bool { return cliNoSandbox }

func resolveAgentDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if processAgentDir != "" {
		return processAgentDir
	}
	return os.Getenv("PIGO_CODING_AGENT_DIR")
}

func defaults() Config {
	off := false
	return Config{
		Enabled: &off,
		Network: Network{
			AllowedDomains: []string{
				"npmjs.org", "*.npmjs.org", "registry.npmjs.org", "registry.yarnpkg.com",
				"pypi.org", "*.pypi.org",
				"github.com", "*.github.com", "api.github.com", "raw.githubusercontent.com",
			},
		},
		Filesystem: Filesystem{
			DenyRead:   []string{"~/.ssh", "~/.aws", "~/.gnupg"},
			AllowWrite: []string{".", "/tmp"},
			DenyWrite:  []string{".env", ".env.*", "*.pem", "*.key"},
		},
	}
}

// Load merges defaults, global agentDir/extensions/sandbox.json, then cwd/.pigo/sandbox.json.
func Load(cwd, agentDir string) Config {
	cfg := defaults()
	if agentDir != "" {
		cfg = merge(cfg, readFile(filepath.Join(agentDir, "extensions", "sandbox.json")))
	}
	if cwd != "" && projectTrusted {
		cfg = merge(cfg, readFile(filepath.Join(cwd, ".pigo", "sandbox.json")))
	}
	return cfg
}

func readFile(path string) Config {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}
	}
	var c Config
	_ = json.Unmarshal(b, &c)
	return c
}

func merge(base, over Config) Config {
	out := base
	if over.Enabled != nil {
		v := *over.Enabled
		out.Enabled = &v
	}
	if over.Network.AllowedDomains != nil {
		out.Network.AllowedDomains = over.Network.AllowedDomains
	}
	if over.Network.DeniedDomains != nil {
		out.Network.DeniedDomains = over.Network.DeniedDomains
	}
	if over.Filesystem.DenyRead != nil {
		out.Filesystem.DenyRead = over.Filesystem.DenyRead
	}
	if over.Filesystem.AllowWrite != nil {
		out.Filesystem.AllowWrite = over.Filesystem.AllowWrite
	}
	if over.Filesystem.DenyWrite != nil {
		out.Filesystem.DenyWrite = over.Filesystem.DenyWrite
	}
	return out
}

// Active is whether bash should be wrapped for this process and platform.
func Active(cfg Config) bool {
	if cliNoSandbox {
		return false
	}
	if cfg.Enabled == nil || !*cfg.Enabled {
		return false
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return false
	}
	return true
}

// Command returns the executable and args used to run a bash command.
// When wrapping is off (or bwrap is missing), this is bash -c <command>.
func Command(command, cwd, agentDir string) (name string, args []string) {
	cfg := Load(cwd, resolveAgentDir(agentDir))
	if !Active(cfg) {
		return "bash", []string{"-c", command}
	}
	br := netBridge{}
	if len(cfg.Network.AllowedDomains) > 0 {
		if runtime.GOOS == "darwin" {
			br = ensureProxy(cfg.Network)
		} else if _, err := lookPath("socat"); err == nil {
			br = ensureProxy(cfg.Network)
		}
	}
	wrap := wrapArgv(command, cwd, cfg, br)
	if len(wrap) == 0 {
		return "bash", []string{"-c", command}
	}
	if _, err := lookPath(wrap[0]); err != nil {
		return "bash", []string{"-c", command}
	}
	return wrap[0], wrap[1:]
}

var lookPath = exec.LookPath

// WrapArgv is the sandbox argv for tests (no live network proxy).
func WrapArgv(command, cwd string, cfg Config) []string {
	return wrapArgv(command, cwd, cfg, netBridge{})
}

func expandPath(p, cwd string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

func expandPatterns(patterns []string, cwd string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range patterns {
		if strings.ContainsAny(p, "*?[") {
			glob := p
			if strings.HasPrefix(glob, "~") || filepath.IsAbs(glob) {
				glob = expandPath(p, cwd)
			} else {
				glob = filepath.Join(cwd, p)
			}
			matches, _ := filepath.Glob(glob)
			for _, m := range matches {
				add(m)
			}
			continue
		}
		add(expandPath(p, cwd))
	}
	return out
}
