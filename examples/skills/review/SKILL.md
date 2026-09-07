---
name: review
description: Review a local change for tests, regressions, and missing docs.
---

# Review

When this skill matches, read the relevant files and:

1. Identify the intended behaviour change.
2. Run the project's tests (`go test ./...` in a Go repo).
3. Call out missing tests, docs, or regressions.

Do not expand the scope beyond the change under review.
