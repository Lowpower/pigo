# Eval scenarios

JSON files for `pigo eval` and the Go harness in `internal/eval`.

Each `*.json` file is one case. A run creates a fresh cwd and agent directory,
writes optional `files`, prompts the model, then grades the final assistant
text. Session JSONL is copied next to `report.json`.

`*.docs.json` files are documentation-lift comparisons. The same case runs as
`without_docs` and `with_docs`: the baseline drops context-doc files
(`AGENTS.md`, `CLAUDE.md`, overrides, `.pigo/AGENTS.md`) and does not inject
`<project_context>`; the candidate keeps the files and the default prompt.
Other files are seeded on both arms. Host cases (`smoke.json` and any other
non-`*.docs.json`) still run once.

```bash
pigo eval                  # ./evals
pigo eval path/to/dir      # explicit directory
pigo eval --out .eval --provider anthropic --model claude-sonnet-4
pigo eval --runs-per-variant 3
pigo eval --container-image debian:bookworm-slim
```

`PIGO_PROVIDER` and `PIGO_MODEL` supply the same defaults. `PIGO_EVAL_RUNS_PER_VARIANT`
and `PIGO_CONTAINER_IMAGE` match the flags above. Credentials come from
the process environment (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, …) or `--api-key`.
The isolated agent directory starts empty, so a host `~/.pigo/agent/auth.json` is
not used.

With no key, every case is **skipped** (exit 0). If `--container-image` is set
and `docker` is not on PATH, the suite is skipped the same way. Failed host
grades and runtime errors exit 1. Docs-lift grade outcomes are observations
used for lift; a baseline miss does not fail the suite. CI must not require a
live provider or Docker.

## File format

```json
{
  "name": "smoke",
  "prompt": "What is the capital of France? Reply with only the city name.",
  "noTools": true,
  "expect": { "contains": ["Paris"] }
}
```

| Field | Meaning |
| --- | --- |
| `name` | Optional; defaults to the filename without `.json` (and without `.docs` for lift cases) |
| `prompt` | User message |
| `noTools` | Disable built-in tools |
| `tools` / `excludeTools` | Allowlist / denylist |
| `systemPrompt` | Replace the default system prompt |
| `files` | Relative paths written into the temp cwd |
| `expect.contains` | All substrings must appear |
| `expect.equals` | Exact match after trim |
| `expect.regex` | Must match the trimmed output |

The report prints host pass rate, tokens, and latency. Docs-lift adds pass-rate
lift plus tokens / latency / cost deltas. A pair is eligible only when both
arms produce a grade (pass or fail). Skip, runtime error, or a missing arm
blocks that pair and omits it from the headline lift.

Artifacts:

```
.eval/report.json
.eval/sessions/<name>.jsonl
.eval/sessions/without_docs/<name>.jsonl
.eval/sessions/with_docs/<name>.jsonl
```

Repetitions append `-<n>` to the session filename.
