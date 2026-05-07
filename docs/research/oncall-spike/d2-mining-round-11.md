# D2 Mining Round 11 — alert-groups command output capture

Captured: 2026-05-06. Binary commit: feat-missing-oncall-features worktree (c3e978ab/89f0ca64 lineage). Stack: ops.

---

## list — help text

```
List alert groups.

By default, lists root alert groups (excluding child groups merged into parents) in
firing, acknowledged, or silenced state. Resolved groups are excluded.

Use --all to bypass these defaults entirely (returns resolved and child groups too).
Use --state to override the status filter (e.g. --state firing,acknowledged).
Use --include-child-groups to keep the status default but include child groups.

Usage:
  gcx irm oncall alert-groups list [flags]

Flags:
      --all                    Bypass the default status and is_root filters (returns resolved groups and child groups too)
      --has-related-incident   Limit to alert groups linked to an incident
  -h, --help                   help for list
      --include-child-groups   Include child groups (drops the is_root filter while keeping the status default)
      --integration strings    Filter by integration PK (repeatable, comma-separated)
      --json string            Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --max-age string         Exclude groups older than this duration (e.g. 1h, 24h, 7d)
      --mine                   Limit to alert groups for the authenticated user
  -o, --output string          Output format. One of: json, table, wide, yaml (default "json")
      --state strings          Filter by state (firing|acknowledged|resolved|silenced; repeatable, comma-separated). Default: firing,acknowledged,silenced
      --team strings           Filter by team PK (repeatable, comma-separated)
      --with-resolution-note   Limit to alert groups that have a resolution note

Global Flags:
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).

Tip:
  Use --json list to discover available fields, --json field1,field2 to select specific fields.
```

Notes:
- Default output format: `json` (not `table`)
- Table and wide are available as explicit `-o` choices
- 5 columns in default table: ID, STATE, ALERTS, STARTED, TITLE

---

## list — default table (`-o table`)

Command: `./bin/gcx --context=ops irm oncall alert-groups list --max-age 24h -o table`

```
ID             STATE         ALERTS  STARTED           TITLE
IB298F1SP9W7A  firing        1       2026-05-06T15:04  TempoTraceByIDErrorBudgetBurn (prod-us-east-1, ...
IUQ1BCRQ7HBHT  firing        1       2026-05-06T15:04  KubePodOOMKilled (prod-ap-northeast-0, grafana-...
I7AGVJZBMZUN8  firing        1       2026-05-06T15:03  KubePodOOMKilled (prod-us-west-0, grafana-alert...
IJY7KSVEHI5VT  firing        1       2026-05-06T15:02  KubeDeploymentRolloutStuck (dev-us-central-0, c...
IEUWTR3LIY54L  firing        1       2026-05-06T15:00  KubeDeploymentRolloutStuck (dev-us-central-0, c...
IJBN9VW8Q8EL4  firing        1       2026-05-06T15:00  KubePodOOMKilled (prod-us-east-0, grafana-ruler)
IC1NY41NE64HK  firing        1       2026-05-06T14:59  KubeDeploymentRolloutStuck (dev-us-central-0, c...
IDB98TWJ2EC9T  firing        1       2026-05-06T14:53  KubePodOOMKilled (prod-us-east-2, grafana-alert...
IHPGI77QKBCAI  firing        3       2026-05-06T14:33  AlertmanagerAlertsRejected (prod-us-west-0)
I9BVD1CBUQ5QF  firing        2       2026-05-06T14:31  UndersizedMemory (prod-gb-south-1, loki-prod-035)
ISVMPYHA3BQIU  firing        11      2026-05-06T14:28  PyroscopePerTenantReadErrorBudgetBurn (ops-eu-s...
IJ75N8SA9W8SP  firing        1       2026-05-06T14:24  CloudDataArchiverSchemaExportFailed (prod-us-ce...
I87U169VQRRBZ  firing        1       2026-05-06T14:23  KubeJobFailed (dev-eu-west-6, assistant)
IQHUXGVHIPURP  firing        1       2026-05-06T14:22  ResourceNotUp-To-Date (prod-gb-south-1, sigil-p...
IWEUKK4QJ4PYM  firing        1       2026-05-06T14:21  MimirRolloutStuck (prod-us-east-2, grafana-aler...
IAJX2PYCL4QL7  firing        1       2026-05-06T14:19  Loki: Reads (High SLA) - Error Budget Burn Rate...
IHWW6S3VYSH4J  firing        1       2026-05-06T14:15  Incident
I2YFWXB4TR887  firing        1       2026-05-06T14:04  MimirRolloutStuck (dev-us-central-0, cortex-dev...
I419N69LG5TQW  acknowledged  1       2026-05-06T13:56  LokiDataObjectsIngestLatencyCritical (prod-us-e...
IS91V3DWMSR11  firing        1       2026-05-06T13:56  LokiDataObjectsIngestLatencyHigh (prod-us-east-...
IYAKZAFKX15IA  firing        1       2026-05-06T13:44  PrometheusNotConnectedToAlertmanagers (dev-us-c...
```

