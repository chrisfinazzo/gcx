package datasources_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/grafana/gcx/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeDatasourceCommand(t *testing.T, args []string) (string, error) {
	t.Helper()
	stdout, _, err := executeDatasourceCommandStreams(t, args)
	return stdout, err
}

func executeDatasourceCommandStreams(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	root := helperRoot(datasources.Command())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	err := root.Execute()
	if err != nil {
		t.Logf("stderr: %s", stderr.String())
	}
	return stdout.String(), stderr.String(), err
}

// newDatasourceServer serves the given items from /api/datasources. It mimics a
// Grafana instance where app-platform discovery (/apis) is unavailable, so the
// dual transport falls back to the REST API.
func newDatasourceServer(t *testing.T, items []map[string]any) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/bootdata":
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == "/apis":
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == "/api/datasources":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(items); err != nil {
				t.Errorf("encode datasources response: %v", err)
			}
			return
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
}

// newDatasourceListServer serves count generated prometheus datasources.
func newDatasourceListServer(t *testing.T, count int) *httptest.Server {
	t.Helper()

	items := make([]map[string]any, 0, count)
	for i := range count {
		items = append(items, map[string]any{
			"uid":       fmt.Sprintf("ds-%02d", i),
			"name":      fmt.Sprintf("Datasource %02d", i),
			"type":      "prometheus",
			"url":       "https://example.com",
			"access":    "proxy",
			"isDefault": i == 0,
			"readOnly":  false,
		})
	}

	return newDatasourceServer(t, items)
}

func TestListNameFilter(t *testing.T) {
	items := []map[string]any{
		{"uid": "prom-prod-eu", "name": "prometheus-prod-eu", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "prom-prod-us", "name": "prometheus-prod-us", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "prom-dev", "name": "prometheus-dev", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "loki-prod-eu", "name": "loki-prod-eu", "type": "loki", "url": "https://example.com", "access": "proxy"},
	}

	server := newDatasourceServer(t, items)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)

	listUIDs := func(t *testing.T, args ...string) []string {
		t.Helper()
		base := append([]string{"datasources", "list", "--config", configFile, "--limit", "0", "-o", "json"}, args...)
		stdout, err := executeDatasourceCommand(t, base)
		require.NoError(t, err)

		var result struct {
			Datasources []struct {
				UID string `json:"uid"`
			} `json:"datasources"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &result))
		uids := make([]string, len(result.Datasources))
		for i, ds := range result.Datasources {
			uids[i] = ds.UID
		}
		return uids
	}

	t.Run("empty name does not filter", func(t *testing.T) {
		assert.Len(t, listUIDs(t), 4)
	})

	t.Run("substring match across types", func(t *testing.T) {
		assert.ElementsMatch(t, []string{"prom-prod-eu", "prom-prod-us", "loki-prod-eu"}, listUIDs(t, "--name", "prod"))
	})

	t.Run("case-insensitive match", func(t *testing.T) {
		assert.ElementsMatch(t, []string{"prom-prod-eu", "loki-prod-eu"}, listUIDs(t, "--name", "PROD-EU"))
	})

	t.Run("composes with type filter (AND)", func(t *testing.T) {
		assert.ElementsMatch(t, []string{"prom-prod-eu", "prom-prod-us"}, listUIDs(t, "--type", "prometheus", "--name", "prod"))
	})

	t.Run("no match returns empty", func(t *testing.T) {
		assert.Empty(t, listUIDs(t, "--name", "does-not-exist"))
	})
}

func TestListNameFilterRespectsLimit(t *testing.T) {
	items := []map[string]any{
		{"uid": "prom-prod-eu", "name": "prometheus-prod-eu", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "prom-prod-us", "name": "prometheus-prod-us", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "prom-dev", "name": "prometheus-dev", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
	}

	server := newDatasourceServer(t, items)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--name", "prod", "--limit", "1", "-o", "json"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	// Two datasources match "prod"; the limit trims the matched set to 1.
	assert.Len(t, result.Datasources, 1)
}

func TestListDefaultReturnsAllDatasources(t *testing.T) {
	server := newDatasourceListServer(t, 60)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--limit", "0", "-o", "json"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Len(t, result.Datasources, 60)
}

func TestListJSONFieldDiscoveryAndSelection(t *testing.T) {
	server := newDatasourceListServer(t, 2)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)

	t.Run("--json list discovers item fields", func(t *testing.T) {
		stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--json", "list"})
		require.NoError(t, err)

		for _, field := range []string{"uid", "name", "type", "url"} {
			assert.Contains(t, stdout, field, "item field %q must be discovered", field)
		}
		assert.NotContains(t, stdout, "datasources", "wrapper key must not be listed")
	})

	t.Run("--json uid,name selects per item", func(t *testing.T) {
		stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--json", "uid,name"})
		require.NoError(t, err)

		var result struct {
			Datasources []map[string]any `json:"datasources"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &result))
		require.Len(t, result.Datasources, 2)
		assert.Equal(t, "ds-00", result.Datasources[0]["uid"])
		assert.Equal(t, "Datasource 00", result.Datasources[0]["name"])
		assert.NotContains(t, result.Datasources[0], "type")
	})
}

