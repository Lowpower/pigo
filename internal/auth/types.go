package auth

import "context"

const (
	// TypeAPIKey is a stored literal or templated API key.
	TypeAPIKey = "api_key"
	// TypeOAuth is a stored OAuth credential.
	TypeOAuth = "oauth"
)

// Credential is one stored provider credential (auth.json value).
type Credential struct {
	Type    string            `json:"type"`
	Key     string            `json:"key,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Access  string            `json:"access,omitempty"`
	Refresh string            `json:"refresh,omitempty"`
	Expires int64             `json:"expires,omitempty"` // Unix ms
	Extra   map[string]any    `json:"-"`
}

// Info is non-secret credential metadata.
type Info struct {
	ProviderID string
	Type       string
}

// ModelAuth is request auth derived from a credential.
type ModelAuth struct {
	APIKey  string
	Headers map[string]string
	BaseURL string
}

// Result is resolved request auth plus a status label.
type Result struct {
	Auth   ModelAuth
	Env    map[string]string
	Source string
}

// Check is a side-effect-free availability result.
type Check struct {
	Source string
	Type   string
}

const (
	// PromptText is a plain-text login prompt.
	PromptText = "text"
	// PromptSecret is a hidden login prompt.
	PromptSecret = "secret"
	// PromptSelect is a multiple-choice login prompt.
	PromptSelect = "select"
	// PromptManualCode is a paste-the-redirect-URL prompt.
	PromptManualCode = "manual_code"
)

// SelectOption is one choice in a login prompt.
type SelectOption struct {
	ID          string
	Label       string
	Description string
}

// Prompt is shown during login (pi AuthPrompt).
type Prompt struct {
	Type        string
	Message     string
	Placeholder string
	Options     []SelectOption
}

const (
	// EventInfo is a generic login status line.
	EventInfo = "info"
	// EventAuthURL asks the UI to open a browser URL.
	EventAuthURL = "auth_url"
	// EventDeviceCode shows a device-code pair.
	EventDeviceCode = "device_code"
	// EventProgress is an in-flow status update.
	EventProgress = "progress"
)

// Event is a login notification (pi AuthEvent).
type Event struct {
	Type             string
	Message          string
	URL              string
	Instructions     string
	UserCode         string
	VerificationURI  string
	IntervalSeconds  int
	ExpiresInSeconds int
}

// Interaction is the login UI contract (pi AuthInteraction).
type Interaction struct {
	Ctx    context.Context
	Prompt func(Prompt) (string, error)
	Notify func(Event)
}

func (ix Interaction) ctx() context.Context {
	if ix.Ctx != nil {
		return ix.Ctx
	}
	return context.Background()
}

// OAuth is one provider's OAuth implementation.
type OAuth interface {
	Name() string
	LoginLabel() string
	IsSubscription() bool
	Login(ix Interaction) (Credential, error)
	Refresh(ctx context.Context, cred Credential) (Credential, error)
	ToAuth(cred Credential) (ModelAuth, error)
}

// APIKeyHandler is api-key login + resolve for one provider.
type APIKeyHandler struct {
	Name    string
	Login   func(ix Interaction) (Credential, error)
	Env     []string // ambient env vars, first match wins
	Resolve func() *Result
}

// Provider is auth configuration for one provider id.
type Provider struct {
	ID     string
	APIKey *APIKeyHandler
	OAuth  OAuth
}

// ResolveOpts controls getAuth / print-bearer-token.
type ResolveOpts struct {
	APIKey             string
	Env                map[string]string
	MinOAuthValidityMs int64
	NoRefresh          bool
}
