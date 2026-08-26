// Package tui implements the terminal UI (bubbletea/bubbles/lipgloss): the editor
// area, streaming rendering, and themes.
//
// Typing a prompt runs the agent loop with tools and a provider: the assistant
// streams as plain text, then renders as markdown via glamour; tool executions
// show inline. Escape interrupts a run; Ctrl+C clears the editor (twice to quit).
package tui
