package login

import (
	"fmt"
	"net/url"
	"strings"
)

const otlpGatewayHostPrefix = "otlp-gateway-"

// sigilEndpointFromOTLP derives the Sigil API endpoint from the OTLP gateway
// URL exposed by GCOM. Both endpoints share the same regional cluster token:
//
//	https://otlp-gateway-prod-eu-west-2.grafana.net/otlp
//	→ https://sigil-prod-eu-west-2.grafana.net
//
// It returns an error when the host does not match the expected gateway shape
// so the caller can fall back to asking the user for an explicit
// --sigil-endpoint instead of writing a guessed value.
func sigilEndpointFromOTLP(otlpURL string) (string, error) {
	trimmed := strings.TrimSpace(otlpURL)
	if trimmed == "" {
		return "", fmt.Errorf("OTLP endpoint is empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse OTLP URL %q: %w", otlpURL, err)
	}
	host := u.Hostname()
	if u.Scheme == "" || host == "" {
		return "", fmt.Errorf("OTLP URL %q is not an absolute URL", otlpURL)
	}
	if !strings.HasPrefix(host, otlpGatewayHostPrefix) {
		return "", fmt.Errorf(
			"cannot derive Sigil endpoint from OTLP host %q (expected %q prefix); pass --sigil-endpoint",
			host, otlpGatewayHostPrefix,
		)
	}
	sigilHost := "sigil-" + strings.TrimPrefix(host, otlpGatewayHostPrefix)
	return u.Scheme + "://" + sigilHost, nil
}
