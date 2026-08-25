// Package tui implements the terminal UI (bubbletea/bubbles/lipgloss): the editor
// area, streaming rendering, and themes.
//
// Typing a prompt runs the agent loop with tools and a provider: the assistant
// streams as plain text, then renders as markdown via glamour; tool executions
// show inline. Ctrl+C interrupts a run (via ctx cancel) and quits when idle.
package tui
