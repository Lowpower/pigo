package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var errReadOnly = errors.New("read-only credential storage cannot modify auth.json")

// Migrate copies oauth.json + settings.json apiKeys into auth.json when auth.json
// is missing. Ported from packages/coding-agent/src/migrations.ts migrateAuthToAuthJson.
func Migrate(agentDir string) []string {
	authPath := filepath.Join(agentDir, "auth.json")
	if _, err := os.Stat(authPath); err == nil {
		return nil
	}
	migrated := map[string]Credential{}
	var providers []string

	oauthPath := filepath.Join(agentDir, "oauth.json")
	if b, err := os.ReadFile(oauthPath); err == nil {
		var oauth map[string]map[string]any
		if json.Unmarshal(stripBOM(b), &oauth) == nil {
			for id, cred := range oauth {
				c := Credential{Type: TypeOAuth, Extra: map[string]any{}}
				for k, v := range cred {
					switch k {
					case "access":
						c.Access, _ = v.(string)
					case "refresh":
						c.Refresh, _ = v.(string)
					case "expires":
						c.Expires = jsonNumberMs(v)
					default:
						c.Extra[k] = v
					}
				}
				migrated[id] = c
				providers = append(providers, id)
			}
			_ = os.Rename(oauthPath, oauthPath+".migrated")
		}
	}

	settingsPath := filepath.Join(agentDir, "settings.json")
	if b, err := os.ReadFile(settingsPath); err == nil {
		var settings map[string]any
		if json.Unmarshal(stripBOM(b), &settings) == nil {
			if keys, ok := settings["apiKeys"].(map[string]any); ok {
				for id, v := range keys {
					if _, exists := migrated[id]; exists {
						continue
					}
					key, ok := v.(string)
					if !ok {
						continue
					}
					migrated[id] = Credential{Type: TypeAPIKey, Key: key}
					providers = append(providers, id)
				}
				delete(settings, "apiKeys")
				if out, err := json.MarshalIndent(settings, "", "  "); err == nil {
					_ = os.WriteFile(settingsPath, append(out, '\n'), 0o600)
				}
			}
		}
	}

	if len(migrated) == 0 {
		return nil
	}
	_ = os.MkdirAll(agentDir, 0o700)
	s := &Store{dir: agentDir}
	_ = s.saveUnlocked(migrated)
	return providers
}

func stripBOM(b []byte) []byte {
	s := string(b)
	s = strings.TrimPrefix(s, "\ufeff")
	return []byte(s)
}

func jsonNumberMs(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