Columns (5): ID | STATE | ALERTS | STARTED | TITLE
- TITLE is truncated with `...` when it exceeds terminal width
- STARTED is truncated to minute precision (2026-05-06T15:04 — no seconds)
- No INTEGRATION, TEAM, SEVERITY, CLUSTER, SERVICE in default table

---

## list — wide table (`-o wide`)

Command: `./bin/gcx --context=ops irm oncall alert-groups list --max-age 24h -o wide`

```
ID             STATE         ALERTS  STARTED           INTEGRATION                                             TEAM                   TITLE
IX54PTHB1G46L  firing        1       2026-05-06T15:03  Prometheus Metamonitoring                               -                      AlwaysFiringAlert (prod-us-east-1, metamonitoring)
I7AGVJZBMZUN8  firing        1       2026-05-06T15:03  am-alerting-prod                                        Alerting               KubePodOOMKilled (prod-us-west-0, grafana-alertmanager)
IJY7KSVEHI5VT  firing        1       2026-05-06T15:02  am-mimir-canary-ingest                                  Mimir                  KubeDeploymentRolloutStuck (dev-us-central-0, cortex-dev-01)
IEUWTR3LIY54L  firing        1       2026-05-06T15:00  am-mimir-canary-ingest                                  Mimir                  KubeDeploymentRolloutStuck (dev-us-central-0, cortex-dev-01, kube-state-metrics)
IJBN9VW8Q8EL4  firing        1       2026-05-06T15:00  am-alerting-prod                                        Alerting               KubePodOOMKilled (prod-us-east-0, grafana-ruler)
IC1NY41NE64HK  firing        1       2026-05-06T14:59  am-mimir-canary-ingest                                  Mimir                  KubeDeploymentRolloutStuck (dev-us-central-0, cortex-dev-01)
IDB98TWJ2EC9T  firing        1       2026-05-06T14:53  am-alerting-prod                                        Alerting               KubePodOOMKilled (prod-us-east-2, grafana-alertmanager)
IHPGI77QKBCAI  firing        3       2026-05-06T14:33  am-alerting-prod                                        Alerting               AlertmanagerAlertsRejected (prod-us-west-0)
I9BVD1CBUQ5QF  firing        2       2026-05-06T14:31  alertmanager-loki-prod-shared                           loki_Ingest-Query      UndersizedMemory (prod-gb-south-1, loki-prod-035)
ISVMPYHA3BQIU  firing        11      2026-05-06T14:28  Incident label - support-urgent                         -                      PyroscopePerTenantReadErrorBudgetBurn (ops-eu-south-0, profiles-ops-002)
IJ75N8SA9W8SP  firing        1       2026-05-06T14:24  alertmanager-loki-prod-ingest                           loki_Ingest-Query      CloudDataArchiverSchemaExportFailed (prod-us-central-5, warning, loki-prod3)
I87U169VQRRBZ  firing        1       2026-05-06T14:23  am-assistant-dev                                        AI/ML                  KubeJobFailed (dev-eu-west-6, assistant)
IQHUXGVHIPURP  firing        1       2026-05-06T14:22  am-assistant                                            AI/ML                  ResourceNotUp-To-Date (prod-gb-south-1, sigil-prod-gb-south-1)
IWEUKK4QJ4PYM  firing        1       2026-05-06T14:21  am-alerting-prod                                        Alerting               MimirRolloutStuck (prod-us-east-2, grafana-alertmanager)
IAJX2PYCL4QL7  firing        1       2026-05-06T14:19  alertmanager-loki-prod-query                            loki_Ingest-Query      Loki: Reads (High SLA) - Error Budget Burn Rate is High (prod-eu-west-0, loki-prod-005)
IHWW6S3VYSH4J  firing        1       2026-05-06T14:15  k6-cloud-staging-stress-tests                           k6-cloud-backend       Incident
I2YFWXB4TR887  firing        1       2026-05-06T14:04  am-mimir-canary-ingest                                  Mimir                  MimirRolloutStuck (dev-us-central-0, cortex-dev-01)
I419N69LG5TQW  acknowledged  1       2026-05-06T13:56  alertmanager-loki-prod-ingest                           loki_Ingest-Query      LokiDataObjectsIngestLatencyCritical (prod-us-east-3, critical, loki-prod-042)
IS91V3DWMSR11  firing        1       2026-05-06T13:56  alertmanager-loki-prod-ingest                           loki_Ingest-Query      LokiDataObjectsIngestLatencyHigh (prod-us-east-3, notify, loki-prod-042)
```

