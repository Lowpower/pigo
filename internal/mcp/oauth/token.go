package oauth

import (
	"context"
	"net/http"
	"time"
)

// Token returns the current access token, refreshing it when it is about to expire.
// A cancelled ctx returns before the call is sent. A refresh that is already running
// still finishes and is saved.
func (c *Client) Token(ctx context.Context, server Server) (string, error) {
	return c.currentAccess(ctx, server)
}

// Authorize sets Authorization: Bearer from the current access token.
// A cancelled ctx returns an error and leaves req unchanged.
func (c *Client) Authorize(ctx context.Context, server Server, req *http.Request) error {
	if req == nil {
		return errNilRequest
	}
	access, err := c.currentAccess(ctx, server)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	return nil
}

// Call performs req with a bearer token. It does not send req when ctx is cancelled,
// including when cancellation wins a race with a refresh that later saves new tokens.
func (c *Client) Call(ctx context.Context, server Server, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errNilRequest
	}
	out := req.Clone(ctx)
	if err := c.Authorize(ctx, server, out); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.httpClient().Do(out)
}

// OnUnauthorized reacts to a 401. insufficient_scope does not refresh; the caller
// signs in again with the granted scope kept.
func (c *Client) OnUnauthorized(ctx context.Context, server Server, wwwAuthenticate string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ParseWWWAuthenticate(wwwAuthenticate).Error == "insufficient_scope" {
		return ErrAuthorizationRequired
	}
	err := c.refresh(context.WithoutCancel(ctx), server)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return ErrAuthorizationRequired
	}
	return nil
}

func (c *Client) currentAccess(ctx context.Context, server Server) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	st, err := c.storeOrErr()
	if err != nil {
		return "", err
	}
	rec, ok, err := st.Load(server.Name, server.URL)
	if err != nil {
		return "", err
	}
	if !ok || rec.Tokens == nil || rec.Tokens.AccessToken == "" {
		return "", ErrAuthorizationRequired
	}
	if !needsRefresh(rec, time.Now()) {
		return rec.Tokens.AccessToken, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.refresh(context.WithoutCancel(ctx), server)
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-done:
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	rec, ok, err = st.Load(server.Name, server.URL)
	if err != nil {
		return "", err
	}
	if !ok || rec.Tokens == nil || rec.Tokens.AccessToken == "" {
		return "", ErrAuthorizationRequired
	}
	return rec.Tokens.AccessToken, nil
}

func (c *Client) refresh(ctx context.Context, server Server) error {
	st, err := c.storeOrErr()
	if err != nil {
		return err
	}
	return st.Update(server.Name, server.URL, func(cur *Record) (*Record, error) {
		if cur == nil || cur.Tokens == nil || cur.Tokens.RefreshToken == "" {
			return nil, ErrAuthorizationRequired
		}
		tokens, exp, err := c.exchangeRefresh(ctx, server.URL, cur)
		if err != nil {
			return nil, err
		}
		next := *cur
		next.Tokens = &tokens
		next.TokensExpireAt = exp
		next.CodeVerifier = ""
		next.OAuthState = ""
		return &next, nil
	})
}
