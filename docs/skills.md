# Skills

A skill is a markdown file with YAML frontmatter. The model sees a short
catalog and reads the file when the task matches.

## Discovery (first match of a name wins)

1. `~/.pigo/agent/skills/`
2. `cwd/.pigo/skills/` (trusted project)
3. `~/.agents/skills/` (user; always)
4. `cwd/.agents/skills` and ancestor `.agents/skills` up to the git root (trusted project)
5. `--skill` paths, `settings.skills[]`, and package resources

`--no-skills` skips discovery. Untrusted projects skip project-local skill
dirs; user `~/.agents/skills` still loads.

## File format

`SKILL.md` in a directory, or any `.md` file. Frontmatter:

```markdown
---
name: review
description: Review a Go change for tests and regressions.
disable-model-invocation: false
---

# Review

1. Run `go test ./...`
2. …
```

- `description` is **required** (files without it are ignored)
- `name` defaults to the parent directory; must match `a-z0-9-`, length ≤ 64
- `disable-model-invocation: true` hides the skill from the LLM catalog

## Slash

When `enableSkillCommands` is true (default), `/skill:name args` expands to an
XML block pointing at the file.
