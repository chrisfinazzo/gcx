package k6 //nolint:testpackage // Tests exercise unexported cache helpers and CloudConfigLoader directly.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// mockCloudConfigLoader implements CloudConfigLoader for testing.
type mockCloudConfigLoader struct {
	cloudCfg    providers.CloudRESTConfig
	cloudErr    error
	grafanaCfg  config.NamespacedRESTConfig
	grafanaErr  error
	providerCfg map[string]string
	saved       map[string]string
	saveErr     error
}

func (m *mockCloudConfigLoader) LoadCloudConfig(_ context.Context) (providers.CloudRESTConfig, error) {
	return m.cloudCfg, m.cloudErr
}

func (m *mockCloudConfigLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return m.grafanaCfg, m.grafanaErr
}

func (m *mockCloudConfigLoader) LoadProviderConfig(_ context.Context, _ string) (map[string]string, string, error) {
	return m.providerCfg, "", nil
}

func (m *mockCloudConfigLoader) SaveProviderConfig(_ context.Context, _, key, value string) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	if m.saved == nil {
		m.saved = make(map[string]string)
	}
	m.saved[key] = value
	return nil
}

// TestLoadCache verifies cache hit/miss logic across all edge cases.
func TestLoadCache(t *testing.T) {
	tests := []struct {
		name           string
		cfg            map[string]string
		currentStackID int
		wantOk         bool
		wantToken      string
		wantOrgID      int
	}{
		{
			name: "hit: all fields match stack",
			cfg: map[string]string{
				"cached-token":    "tok",
				"cached-org-id":   "42",
				"cached-stack-id": "999",
			},
			currentStackID: 999,
			wantOk:         true,
			wantToken:      "tok",
			wantOrgID:      42,
		},
		{
			name:           "miss: nil map",
			cfg:            nil,
			currentStackID: 999,
			wantOk:         false,
		},
		{
			name: "miss: empty token",
			cfg: map[string]string{
				"cached-token":    "",
				"cached-org-id":   "42",
				"cached-stack-id": "999",
			},
			currentStackID: 999,
			wantOk:         false,
		},
		{
			name: "miss: stack mismatch",
			cfg: map[string]string{
				"cached-token":    "tok",
				"cached-org-id":   "42",
				"cached-stack-id": "888",
			},
			currentStackID: 999,
			wantOk:         false,
		},
		{
			name: "miss: non-numeric org ID",
			cfg: map[string]string{
				"cached-token":    "tok",
				"cached-org-id":   "bad",
				"cached-stack-id": "999",
			},
			currentStackID: 999,
			wantOk:         false,
		},
		{
			name: "miss: non-numeric cached stack ID",
			cfg: map[string]string{
				"cached-token":    "tok",
				"cached-org-id":   "42",
				"cached-stack-id": "bad",
			},
			currentStackID: 999,
			wantOk:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, orgID, ok := loadCache(tt.cfg, tt.currentStackID)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantToken, token)
				assert.Equal(t, tt.wantOrgID, orgID)
			}
		})
	}
}

// TestPersistCache_SavesAllThreeKeys verifies that persistCache writes all three cache fields.
func TestPersistCache_SavesAllThreeKeys(t *testing.T) {
	loader := &mockCloudConfigLoader{}
	persistCache(context.Background(), loader, "tok-xyz", 42, 999)
	assert.Equal(t, "tok-xyz", loader.saved["cached-token"])
	assert.Equal(t, "42", loader.saved["cached-org-id"])
	assert.Equal(t, "999", loader.saved["cached-stack-id"])
}

// TestClearCache_ClearsAllThreeKeys verifies that clearCache zeroes all three cache fields.
func TestClearCache_ClearsAllThreeKeys(t *testing.T) {
	loader := &mockCloudConfigLoader{}
	clearCache(context.Background(), loader)
	assert.Empty(t, loader.saved["cached-token"])
	assert.Empty(t, loader.saved["cached-org-id"])
	assert.Empty(t, loader.saved["cached-stack-id"])
}

