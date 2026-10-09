# Skills

A skill is a markdown file with YAML frontmatter. The model sees a short
catalog and reads the file when the task matches.

## Discovery (first match of a name wins)

Project skills win over user skills. Within one level, the first file found is kept.

1. `--skill` paths
2. Project `settings.skills[]` (trusted project)
3. `cwd/.pigo/skills/`, then `cwd/.agents/skills` and ancestor `.agents/skills` up to the git root (trusted project)
4. User `settings.skills[]`
5. `~/.pigo/agent/skills/`, then `~/.agents/skills/` (user; always)
6. Package resources

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

- `description` is **required** on `SKILL.md`. A missing description, including a file with no frontmatter or an unclosed `---`, is not loaded and produces a startup warning. A plain `.md` file without a description is ignored silently
- a `SKILL.md` whose closed frontmatter is invalid YAML is not loaded and produces a warning. An unclosed fence is treated as a missing description, not a YAML error
- `name` defaults to the parent directory. It does not have to match that directory. Preferred form is `a-z0-9-`, length ≤ 64, no leading, trailing, or doubled hyphens. An invalid name is still loaded, with a warning
- `description` should be at most 1024 characters. A longer description is still loaded, with a warning
- the same name from two different files keeps the first one and warns with both paths. A symlink to a file already loaded is not a conflict
- `disable-model-invocation: true` hides the skill from the LLM catalog

## Slash

When `enableSkillCommands` is true (default), `/skill:name args` expands to an
XML block pointing at the file.
