package integrations

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &IntegrationsProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&IntegrationsProvider{})
}

// IntegrationsProvider lists the curated catalog of Grafana Cloud integrations.
// The catalog is static (embedded), so the command needs no cloud config or auth.
type IntegrationsProvider struct{}

func (p *IntegrationsProvider) Name() string { return "integrations" }

func (p *IntegrationsProvider) ShortDesc() string {
	return "List available Grafana Cloud integrations"
}

func (p *IntegrationsProvider) Commands() []*cobra.Command {
	integrationsCmd := &cobra.Command{
		Use:   "integrations",
		Short: p.ShortDesc(),
	}

	integrationsCmd.AddCommand(newListCommand())
	integrationsCmd.AddCommand(newDocsCommand())

	return []*cobra.Command{integrationsCmd}
}

func (p *IntegrationsProvider) Validate(_ map[string]string) error { return nil }

func (p *IntegrationsProvider) ConfigKeys() []providers.ConfigKey { return nil }

func (p *IntegrationsProvider) TypedRegistrations() []adapter.Registration { return nil }
