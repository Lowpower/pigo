// Package protocol defines the cross-process wire format used by the extension
// host (internal/ext). Frames follow pi's framing (packages/protocol/framing.ts):
// an unsigned 32-bit big-endian length prefix followed by the payload, capped at
// 16 MiB.
//
// pi encodes the payload as CBOR; pigo uses JSON (no extra dependency, easy to
// debug). The framing is identical.
package protocol
