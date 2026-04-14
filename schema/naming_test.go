package schema

import "testing"

func TestToSnakeCase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"Player", "player"},
		{"Score", "score"},
		{"A", "a"},
		{"PlayerSession", "player_session"},
		{"ExpiresAt", "expires_at"},
		{"PlayerID", "player_id"},
		{"GuildID", "guild_id"},
		{"HTTPServer", "http_server"},
		{"URL", "url"},
		{"ID", "id"},
		{"getHTTPSUrl", "get_https_url"},
	}
	for _, c := range cases {
		got := ToSnakeCase(c.in)
		if got != c.want {
			t.Errorf("ToSnakeCase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
