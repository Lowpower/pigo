// Package tui implements the terminal UI (bubbletea/bubbles/lipgloss): the editor
// area with autocomplete, streaming rendering, high-frequency event batching, and
// themes. This replaces pi's pi-tui and is a primary reason for the Go rewrite.
//
// Port of pi's packages/tui + modes/interactive. Phase 0 provides a minimal
// multiline editor with an echo transcript (see model.go); autocomplete,
// streaming rendering, and themes land in Phase 5.
package tui
