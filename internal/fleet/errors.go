package fleet

import (
	"fmt"
	"io"
	"net/http"
)

// ReadErrorBody reads and returns the response body as a string for error messages.
func ReadErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "(could not read body)"
	}
	return string(body)
}

// HTTPError represents a non-2xx HTTP response from the Fleet Management API.
// It is returned by the instrumentation client when the server returns an
// unexpected HTTP status code, enabling typed error detection in converters.
type HTTPError struct {
	// Status is the HTTP status code.
	Status int
	// Path is the Connect endpoint path.
	Path string
	// Body is the trimmed response body (for diagnostics).
	Body string
}

func (e *HTTPError) Error() string {
	base := fmt.Sprintf("fleet: HTTP %d from %s: %s", e.Status, e.Path, e.Body)
	if hint := e.roleHint(); hint != "" {
		return base + " (" + hint + ")"
	}
	return base
}

// roleHint returns an actionable message for the auth/RBAC statuses the Grafana
// collector-app plugin proxy returns before it forwards to Fleet Management,
// distinguishing a missing-role denial from an FM request rejection. It is
// empty for statuses that originate from Fleet Management itself (e.g. 400/404/
// 409), whose body already carries an FM-specific diagnostic.
func (e *HTTPError) roleHint() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "the Grafana credential was rejected by the collector-app proxy; re-authenticate with `gcx login`"
	case http.StatusForbidden:
		return "insufficient Grafana role for this operation via the grafana-collector-app proxy: reads require Viewer, mutations require Grafana Admin"
	default:
		return ""
	}
}
