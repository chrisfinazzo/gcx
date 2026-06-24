//nolint:testpackage // Internal test: exercises unexported helpers (secretLikelyRequired, mapNotFound).
package datasources

import (
	"net/http"
	"testing"

	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretLikelyRequired(t *testing.T) {
	tests := []struct {
		name string
		ds   dsclient.Datasource
		want bool
	}{
		{
			name: "public datasource without auth",
			ds:   dsclient.Datasource{Type: "prometheus"},
			want: false,
		},
		{
			name: "basic auth without secret",
			ds:   dsclient.Datasource{Type: "prometheus", BasicAuth: true},
			want: true,
		},
		{
			name: "basic auth with secret",
			ds:   dsclient.Datasource{Type: "prometheus", BasicAuth: true, SecureJSONData: map[string]string{"basicAuthPassword": "x"}},
			want: false,
		},
		{
			name: "secret-requiring type without secret",
			ds:   dsclient.Datasource{Type: "postgres"},
			want: true,
		},
		{
			name: "secret-requiring type with secret",
			ds:   dsclient.Datasource{Type: "postgres", SecureJSONData: map[string]string{"password": "x"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := tt.ds
			assert.Equal(t, tt.want, secretLikelyRequired(&ds))
		})
	}
}

func TestMapNotFound(t *testing.T) {
	notFound := dsclient.NewAPIError("get datasource", "x", http.StatusNotFound, []byte(`{"message":"nope"}`))
	require.ErrorIs(t, mapNotFound("x", notFound), adapter.ErrNotFound)

	forbidden := dsclient.NewAPIError("get datasource", "x", http.StatusForbidden, []byte(`{"message":"nope"}`))
	mapped := mapNotFound("x", forbidden)
	require.Error(t, mapped)
	assert.NotErrorIs(t, mapped, adapter.ErrNotFound)
}
