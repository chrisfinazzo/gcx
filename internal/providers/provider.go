package providers

import (
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

// ConfigKey describes a single configuration key for a provider.
//
// This is a type alias for adapter.ConfigKey, not a distinct type. adapter
// cannot import this package (this package already imports adapter for
// Registration/TypedRegistrations, so the reverse import would cycle), so
// adapter.NewProvider's declarative Provider type defines ConfigKeys() in
// terms of adapter.ConfigKey directly. Aliasing here keeps that method's
// signature identical to this interface's ConfigKeys() []ConfigKey, letting
// *adapter.Provider structurally satisfy Provider without adapter importing
// this package. See adapter.ConfigKey's doc comment for the mirrored field
// documentation.
type ConfigKey = adapter.ConfigKey

// Provider defines the interface for a gcx provider.
// Providers extend gcx with commands for managing Grafana Cloud
// product resources (e.g., SLO, Synthetic Monitoring, OnCall).
type Provider interface {
	// Name returns the unique identifier for this provider.
	Name() string

	// ShortDesc returns a one-line description of the provider.
	ShortDesc() string

	// Commands returns the Cobra commands contributed by this provider.
	Commands() []*cobra.Command

	// Validate checks that the given provider configuration is valid.
	Validate(cfg map[string]string) error

	// ConfigKeys returns the configuration keys used by this provider,
	// including metadata about which keys are secrets.
	ConfigKeys() []ConfigKey

	// TypedRegistrations returns adapter registrations for resource types that
	// this provider exposes through the unified resources pipeline. Providers
	// that do not support resource adapters return nil. The returned
	// registrations are auto-registered by providers.Register().
	TypedRegistrations() []adapter.Registration
}
