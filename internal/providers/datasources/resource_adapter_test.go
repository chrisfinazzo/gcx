package datasources_test

import (
	"encoding/json"
	"testing"

	dsclient "github.com/grafana/gcx/internal/datasources"
	provds "github.com/grafana/gcx/internal/providers/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticDescriptor(t *testing.T) {
	desc := provds.StaticDescriptor()
	assert.Equal(t, "datasource.grafana.app", desc.GroupVersion.Group)
	assert.Equal(t, "v0alpha1", desc.GroupVersion.Version)
	assert.Equal(t, "DataSource", desc.Kind)
	assert.Equal(t, "datasource", desc.Singular)
	assert.Equal(t, "datasources", desc.Plural)
}

func TestProviderTypedRegistrations(t *testing.T) {
	regs := (&provds.Provider{}).TypedRegistrations()
	require.Len(t, regs, 1)

	reg := regs[0]
	assert.Equal(t, provds.StaticDescriptor().GroupVersionKind(), reg.GVK)
	// CONSTITUTION: every registration must carry a non-nil Schema, and a
	// non-nil Example for writable resources.
	require.NotNil(t, reg.Schema)
	require.NotNil(t, reg.Example)
	require.NotNil(t, reg.Factory)
}

func TestDatasourceSchemaIsValidEnvelope(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal(provds.DatasourceSchema(), &schema))

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	for _, key := range []string{"apiVersion", "kind", "metadata", "spec"} {
		assert.Contains(t, props, key)
	}
}

func TestDatasourceExampleIsValidManifest(t *testing.T) {
	var example map[string]any
	require.NoError(t, json.Unmarshal(provds.DatasourceExample(), &example))

	assert.Equal(t, provds.StaticDescriptor().GroupVersion.String(), example["apiVersion"])
	assert.Equal(t, "DataSource", example["kind"])
	require.Contains(t, example, "metadata")
	require.Contains(t, example, "spec")

	// The example must decode into the domain type so it stays a valid template.
	spec, err := json.Marshal(example["spec"])
	require.NoError(t, err)
	var ds dsclient.Datasource
	require.NoError(t, json.Unmarshal(spec, &ds))
	assert.Equal(t, "prometheus", ds.Type)
}