// TestAuthenticatedClient_CachePath verifies that a valid cache hit skips the HTTP exchange.
func TestAuthenticatedClient_CachePath(t *testing.T) {
	loader := &mockCloudConfigLoader{
		cloudCfg: providers.CloudRESTConfig{
			Stack:     cloud.StackInfo{ID: 999},
			Namespace: "stack-999",
		},
		grafanaCfg: config.NamespacedRESTConfig{
			Config: rest.Config{BearerToken: "glsa_test"},
		},
		providerCfg: map[string]string{
			"api-domain":      "http://unused-because-cache-hit",
			"cached-token":    "cached-v3-token",
			"cached-org-id":   "42",
			"cached-stack-id": "999",
		},
	}

	client, ns, err := authenticatedClient(context.Background(), loader)
	require.NoError(t, err)
	assert.Equal(t, "stack-999", ns)
	assert.Equal(t, "cached-v3-token", client.Token())
	assert.Equal(t, 42, client.OrgID())
	// SaveProviderConfig must NOT have been called (cache was hit, no persist needed)
	assert.Empty(t, loader.saved)
}

// startServer creates a test server that handles PUT /v3/account/grafana-app/start
// and returns the given token and orgID.
func startServer(t *testing.T, token string, orgID string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == authPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id":  orgID,
				"v3_grafana_token": token,
			})
			return
		}
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAuthenticatedClient_ColdPath verifies the exchange path when no cache exists.
func TestAuthenticatedClient_ColdPath(t *testing.T) {
	srv := startServer(t, "fresh-k6-token", "42")

	loader := &mockCloudConfigLoader{
		cloudCfg: providers.CloudRESTConfig{
			Stack:     cloud.StackInfo{ID: 999},
			Namespace: "stack-999",
		},
		grafanaCfg: config.NamespacedRESTConfig{
			Config: rest.Config{BearerToken: "glsa_test_bearer"},
		},
		providerCfg: map[string]string{
			"api-domain": srv.URL,
			// no cached-token, cached-org-id, cached-stack-id
		},
	}

	client, ns, err := authenticatedClient(context.Background(), loader)
	require.NoError(t, err)
	assert.Equal(t, "stack-999", ns)
	assert.Equal(t, "fresh-k6-token", client.Token())
	assert.Equal(t, 42, client.OrgID())
	// persistCache must have been called with the new credentials
	assert.Equal(t, "fresh-k6-token", loader.saved["cached-token"])
	assert.Equal(t, "42", loader.saved["cached-org-id"])
	assert.Equal(t, "999", loader.saved["cached-stack-id"])
}

// TestAuthenticatedClient_MissingBearerToken verifies that an empty BearerToken returns a helpful error.
func TestAuthenticatedClient_MissingBearerToken(t *testing.T) {
	loader := &mockCloudConfigLoader{
		cloudCfg: providers.CloudRESTConfig{
			Stack:     cloud.StackInfo{ID: 999},
			Namespace: "stack-999",
		},
		grafanaCfg: config.NamespacedRESTConfig{}, // BearerToken is empty
		providerCfg: map[string]string{
			"api-domain": "http://unused",
		},
	}

	_, _, err := authenticatedClient(context.Background(), loader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "grafana.token is required")
}

// TestAuthenticatedClient_StaleCache verifies that a cache bound to a different stack causes a fresh exchange.
func TestAuthenticatedClient_StaleCache(t *testing.T) {
	srv := startServer(t, "fresh-stale-token", "42")

	loader := &mockCloudConfigLoader{
		cloudCfg: providers.CloudRESTConfig{
			Stack:     cloud.StackInfo{ID: 999},
			Namespace: "stack-999",
		},
		grafanaCfg: config.NamespacedRESTConfig{
			Config: rest.Config{BearerToken: "glsa_test"},
		},
		providerCfg: map[string]string{
			"api-domain":      srv.URL,
			"cached-token":    "stale-token",
			"cached-org-id":   "42",
			"cached-stack-id": "888", // different stack
		},
	}

	client, _, err := authenticatedClient(context.Background(), loader)
	require.NoError(t, err)
	// Should have exchanged and gotten fresh credentials, not the stale ones
	assert.NotEqual(t, "stale-token", client.Token())
	// Cache should be updated for the new stack
	assert.Equal(t, "999", loader.saved["cached-stack-id"])
}
