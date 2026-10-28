package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	openaiChatGPTDynamicClient = "dynamic_agent_client"
	openaiChatGPTAgentName     = "pigo"
	openaiChatGPTCallbackPort  = 1455
	openaiChatGPTRedirect      = "http://127.0.0.1:1455/auth/callback"
	openaiChatGPTCallbackPath  = "/auth/callback"
	openaiChatGPTResource      = "https://api.openai.com/v1"
	openaiChatGPTDirectScope   = "chatgpt.tokens.use.direct"
	openaiChatGPTScope         = "openid profile email offline_access resource.invoke " + openaiChatGPTDirectScope
	openaiChatGPTExpiryMargin  = 3 * time.Minute
)

var (
	openaiChatGPTAuthURL  = "https://auth.openai.com/api/accounts/authorize"
	openaiChatGPTTokenURL = "https://auth.openai.com/api/accounts/oauth/token"
	openaiChatGPTUUID     = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type openaiChatGPTOAuth struct{}

func (openaiChatGPTOAuth) Name() string         { return "OpenAI (ChatGPT subscription)" }
func (openaiChatGPTOAuth) LoginLabel() string   { return "Sign in with ChatGPT" }
func (openaiChatGPTOAuth) IsSubscription() bool { return true }

func (o openaiChatGPTOAuth) Login(ix Interaction) (Credential, error) {
	ctx := ix.ctx()
	deviceID, err := chatGPTDeviceID(ix)
	if err != nil {
		return Credential{}, err
	}
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return Credential{}, err
	}
	state, err := randomToken()
	if err != nil {
		return Credential{}, err
	}
	nonce, err := randomToken()
	if err != nil {
		return Credential{}, err
	}
	srv, err := startCallback(ctx, callbackHost(), openaiChatGPTCallbackPort, openaiChatGPTCallbackPath, state)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return Credential{}, fmt.Errorf("port %d is in use, probably by an unfinished login in another pigo session or by the Codex CLI", openaiChatGPTCallbackPort)
		}
		return Credential{}, err
	}
	defer srv.Close()
	q := url.Values{
		"client_id":             {openaiChatGPTDynamicClient},
		"agent_name_hint":       {openaiChatGPTAgentName},
		"ext_agent_host_id":     {"urn:uuid:" + deviceID},
		"response_type":         {"code"},
		"redirect_uri":          {openaiChatGPTRedirect},
		"resource":              {openaiChatGPTResource},
		"scope":                 {openaiChatGPTScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"nonce":                 {nonce},
	}
	notifyAuthURL(ix, openaiChatGPTAuthURL+"?"+q.Encode(),
		"Complete sign-in in your browser. If the callback does not complete, paste the final redirect URL here.")
	code, clientID, err := waitChatGPT(ix, srv, state)
	if err != nil {
		return Credential{}, err
	}
	if code == "" || clientID == "" {
		return Credential{}, fmt.Errorf("missing authorization code")
	}
	notifyProgress(ix, "Exchanging authorization code for tokens...")
	return exchangeChatGPTCode(ctx, code, verifier, clientID)
}

func (openaiChatGPTOAuth) Refresh(ctx context.Context, cred Credential) (Credential, error) {
	clientID := strings.TrimSpace(cred.extraString("clientId"))
	if clientID == "" {
		return Credential{}, fmt.Errorf("stored OpenAI OAuth credential does not contain an issued client ID; reconnect ChatGPT")
	}
	body, status, err := postForm(ctx, openaiChatGPTTokenURL, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {cred.Refresh},
		"resource":      {openaiChatGPTResource},
	})
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("openAI OAuth token request failed (%d): %s", status, body)
	}
	return parseChatGPTToken(body, clientID, false)
}

func (openaiChatGPTOAuth) ToAuth(cred Credential) (ModelAuth, error) {
	return ModelAuth{APIKey: cred.Access}, nil
}

func chatGPTDeviceID(ix Interaction) (string, error) {
	if ix.DeviceID == nil {
		return "", fmt.Errorf("sign in with ChatGPT requires a device ID (UUID) for this installation")
	}
	id, err := ix.DeviceID()
	if err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if !openaiChatGPTUUID.MatchString(id) {
		return "", fmt.Errorf("sign in with ChatGPT requires a device ID (UUID) for this installation")
	}
	return strings.ToLower(id), nil
}

func exchangeChatGPTCode(ctx context.Context, code, verifier, clientID string) (Credential, error) {
	body, status, err := postForm(ctx, openaiChatGPTTokenURL, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {openaiChatGPTRedirect},
		"resource":      {openaiChatGPTResource},
	})
	if err != nil {
		return Credential{}, err
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("openAI OAuth token request failed (%d): %s", status, body)
	}
	return parseChatGPTToken(body, clientID, true)
}

