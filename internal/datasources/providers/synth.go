package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/synth"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&synthDSProvider{})
}

type synthDSProvider struct{}

func (p *synthDSProvider) Kind() string      { return "synthetic-monitoring" }
func (p *synthDSProvider) Aliases() []string { return []string{"sm", "synth"} }
func (p *synthDSProvider) ShortDesc() string { return "Query Synthetic Monitoring datasources" }

// SM has no query verb — its surface is the probes/checks resource subcommands
// returned from ExtraCommands. Returning nil here keeps the command tree clean.
func (p *synthDSProvider) QueryCmd(_ *providers.ConfigLoader) *cobra.Command {
	return nil
}

func (p *synthDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		synth.ProbesCmd(loader),
		synth.ChecksCmd(loader),
	}
}
