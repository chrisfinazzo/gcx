## gcx kg entities

Manage Knowledge Graph entities.

### Synopsis

Manage Knowledge Graph entities.

Pick the read verb by what you start with:

  correlate  You have a firing alert (its labels) but not the entity → find
             which entity the alert hangs off. The "I have an alert, which
             entity is it?" entry point.
  list       You know the entity type (and maybe scope) → cheap identity +
             properties lookup (the labels used to build PromQL/Loki queries).
             The default for listing or basic lookups.
  inspect    You know the specific entity → heavy root-cause view: insight
             timeline + related entities. Don't use it just to read properties.
  query      You want arbitrary Cypher over the graph.

### Options

```
  -h, --help   help for entities
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights
* [gcx kg entities correlate](gcx_kg_entities_correlate.md)	 - Resolve the affected entities for a firing alert from its labels.
* [gcx kg entities create](gcx_kg_entities_create.md)	 - Create or update a custom entity (upsert) [experimental].
* [gcx kg entities delete](gcx_kg_entities_delete.md)	 - Delete a custom entity [experimental].
* [gcx kg entities inspect](gcx_kg_entities_inspect.md)	 - Show the insight timeline and related entities for a single entity (root-cause analysis).
* [gcx kg entities list](gcx_kg_entities_list.md)	 - List entities by type/scope, or look up an entity's identity and properties.
* [gcx kg entities query](gcx_kg_entities_query.md)	 - Query entities by running a read-only Cypher query against the Knowledge Graph.

