package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/models"
)

type providerBackup struct {
	id      string
	spec    models.ProviderSpec
	hadSpec bool
	auth    auth.Provider
	hadAuth bool
}

func (e *Engine) applyProviders() {
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		h.SetProviderHook(func(id string, args map[string]any, drop bool) {
			if drop {
				e.dropProvider(id)
				return
			}
			e.installProvider(h, id, args)
		})
		for _, p := range h.Providers() {
			e.installProvider(h, p.ID, p.Args)
		}
	}
}

func (e *Engine) dropAllProviders() {
	e.mu.Lock()
	ids := make([]string, 0, len(e.extProviderIDs))
	for id := range e.extProviderIDs {
		ids = append(ids, id)
	}
	e.mu.Unlock()
	for _, id := range ids {
		e.dropProvider(id)
	}
}

func (e *Engine) installProvider(h *ext.Host, id string, args map[string]any) {
	if id == "" {
		return
	}
	if args == nil {
		args = map[string]any{}
	}
	stream, _ := args["stream"].(bool)
	api, _ := args["api"].(string)
	if !stream && api != "" {
		if _, ok := ai.LookupAPI(api); !ok {
			fmt.Fprintf(os.Stderr, "pigo: extension provider %q: unknown api %q (skipped)\n", id, api)
			return
		}
	}
	if !stream && api == "" {
		fmt.Fprintf(os.Stderr, "pigo: extension provider %q: missing api (skipped)\n", id)
		return
	}

	e.mu.Lock()
	if e.extProviderIDs == nil {
		e.extProviderIDs = map[string]bool{}
	}
	if e.providerBackup == nil {
		e.providerBackup = map[string]providerBackup{}
	}
	if e.extStreams == nil {
		e.extStreams = map[string]ai.StreamFn{}
	}
	if !e.extProviderIDs[id] {
		spec, ok := models.LookupProvider(id)
		ap, aok := auth.Lookup(id)
		e.providerBackup[id] = providerBackup{id: id, spec: spec, hadSpec: ok, auth: ap, hadAuth: aok}
		e.extProviderIDs[id] = true
	}
	e.mu.Unlock()

	spec := parseProviderSpec(id, args)
	if asBool(args["refreshModels"]) {
		host := h
		spec.RefreshModels = func(store models.CatalogStore) error {
			list, err := host.RefreshModels(context.Background())
			if err != nil {
				return err
			}
			var modelsOut []models.Model
			for _, m := range list {
				modelsOut = append(modelsOut, modelFromMap(id, m))
			}
			if store != nil {
				_ = store.Write(id, models.StoreEntry{Models: modelsOut})
			}
			models.SetRemoteOverlay(id, modelsOut)
			return nil
		}
	}
	models.RegisterProvider(spec)

	key := expandEnvRef(asString(args["apiKey"]))
	headers := stringMap(args["headers"])
	baseURL := asString(args["baseUrl"])
	if stream {
		e.mu.Lock()
		e.extStreams[id] = h.Stream(id)
		e.mu.Unlock()
	} else {
		fn := ai.StreamWithAuth(id, key, baseURL, headers)
		e.mu.Lock()
		e.extStreams[id] = fn
		e.mu.Unlock()
	}

	if oauth, ok := args["oauth"].(map[string]any); ok && oauth != nil {
		auth.RegisterProvider(auth.Provider{
			ID:    id,
			OAuth: extOAuth{host: h, name: asString(oauth["name"]), sub: asBool(oauth["isSubscription"])},
		})
	} else if key != "" || asString(args["apiKey"]) != "" {
		envKey := asString(args["apiKey"])
		auth.RegisterProvider(auth.Provider{
			ID: id,
			APIKey: &auth.APIKeyHandler{
				Name: asString(args["name"]),
				Env:  envNames(envKey),
				Login: func(ix auth.Interaction) (auth.Credential, error) {
					if ix.Prompt == nil {
						return auth.Credential{}, fmt.Errorf("no prompt available")
					}
					got, err := ix.Prompt(auth.Prompt{Type: auth.PromptSecret, Message: id + " API key:"})
					if err != nil {
						return auth.Credential{}, err
					}
					return auth.Credential{Type: auth.TypeAPIKey, Key: got}, nil
				},
			},
		})
	}
}

func (e *Engine) dropProvider(id string) {
	e.mu.Lock()
	b, ok := e.providerBackup[id]
	delete(e.extProviderIDs, id)
	delete(e.extStreams, id)
	delete(e.providerBackup, id)
	e.mu.Unlock()
	if !ok {
		models.UnregisterProvider(id)
		auth.UnregisterProvider(id)
		return
	}
	if b.hadSpec {
		models.RegisterProvider(b.spec)
	} else {
		models.UnregisterProvider(id)
	}
	if b.hadAuth {
		auth.RegisterProvider(b.auth)
	} else {
		auth.UnregisterProvider(id)
	}
}

