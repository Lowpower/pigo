#!/usr/bin/env python3
"""Remove Cursor Cloud Agent Open in Web / Open in Cursor PR footers."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from typing import Any

# Cursor injects this HTML after <!-- CURSOR_AGENT_PR_BODY_END -->. One template
# is used across pigo PRs; the regex also allows minor whitespace drift.
FOOTER_RE = re.compile(
    r"(?:\r?\n)*"
    r'<div>\s*<a href="https://cursor\.com/agents/[^"]*"[^>]*>'
    r".*?"
    r"</div>"
    r"\s*",
    re.DOTALL | re.IGNORECASE,
)

INLINE_ARTIFACTS_SUB_RE = re.compile(
    r"(?:\r?\n)*"
    r"<sub>To show artifacts inline,\s*"
    r'<a href="https://cursor\.com/dashboard/cloud-agents#my-pull-requests">'
    r"enable</a> in settings\.</sub>"
    r"\s*",
    re.IGNORECASE,
)

AGENT_MARKDOWN_LINK_RE = re.compile(
    r"\[([^\]]+)\]\(https://cursor\.com/(?:agents|background-agent)[^)]*\)",
    re.IGNORECASE,
)


def strip_cursor_pr_footer(body: str | None) -> str:
    if not body:
        return ""
    new = FOOTER_RE.sub("", body)
    new = INLINE_ARTIFACTS_SUB_RE.sub("", new)
    new = AGENT_MARKDOWN_LINK_RE.sub(r"\1", new)
    if new == body:
        return body
    return new.rstrip() + "\n"


def _gh_json(args: list[str]) -> Any:
    env = os.environ.copy()
    if "GH_TOKEN" not in env and "GITHUB_TOKEN" in env:
        env["GH_TOKEN"] = env["GITHUB_TOKEN"]
    proc = subprocess.run(
        ["gh", "api", *args],
        check=False,
        capture_output=True,
        text=True,
        env=env,
    )
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr)
        raise SystemExit(proc.returncode or 1)
    if not proc.stdout.strip():
        return None
    return json.loads(proc.stdout)


def _list_pulls(repo: str) -> list[dict[str, Any]]:
    return _gh_json(
        [
            "--paginate",
            f"/repos/{repo}/pulls?state=all&per_page=100",
        ]
    )


def _get_pull(repo: str, number: int) -> dict[str, Any]:
    return _gh_json([f"/repos/{repo}/pulls/{number}"])


def _set_body(repo: str, number: int, body: str) -> None:
    payload = json.dumps({"body": body})
    env = os.environ.copy()
    if "GH_TOKEN" not in env and "GITHUB_TOKEN" in env:
        env["GH_TOKEN"] = env["GITHUB_TOKEN"]
    proc = subprocess.run(
        [
            "gh",
            "api",
            "-X",
            "PATCH",
            f"/repos/{repo}/pulls/{number}",
            "--input",
            "-",
        ],
        check=False,
        capture_output=True,
        text=True,
        input=payload,
        env=env,
    )
    if proc.returncode != 0:
        err = (proc.stderr or proc.stdout or "").strip()
        raise RuntimeError(err or f"gh api PATCH failed ({proc.returncode})")


def _clean_one(repo: str, pr: dict[str, Any], dry_run: bool) -> str:
    original = pr.get("body") or ""
    cleaned = strip_cursor_pr_footer(original)
    if cleaned == original:
        return "skip"
    if dry_run:
        return "dry-run"
    _set_body(repo, int(pr["number"]), cleaned)
    return "updated"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", required=True, help="owner/name")
    parser.add_argument("--pr", type=int, help="single pull request number")
    parser.add_argument("--all", action="store_true", help="every PR, including closed")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if bool(args.pr) == bool(args.all):
        parser.error("exactly one of --pr or --all is required")

    if args.pr:
        pulls = [_get_pull(args.repo, args.pr)]
    else:
        pulls = _list_pulls(args.repo)

    counts = {"updated": 0, "skip": 0, "dry-run": 0, "error": 0}
    for pr in pulls:
        try:
            action = _clean_one(args.repo, pr, args.dry_run)
        except RuntimeError as exc:
            action = "error"
            print(f"#{pr['number']}\t{action}\t{exc}", file=sys.stderr)
        else:
            print(f"#{pr['number']}\t{action}")
        counts[action] += 1
    print(
        "done updated={updated} dry-run={dry-run} skip={skip} error={error}".format(
            **counts
        )
    )
    if counts["error"]:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