Columns (7): ID | STATE | ALERTS | STARTED | INTEGRATION | TEAM | TITLE
- Delta from default: adds INTEGRATION (name) and TEAM (name)
- Some teams show `-` (no team assigned) — e.g. `Prometheus Metamonitoring` integration has no team
- INTEGRATION and TEAM are NOT truncated in wide mode — full names shown
- Still no SEVERITY, CLUSTER, SERVICE, or SLO info

---

## list — YAML output (first 30 lines, field ordering)

Command: `./bin/gcx --context=ops irm oncall alert-groups list --max-age 24h -o yaml`

```yaml
- apiVersion: oncall.ext.grafana.app/v1alpha1
  kind: AlertGroup
  metadata:
    creationTimestamp: "2026-05-06T15:03:27.467766Z"
    name: I7AGVJZBMZUN8
    namespace: stacks-27821
  spec:
    integration:
      id: CIX93MUQ79L4F
      name: am-alerting-prod
      type: alertmanager
    permalinks:
      slack: https://raintank-corp.slack.com/archives/C07CGC5SSSW/p1778079807829729
      slack_app: https://slack.com/app_redirect?channel=C07CGC5SSSW&team=T02S4RCS0&message=1778079807.829729
      web: https://ops.grafana-ops.net/a/grafana-irm-app/alert-groups/I7AGVJZBMZUN8
    team:
      id: T7BX6FGR3Y9IP
      name: Alerting
  status:
    alertsCount: 1
    state: firing
    timestamps:
      started: "2026-05-06T15:03:27.467766Z"
    title: KubePodOOMKilled (prod-us-west-0, grafana-alertmanager)
- apiVersion: oncall.ext.grafana.app/v1alpha1
  kind: AlertGroup
  metadata:
    creationTimestamp: "2026-05-06T15:02:15.105516Z"
    name: IJY7KSVEHI5VT
    namespace: stacks-27821
```

Field ordering analysis:
- Top-level keys: `apiVersion`, `kind`, `metadata`, `spec`, `status` — alphabetical order (a, k, m, sp, st)
- `spec` sub-keys: `integration`, `permalinks`, `team` — alphabetical (i, p, t)
- `status` sub-keys: `alertsCount`, `state`, `timestamps`, `title` — alphabetical (a, s, t, t)
- This confirms the list path retains `unstructured.Unstructured` and YAML marshaling produces alphabetical key ordering

This is a basic alert group (alertmanager integration type) — it lacks `severity`, `summary`, `target`, `links`, `runbookURL` in the status. Those fields only appear for enriched/SLO-driven groups (see IWDIPP8VLKENJ below).

