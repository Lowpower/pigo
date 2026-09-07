# Prompt templates

Markdown files that become slash commands. The file name (without `.md`) is the
command: `review.md` → `/review`.

## Discovery

1. `~/.pigo/agent/prompts/*.md`
2. `cwd/.pigo/prompts/*.md` (trusted project)
3. `--prompt-template` files or directories, `settings.prompts[]`, packages

`--no-prompt-templates` skips discovery.

## Frontmatter

```markdown
---
description: Review the named files
argument-hint: <path>…
---

Review these paths: $@

$1
```

- `description` — `/help` text (defaults to the first non-empty line)
- `argument-hint` — shown in the TUI

Body substitutions: `$1`…, `$@` / `$ARGUMENTS`, `${1:-default}`, `${@:N}`,
`${@:N:L}`.
