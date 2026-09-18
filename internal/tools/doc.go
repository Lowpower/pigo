// Package tools implements the built-in tools: read, bash, edit, write, grep,
// find, and ls. Tool parameter schemas are generated from Go structs via
// invopop/jsonschema. A Registry exposes them to a provider (as ai.Tool) and
// dispatches tool calls (usable as an agent ToolExecutor). Optional
// settings.container.image routes those tools through a Docker Runner while
// the pigo process and auth stay on the host.
package tools