---

## list — JSON output (first 100 lines, path discovery)

Command: `./bin/gcx --context=ops irm oncall alert-groups list --max-age 24h -o json | head -100`

```json
hint: use --json list to discover fields, --json field1,field2 to select — no external parsing needed
[
  {
    "apiVersion": "oncall.ext.grafana.app/v1alpha1",
    "kind": "AlertGroup",
    "metadata": {
      "creationTimestamp": "2026-05-06T15:03:27.467766Z",
      "name": "I7AGVJZBMZUN8",
      "namespace": "stacks-27821"
    },
    "spec": {
      "integration": {
        "id": "CIX93MUQ79L4F",
        "name": "am-alerting-prod",
        "type": "alertmanager"
      },
      "permalinks": {
        "slack": "https://raintank-corp.slack.com/archives/C07CGC5SSSW/p1778079807829729",
        "slack_app": "https://slack.com/app_redirect?channel=C07CGC5SSSW&team=T02S4RCS0&message=1778079807.829729",
        "web": "https://ops.grafana-ops.net/a/grafana-irm-app/alert-groups/I7AGVJZBMZUN8"
      },
      "team": {
        "id": "T7BX6FGR3Y9IP",
        "name": "Alerting"
      }
    },
    "status": {
      "alertsCount": 1,
      "state": "firing",
      "timestamps": {
        "started": "2026-05-06T15:03:27.467766Z"
      },
      "title": "KubePodOOMKilled (prod-us-west-0, grafana-alertmanager)"
    }
  },
  ...
]
```

Note: JSON output also uses alphabetical key ordering (same as YAML — both come from `unstructured.Unstructured`).

---

## list — JSON field discovery (`--json list`)

Command: `./bin/gcx --context=ops irm oncall alert-groups list --max-age 24h --json list`

```
apiVersion
kind
metadata
spec
spec.integration
spec.permalinks
spec.team
status
```

Note: Top-level `status` sub-fields (alertsCount, state, timestamps, title) are not discoverable individually — `status` is an opaque leaf in the field path list. This means `--json status.state` would need to be verified to work, or the full `status` object is selected.

---

## get — default output (JSON)

Command: `./bin/gcx --context=ops irm oncall alert-groups get IWDIPP8VLKENJ`

```json
hint: use --json list to discover fields, --json field1,field2 to select — no external parsing needed
{
  "apiVersion": "oncall.ext.grafana.app/v1alpha1",
  "kind": "AlertGroup",
  "metadata": {
    "name": "IWDIPP8VLKENJ",
    "namespace": "stacks-27821",
    "creationTimestamp": "2026-05-05T19:29:23.381143Z"
  },
  "spec": {
    "integration": {
      "id": "CR7MM8GWK6XCD",
      "name": "am-app-platform",
      "type": "alertmanager"
    },
    "team": {
      "id": "TKH52TW6TH7UE",
      "name": "App Platform"
    },
    "permalinks": {
      "web": "https://ops.grafana-ops.net/a/grafana-irm-app/alert-groups/IWDIPP8VLKENJ",
      "slack": "https://raintank-corp.slack.com/archives/C09NR2APMEJ/p1778009363731639",
      "slack_app": "https://slack.com/app_redirect?channel=C09NR2APMEJ&team=T02S4RCS0&message=1778009363.731639"
    }
  },
  "status": {
    "title": "Dashboard Service: Error Rate - Error Budget Burn Rate is Very High ",
    "summary": "Error budget is burning too fast due to a high rate of 5xx responses from the dashboard service.",
    "severity": "warning",
    "state": "resolved",
    "runbookURL": "https://github.com/grafana/deployment_tools/blob/master/docs/grafana-app-platform/runbooks.md#dashboard-service-error-rate",
    "target": {
      "cluster": "prod-us-east-0",
      "service": "dashboard-service"
    },
    "timestamps": {
      "started": "2026-05-05T19:29:23.381143Z",
      "acknowledged": "2026-05-05T19:32:47.471213Z",
      "resolved": "2026-05-06T08:49:23.434101Z"
    },
    "links": {
      "alert": {
        "rule": {
          "uid": "dfh3yvlw5owlcc",
          "url": "https://ops.grafana-ops.net/alerting/grafana/dfh3yvlw5owlcc/view"
        },
        "instance": {
          "id": "aca947af06950ed1"
        }
      },
      "dashboard": {
        "uid": "grafana_slo_app-vokcpl8zr3j0j12mm0o5y",
        "url": "https://ops.grafana-ops.net/d/st6qzlk/git-sync?var-cluster=prod-us-east-0&from=now-6h&to=now",
        "panel": {
          "id": 1
        }
      },
      "slo": {
        "uid": "vokcpl8zr3j0j12mm0o5y",
        "name": "Dashboard Service: Error Rate"
      }
    },
    "alertsCount": 5
  }
}
```

