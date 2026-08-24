// Package compaction implements conversation history compaction: when the
// context grows past a threshold, older messages are replaced by an LLM-generated
// structured summary while recent messages are kept verbatim.
//
// Port of pi's packages/coding-agent/src/core/compaction/compaction.ts (trigger,
// cut point, summarization prompt, and token estimation).
package compaction
