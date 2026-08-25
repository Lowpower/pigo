// Package ext hosts the extension system as subprocess RPC: an extension is a
// separate binary/script that the host spawns and talks to over stdin/stdout
// using internal/protocol frames.
//
// Host is the host side (spawn, handshake, collect registrations, dispatch tool
// calls and events). Serve is the extension-author SDK (handshake, register
// tools, answer tool calls). Registered extension tools plug into the agent loop
// exactly like built-in tools.
package ext
