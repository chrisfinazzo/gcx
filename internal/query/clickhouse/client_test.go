package clickhouse_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestQuery_SendsRawSQLAndParsesFrame(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"results": {
				"A": {
					"frames": [{
						"schema": {
							"name": "A",
							"refId": "A",
							"fields": [
								{"name": "n", "type": "number", "typeInfo": {"frame": "uint64"}},
								{"name": "s", "type": "string", "typeInfo": {"frame": "string"}}
							]
						},
						"data": {"values": [[1, 2], ["a", "b"]]}
					}],
					"status": 200
				}
			}
		}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stacks-1",
	}
	client, err := clickhouse.NewClient(cfg)
	require.NoError(t, err)

	resp, err := client.Query(context.Background(), "ch-uid", clickhouse.QueryRequest{SQL: "SELECT * FROM t"})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the query body shape: rawSql + datasource + format=1.
	queries, _ := capturedBody["queries"].([]any)
	require.Len(t, queries, 1)
	q := queries[0].(map[string]any)
	assert.Equal(t, "SELECT * FROM t", q["rawSql"])
	assert.EqualValues(t, 1, q["format"])
	ds := q["datasource"].(map[string]any)
	assert.Equal(t, "grafana-clickhouse-datasource", ds["type"])
	assert.Equal(t, "ch-uid", ds["uid"])

	// Verify the parsed frame.
	require.Len(t, resp.Schema.Fields, 2)
	assert.Equal(t, "n", resp.Schema.Fields[0].Name)
	assert.Equal(t, "number", resp.Schema.Fields[0].Type)
	assert.Equal(t, 2, resp.RowCount())
}

func TestQuery_FallsBackToLegacyEndpointOn404(t *testing.T) {
	var k8sCalls, legacyCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/query.grafana.app/v0alpha1/namespaces/stacks-1/query":
			k8sCalls++
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		case "/api/ds/query":
			legacyCalls++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[]},"data":{"values":[]}}],"status":200}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stacks-1",
	}
	client, err := clickhouse.NewClient(cfg)
	require.NoError(t, err)

	_, err = client.Query(context.Background(), "ch-uid", clickhouse.QueryRequest{SQL: "SELECT 1"})
	require.NoError(t, err)
	assert.Equal(t, 1, k8sCalls, "K8s endpoint should be tried first")
	assert.Equal(t, 1, legacyCalls, "legacy /api/ds/query should be the fallback")
}

func TestQuery_ReturnsTypedAPIErrorOnEnvelopeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"DB::Exception: Syntax error","errorSource":"downstream","status":400}}}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stacks-1",
	}
	client, err := clickhouse.NewClient(cfg)
	require.NoError(t, err)

	_, err = client.Query(context.Background(), "ch-uid", clickhouse.QueryRequest{SQL: "SLECT"})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "clickhouse", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "downstream", apiErr.ErrorSource)
}
