package main

import (
	"fmt"
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
		"-np":  "--no-prompt-templates",
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
