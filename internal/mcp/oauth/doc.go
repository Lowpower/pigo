// Package oauth signs MCP HTTP servers in with OAuth.
//
// Tokens are stored in the agent directory as mcp-auth.json, keyed by server
// name and URL. Dynamic client registration is the default. clientRegistration
// "cimd" uses ClientMetadataURL instead, the fixed document at
// docs/oauth/client.json.
package oauth
