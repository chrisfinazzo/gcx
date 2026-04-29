## gcx kg entities describe

Show detailed info, insights, and summary for a single entity.

```
gcx kg entities describe [Type--Name] [flags]
```

### Options

```
      --env string         Environment scope
      --from string        Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help               help for describe
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string        Entity name
      --namespace string   Namespace scope
  -o, --output string      Output format. One of: json, yaml (default "json")
      --since string       Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)
      --site string        Site scope
      --to string          End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string        Entity type
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg entities](gcx_kg_entities.md)	 - Manage Knowledge Graph entities.

