#!/usr/bin/env python3
"""Tests for strip_cursor_pr_footer (run: python3 strip_cursor_pr_footer_test.py)."""

from __future__ import annotations

import unittest

from strip_cursor_pr_footer import strip_cursor_pr_footer

FOOTER = (
    '<div><a href="https://cursor.com/agents/bc-fd3984d0-d966-4bbf-b670-e4eca9dd9197'
    '?cursor_ref=pr_footer&cursor_cta=open_in_web"><picture>'
    '<source media="(prefers-color-scheme: dark)" '
    'srcset="https://cursor.com/assets/images/open-in-web-dark.png">'
    '<source media="(prefers-color-scheme: light)" '
    'srcset="https://cursor.com/assets/images/open-in-web-light.png">'
    '<img alt="Open in Web" width="114" height="28" '
    'src="https://cursor.com/assets/images/open-in-web-dark.png"></picture></a>'
    '&nbsp;<a href="https://cursor.com/background-agent?bcId=bc-fd3984d0-d966-4bbf-b670-e4eca9dd9197'
    '&cursor_ref=pr_footer&cursor_cta=open_in_cursor"><picture>'
    '<source media="(prefers-color-scheme: dark)" '
    'srcset="https://cursor.com/assets/images/open-in-cursor-dark.png">'
    '<source media="(prefers-color-scheme: light)" '
    'srcset="https://cursor.com/assets/images/open-in-cursor-light.png">'
    '<img alt="Open in Cursor" width="131" height="28" '
    'src="https://cursor.com/assets/images/open-in-cursor-dark.png"></picture></a>&nbsp;</div>'
)

BODY = """<!-- CURSOR_AGENT_PR_BODY_BEGIN -->
Fixes #125.

Keep this prose and the HTML comments.

<!-- CURSOR_AGENT_PR_BODY_END -->
"""


class StripCursorPRFooterTest(unittest.TestCase):
    def test_strips_injected_open_in_web_div(self):
        got = strip_cursor_pr_footer(BODY + "\n" + FOOTER + "\n")
        self.assertEqual(got, BODY)
        self.assertNotIn("cursor.com/agents/", got)
        self.assertNotIn("background-agent", got)
        self.assertIn("CURSOR_AGENT_PR_BODY_END", got)
        self.assertIn("Keep this prose", got)

    def test_leaves_body_without_footer_unchanged(self):
        self.assertEqual(strip_cursor_pr_footer(BODY), BODY)

    def test_empty_and_none(self):
        self.assertEqual(strip_cursor_pr_footer(""), "")
        self.assertEqual(strip_cursor_pr_footer(None), "")

    def test_idempotent(self):
        once = strip_cursor_pr_footer(BODY + FOOTER)
        self.assertEqual(strip_cursor_pr_footer(once), once)

    def test_strips_every_injected_div(self):
        other = FOOTER.replace("bc-fd3984d0-d966-4bbf-b670-e4eca9dd9197", "bc-11111111-1111-1111-1111-111111111111")
        got = strip_cursor_pr_footer(BODY + FOOTER + "\n" + other + "\n")
        self.assertEqual(got, BODY)
        self.assertNotIn("cursor.com/agents/", got)

    def test_does_not_strip_prose_mention_of_cursor_agents(self):
        prose = "See https://cursor.com/agents/ for the product page.\n"
        self.assertEqual(strip_cursor_pr_footer(prose), prose)

    def test_strips_inline_artifacts_sub_and_keeps_link_text(self):
        body = (
            "## Demos\n\n"
            "[demo.mp4](https://cursor.com/agents/bc-9fb6361d-1261-4377-8d35-948d572cdba7"
            "/artifacts?path=%2Fopt%2Fcursor%2Fartifacts%2Fdemo.mp4)\n\n"
            "<sub>To show artifacts inline, "
            '<a href="https://cursor.com/dashboard/cloud-agents#my-pull-requests">'
            "enable</a> in settings.</sub>\n"
        )
        got = strip_cursor_pr_footer(body)
        self.assertEqual(got, "## Demos\n\ndemo.mp4\n")
        self.assertNotIn("cursor.com/agents/", got)
        self.assertNotIn("dashboard/cloud-agents", got)


if __name__ == "__main__":
    unittest.main()
