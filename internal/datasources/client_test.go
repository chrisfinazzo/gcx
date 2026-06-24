package datasources_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *datasources.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	client, err := datasources.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestList_ReturnsTypedAPIError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources", r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"access denied"}`))
	}))

	_, err := client.List(context.Background())
	require.Error(t, err)

	var apiErr *datasources.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "list datasources", apiErr.Operation)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, "access denied", apiErr.Message)
}

func TestGetByUID_ReturnsTypedNotFoundError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/uid/missing", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Datasource not found"}`))
	}))

	_, err := client.GetByUID(context.Background(), "missing")
	require.Error(t, err)

	var apiErr *datasources.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "get datasource", apiErr.Operation)
	assert.Equal(t, "missing", apiErr.Identifier)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, "Datasource not found", apiErr.Message)
	assert.True(t, apiErr.NotFound())
}

func TestCreate_PostsDatasourceAndParsesEnvelope(t *testing.T) {
	var gotBody datasources.Datasource
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/datasources", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		if !assert.NoError(t, json.Unmarshal(body, &gotBody)) {
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":7,"message":"Datasource added","datasource":{"uid":"new-ds","name":"New DS","type":"prometheus"}}`))
	}))

	created, err := client.Create(context.Background(), &datasources.Datasource{
		UID:            "new-ds",
		Name:           "New DS",
		Type:           "prometheus",
		SecureJSONData: map[string]string{"basicAuthPassword": "secret"},
	})
	require.NoError(t, err)
	assert.Equal(t, "new-ds", created.UID)
	assert.Equal(t, "New DS", created.Name)

	// The request body carries the write-only secret.
	assert.Equal(t, "secret", gotBody.SecureJSONData["basicAuthPassword"])
}

func TestUpdate_PutsToUIDPath(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/datasources/uid/my-ds", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"Datasource updated","datasource":{"uid":"my-ds","name":"Updated","type":"loki"}}`))
	}))

	updated, err := client.Update(context.Background(), "my-ds", &datasources.Datasource{UID: "my-ds", Name: "Updated", Type: "loki"})
	require.NoError(t, err)
	assert.Equal(t, "Updated", updated.Name)
	assert.Equal(t, "loki", updated.Type)
}

func TestDelete_DeletesUIDPath(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/datasources/uid/my-ds", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"Data source deleted"}`))
	}))

	require.NoError(t, client.Delete(context.Background(), "my-ds"))
}

func TestDelete_ReturnsTypedErrorOnFailure(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Data source not found"}`))
	}))

	err := client.Delete(context.Background(), "missing")
	require.Error(t, err)

	var apiErr *datasources.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "delete datasource", apiErr.Operation)
	assert.True(t, apiErr.NotFound())
}

func TestIdentityRoundTrip(t *testing.T) {
	ds := datasources.Datasource{UID: "abc"}
	assert.Equal(t, "abc", ds.GetResourceName())

	ds.SetResourceName("xyz")
	assert.Equal(t, "xyz", ds.UID)
	assert.Equal(t, "xyz", ds.GetResourceName())
}
