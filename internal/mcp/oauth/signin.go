package oauth

import (
	"context"
	"net/url"

	"github.com/Lowpower/pigo/internal/auth"
)

// SignIn runs dynamic registration or CIMD, then the authorization-code flow.
// insufficient_scope requests the union of the granted scope and the challenge.
func (c *Client) SignIn(ctx context.Context, server Server, req Request) error {
	if err := ctx.Err(); err != nil {
		return ErrSignInCancelled
	}
	if err := req.Settings.validate(); err != nil {
		return err
	}
	if _, err := storageKey(server.Name, server.URL); err != nil {
		return err
	}
	if err := c.signIn(ctx, server, req); err != nil {
		if ctx.Err() != nil {
			return ErrSignInCancelled
		}
		return err
	}
	return nil
}

func (c *Client) signIn(ctx context.Context, server Server, req Request) error {
	st, err := c.storeOrErr()
	if err != nil {
		return err
	}
	stored, ok, err := st.Load(server.Name, server.URL)
	if err != nil {
		return err
	}
	var cur *Record
	if ok {
		cur = &stored
	}
	stepUp := req.Challenge.Error == "insufficient_scope"
	if !stepUp && cur != nil && cur.Tokens != nil && cur.Tokens.RefreshToken != "" {
		if err := c.refresh(ctx, server); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ErrSignInCancelled
		}
	}
	d, err := c.discover(ctx, server.URL, req.Settings.AuthServerMetadataURL, req.Challenge.ResourceMetadataURL)
	if err != nil {
		return err
	}
	if err := d.supportsCode(); err != nil {
		return err
	}
	granted := ""
	if cur != nil && cur.Tokens != nil {
		granted = cur.Tokens.Scope
	}
	scope := requestedScope(req.Settings, granted, req.Challenge, d.Scopes)
	verifier, challenge, err := auth.GeneratePKCE()
	if err != nil {
		return err
	}
	state, err := randomState()
	if err != nil {
		return err
	}
	cb, err := c.listen(ctx, req.Settings, cur, state, d.Issuer)
	if err != nil {
		return err
	}
	defer cb.Close()
	redirect := cb.redirect
	client, err := c.clientFor(ctx, d, req.Settings, cur, redirect, scope)
	if err != nil {
		return err
	}
	if err := st.Update(server.Name, server.URL, func(existing *Record) (*Record, error) {
		next := Record{}
		if existing != nil {
			next = *existing
		}
		next.ClientID = client.id
		next.ClientSecret = client.secret
		next.TokenEndpointAuthMethod = client.method
		next.RedirectURI = redirect
		next.AuthServerMetadataURL = req.Settings.AuthServerMetadataURL
		next.Resource = d.Resource
		next.CodeVerifier = verifier
		next.OAuthState = state
		return &next, nil
	}); err != nil {
		return err
	}
	authURL, err := authorizationURL(d, client.id, redirect, challenge, state, scope, d.Resource)
	if err != nil {
		return err
	}
	req.Prompt.show(authURL)
	code, iss, err := cb.Wait(ctx)
	if err != nil {
		return err
	}
	// Do not call the token endpoint unless iss matches the discovered issuer.
	if err := matchResponseIssuer(d.Issuer, iss); err != nil {
		return err
	}
	tokens, exp, err := c.exchangeCode(ctx, d, client, redirect, verifier, code, scope, d.Resource)
	if err != nil {
		return err
	}
	return st.Update(server.Name, server.URL, func(existing *Record) (*Record, error) {
		next := Record{}
		if existing != nil {
			next = *existing
		}
		next.ClientID = client.id
		next.ClientSecret = client.secret
		next.TokenEndpointAuthMethod = client.method
		next.RedirectURI = redirect
		next.AuthServerMetadataURL = req.Settings.AuthServerMetadataURL
		next.Resource = d.Resource
		next.Tokens = &tokens
		next.TokensExpireAt = exp
		next.CodeVerifier = ""
		next.OAuthState = ""
		return &next, nil
	})
}

func (c *Client) clientFor(ctx context.Context, d discovery, settings Settings, stored *Record, redirect, scope string) (clientCreds, error) {
	if settings.ClientRegistration == "cimd" {
		if err := d.supportsCIMD(); err != nil {
			return clientCreds{}, err
		}
		return clientCreds{id: ClientMetadataURL, method: "none"}, nil
	}
	if settings.ClientID != "" {
		return clientCreds{
			id:     settings.ClientID,
			secret: settings.ClientSecret,
			method: selectMethod(d.AuthMethods, settings.ClientSecret),
		}, nil
	}
	if stored != nil && stored.ClientID != "" && stored.RedirectURI == redirect {
		method := stored.TokenEndpointAuthMethod
		if method == "" {
			method = selectMethod(d.AuthMethods, stored.ClientSecret)
		}
		return clientCreds{id: stored.ClientID, secret: stored.ClientSecret, method: method}, nil
	}
	return c.registerClient(ctx, d, redirect, settings.ClientName, scope)
}

func (c *Client) listen(ctx context.Context, settings Settings, stored *Record, state, issuer string) (*callbackServer, error) {
	port := settings.CallbackPort
	required := port != 0
	if port == 0 && stored != nil {
		port = portOf(stored.RedirectURI)
	}
	cb, err := listenCallback(ctx, port, state, issuer)
	if err != nil && !required && port != 0 {
		return listenCallback(ctx, 0, state, issuer)
	}
	return cb, err
}

func authorizationURL(d discovery, clientID, redirect, challenge, state, scope, resource string) (string, error) {
	u, err := url.Parse(d.AuthorizationEndpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("redirect_uri", redirect)
	q.Set("state", state)
	if scope != "" {
		q.Set("scope", scope)
	}
	if resource != "" {
		q.Set("resource", resource)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
