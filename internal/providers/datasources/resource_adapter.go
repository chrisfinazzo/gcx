// Package datasources bridges Grafana datasources into the unified resources
// pipeline so they can be managed with `gcx resources get/pull/push/delete`.
//
// Unlike dashboards or folders, datasources are not exposed through Grafana's
// Kubernetes-compatible /apis surface on Grafana Cloud. This provider therefore
// backs a synthetic resource descriptor (datasource.grafana.app/v0alpha1,
// DataSource) with the legacy /api/datasources REST API via a TypedCRUD
// adapter, the same mechanism SLO and Synthetic Monitoring use.
package datasources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Compile-time guard that the datasource domain type satisfies the full
// ResourceIdentity contract (GetResourceName + SetResourceName), which TypedCRUD
// relies on for name extraction and restoration across round-trips.
var _ adapter.ResourceIdentity = &dsclient.Datasource{}

func init() { //nolint:gochecknoinits // Natural key registration for cross-stack push identity matching.
	adapter.RegisterNaturalKey(
		StaticDescriptor().GroupVersionKind(),
		adapter.SpecFieldKey("name"),
	)
}

// StaticDescriptor returns the resource descriptor for datasources.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "datasource.grafana.app",
			Version: "v0alpha1",
		},
		Kind:     "DataSource",
		Singular: "datasource",
		Plural:   "datasources",
	}
}

// DatasourceSchema returns a JSON Schema for the datasource resource type.
func DatasourceSchema() json.RawMessage {
	return adapter.SchemaFromType[dsclient.Datasource](StaticDescriptor())
}

// DatasourceExample returns an example datasource manifest as JSON.
func DatasourceExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": StaticDescriptor().GroupVersion.String(),
		"kind":       StaticDescriptor().Kind,
		"metadata": map[string]any{
			// metadata.name is the datasource UID — stable and user-chosen.
			"name": "my-prometheus",
		},
		"spec": map[string]any{
			"name":      "My Prometheus",
			"type":      "prometheus",
			"access":    "proxy",
			"url":       "https://prometheus.example.com",
			"isDefault": false,
			"jsonData": map[string]any{
				"httpMethod": "POST",
			},
			// secureJsonData is write-only and never returned on reads; include
			// it on push to set credentials such as passwords or API keys.
			"secureJsonData": map[string]any{
				"basicAuthPassword": "REDACTED",
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("providers/datasources: failed to marshal example: %v", err))
	}
	return b
}

// NewTypedCRUD creates a TypedCRUD for datasources backed by the legacy REST API.
func NewTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[dsclient.Datasource], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for datasources: %w", err)
	}

	client, err := dsclient.NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create datasource client: %w", err)
	}

	crud := &adapter.TypedCRUD[dsclient.Datasource]{
		ListFn: adapter.LimitedListFn(func(ctx context.Context) ([]dsclient.Datasource, error) {
			items, err := client.List(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]dsclient.Datasource, 0, len(items))
			for _, item := range items {
				out = append(out, *item)
			}
			return out, nil
		}),
		GetFn: func(ctx context.Context, name string) (*dsclient.Datasource, error) {
			ds, err := client.GetByUID(ctx, name)
			if err != nil {
				return nil, mapNotFound(name, err)
			}
			return ds, nil
		},
		CreateFn: func(ctx context.Context, ds *dsclient.Datasource) (*dsclient.Datasource, error) {
			warnIfSecretMissing(ds)
			created, err := client.Create(ctx, ds)
			if err != nil {
				return nil, fmt.Errorf("failed to create datasource: %w", err)
			}
			return created, nil
		},
		UpdateFn: func(ctx context.Context, name string, ds *dsclient.Datasource) (*dsclient.Datasource, error) {
			warnIfSecretMissing(ds)
			updated, err := client.Update(ctx, name, ds)
			if err != nil {
				return nil, fmt.Errorf("failed to update datasource %q: %w", name, err)
			}
			return updated, nil
		},
		DeleteFn: func(ctx context.Context, name string) error {
			return client.Delete(ctx, name)
		},
		Namespace:   cfg.Namespace,
		StripFields: []string{"uid", "readOnly"},
		Descriptor:  StaticDescriptor(),
	}
	return crud, cfg, nil
}

// NewLazyFactory returns an adapter.Factory that loads its config lazily from
// the default config file when invoked.
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		crud, _, err := NewTypedCRUD(ctx, &loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// mapNotFound translates a datasource API 404 into adapter.ErrNotFound so the
// push pipeline's upsert path can fall through to Create.
func mapNotFound(name string, err error) error {
	var apiErr *dsclient.APIError
	if errors.As(err, &apiErr) && apiErr.NotFound() {
		return fmt.Errorf("datasource %q: %w", name, adapter.ErrNotFound)
	}
	return err
}
