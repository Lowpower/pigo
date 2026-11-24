package auth

import "context"

// openRouterCatalogKey is the bearer token for OpenRouter GET /models/user.
// It follows the request path: process key, stored api key, OAuth access, then
// OPENROUTER_API_KEY. auth.APIKey misses the OAuth access token.
func openRouterCatalogKey(agentDir string) string {
	p, ok := Lookup("openrouter")
	if !ok {
		return ""
	}
	res, err := Resolve(context.Background(), Open(agentDir), p, ResolveOpts{NoRefresh: true})
	if err != nil || res == nil {
		return ""
	}
	return Secret(res)
}
