package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type discovery struct {
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	ResponseTypes         []string
	AuthMethods           []string
	CodeChallenges        []string
	CIMD                  bool
	Resource              string
	Scopes                []string
}

func (c *Client) discover(ctx context.Context, serverURL, metadataURL, resourceHint string) (discovery, error) {
	u, err := parseServerURL(serverURL)
	if err != nil {
		return discovery{}, err
	}
	prm, err := c.discoverPRM(ctx, u, resourceHint)
	if err != nil && resourceHint != "" {
		return discovery{}, err
	}
	if err != nil {
		prm = nil
	}
	var meta asMeta
	if metadataURL != "" {
		meta, err = c.loadASMetadata(ctx, metadataURL)
		if err != nil {
			return discovery{}, err
		}
	} else {
		asURL := originOf(u)
		if prm != nil && len(prm.Servers) > 0 {
			asURL = prm.Servers[0]
		}
		meta, err = c.discoverAS(ctx, asURL)
		if err != nil {
			return discovery{}, err
		}
	}
	resource, err := selectResource(u, prm)
	if err != nil {
		return discovery{}, err
	}
	d := discovery{
		Issuer:                meta.Issuer,
		AuthorizationEndpoint: meta.AuthorizationEndpoint,
		TokenEndpoint:         meta.TokenEndpoint,
		RegistrationEndpoint:  meta.RegistrationEndpoint,
		ResponseTypes:         meta.ResponseTypes,
		AuthMethods:           meta.AuthMethods,
		CodeChallenges:        meta.CodeChallenges,
		CIMD:                  meta.CIMD,
		Resource:              resource,
		Scopes:                nil,
	}
	if prm != nil {
		d.Scopes = prm.Scopes
	}
	if err := d.secure(); err != nil {
		return discovery{}, err
	}
	return d, nil
}

func (d *discovery) secure() error {
	var err error
	if d.AuthorizationEndpoint, err = secureEndpoint(d.AuthorizationEndpoint); err != nil {
		return err
	}
	if d.TokenEndpoint, err = secureEndpoint(d.TokenEndpoint); err != nil {
		return err
	}
	if d.RegistrationEndpoint != "" {
		if d.RegistrationEndpoint, err = secureEndpoint(d.RegistrationEndpoint); err != nil {
			return err
		}
	}
	return nil
}

func (d discovery) supportsCode() error {
	if len(d.ResponseTypes) > 0 && !contains(d.ResponseTypes, "code") {
		return errors.New("authorization server does not support authorization codes")
	}
	if len(d.CodeChallenges) > 0 && !contains(d.CodeChallenges, "S256") {
		return errors.New("authorization server does not support PKCE S256")
	}
	return nil
}

func (d discovery) supportsCIMD() error {
	if !d.CIMD || !contains(d.AuthMethods, "none") {
		return errors.New(`authorization server does not support client id metadata documents for public clients; remove oauth.clientRegistration "cimd"`)
	}
	return nil
}

func (c *Client) discoverPRM(ctx context.Context, server *url.URL, hinted string) (*protected, error) {
	if hinted != "" {
		status, body, err := c.getJSON(ctx, hinted)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("HTTP %d loading OAuth protected resource metadata", status)
		}
		return parsePRM(body)
	}
	first := originOf(server) + "/.well-known/oauth-protected-resource" + pathSuffix(server.Path)
	status, body, err := c.getJSON(ctx, first)
	if err != nil {
		return nil, err
	}
	if (status < 200 || status >= 300) && server.Path != "" && server.Path != "/" && isMiss(status) {
		status, body, err = c.getJSON(ctx, originOf(server)+"/.well-known/oauth-protected-resource")
		if err != nil {
			return nil, err
		}
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("HTTP %d loading OAuth protected resource metadata", status)
	}
	return parsePRM(body)
}

func (c *Client) discoverAS(ctx context.Context, issuer string) (asMeta, error) {
	issuerURL, err := httpURL(issuer, "authorization server issuer")
	if err != nil {
		return asMeta{}, err
	}
	for _, cand := range asCandidates(issuerURL) {
		status, body, err := c.getJSON(ctx, cand)
		if err != nil {
			return asMeta{}, err
		}
		if isMiss(status) {
			continue
		}
		if status < 200 || status >= 300 {
			return asMeta{}, fmt.Errorf("HTTP %d loading authorization server metadata from %s", status, cand)
		}
		meta, err := parseAS(body)
		if err != nil {
			return asMeta{}, err
		}
		if !sameIssuer(meta.Issuer, issuerURL) {
			return asMeta{}, &IssuerError{Expected: issuerURL, Got: meta.Issuer}
		}
		return meta, nil
	}
	return asMeta{}, errors.New("authorization server metadata was not found")
}

func (c *Client) loadASMetadata(ctx context.Context, rawURL string) (asMeta, error) {
	status, body, err := c.getJSON(ctx, rawURL)
	if err != nil {
		return asMeta{}, err
	}
	if status < 200 || status >= 300 {
		return asMeta{}, fmt.Errorf("HTTP %d loading authorization server metadata from %s", status, rawURL)
	}
	return parseAS(body)
}

func asCandidates(issuer string) []string {
	u, err := url.Parse(issuer)
	if err != nil {
		return nil
	}
	origin := originOf(u)
	suffix := pathSuffix(u.Path)
	out := []string{
		origin + "/.well-known/oauth-authorization-server" + suffix,
		origin + "/.well-known/openid-configuration" + suffix,
	}
	if suffix != "" {
		out = append(out, origin+suffix+"/.well-known/openid-configuration")
	}
	return out
}

func originOf(u *url.URL) string { return u.Scheme + "://" + u.Host }

func pathSuffix(pathname string) string {
	if pathname == "" || pathname == "/" {
		return ""
	}
	return strings.TrimSuffix(pathname, "/")
}

func isMiss(status int) bool {
	return (status >= 400 && status < 500) || status == 502
}

func selectResource(server *url.URL, meta *protected) (string, error) {
	if meta == nil || meta.Resource == "" {
		return "", nil
	}
	cfg, err := url.Parse(meta.Resource)
	if err != nil || cfg.Host == "" {
		return "", fmt.Errorf("invalid protected resource %q", meta.Resource)
	}
	if !strings.EqualFold(server.Scheme, cfg.Scheme) || !strings.EqualFold(server.Host, cfg.Host) {
		return "", fmt.Errorf("protected resource %s does not match MCP server %s", meta.Resource, server)
	}
	if !strings.HasPrefix(slashPath(server.Path), slashPath(cfg.Path)) {
		return "", fmt.Errorf("protected resource %s does not match MCP server %s", meta.Resource, server)
	}
	return meta.Resource, nil
}

func slashPath(p string) string {
	if p == "" {
		p = "/"
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
