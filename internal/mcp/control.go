package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Lowpower/pigo/internal/mcp/oauth"
)

// ListEntry is one `pigo mcp list --json` row.
// Resources is true only when the server says it has resources.
// Counts are not included.
type ListEntry struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Exposure  string `json:"exposure"`
	Resources bool   `json:"resources,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ListEntries maps connection statuses to list rows.
func ListEntries(statuses []Status) []ListEntry {
	out := make([]ListEntry, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, ListEntry{
			Name:      st.Name,
			Status:    st.Status,
			Exposure:  st.Exposure,
			Resources: st.HasResources,
			Error:     st.Err,
		})
	}
	return out
}

// Probe connects the servers and returns a status for each one.
// Disabled servers are not connected. The manager is closed before return.
func Probe(ctx context.Context, servers []Server, workDir string) ([]Status, error) {
	mgr := New(servers, workDir)
	mgr.Start(ctx)
	err := mgr.WaitSettled(ctx)
	statuses := mgr.Statuses()
	mgr.Close()
	return statuses, err
}

// OAuthSettings reads the oauth object kept on a server entry.
func OAuthSettings(cfg Config) (oauth.Settings, error) {
	if len(cfg.Raw) == 0 {
		return oauth.Settings{}, nil
	}
	var body struct {
		OAuth struct {
			ClientID              string `json:"clientId"`
			ClientSecret          string `json:"clientSecret"`
			CallbackPort          int    `json:"callbackPort"`
			Scope                 string `json:"scope"`
			ClientName            string `json:"clientName"`
			ClientRegistration    string `json:"clientRegistration"`
			AuthServerMetadataURL string `json:"authServerMetadataUrl"`
		} `json:"oauth"`
	}
	if err := json.Unmarshal(cfg.Raw, &body); err != nil {
		return oauth.Settings{}, err
	}
	return oauth.Settings{
		ClientID:              body.OAuth.ClientID,
		ClientSecret:          body.OAuth.ClientSecret,
		CallbackPort:          body.OAuth.CallbackPort,
		Scope:                 body.OAuth.Scope,
		ClientName:            body.OAuth.ClientName,
		ClientRegistration:    body.OAuth.ClientRegistration,
		AuthServerMetadataURL: body.OAuth.AuthServerMetadataURL,
	}, nil
}

// Login runs one server's OAuth flow. ctx bounds the whole sign-in, including
// the wait for the browser and requests to the authorization server.
func Login(ctx context.Context, agentDir string, srv Server, prompt oauth.Prompt) error {
	if srv.Config.URL == "" {
		return fmt.Errorf("mcp server %q has no url", srv.Name)
	}
	settings, err := OAuthSettings(srv.Config)
	if err != nil {
		return err
	}
	client := &oauth.Client{Store: oauth.Open(agentDir)}
	return client.SignIn(ctx, oauth.Server{Name: srv.Name, URL: srv.Config.URL}, oauth.Request{
		Settings: settings,
		Prompt:   prompt,
	})
}

// Logout deletes the stored OAuth credentials for one server.
// The server entry in mcp.json is left in place.
func Logout(agentDir string, srv Server) error {
	if srv.Config.URL == "" {
		return fmt.Errorf("mcp server %q has no url", srv.Name)
	}
	return oauth.Open(agentDir).Delete(srv.Name, srv.Config.URL)
}

// Find returns one loaded server by name.
func Find(agentDir, cwd string, trusted bool, name string) (Server, error) {
	servers, diags := Load(agentDir, cwd, trusted)
	for _, srv := range servers {
		if srv.Name == name {
			return srv, nil
		}
	}
	if len(diags) > 0 {
		return Server{}, fmt.Errorf("%s", diags[0].Error())
	}
	return Server{}, fmt.Errorf("mcp server %q not found", name)
}
