package types

import "encoding/json"

// AuthClaims carries bounded, app-visible JWT claim values selected by runtime
// auth configuration. Values are compact JSON values, not original token bytes.
type AuthClaims struct {
	Values map[string]json.RawMessage
}

// Copy returns a detached copy of c.
func (c AuthClaims) Copy() AuthClaims {
	if len(c.Values) == 0 {
		return AuthClaims{}
	}
	out := AuthClaims{Values: make(map[string]json.RawMessage, len(c.Values))}
	for name, value := range c.Values {
		out.Values[name] = append(json.RawMessage(nil), value...)
	}
	return out
}

// Get returns a detached claim value by name.
func (c AuthClaims) Get(name string) (json.RawMessage, bool) {
	value, ok := c.Values[name]
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), value...), true
}
