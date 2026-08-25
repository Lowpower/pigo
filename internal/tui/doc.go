// Package tui implements the terminal UI (bubbletea/bubbles/lipgloss): the editor
// area, streaming rendering, and themes. This replaces pi's pi-tui and is a
// primary reason for the Go rewrite.
//
// Port of pi's packages/tui + modes/interactive. Typing a prompt runs the agent
// loop with tools and a provider: the assistant streams as plain text, then
// renders as markdown via glamour; tool executions show inline. Ctrl+C interrupts
// a run (via ctx cancel) and quits when idle.
package tui
