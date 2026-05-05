package cache

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/cache"
	"github.com/spf13/cobra"
)

// Command returns the `cache` command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the local gcx cache",
	}

	cmd.AddCommand(clearCmd())
	return cmd
}

func clearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Remove all cached data (query results, discovery, OpenAPI schemas)",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := cache.Clear()
			if err != nil {
				return err
			}
			if n == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "cache is already empty")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "cleared %d cached entries\n", n)
			}
			return nil
		},
	}
	return cmd
}
