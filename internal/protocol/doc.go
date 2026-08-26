// Package protocol defines the cross-process wire format used by the extension
// host (internal/ext). Frames are an unsigned 32-bit big-endian length prefix
// followed by a JSON payload, capped at 16 MiB.
package protocol
