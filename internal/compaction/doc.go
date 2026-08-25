// Package compaction implements conversation history compaction: when the
// context grows past a threshold, older messages are replaced by an LLM-generated
// structured summary while recent messages are kept verbatim.
package compaction
