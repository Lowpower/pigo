package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type externalEditorDoneMsg struct {
	content string
	ok      bool
}

func prepareExternalEditor(content string) (filePath, dir string, err error) {
	dir, err = os.MkdirTemp("", "pigo-editor-")
	if err != nil {
		return "", "", err
	}
	filePath = filepath.Join(dir, "prompt.md")
	if err = os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	return filePath, dir, nil
}

func externalEditorCmd(command, filePath string) *exec.Cmd {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return exec.Command("nano", filePath)
	}
	args := append(append([]string{}, fields[1:]...), filePath)
	return exec.Command(fields[0], args...)
}

func finishExternalEditor(filePath, dir string, runErr error) (string, bool) {
	defer func() { _ = os.RemoveAll(dir) }()
	if runErr != nil {
		return "", false
	}
	b, err := os.ReadFile(filePath)
	if err != nil {
		return "", false
	}
	s := strings.TrimPrefix(string(b), "\ufeff")
	return strings.TrimSuffix(s, "\n"), true
}

// EditInExternalEditor writes content to a temp file, runs command, and reads it back.
// Non-zero exit keeps the original (ok=false). Trailing newline and a leading BOM are stripped.
func EditInExternalEditor(command, content string) (string, bool) {
	path, dir, err := prepareExternalEditor(content)
	if err != nil {
		return "", false
	}
	cmd := externalEditorCmd(command, path)
	return finishExternalEditor(path, dir, cmd.Run())
}

func (m Model) openExternalEditor() (tea.Model, tea.Cmd) {
	path, dir, err := prepareExternalEditor(m.editor.Expanded())
	if err != nil {
		return m, nil
	}
	cmd := externalEditorCmd(m.cfg.ExternalEditorCommand(), path)
	return m, tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		content, ok := finishExternalEditor(path, dir, runErr)
		return externalEditorDoneMsg{content: content, ok: ok}
	})
}
