package schemads_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/gcx/internal/schemads"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *schemads.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	c, err := schemads.NewClient(cfg)
	require.NoError(t, err)
	return c
}

func TestFullSchema_HappyPath(t *testing.T) {
	var gotPath, gotMethod string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"fullSchema": {
				"tables": [
					{"name": "up", "columns": [{"name": "timestamp", "type": "datetime"}, {"name": "value", "type": "float64"}]}
				],
				"capabilities": {"aggregateFunctions": ["SUM", "COUNT"]}
			}
		}`))
	}))

	schema, err := client.FullSchema(context.Background(), "abc123")
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/api/datasources/uid/abc123/resources/abstractionSchema/fullSchema", gotPath)
	require.Len(t, schema.Tables, 1)
	assert.Equal(t, "up", schema.Tables[0].Name)
	require.NotNil(t, schema.Capabilities)
	assert.Equal(t, []string{"SUM", "COUNT"}, schema.Capabilities.AggregateFunctions)
}

func TestFullSchema_EscapesUID(t *testing.T) {
	var gotRawPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawPath = r.URL.RawPath
		if gotRawPath == "" {
			gotRawPath = r.URL.Path
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"fullSchema":{}}`))
	}))

	uid := "uid/../admin"
	_, err := client.FullSchema(context.Background(), uid)
	require.NoError(t, err)
	assert.Contains(t, gotRawPath, url.PathEscape(uid),
		"escaped uid must be preserved on the wire (RawPath=%q)", gotRawPath)
}

func TestFullSchema_ErrorBody(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"datasource not found"}`))
	}))

	_, err := client.FullSchema(context.Background(), "missing")
	require.Error(t, err)
	var apiErr *queryerror.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Contains(t, strings.ToLower(apiErr.Message), "datasource not found")
}

func TestFullSchema_NilFullSchemaIsEmpty(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	schema, err := client.FullSchema(context.Background(), "x")
	require.NoError(t, err)
	require.NotNil(t, schema)
	assert.Empty(t, schema.Tables)
}
