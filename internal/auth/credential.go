package auth

import "encoding/json"

var knownCredentialFields = map[string]struct{}{
	"type": {}, "key": {}, "env": {}, "access": {}, "refresh": {}, "expires": {},
}

// MarshalJSON keeps extra OAuth fields (accountId, enterpriseUrl, …) on disk.
func (c Credential) MarshalJSON() ([]byte, error) {
	type alias Credential
	b, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}
	if len(c.Extra) == 0 {
		return b, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range c.Extra {
		if _, known := knownCredentialFields[k]; known {
			continue
		}
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

// UnmarshalJSON captures unknown fields into Extra.
func (c *Credential) UnmarshalJSON(b []byte) error {
	type alias Credential
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*c = Credential(a)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.Extra = map[string]any{}
	for k, v := range raw {
		if _, known := knownCredentialFields[k]; known {
			continue
		}
		var val any
		if err := json.Unmarshal(v, &val); err != nil {
			continue
		}
		c.Extra[k] = val
	}
	if len(c.Extra) == 0 {
		c.Extra = nil
	}
	return nil
}

func (c Credential) extraString(key string) string {
	if c.Extra == nil {
		return ""
	}
	v, ok := c.Extra[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func (c Credential) withExtra(key string, value any) Credential {
	out := c
	out.Extra = map[string]any{}
	for k, v := range c.Extra {
		out.Extra[k] = v
	}
	out.Extra[key] = value
	return out
}
