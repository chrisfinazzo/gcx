package datasources

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&Provider{})
}

// Provider bridges Grafana datasources into the unified resources pipeline.
// It contributes no commands of its own — the human-facing `gcx datasources`
// command tree is mounted separately — but registers a resource adapter so
// datasources can be managed declaratively via `gcx resources`.
type Provider struct{}

// Name returns the unique identifier for this provider.
func (p *Provider) Name() string { return "datasources" }

// ShortDesc returns a one-line description of the provider.
func (p *Provider) ShortDesc() string { return "Manage Grafana datasources as resources" }

// Commands returns the Cobra commands contributed by this provider. The
// datasource resource type is exposed through `gcx resources`, so this provider
// adds no commands of its own.
func (p *Provider) Commands() []*cobra.Command { return nil }

// Validate checks that the given provider configuration is valid. Datasources
// use Grafana's built-in authentication, so no extra keys are required.
func (p *Provider) Validate(cfg map[string]string) error { return nil }

// ConfigKeys returns the configuration keys used by this provider. Datasources
// use Grafana's built-in authentication and require no provider-specific keys.
func (p *Provider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns adapter registrations for the datasource resource type.
func (p *Provider) TypedRegistrations() []adapter.Registration {
	desc := StaticDescriptor()
	return []adapter.Registration{
		{
			Factory:     NewLazyFactory(),
			Descriptor:  desc,
			GVK:         desc.GroupVersionKind(),
			Schema:      DatasourceSchema(),
			Example:     DatasourceExample(),
			URLTemplate: "/connections/datasources/edit/{name}",
		},
	}
}
