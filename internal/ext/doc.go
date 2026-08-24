// Package ext hosts the extension system, redesigned as subprocess RPC: an
// extension is a separate binary/script that the host spawns and talks to over
// stdin/stdout using internal/protocol frames.
//
// Host is the host side (spawn, handshake, collect registrations, dispatch tool
// calls and events). Serve is the extension-author SDK (handshake, register
// tools, answer tool calls). Registered extension tools plug into the agent loop
// exactly like built-in tools.
//
// Reimagines pi's core/extensions as out-of-process RPC (Go cannot hot-load code
// the way pi's in-process jiti loader does). Deliberately out of scope for v1:
// custom self-drawn UI widgets (ctx.ui.custom).
package ext
