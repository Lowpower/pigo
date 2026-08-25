// Package agent implements the agent loop: the turn cycle, tool scheduling
// (sequential and parallel), cancellation, steering/follow-up queues, and event
// broadcasting to consumers (the TUI renderer and session writer).
//
// Port of pi's packages/agent (agent-loop.ts / agent.ts).
package agent
