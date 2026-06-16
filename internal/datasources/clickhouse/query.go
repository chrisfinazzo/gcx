// Package clickhouse builds the `gcx datasources clickhouse` subcommand tree.
package clickhouse

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

// QueryCmd returns the `query` subcommand for a ClickHouse datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var datasource string

	cmd := &cobra.Command{
		Use:   "query [SQL]",
		Short: "Execute a SQL query against a ClickHouse datasource",
		Long: `Execute a SQL query against a ClickHouse datasource.

SQL is the ClickHouse SQL statement to execute, passed as a positional argument
or via --expr.
Datasource is resolved from -d flag or datasources.clickhouse in your context.

The query runs through Grafana's datasource proxy, so the plugin's macros
($__timeFilter(), $__fromTime, $__toTime, ...) expand against the --from/--to
window when provided. Without an explicit time range, a small default window
is sent and queries without macros run unaffected.`,
		Example: `
  # Simple query using configured default datasource
  gcx datasources clickhouse query 'SELECT 1'

  # Multi-column query with explicit datasource UID
  gcx datasources clickhouse query -d UID 'SELECT number, number * 2 AS doubled FROM numbers(5)'

  # Query the last hour using $__timeFilter() macro
  gcx datasources clickhouse query -d UID \
    'SELECT toStartOfMinute(event_time) AS t, count() FROM events WHERE $__timeFilter(event_time) GROUP BY t ORDER BY t' \
    --since 1h

  # Output as JSON
  gcx datasources clickhouse query -d UID 'SELECT 1' -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			expr, err := shared.ResolveExpr(args, 0)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err != nil {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			} else {
				cfgCtx = fullCfg.GetCurrentContext()
			}

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "clickhouse")
			if err != nil {
				return err
			}

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "clickhouse"); err != nil {
				return err
			}

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := clickhouse.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := clickhouse.QueryRequest{
				SQL:   expr,
				Start: start,
				End:   end,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources clickhouse query -d UID 'SELECT 1' -o json`,
	}

	shared.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.clickhouse is configured)")

	return cmd
}
