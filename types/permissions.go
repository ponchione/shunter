package types

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
		found := false
		for _, granted := range caller.Permissions {
			if granted == requiredPermission {
				found = true
				break
			}
		}
		if !found {
			return requiredPermission, true
		}
	}
	return "", false
}
