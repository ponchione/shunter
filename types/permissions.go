package types

import "slices"

// MissingRequiredPermission returns the first non-empty required permission not
// granted to caller. AllowAllPermissions bypasses the check.
func MissingRequiredPermission(caller CallerContext, required []string) (string, bool) {
	if caller.AllowAllPermissions {
		return "", false
	}
	for _, requiredPermission := range required {
		if requiredPermission == "" {
			continue
		}
		if !slices.Contains(caller.Permissions, requiredPermission) {
			return requiredPermission, true
		}
	}
	return "", false
}