Notes:
- `get` uses a typed Go struct, so key ordering is struct-declared order (NOT alphabetical):
  - `spec` sub-keys: `integration`, `team`, `permalinks` (struct order: i, t, p — not alphabetical)
  - `status` sub-keys: `title`, `summary`, `severity`, `state`, `runbookURL`, `target`, `timestamps`, `links`, `alertsCount` (struct order)
- Rich enriched fields visible: `severity`, `summary`, `runbookURL`, `target.cluster`, `target.service`, `links.slo.uid`, `links.slo.name`, `links.dashboard.uid`, `links.alert.rule.uid`
- `timestamps` includes `acknowledged` and `resolved` (not just `started`)

---

## get — wide mode (`-o wide`)

Command: `./bin/gcx --context=ops irm oncall alert-groups get IWDIPP8VLKENJ -o wide`

```
stderr: {"error":{"summary":"Unknown output format 'wide'. Valid formats are","exitCode":1,"details":"json, yaml, yaml"}}
```

Exit code: 1. `get` does NOT support `-o wide`. Valid formats per help: `json, yaml` (note: `yaml` appears twice in the error details — likely a display bug).

---

## get — table mode (`-o table`)

Command: `./bin/gcx --context=ops irm oncall alert-groups get IWDIPP8VLKENJ -o table`

```
stderr: {"error":{"summary":"Unknown output format 'table'. Valid formats are","exitCode":1,"details":"json, yaml, yaml"}}
```

Exit code: 1. `get` does NOT support `-o table`. Only `json` and `yaml` are valid.

---

## get — help text

```
Get an alert group by ID.

Usage:
  gcx irm oncall alert-groups get <id> [flags]

Flags:
  -h, --help            help for get
      --include-raw     Include the unprocessed Alertmanager-shape payload under status.raw (hidden by default; the curated status.{target,links,...} blocks are the promoted view of the same data)
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, yaml, yaml (default "json")

Global Flags:
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).

Tip:
  Use --json list to discover available fields, --json field1,field2 to select specific fields.
```

Notes:
- Valid output formats: `json, yaml, yaml` (the `yaml` duplicate in the help text is a display artifact from `--output` flag definition)
- `--include-raw` flag exists to expose unprocessed Alertmanager payload
- No `table` or `wide` support on `get`

---

## list-alerts — help text

```
List individual alerts for an alert group.

Usage:
  gcx irm oncall alert-groups list-alerts <alert-group-id> [flags]

Flags:
  -h, --help            help for list-alerts
      --include-raw     Include the unprocessed Alertmanager-shape payload under status.raw on each alert (hidden by default; status.{target,links,...} are the promoted view of the same data)
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Cap on number of alerts retrieved (0 = no cap) (default 100)
  -o, --output string   Output format. One of: json, table, yaml (default "json")
      --slim            Skip per-alert retrieval; emit only metadata + alert-group back-pointer

Global Flags:
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).

Tip:
  Use --json list to discover available fields, --json field1,field2 to select specific fields.
```

Notes:
- Valid formats: `json, table, yaml` — has `table` but NOT `wide`
- `--slim` flag: skips per-alert retrieval, emits only metadata + back-pointer (empty `status: {}`)
- `--limit` default: 100

