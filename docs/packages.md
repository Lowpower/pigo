# Packages

Install extra extensions, skills, prompts, and themes.

```bash
pigo install npm:some-package
pigo install git:github.com/org/repo
pigo install /path/to/local
pigo list
pigo update --extensions
pigo remove some-package
```

`pigo config` toggles discovered resources. `--local` writes
`cwd/.pigo/settings.json`. `--approve` / `--no-approve` override project trust
for that command.

`pigo update` flags: `--self`, `--extensions`, `--models`, `--all`, `--force`,
`--extension <name>`.

## Manifest

A package root may include `package.json`:

```json
{
  "pigo": {
    "extensions": ["bin/my-ext"],
    "skills": ["skills"],
    "prompts": ["prompts"],
    "themes": ["themes"]
  }
}
```

Without a manifest, pigo looks for `extensions/`, spawnable binaries, and
`.js` / `.ts` files under `~/.pigo/agent/extensions/`.

Settings entries: `packages[]`, plus top-level `extensions` / `skills` /
`prompts` / `themes`. Package entries may set `autoload` and per-kind filters.

See [extensions.md](extensions.md) for how binaries are spawned.