func parseProviderSpec(id string, args map[string]any) models.ProviderSpec {
	spec := models.ProviderSpec{
		ID:         id,
		BaseURL:    asString(args["baseUrl"]),
		DefaultAPI: asString(args["api"]),
	}
	if raw, ok := args["models"].([]any); ok {
		for _, item := range raw {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			md := modelFromMap(id, m)
			if md.ID == "" {
				continue
			}
			spec.Models = append(spec.Models, md)
			if spec.DefaultID == "" {
				spec.DefaultID = md.ID
			}
		}
	}
	return spec
}

func modelFromMap(provider string, m map[string]any) models.Model {
	md := models.Model{
		Provider: provider,
		ID:       asString(m["id"]),
		API:      asString(m["api"]),
		BaseURL:  asString(m["baseUrl"]),
	}
	if md.ID == "" {
		md.ID = asString(m["name"])
	}
	return md
}

func expandEnvRef(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		return os.Getenv(s[2 : len(s)-1])
	}
	if strings.HasPrefix(s, "$") && len(s) > 1 {
		return os.Getenv(s[1:])
	}
	return s
}

func envNames(ref string) []string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "${") && strings.HasSuffix(ref, "}") {
		return []string{ref[2 : len(ref)-1]}
	}
	if strings.HasPrefix(ref, "$") && len(ref) > 1 {
		return []string{ref[1:]}
	}
	return nil
}

func stringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return nil
	}
	out := map[string]string{}
	for k, val := range m {
		out[k] = fmt.Sprint(val)
	}
	return out
}

type extOAuth struct {
	host *ext.Host
	name string
	sub  bool
}

func (o extOAuth) Name() string         { return o.name }
func (o extOAuth) LoginLabel() string   { return o.name }
func (o extOAuth) IsSubscription() bool { return o.sub }

func (o extOAuth) Login(ix auth.Interaction) (auth.Credential, error) {
	ctx := ix.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := o.host.OAuth(ctx, "login", nil)
	if err != nil {
		return auth.Credential{}, err
	}
	return credFromOAuth(res), nil
}

func (o extOAuth) Refresh(ctx context.Context, cred auth.Credential) (auth.Credential, error) {
	res, err := o.host.OAuth(ctx, "refresh", credPayload(cred))
	if err != nil {
		return auth.Credential{}, err
	}
	out := credFromOAuth(res)
	if out.Access == "" {
		return cred, nil
	}
	return out, nil
}

func (o extOAuth) ToAuth(cred auth.Credential) (auth.ModelAuth, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if cred.Expires > 0 && time.Now().UnixMilli() >= cred.Expires {
		if next, err := o.Refresh(ctx, cred); err == nil {
			cred = next
		}
	}
	res, err := o.host.OAuth(ctx, "get_api_key", credPayload(cred))
	if err != nil {
		return auth.ModelAuth{}, err
	}
	key := asString(res["apiKey"])
	if key == "" {
		key = cred.Access
	}
	return auth.ModelAuth{APIKey: key}, nil
}

func credFromOAuth(res map[string]any) auth.Credential {
	c := auth.Credential{Type: auth.TypeOAuth, Access: asString(res["access"]), Refresh: asString(res["refresh"])}
	switch v := res["expires"].(type) {
	case float64:
		c.Expires = int64(v)
	case int64:
		c.Expires = v
	case int:
		c.Expires = int64(v)
	}
	return c
}

func credPayload(c auth.Credential) map[string]any {
	return map[string]any{
		"access":  c.Access,
		"refresh": c.Refresh,
		"expires": c.Expires,
	}
}

func (e *Engine) bindStream(provider string) ai.StreamFn {
	e.mu.Lock()
	fn := e.extStreams[provider]
	e.mu.Unlock()
	if fn != nil {
		return e.gatedStream(fn)
	}
	if sf := boundStream(e.Opts.AgentDir, provider); sf != nil {
		return e.gatedStream(sf)
	}
	return e.gatedStream(nil)
}

func (e *Engine) gatedStream(fn ai.StreamFn) ai.StreamFn {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		e.mu.Lock()
		stop := e.stopAfterTools
		e.stopAfterTools = false
		e.mu.Unlock()
		if stop {
			return ai.EmitMessage(ctx, &ai.AssistantMessage{
				Role:       ai.RoleAssistant,
				StopReason: ai.StopStop,
				Content:    []*ai.Content{},
			}), nil
		}
		return fn(ctx, req, opts)
	}
}