---

## list-alerts — default JSON (IWDIPP8VLKENJ, limit 5)

Command: `./bin/gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ --limit 5`

```json
hint: use --json list to discover fields, --json field1,field2 to select — no external parsing needed
[
  {
    "apiVersion": "oncall.ext.grafana.app/v1alpha1",
    "kind": "Alert",
    "metadata": {
      "name": "AC78DZJCPB22S",
      "namespace": "stacks-27821"
    },
    "spec": {
      "alertGroupID": "IWDIPP8VLKENJ"
    },
    "status": {
      "state": "resolved",
      "severity": "warning",
      "target": {
        "cluster": "prod-us-east-0",
        "service": "dashboard-service"
      },
      "links": {
        "alert": {
          "rule": {
            "uid": "dfh3yvlw5owlcc",
            "url": "https://ops.grafana-ops.net/alerting/grafana/dfh3yvlw5owlcc/view"
          },
          "instance": {
            "id": "aca947af06950ed1"
          }
        },
        "dashboard": {
          "uid": "grafana_slo_app-vokcpl8zr3j0j12mm0o5y",
          "url": "https://ops.grafana-ops.net/d/st6qzlk/git-sync?var-cluster=prod-us-east-0&from=now-6h&to=now",
          "panel": {
            "id": 1
          }
        },
        "slo": {
          "uid": "vokcpl8zr3j0j12mm0o5y",
          "name": "Dashboard Service: Error Rate"
        }
      }
    }
  },
  {
    "apiVersion": "oncall.ext.grafana.app/v1alpha1",
    "kind": "Alert",
    "metadata": {
      "name": "AV41BJT8JHYKU",
      ...
    },
    "spec": {
      "alertGroupID": "IWDIPP8VLKENJ"
    },
    "status": {
      "state": "firing",
      "severity": "warning",
      "target": { "cluster": "prod-us-east-0", "service": "dashboard-service" },
      "links": { ... (same shape as above) ... }
    }
  }
  ... (5 alerts total, same shape)
]
```

Notes:
- All 5 alerts for IWDIPP8VLKENJ (SLO-driven alertmanager group) have `severity`, `target`, `links.slo`, `links.dashboard`
- No per-alert `title` — titles live only at the group level
- No per-alert `timestamp` / `started` time
- Individual alert IDs are the `metadata.name` (A-prefixed IDs)

---

## list-alerts — wide mode (IWDIPP8VLKENJ, limit 5)

Command: `./bin/gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ --limit 5 -o wide`

```
stderr: {"error":{"summary":"Unknown output format 'wide'. Valid formats are","exitCode":1,"details":"json, table, yaml, yaml"}}
```

Exit code: 1. `list-alerts` does NOT support `-o wide`.

---

## list-alerts — table mode (IWDIPP8VLKENJ, limit 5)

Command: `./bin/gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ --limit 5 -o table`

```
stderr: {"error":{"summary":"Invalid data type for table codec","exitCode":1,"details":"expected []unstructured.Unstructured"}}
```

Exit code: 1. Table codec fails for `list-alerts` — even though the help says `table` is a valid format, the codec cannot handle `Alert` objects because they are typed structs (not `[]unstructured.Unstructured`). This is a bug / incomplete implementation.

---

## list-alerts — YAML output, first 30 lines (IWDIPP8VLKENJ, limit 5)

Command: `./bin/gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ --limit 5 -o yaml`

```yaml
  - apiVersion: oncall.ext.grafana.app/v1alpha1
    kind: Alert
    metadata:
      name: AC78DZJCPB22S
      namespace: stacks-27821
    spec:
      alertGroupID: IWDIPP8VLKENJ
    status:
      state: resolved
      severity: warning
      target:
        cluster: prod-us-east-0
        service: dashboard-service
      links:
        alert:
          rule:
            uid: dfh3yvlw5owlcc
            url: https://ops.grafana-ops.net/alerting/grafana/dfh3yvlw5owlcc/view
          instance:
            id: aca947af06950ed1
        dashboard:
          uid: grafana_slo_app-vokcpl8zr3j0j12mm0o5y
          url: https://ops.grafana-ops.net/d/st6qzlk/git-sync?var-cluster=prod-us-east-0&from=now-6h&to=now
          panel:
            id: 1
        slo:
          uid: vokcpl8zr3j0j12mm0o5y
          name: "Dashboard Service: Error Rate"
  - apiVersion: oncall.ext.grafana.app/v1alpha1
    kind: Alert
```

