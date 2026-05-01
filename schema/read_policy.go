package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// TableAccess describes external raw-read access for a table.
type TableAccess int

const (
	// TableAccessPrivate denies external raw SQL reads unless a later caller
	// path grants bypass privileges.
	TableAccessPrivate TableAccess = iota

	// TableAccessPublic allows external raw SQL reads without permission tags.
	TableAccessPublic

	// TableAccessPermissioned allows external raw SQL reads when all declared
	// permission tags are present.
	TableAccessPermissioned
)

// ErrInvalidTableReadPolicy reports malformed table read policy metadata.
var ErrInvalidTableReadPolicy = errors.New("invalid table read policy")

// ReadPolicy records external raw-read policy metadata for a table.
type ReadPolicy struct {
	Access      TableAccess `json:"access"`
	Permissions []string    `json:"permissions"`
}

// MarshalJSON emits a stable policy shape with an array-valued permissions
// field even when no permissions are required.
func (p ReadPolicy) MarshalJSON() ([]byte, error) {
	out := struct {
		Access      TableAccess `json:"access"`
		Permissions []string    `json:"permissions"`
	}{
		Access:      p.Access,
		Permissions: append([]string(nil), p.Permissions...),
	}
	if out.Permissions == nil {
		out.Permissions = []string{}
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads a detached policy value from schema exports and module
// contracts.
func (p *ReadPolicy) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "null" {
		return fmt.Errorf("%w: policy must be an object", ErrInvalidTableReadPolicy)
	}
	var raw struct {
		Access      json.RawMessage `json:"access"`
		Permissions json.RawMessage `json:"permissions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Access) == 0 {
		return fmt.Errorf("%w: access is required", ErrInvalidTableReadPolicy)
	}
	if len(raw.Permissions) == 0 {
		return fmt.Errorf("%w: permissions is required", ErrInvalidTableReadPolicy)
	}
	if string(raw.Permissions) == "null" {
		return fmt.Errorf("%w: permissions must be an array", ErrInvalidTableReadPolicy)
	}
	var in struct {
		Access      TableAccess `json:"access"`
		Permissions []string    `json:"permissions"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	*p = ReadPolicy{
		Access:      in.Access,
		Permissions: append([]string(nil), in.Permissions...),
	}
	return nil
}

// String returns the stable contract spelling for a table access mode.
func (a TableAccess) String() string {
	switch a {
	case TableAccessPrivate:
		return "private"
	case TableAccessPublic:
		return "public"
	case TableAccessPermissioned:
		return "permissioned"
	default:
		return fmt.Sprintf("TableAccess(%d)", a)
	}
}

// MarshalJSON emits the stable string representation used by schema exports
// and module contracts.
func (a TableAccess) MarshalJSON() ([]byte, error) {
	switch a {
	case TableAccessPrivate, TableAccessPublic, TableAccessPermissioned:
		return []byte(strconv.Quote(a.String())), nil
	default:
		return nil, fmt.Errorf("%w: access %d", ErrInvalidTableReadPolicy, a)
	}
}

// UnmarshalJSON accepts the stable string representation used by schema exports
// and module contracts.
func (a *TableAccess) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("%w: access must be a string", ErrInvalidTableReadPolicy)
	}
	switch value {
	case "private":
		*a = TableAccessPrivate
	case "public":
		*a = TableAccessPublic
	case "permissioned":
		*a = TableAccessPermissioned
	default:
		return fmt.Errorf("%w: access %q", ErrInvalidTableReadPolicy, value)
	}
	return nil
}

// ValidateReadPolicy verifies that policy metadata is internally consistent.
func ValidateReadPolicy(policy ReadPolicy) error {
	switch policy.Access {
	case TableAccessPrivate, TableAccessPublic:
		if len(policy.Permissions) != 0 {
			return fmt.Errorf("%w: %s read policy must not include permissions", ErrInvalidTableReadPolicy, policy.Access)
		}
	case TableAccessPermissioned:
		if len(policy.Permissions) == 0 {
			return fmt.Errorf("%w: permissioned read policy requires at least one permission", ErrInvalidTableReadPolicy)
		}
	default:
		return fmt.Errorf("%w: access %d", ErrInvalidTableReadPolicy, policy.Access)
	}

	seen := make(map[string]struct{}, len(policy.Permissions))
	for _, permission := range policy.Permissions {
		if strings.TrimSpace(permission) == "" {
			return fmt.Errorf("%w: permission must not be empty", ErrInvalidTableReadPolicy)
		}
		if _, exists := seen[permission]; exists {
			return fmt.Errorf("%w: duplicate permission %q", ErrInvalidTableReadPolicy, permission)
		}
		seen[permission] = struct{}{}
	}
	return nil
}

func copyReadPolicy(in ReadPolicy) ReadPolicy {
	return ReadPolicy{
		Access:      in.Access,
		Permissions: append([]string(nil), in.Permissions...),
	}
}

func normalizeReadPolicy(in ReadPolicy) ReadPolicy {
	out := copyReadPolicy(in)
	if out.Permissions == nil {
		out.Permissions = []string{}
	}
	return out
}
