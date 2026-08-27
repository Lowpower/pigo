// Package shell resolves bash/PowerShell executables and process-group cleanup.
package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Config is how to invoke a shell for one command.
type Config struct {
	Shell string
	Args  []string
	Stdin bool // true: command is written to stdin (legacy WSL bash -s)
}

var (
	customPath string
	goos       = runtime.GOOS
	lookPath   = exec.LookPath
	stat       = os.Stat
	getenv     = os.Getenv
)

// SetPath records settings.shellPath for this process.
func SetPath(p string) { customPath = strings.TrimSpace(p) }

// Path is the process-wide custom shell path.
func Path() string { return customPath }

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := stat(path)
	return err == nil && !st.IsDir()
}

func bashConfig(shell string) Config {
	if IsLegacyWSLBash(shell) {
		return Config{Shell: shell, Args: []string{"-s"}, Stdin: true}
	}
	return Config{Shell: shell, Args: []string{"-c"}}
}

// GetConfig resolves bash using custom path, then platform defaults.
func GetConfig() (Config, error) {
	return GetConfigPath(customPath)
}

// GetConfigPath resolves bash for an explicit custom path (empty = auto).
func GetConfigPath(custom string) (Config, error) {
	if custom != "" {
		custom = expandHome(custom)
		if fileExists(custom) {
			return bashConfig(custom), nil
		}
		return Config{}, fmt.Errorf("custom shell path not found: %s", custom)
	}
	if goos == "windows" {
		return windowsBash()
	}
	if fileExists("/bin/bash") {
		return bashConfig("/bin/bash"), nil
	}
	if p, err := lookPath("bash"); err == nil && p != "" {
		return bashConfig(p), nil
	}
	return Config{Shell: "sh", Args: []string{"-c"}}, nil
}

func windowsBash() (Config, error) {
	var paths []string
	if pf := getenv("ProgramFiles"); pf != "" {
		paths = append(paths, pf+`\Git\bin\bash.exe`)
	}
	if pf86 := getenv("ProgramFiles(x86)"); pf86 != "" {
		paths = append(paths, pf86+`\Git\bin\bash.exe`)
	}
	for _, p := range paths {
		if fileExists(p) {
			return bashConfig(p), nil
		}
	}
	if p, err := lookPath("bash.exe"); err == nil && fileExists(p) {
		return bashConfig(p), nil
	}
	return Config{}, fmt.Errorf("no bash shell found; install Git for Windows or add bash.exe to PATH")
}

// GetPowerShellConfig resolves pwsh.exe or powershell.exe. Windows only.
func GetPowerShellConfig() (Config, error) {
	if goos != "windows" {
		return Config{}, fmt.Errorf("the powershell tool is only available on Windows")
	}
	if p, err := lookPath("pwsh.exe"); err == nil && fileExists(p) {
		return Config{Shell: p, Args: powershellArgs()}, nil
	}
	if p, err := lookPath("powershell.exe"); err == nil && fileExists(p) {
		return Config{Shell: p, Args: powershellArgs()}, nil
	}
	return Config{}, fmt.Errorf("no PowerShell executable found")
}

func powershellArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command"}
}

// CommandContext builds exec.Cmd for running command with cfg.
func CommandContext(ctx context.Context, cfg Config, command string) *exec.Cmd {
	args := append([]string{}, cfg.Args...)
	if !cfg.Stdin {
		args = append(args, command)
	}
	cmd := exec.CommandContext(ctx, cfg.Shell, args...)
	if cfg.Stdin {
		cmd.Stdin = strings.NewReader(command + "\n")
	}
	PrepareContext(cmd)
	return cmd
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// IsLegacyWSLBash reports System32/Sysnative bash.exe that needs stdin (-s).
func IsLegacyWSLBash(path string) bool {
	n := strings.ToLower(strings.ReplaceAll(path, "/", `\`))
	return strings.Contains(n, `\windows\system32\bash.exe`) ||
		strings.Contains(n, `\windows\sysnative\bash.exe`)
}

// NormalizeWindowsShellPath converts Git Bash / Cygwin / WSL drive paths.
func NormalizeWindowsShellPath(filePath string) string {
	if !strings.HasPrefix(filePath, "/") || strings.HasPrefix(filePath, "//") || strings.Contains(filePath, `\`) {
		return filePath
	}
	body := filePath
	lower := strings.ToLower(body)
	switch {
	case strings.HasPrefix(lower, "/mnt/"):
		body = body[4:] // keep /X/...
	case strings.HasPrefix(lower, "/cygdrive/"):
		body = body[10:]
		if !strings.HasPrefix(body, "/") {
			body = "/" + body
		}
	}
	if len(body) >= 2 && body[0] == '/' {
		drive := body[1]
		if (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z') {
			if len(body) == 2 {
				return strings.ToUpper(string(drive)) + `:\`
			}
			if body[2] == '/' {
				suffix := strings.ReplaceAll(body[3:], "/", `\`)
				return strings.ToUpper(string(drive)) + `:\` + suffix
			}
		}
	}
	return filePath
}
