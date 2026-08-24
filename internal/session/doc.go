// Package session implements the JSONL append-only session log
// ({timestamp}_{id}.jsonl under ~/.pi/agent/sessions/--<cwd>--/), kept
// format-compatible with pi so existing session files can be read directly.
//
// Port of pi's packages/coding-agent/src/core/session-manager.ts.
package session
