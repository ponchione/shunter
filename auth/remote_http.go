package auth

import (
	"fmt"
	"net/http"
	"strings"
)

const maxRemoteAuthRedirects = 5

var remoteAuthHTTPClient = &http.Client{
	Transport: cloneDefaultHTTPTransport(),
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRemoteAuthRedirects {
			return fmt.Errorf("remote auth redirect limit exceeded: maximum %d", maxRemoteAuthRedirects)
		}
		if err := validateRemoteAuthURL(req.URL.String(), "remote auth redirect url"); err != nil {
			return err
		}
		if len(via) != 0 && strings.EqualFold(via[len(via)-1].URL.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
			return fmt.Errorf("remote auth redirect must not downgrade https to %s", req.URL.Scheme)
		}
		return nil
	},
}

func cloneDefaultHTTPTransport() http.RoundTripper {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return http.DefaultTransport
}