func TestListExplicitLimitTrimsDatasources(t *testing.T) {
	server := newDatasourceListServer(t, 60)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, stderr, err := executeDatasourceCommandStreams(t, []string{"datasources", "list", "--config", configFile, "--limit", "10", "-o", "json"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
		ListMeta    *struct {
			Truncated bool   `json:"truncated"`
			Returned  int    `json:"returned"`
			Total     *int   `json:"total"`
			Continue  string `json:"continue"`
		} `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Len(t, result.Datasources, 10)

	// Truncation is machine-legible in the payload: the source is fully
	// fetched, so the total is the observed count.
	require.NotNil(t, result.ListMeta)
	assert.True(t, result.ListMeta.Truncated)
	assert.Equal(t, 10, result.ListMeta.Returned)
	require.NotNil(t, result.ListMeta.Total)
	assert.Equal(t, 60, *result.ListMeta.Total)
	assert.Equal(t, "gcx datasources list --limit 0", result.ListMeta.Continue)

	// ...and human-legible on stderr.
	assert.Contains(t, stderr, "hint: showing first 10 of 60: gcx datasources list --limit 0")
}

// TestListDefaultReturnsAllWithoutMeta locks the "cheaply complete source"
// default: no --limit means the full set, with no truncation metadata.
func TestListDefaultReturnsAllWithoutMeta(t *testing.T) {
	server := newDatasourceListServer(t, 60)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, stderr, err := executeDatasourceCommandStreams(t, []string{"datasources", "list", "--config", configFile, "-o", "json"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
		ListMeta    *map[string]any  `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Len(t, result.Datasources, 60)
	assert.Nil(t, result.ListMeta)
	assert.NotContains(t, stderr, "showing first")
}

// TestListAgentModeStructuredTruncation locks the agent-mode contract: a
// truncated page carries list_meta in the structured stdout payload itself,
// so an agent cannot mistake the page for the complete set.
func TestListAgentModeStructuredTruncation(t *testing.T) {
	t.Cleanup(agent.ResetForTesting) // runs after t.Setenv restores the env
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()

	server := newDatasourceListServer(t, 30)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	// No -o flag: agent mode defaults to the agents codec (compact JSON).
	stdout, stderr, err := executeDatasourceCommandStreams(t, []string{"datasources", "list", "--config", configFile, "--limit", "5"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
		ListMeta    *struct {
			Truncated bool `json:"truncated"`
			Returned  int  `json:"returned"`
			Total     *int `json:"total"`
		} `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Len(t, result.Datasources, 5)
	require.NotNil(t, result.ListMeta)
	assert.True(t, result.ListMeta.Truncated)
	assert.Equal(t, 5, result.ListMeta.Returned)
	require.NotNil(t, result.ListMeta.Total)
	assert.Equal(t, 30, *result.ListMeta.Total)

	// The stderr hint switches to the JSONL class:"hint" form in agent mode.
	assert.Contains(t, stderr, `{"class":"hint","summary":"showing first 5 of 30"`)
}
