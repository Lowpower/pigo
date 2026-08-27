package migrate

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/keys"
	"github.com/Lowpower/pigo/internal/session"
)

// Result is the outcome of startup migrations.
type Result struct {
	Warnings []string
}

// Run applies one-time on-disk migrations for agentDir and the project .pi dir.
func Run(cwd, agentDir string) Result {
	var res Result
	migrateSessionsFromAgentRoot(agentDir)
	migrateToolsToBin(agentDir)
	_ = keys.RewriteLegacyFile(filepath.Join(agentDir, "keybindings.json"))
	migrateCommandsToPrompts(agentDir)
	if cwd != "" {
		migrateCommandsToPrompts(filepath.Join(cwd, ".pi"))
		res.Warnings = append(res.Warnings, deprecatedExtensionDirs(agentDir, "Global")...)
		res.Warnings = append(res.Warnings, deprecatedExtensionDirs(filepath.Join(cwd, ".pi"), "Project")...)
	} else {
		res.Warnings = append(res.Warnings, deprecatedExtensionDirs(agentDir, "Global")...)
	}
	return res
}

func migrateSessionsFromAgentRoot(agentDir string) {
	ents, err := os.ReadDir(agentDir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		src := filepath.Join(agentDir, e.Name())
		cwd := sessionCwdFromFile(src)
		if cwd == "" {
			continue
		}
		dir := session.StorageDir(cwd, agentDir, "")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			continue
		}
		dst := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		_ = os.Rename(src, dst)
	}
}

func sessionCwdFromFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return ""
	}
	var h struct {
		Type string `json:"type"`
		Cwd  string `json:"cwd"`
	}
	if json.Unmarshal(sc.Bytes(), &h) != nil || h.Type != "session" {
		return ""
	}
	return h.Cwd
}

func migrateCommandsToPrompts(baseDir string) {
	commandsDir := filepath.Join(baseDir, "commands")
	promptsDir := filepath.Join(baseDir, "prompts")
	if _, err := os.Stat(commandsDir); err != nil {
		return
	}
	if _, err := os.Stat(promptsDir); err == nil {
		return
	}
	_ = os.Rename(commandsDir, promptsDir)
}

func migrateToolsToBin(agentDir string) {
	toolsDir := filepath.Join(agentDir, "tools")
	if _, err := os.Stat(toolsDir); err != nil {
		return
	}
	binDir := filepath.Join(agentDir, "bin")
	for _, name := range []string{"fd", "rg", "fd.exe", "rg.exe"} {
		oldPath := filepath.Join(toolsDir, name)
		if _, err := os.Stat(oldPath); err != nil {
			continue
		}
		_ = os.MkdirAll(binDir, 0o755)
		newPath := filepath.Join(binDir, name)
		if _, err := os.Stat(newPath); err == nil {
			_ = os.Remove(oldPath)
			continue
		}
		_ = os.Rename(oldPath, newPath)
	}
}

func deprecatedExtensionDirs(baseDir, label string) []string {
	var warnings []string
	if _, err := os.Stat(filepath.Join(baseDir, "hooks")); err == nil {
		warnings = append(warnings, label+" hooks/ directory found. Hooks have been renamed to extensions.")
	}
	toolsDir := filepath.Join(baseDir, "tools")
	ents, err := os.ReadDir(toolsDir)
	if err != nil {
		return warnings
	}
	for _, e := range ents {
		lower := strings.ToLower(e.Name())
		if lower == "fd" || lower == "rg" || lower == "fd.exe" || lower == "rg.exe" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		warnings = append(warnings, label+" tools/ directory contains custom tools. Custom tools have been merged into extensions.")
		break
	}
	return warnings
}
