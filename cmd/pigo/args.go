package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/models"
)

// expandShortFlags rewrites two-letter flags before cobra sees them.
func expandShortFlags(args []string) []string {
	aliases := map[string]string{
		"-nt":  "--no-tools",
		"-nbt": "--no-builtin-tools",
		"-ns":  "--no-skills",
		"-nc":  "--no-context-files",
		"-ne":  "--no-extensions",
		"-xt":  "--exclude-tools",
		"-na":  "--no-approve",
	}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		if mapped, ok := aliases[a]; ok {
			out = append(out, mapped)
			continue
		}
		out = append(out, a)
	}
	return out
}

func applyModelSpec(provider, model *string, thinking *string, spec string) {
	p, id, t := models.ParseSpec(spec)
	if p != "" {
		*provider = p
	}
	if id != "" {
		*model = id
	}
	if t != "" && thinking != nil {
		*thinking = t
	}
}

func splitPromptArgs(args []string) (messages []string, files []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "@") && len(a) > 1 {
			files = append(files, a[1:])
			continue
		}
		messages = append(messages, a)
	}
	return messages, files
}

func inlineFiles(cwd string, files []string) (string, error) {
	var b strings.Builder
	for _, f := range files {
		p := f
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, f)
		}
		info, err := os.Stat(p)
		if err != nil {
			return "", fmt.Errorf("file not found: %s", p)
		}
		if info.Size() == 0 {
			continue
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "<file name=\"%s\">\n%s\n</file>\n", p, string(body))
	}
	return b.String(), nil
}

func resolvePromptInput(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return input
	}
	info, err := os.Stat(input)
	if err != nil || !info.Mode().IsRegular() {
		return input
	}
	body, err := os.ReadFile(input)
	if err != nil {
		return input
	}
	return string(body)
}

func buildInitialMessage(stdin, fileText string, messages []string) (string, []string) {
	parts := make([]string, 0, 3)
	if stdin != "" {
		parts = append(parts, stdin)
	}
	if fileText != "" {
		parts = append(parts, fileText)
	}
	rest := messages
	if len(messages) > 0 {
		parts = append(parts, messages[0])
		rest = append([]string(nil), messages[1:]...)
	}
	if len(parts) == 0 {
		return "", rest
	}
	return strings.Join(parts, ""), rest
}

func resolveSessionDir(flag, settings, envFallback string) string {
	if s := strings.TrimSpace(flag); s != "" {
		return s
	}
	if s := strings.TrimSpace(settings); s != "" {
		return s
	}
	if s := strings.TrimSpace(envFallback); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("PIGO_CODING_AGENT_SESSION_DIR"))
}

func applyHTTPProxy(proxy string) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return
	}
	if os.Getenv("HTTP_PROXY") == "" && os.Getenv("http_proxy") == "" {
		_ = os.Setenv("HTTP_PROXY", proxy)
	}
	if os.Getenv("HTTPS_PROXY") == "" && os.Getenv("https_proxy") == "" {
		_ = os.Setenv("HTTPS_PROXY", proxy)
	}
}

func readPipedStdin() string {
	if isTTY() {
		return ""
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return string(b)
}