Field ordering in list-alerts YAML:
- `status` sub-keys: `state`, `severity`, `target`, `links` (struct-declared order — NOT alphabetical)
- `links` sub-keys: `alert`, `dashboard`, `slo` (alphabetical — but these happen to be struct order too)
- Contrast with list path (alphabetical) — list-alerts appears to use typed struct serialization

Note: The output has a leading 2-space indent (`  - apiVersion:`) suggesting it may be wrapped in a list with additional indentation.

---

## list-alerts — JSON field discovery (`--json list`)

Command: `./bin/gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ --limit 5 --json list`

```
apiVersion
kind
metadata
spec
spec.alertGroupID
status
```

Notes:
- Very sparse discovery — only top-level groups visible
- `status.state`, `status.severity`, `status.target`, `status.links` are not individually discoverable

---

## list-alerts — ILILSGFC6RB9W (grafana_alerting integration, slim)

Command: `./bin/gcx --context=ops irm oncall alert-groups list-alerts ILILSGFC6RB9W --slim --limit 3`

```json
hint: use --json list to discover fields, --json field1,field2 to select — no external parsing needed
[
  {
    "apiVersion": "oncall.ext.grafana.app/v1alpha1",
    "kind": "Alert",
    "metadata": {
      "name": "ADQKZ7KXG96WE",
      "namespace": "stacks-27821"
    },
    "spec": {
      "alertGroupID": "ILILSGFC6RB9W"
    },
    "status": {}
  },
  {
    "apiVersion": "oncall.ext.grafana.app/v1alpha1",
    "kind": "Alert",
    "metadata": {
      "name": "ABQWHA828LW1U",
      "namespace": "stacks-27821"
    },
    "spec": {
      "alertGroupID": "ILILSGFC6RB9W"
    },
    "status": {}
  }
]
```

Notes: `--slim` produces `status: {}` — empty status. Back-pointer only (`spec.alertGroupID`). 2 alerts total (not 3 — only 2 exist).

---

## list-alerts — ILILSGFC6RB9W (grafana_alerting integration, full)

Command: `./bin/gcx --context=ops irm oncall alert-groups list-alerts ILILSGFC6RB9W --limit 3`

```json
hint: use --json list to discover fields, --json field1,field2 to select — no external parsing needed
[
  {
    "apiVersion": "oncall.ext.grafana.app/v1alpha1",
    "kind": "Alert",
    "metadata": {
      "name": "ADQKZ7KXG96WE",
      "namespace": "stacks-27821"
    },
    "spec": {
      "alertGroupID": "ILILSGFC6RB9W"
    },
    "status": {
      "state": "resolved",
      "links": {
        "alert": {
          "rule": {
            "uid": "celdirtjz4x6of",
            "url": "https://ops.grafana-ops.net/alerting/grafana/celdirtjz4x6of/view?orgId=1"
          },
          "instance": {
            "id": "ea2811b3906b11c0",
            "silenceURL": "https://ops.grafana-ops.net/alerting/silence/new?alertmanager=grafana&matcher=__alert_rule_uid__%3Dceldirtjz4x6of&matcher=event_actor_alternateId%3Dkevin.leimkuhler%40grafana.com&orgId=1"
          }
        }
      }
    }
  },
  {
    "apiVersion": "oncall.ext.grafana.app/v1alpha1",
    "kind": "Alert",
    "metadata": {
      "name": "ABQWHA828LW1U",
      "namespace": "stacks-27821"
    },
    "spec": {
      "alertGroupID": "ILILSGFC6RB9W"
    },
    "status": {
      "state": "firing",
      "links": {
        "alert": {
          "rule": {
            "uid": "celdirtjz4x6of",
            "url": "https://ops.grafana-ops.net/alerting/grafana/celdirtjz4b6of/view?orgId=1"
          },
          "instance": {
            "id": "ea2811b3906b11c0",
            "silenceURL": "https://ops.grafana-ops.net/alerting/silence/new?alertmanager=grafana&matcher=..."
          }
        }
      }
    }
  }
]
```

