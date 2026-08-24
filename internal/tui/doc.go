// Package tui implements the terminal UI (bubbletea/bubbles/lipgloss): the editor
// area with autocomplete, streaming rendering, high-frequency event batching, and
// themes. This replaces pi's pi-tui and is a primary reason for the Go rewrite.
//
// Port of pi's packages/tui + modes/interactive. Phase 5 drives the agent loop
// (internal/agent) with real tools and a provider (internal/ai): the assistant
// response streams to screen as plain text during the turn, then renders as
// markdown via glamour once the turn ends; tool executions show inline. Ctrl+C
// interrupts a run (via ctx cancel) and quits when idle. Autocomplete, themes,
// and ~30fps token batching are refinements still to come.
package tui