func parseChatGPTToken(body []byte, clientID string, requireIDToken bool) (Credential, error) {
	var data struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
		IDToken      string  `json:"id_token"`
		Scope        string  `json:"scope"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return Credential{}, err
	}
	if strings.TrimSpace(data.AccessToken) == "" {
		return Credential{}, fmt.Errorf("openAI OAuth token response has invalid access_token")
	}
	if strings.TrimSpace(data.RefreshToken) == "" {
		return Credential{}, fmt.Errorf("openAI OAuth token response has invalid refresh_token")
	}
	if strings.TrimSpace(data.Scope) == "" {
		return Credential{}, fmt.Errorf("openAI OAuth token response has invalid scope")
	}
	if data.ExpiresIn <= 0 {
		return Credential{}, fmt.Errorf("openAI OAuth token response has invalid expires_in")
	}
	scopes := strings.Fields(data.Scope)
	direct := false
	for _, scope := range scopes {
		if scope == openaiChatGPTDirectScope {
			direct = true
			break
		}
	}
	if !direct {
		return Credential{}, fmt.Errorf("openAI OAuth grant did not include %s", openaiChatGPTDirectScope)
	}
	if requireIDToken && strings.TrimSpace(data.IDToken) == "" {
		return Credential{}, fmt.Errorf("openAI OAuth token response did not contain an ID token")
	}
	c := Credential{
		Type:    TypeOAuth,
		Access:  data.AccessToken,
		Refresh: data.RefreshToken,
		Expires: time.Now().Add(time.Duration(data.ExpiresIn)*time.Second - openaiChatGPTExpiryMargin).UnixMilli(),
	}
	c = c.withExtra("clientId", clientID)
	c = c.withExtra("scopes", scopes)
	return c, nil
}

func parseChatGPTCallback(input, expectedState string) (code, clientID string, err error) {
	raw := strings.TrimSpace(input)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("paste the full callback URL from the browser")
	}
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() != fmt.Sprint(openaiChatGPTCallbackPort) || u.Path != openaiChatGPTCallbackPath {
		return "", "", fmt.Errorf("the pasted callback URL must start with %s", openaiChatGPTRedirect)
	}
	q := u.Query()
	if authErr := q.Get("error"); authErr != "" {
		return "", "", fmt.Errorf("ChatGPT authorization failed: %s", authErr)
	}
	code = q.Get("code")
	if code == "" {
		return "", "", fmt.Errorf("missing authorization code")
	}
	state := q.Get("state")
	if state == "" {
		return "", "", fmt.Errorf("missing OAuth state")
	}
	if state != expectedState {
		return "", "", fmt.Errorf("OAuth state mismatch")
	}
	clientID = strings.TrimSpace(q.Get("client_id"))
	if clientID == "" {
		return "", "", fmt.Errorf("openAI OAuth registration callback did not contain an issued client ID")
	}
	return code, clientID, nil
}

func waitChatGPT(ix Interaction, srv *callbackServer, state string) (code, clientID string, err error) {
	ctx := ix.ctx()
	type result struct {
		code, clientID string
		err            error
	}
	ch := make(chan result, 1)
	go func() {
		c, _, e := srv.Wait()
		if e != nil {
			ch <- result{err: e}
			return
		}
		if c == "" {
			return
		}
		id := strings.TrimSpace(srv.issuedClientID())
		if id == "" {
			ch <- result{err: fmt.Errorf("openAI OAuth registration callback did not contain an issued client ID")}
			return
		}
		ch <- result{code: c, clientID: id}
	}()
	if ix.Prompt != nil {
		go func() {
			input, e := ix.Prompt(Prompt{
				Type:        PromptManualCode,
				Message:     "Complete login in your browser, or paste the final redirect URL here:",
				Placeholder: openaiChatGPTRedirect,
			})
			send := func(r result) {
				select {
				case ch <- r:
				default:
				}
				srv.Cancel()
			}
			if e != nil {
				send(result{err: e})
				return
			}
			c, id, e := parseChatGPTCallback(input, state)
			if e != nil {
				send(result{err: e})
				return
			}
			send(result{code: c, clientID: id})
		}()
	}
	select {
	case <-ctx.Done():
		srv.Cancel()
		select {
		case r := <-ch:
			if r.err != nil || r.code != "" {
				return r.code, r.clientID, r.err
			}
		default:
		}
		return "", "", fmt.Errorf("login cancelled")
	case r := <-ch:
		return r.code, r.clientID, r.err
	}
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
