## gcx datasources clickhouse query

Execute a SQL query against a ClickHouse datasource

### Synopsis

Execute a SQL query against a ClickHouse datasource.

SQL is the ClickHouse SQL statement to execute, passed as a positional argument
or via --expr.
Datasource is resolved from -d flag or datasources.clickhouse in your context.

The query runs through Grafana's datasource proxy, so the plugin's macros
($__timeFilter(), $__fromTime, $__toTime, ...) expand against the --from/--to
window when provided. Without an explicit time range, a small default window
is sent and queries without macros run unaffected.

```
gcx datasources clickhouse query [SQL] [flags]
```

### Examples

```

  # Simple query using configured default datasource
  gcx datasources clickhouse query 'SELECT 1'

  # Multi-column query with explicit datasource UID
  gcx datasources clickhouse query -d UID 'SELECT number, number * 2 AS doubled FROM numbers(5)'

  # Query the last hour using $__timeFilter() macro
  gcx datasources clickhouse query -d UID \
    'SELECT toStartOfMinute(event_time) AS t, count() FROM events WHERE $__timeFilter(event_time) GROUP BY t ORDER BY t' \
    --since 1h

  # Output as JSON
  gcx datasources clickhouse query -d UID 'SELECT 1' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.clickhouse is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string        Duration before --to (or now if omitted); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources clickhouse](gcx_datasources_clickhouse.md)	 - Query ClickHouse datasources