Notes:
- grafana_alerting integration: `status.links.alert.instance.silenceURL` is populated
- No `severity`, `target`, `links.slo`, `links.dashboard` — those are absent for grafana_alerting-type alerts
- Only `state` and `links.alert` present in status

---

## list-alerts — INNXAABB8Y2R1 (formatted_webhook, sparse)

Command: `./bin/gcx --context=ops irm oncall alert-groups list-alerts INNXAABB8Y2R1 --limit 3`

```json
hint: use --json list to discover fields, --json field1,field2 to select — no external parsing needed
[
  {
    "apiVersion": "oncall.ext.grafana.app/v1alpha1",
    "kind": "Alert",
    "metadata": {
      "name": "A92H9RJKX33J6",
      "namespace": "stacks-27821"
    },
    "spec": {
      "alertGroupID": "INNXAABB8Y2R1"
    },
    "status": {}
  }
]
```

YAML output:
```yaml
  - apiVersion: oncall.ext.grafana.app/v1alpha1
    kind: Alert
    metadata:
      name: A92H9RJKX33J6
      namespace: stacks-27821
    spec:
      alertGroupID: INNXAABB8Y2R1
    status: {}
```

Notes:
- formatted_webhook type: completely empty status even on full (non-slim) retrieval
- Only 1 alert in this group (not 3)
- No `state`, `severity`, `links`, `target` — nothing in status

---

## Summary of format support matrix

| Command       | json | yaml | table | wide |
|---------------|------|------|-------|------|
| list          |  ✓   |  ✓   |  ✓    |  ✓   |
| get           |  ✓   |  ✓   |  ✗ (error) | ✗ (error) |
| list-alerts   |  ✓   |  ✓   |  ✗ (codec type mismatch runtime error) | ✗ (error) |

---

## Key observations for SRE persona design

1. **`list` default table is sparse**: 5 columns only (ID, STATE, ALERTS, STARTED, TITLE). No INTEGRATION, TEAM, SEVERITY, CLUSTER, or SERVICE. The TITLE often encodes cluster and service names (e.g. "KubePodOOMKilled (prod-us-east-0, grafana-ruler)") but not as structured data.

2. **`list -o wide` adds only INTEGRATION and TEAM** (7 columns). Still no SEVERITY, CLUSTER, SERVICE, SLO info.

3. **`get` has no table/wide mode** — only json and yaml. For SRE inspection workflows, the full JSON output is the only option for rich fields (severity, runbookURL, target.cluster, target.service, links.slo, links.dashboard).

4. **`list-alerts -o table` is broken** — runtime error "Invalid data type for table codec". The format is advertised in help but fails at runtime.

5. **Field ordering diverges between list and get**:
   - `list` (via unstructured): alphabetical keys in both YAML and JSON
   - `get` (typed struct): struct-declared order
   - `list-alerts` (typed struct): struct-declared order

6. **Enriched fields are integration-dependent**:
   - alertmanager + SLO-driven: full set (severity, target, links.slo, links.dashboard, links.alert + silenceURL)
   - grafana_alerting: state + links.alert + silenceURL only, no severity/target/SLO
   - formatted_webhook: empty status `{}`

7. **Team UID is never exposed in table outputs** — only team name. The wide table shows team name as a string (truncated in very long names). Team ID (e.g. `TKH52TW6TH7UE`) is only visible in JSON/YAML.

8. **`--json list` discovery is coarse**: only 8 top-level paths returned for `list`. Sub-fields of `status` (state, alertsCount, title, timestamps.started) are not individually addressable through discovery.
