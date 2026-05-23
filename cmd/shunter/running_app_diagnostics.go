package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

func normalizeRunningAppDiagnosticsURL(raw, endpoint string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http", "https":
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	default:
		return "", fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("URL host is required")
	}
	endpoint = "/" + strings.TrimPrefix(endpoint, "/")
	switch cleanPath := path.Clean(parsed.Path); cleanPath {
	case ".", "/", endpoint:
		parsed.Path = endpoint
	default:
		if strings.HasSuffix(cleanPath, endpoint) {
			parsed.Path = cleanPath
		} else if strings.HasSuffix(cleanPath, "/subscribe") {
			base := strings.TrimSuffix(cleanPath, "/subscribe")
			parsed.Path = path.Join(base, endpoint)
		} else {
			parsed.Path = path.Join(cleanPath, endpoint)
		}
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func getRunningAppDiagnosticsJSON(target string, timeout time.Duration, allowStatus func(int) bool, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if allowStatus == nil {
		allowStatus = diagnosticsSuccessStatus
	}
	if !allowStatus(resp.StatusCode) {
		return diagnosticsHTTPStatusError{StatusCode: resp.StatusCode}
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode diagnostics JSON: %w", err)
	}
	return nil
}

func diagnosticsSuccessStatus(code int) bool {
	return code >= 200 && code < 300
}

func healthDiagnosticsStatus(code int) bool {
	return diagnosticsSuccessStatus(code) || code == http.StatusServiceUnavailable
}

type diagnosticsHTTPStatusError struct {
	StatusCode int
}

func (e diagnosticsHTTPStatusError) Error() string {
	return fmt.Sprintf("diagnostics endpoint returned HTTP %d", e.StatusCode)
}

func isDiagnosticsHTTPStatusError(err error) bool {
	var statusErr diagnosticsHTTPStatusError
	return errors.As(err, &statusErr)
}
