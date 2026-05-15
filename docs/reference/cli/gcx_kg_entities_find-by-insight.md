## gcx kg entities find-by-insight

Find entities with active insights matching the given rules.

### Synopsis

Find entities with active insights matching the given rules.

Each --property flag is a separate rule (ORed together); severities are ANDed
into every rule. Only the "name" property is supported today.

```
gcx kg entities find-by-insight [flags]
```

### Examples

```
  gcx kg entities find-by-insight --property name=~Saturation
  gcx kg entities find-by-insight --property name=ErrorRatioBreach --severity critical
  gcx kg entities find-by-insight --severity critical,warning --namespace mimir-prod-01
  gcx kg entities find-by-insight --type Namespace --property name=~Latency --since 1h
```

### Options

```
      --env string             Environment scope
      --from string            Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                   help for find-by-insight
      --namespace string       Namespace scope
      --property stringArray   Filter by insight property: name=value (exact) or name=~value (contains); repeatable, rules are ORed. Only "name" is supported today.
      --severity strings       Filter by insight severity: critical, warning, info (comma-separated)
      --since string           Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)
      --site string            Site scope
      --to string              End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string            Root entity type (e.g. Service, Namespace, Node) (default "Service")
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

* [gcx kg entities](gcx_kg_entities.md)	 - Manage Knowledge Graph entities.

