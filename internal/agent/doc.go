// Package agent implements the agent loop: the turn cycle, tool scheduling
// (sequential and parallel), cancellation, and event broadcasting to consumers
// (the TUI renderer and session writer).
//
// Port of pi's packages/agent (agent-loop.ts). Phase 2 implements the core
// turn/tool loop and the AgentEvent stream. Mid-turn steering/follow-up queueing
// (QueueMode) integrates with the TUI in a later phase.
package agent
