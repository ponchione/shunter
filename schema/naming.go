package schema

import "unicode"

// ToSnakeCase converts a CamelCase Go identifier to snake_case.
// Handles acronym boundaries: "PlayerID" → "player_id", "HTTPServer" → "http_server".
func ToSnakeCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var out []rune
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					// lowercase/digit → uppercase transition
					out = append(out, '_')
				} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
					// end of uppercase run before lowercase: "HTTPServer" → "http_server"
					out = append(out, '_')
				}
			}
			out = append(out, unicode.ToLower(r))
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}
