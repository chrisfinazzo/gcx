// Package irm — spike harness for ADR 001 (oncall-feature-expansion).
// All `spike_*.go` files in this package register a Cobra command builder via
// init() and the resulting tree is exposed under `gcx irm oncall spike <name>`.
//
// THIS IS A THROWAWAY POC. NO TESTS, NO ERROR HANDLING DISCIPLINE, NO
// ARCHITECTURAL CONCERNS. DELETE WHEN THE ADR IS BUILT.
package irm

import "github.com/spf13/cobra"

var spikeBuilders []func(OnCallConfigLoader) *cobra.Command

// registerSpikeBuilder is called from each spike file's init() to register
// itself. The builder is invoked once at provider bootstrap with the shared
// config loader.
func registerSpikeBuilder(b func(OnCallConfigLoader) *cobra.Command) {
	spikeBuilders = append(spikeBuilders, b)
}

func newSpikeCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "spike",
		Short:  "[POC] Quick-and-dirty validation commands for ADR 001 (oncall feature expansion).",
		Hidden: true,
	}
	for _, b := range spikeBuilders {
		cmd.AddCommand(b(loader))
	}
	return cmd
}
