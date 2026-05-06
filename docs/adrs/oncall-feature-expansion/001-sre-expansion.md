# Expand `gcx irm oncall` for the SRE Persona

**Created**: 2026-05-05
**Status**: proposed
**Supersedes**: none

## Context

A prior gap analysis of `gcx irm oncall` against the Grafana IRM product surface surfaced three concrete pain points that block SRE / on-call engineers from using the CLI on real Grafana Cloud stacks:

1. **`alert-groups list` is unusable on real stacks.** It iterates the entire alert-group history, includes child groups the UI hides via `is_root=true`, and mixes resolved with active state.
2. **`alert-groups list-alerts` returns useless IDs.** The `Alert` type is truncated to `ID`, `LinkToUpstreamDetails`, `CreatedAt`, `RenderForWeb`. The OnCall internal API exposes the full Alertmanager payload — labels, annotations, status, fingerprint, generatorURL — but only on the per-alert retrieve endpoint; `list-alerts` uses a slim serializer that excludes it. Decision 2 covers how we navigate that asymmetry.
3. **`oncall alerts get` returns the same skeletal data as `list-alerts` items.** Same root cause as #2 — the type was impoverished, so a single-alert deep-dive added no value over the list. Decision 2 fixes the type richness; the verb is retained.

Behind the pain points sit broader gaps that the SRE persona feels in practice: no pivot affordances from OnCall back into Grafana alerting, no "who is on call right now" view, no bulk acknowledge for storms, and a few verbs that violate the project's CLI grammar invariant (e.g. `oncall escalate` is a bare verb without a noun).

This ADR scopes the SRE-side OnCall work and binds the affected commands to the project's agent-mode output contract and hint conventions.

### Personas and themes

- **SRE / on-call engineer (primary persona of this ADR)** — engages with live alerts, drills into context, pivots between OnCall and Grafana alerting, pages people. The three pain points all live here.
- **Agentic theme (cross-cutting principle)** — every human-facing affordance MUST have a CLI verb. Output MUST be predictable across human and agent invocation. Pivot identifiers between providers MUST be machine-extractable from structured output without string parsing. The stdout/stderr split is invariant: stdout is the result; stderr is diagnostics.

### Tier convention

The gcx project distinguishes two CLI tiers, which disambiguates several decisions below:

> `gcx resources` is the **generic + GitOps** tier (canonical resource shape, push/pull round-trip). `gcx <provider> <resource> <verb>` is the **tailored + action-oriented** tier (filters, defaults, imperative actions on the canonical shape).

The tailored tier MUST NOT diverge in resource shape from the generic tier. Filters, defaults, and bulk actions are the tailored tier's value-add — not type reshaping.

### CLI grammar invariant

The project's CLI grammar invariant requires all resource and provider commands to follow `$AREA $NOUN $VERB`. Bare top-level verbs are reserved for a closed enumeration of foundational bootstrapping commands. Today's `gcx irm oncall escalate` violates this — `escalate` has no noun.

### Out of scope (deferred)

- All incident-side gaps.
- OnCall→Incident bridge verbs (`attach`/`unattach`).
- Per-resource imperative `create`/`update`/`delete` CLI verbs (the generic tier already covers them through declarative push/pull).
- GitOps-side OnCall expansion (new declarative resources, etc.).

## Decision

We will expand `gcx irm oncall` for the SRE persona along eight concrete sub-decisions.

### 1. `alert-groups list`: actionable defaults, immediate flip

Default behaviour after this ADR ships:

- `status` filter set to `firing,acknowledged,silenced` (excludes `resolved`).
- `is_root=true` always applied (excludes child groups, matching the UI).
- New filter flags: `--state` (multi), `--team`, `--integration`, `--mine`, `--with-resolution-note`, `--has-related-incident`. Existing `--max-age` retained.
- `--all` shortcut for "no status filter, no `is_root` filter" — the escape hatch for scripted consumers and one-off audits.
- `--include-child-groups` opts back into child-group inclusion without dropping the status filter.
- Help text MUST document the default explicitly.

The default flips **immediately** rather than ramping through an opt-in flag first. gcx is pre-1.0 and the primary consumer is the project author; an opt-in ramp adds release latency without preserving meaningful contracts. The breaking change is documented loudly in CHANGELOG.

### 2. Rich `AlertGroup` and `Alert` shapes with K8s envelope; `alerts get` retained

A spike against live Grafana Cloud stacks reshaped this decision in three substantive ways from the earlier draft. First, the OnCall internal API's behaviour around payload richness is split: the list endpoint `/alerts/?alert_group_id=X` uses a slim serializer that returns only `id, link_to_upstream_details, render_for_web, created_at, rule_name, generator_url`; the rich Alertmanager-shape `raw_request_data` lives only on the retrieve endpoint `/alerts/<id>/`. The earlier framing ("the API returns the full Alertmanager payload that we discard") was true for retrieve, false for list. Second, the `alertgroups/<id>/` response already inlines `last_alert.raw_request_data`, so the 99% SRE drilldown ("what's this alert about") gets the full payload in one round trip — independent of how `list-alerts` behaves. Third, the AlertGroup ergonomics needed work too (integer enum `status`, primary-key `team`, wall-of-HTML `render_for_web`, the actually-useful structured fields buried inside `last_alert.raw_request_data`), so the redesign covers AlertGroup AND Alert rather than just promoting Alert fields.

The chosen shape wraps both resources in the project's K8s-style envelope (`apiVersion`/`kind`/`metadata`/`spec`/`status`) so meta, configuration, and runtime state are visually separated, and groups the runtime fields hierarchically by what they point at (`target`, `rule`, `instance`, `dashboard`, `dashboard.panel`, `timestamps`, `raw`) instead of flattening pivot identifiers next to opaque IDs.

#### 2.1 `AlertGroup`

```yaml
apiVersion: oncall.ext.grafana.app/v1alpha1
kind: AlertGroup
metadata:
  name: IWDIPP8VLKENJ                         # was pk
  namespace: stacks-27821
  creationTimestamp: "2026-05-05T19:29:23Z"   # was started_at
  labels: {}                                  # OnCall app's user-set labels (the OnCall labels[] array — not extracted UIDs)
spec:
  integration:
    id: CR7MM8GWK6XCD
    name: am-app-platform                     # was verbal_name
    type: alertmanager
  team:
    id: TKH52TW6TH7UE                         # for queries
    name: <resolved>                          # for display — resolved via cached teams list (1 extra GET per command)
  permalinks:
    web: "..."
    slack: "..."
    slack_app: "..."
    telegram: ""                              # omitempty
status:
  title: "Dashboard Service: Error Rate..."
  summary: "Error budget burning..."          # commonAnnotations.summary OR commonAnnotations.description
  severity: warning                           # commonLabels.severity
  state: acknowledged                         # decoded enum: 0→firing, 1→acknowledged, 2→resolved, 3→silenced
  runbookURL: "https://github.com/..."        # commonAnnotations.runbook_url
  target:
    cluster: prod-us-east-0                   # commonLabels.cluster
    service: dashboard-service                # commonLabels.service
    namespace: ""                             # commonLabels.namespace — omitempty (absent on Grafana-SLO-style alerts)
  timestamps:
    started: "2026-05-05T19:29:23Z"
    acknowledged: "2026-05-05T19:32:47Z"
    resolved: ""                              # omitempty
    silenced: ""                              # omitempty
  links:                                      # cross-provider pivot identifiers + URLs (omitempty when no links found)
    alert:                                    # the alert that fired
      rule:
        uid: dfh3yvlw5owlcc                   # extraction fallback chain — see § 2.3
        url: "..."                            # alerts[0].generatorURL
      instance:
        id: aca947af06950ed1                  # = Alertmanager fingerprint (alerts[0].fingerprint)
        silenceURL: ""                        # omitempty — grafana_alerting integration only
    dashboard:                                # the linked Grafana dashboard (omitempty when no dashboard link)
      uid: grafana_slo_app-vokcpl8zr3j0j12mm0o5y
      url: "..."
      panel:
        id: 1
        url: ""                               # omitempty — grafana_alerting only
    slo:                                      # the backing Grafana SLO (omitempty unless this is an SLO-driven alert)
      uid: vokcpl8zr3j0j12mm0o5y              # commonLabels.grafana_slo_uuid
      name: "Dashboard Service: Error Rate"   # commonAnnotations.slo_name
  alertsCount: 3                              # last visible field
  raw:                                        # hidden by default — opt in via --include-raw (see §2.6)
    commonLabels: {...}
    commonAnnotations: {...}
    groupLabels: {...}
```

`status.state` is decoded from the OnCall integer enum (`0→firing, 1→acknowledged, 2→resolved, 3→silenced`) into the corresponding string — agents and humans read the same value and `--json` projection works without enum-table lookups. `spec.team.name` is resolved via a cached list of teams (one extra GET per command invocation, then cached), trading a small fixed cost for human-readable team display in every AlertGroup. `lastAlert` is intentionally NOT exposed on `AlertGroup`; callers that need per-alert data run `list-alerts <group-id>`. `status.raw` carries the unprocessed Alertmanager-shape passthrough (commonLabels / commonAnnotations / groupLabels) and is hidden by default — see §2.6.

#### 2.2 `Alert`

`Alert` is returned by both `list-alerts <group-id>` and `alerts get <id>`. The shape mirrors `AlertGroup`'s status block (one promoted-field tree), and exposes the full Alertmanager-shape group-webhook body under `status.raw` (hidden by default — see §2.6):

```yaml
apiVersion: oncall.ext.grafana.app/v1alpha1
kind: Alert
metadata:
  name: A61154KIR7PF8                         # was id
  namespace: stacks-27821
  creationTimestamp: "2026-05-06T03:30:53Z"   # was created_at
spec:
  alertGroupID: IWDIPP8VLKENJ                 # back-pointer to the parent group
status:
  state: firing                               # from payload.alerts[0].status — per-alert, not the alertgroup-wide state
  severity: warning
  target:                                     # extracted from payload.alerts[0].labels (not commonLabels — per-alert)
    cluster: prod-us-east-0
    service: dashboard-service
    namespace: ""
  links:                                      # same shape as AlertGroup.status.links — see §2.1
    alert:
      rule: {uid, url}                        # same fallback chain as AlertGroup, applied to payload.alerts[0]
      instance: {id, silenceURL}              # id = payload.alerts[0].fingerprint
    dashboard: {uid, url, panel: {id, url}}
    slo: {uid, name}                          # omitempty unless backed by a Grafana SLO
  raw:                                        # hidden by default — opt in via --include-raw (see §2.6)
    status: firing                            # full raw Alertmanager-shape group webhook (= API's raw_request_data)
    groupLabels: {...}
    commonLabels: {...}
    commonAnnotations: {...}
    groupKey: "..."
    externalURL: "..."
    receiver: "..."
    numFiring: 1
    numResolved: 0
    truncatedAlerts: 0
    alerts:                                   # nested per-alert array — usually 1 entry, sometimes more
      - status: firing
        labels: {namespace: cell-a, ...}
        annotations: {...}
        fingerprint: "..."
        generatorURL: "..."
        startsAt: "..."
        endsAt: "..."
```

Caveat: each `Alert` *record* carries a `raw.alerts[]` array (when `--include-raw` is passed). Empirically, ~99% of records have exactly one entry — that single entry is what populates the promoted `status.target` and `status.links` fields, which are always present regardless of the flag. Multi-entry batches lose per-entry distinguishing data in the promoted view; the full `raw.alerts[]` array is the escape hatch for that case and is fetched even when hidden, so passing `--include-raw` reveals it without a re-fetch.

#### 2.3 Promoted-field extraction with fallback chains

Two integration shapes diverge in where the pivot UIDs live. `grafana_alerting` (native Grafana 11+) puts them first-class on `alerts[].ruleUID`, `alerts[].dashboardURL`, `alerts[].panelURL`, `alerts[].silenceURL`. `alertmanager` (Grafana-managed routed via Alertmanager) buries them in `alerts[].labels.__alert_rule_uid__`, `alerts[].annotations.__dashboardUid__`, `alerts[].annotations.__panelId__` — there is NO `dashboard_uid` label or `panel_id` label, contrary to the earlier draft. Two non-extractable shapes also exist (`formatted_webhook`, `webhook`) that populate few or no promoted fields; `omitempty` handles them.

Each promoted field is extracted via an ordered fallback chain that walks both shapes. All paths below are relative to `status.links` unless stated otherwise:

| Field | Fallback chain |
|---|---|
| `links.alert.rule.uid` | `alerts[0].ruleUID` (grafana_alerting first-class) → `alerts[0].labels.__alert_rule_uid__` (alertmanager-via-Grafana) → `commonLabels.__alert_rule_uid__` |
| `links.alert.rule.url` | `alerts[0].generatorURL` |
| `links.alert.instance.id` | `alerts[0].fingerprint` |
| `links.alert.instance.silenceURL` | `alerts[0].silenceURL` (grafana_alerting only) |
| `links.dashboard.uid` | `alerts[0].annotations.__dashboardUid__` → `commonAnnotations.__dashboardUid__` → parse from `alerts[0].dashboardURL` (grafana_alerting URL parse: `.../d/<UID>/...`) |
| `links.dashboard.url` | `alerts[0].dashboardURL` (grafana_alerting first-class) → `alerts[0].annotations.dashboard_url` |
| `links.dashboard.panel.id` | `alerts[0].annotations.__panelId__` → parse from `alerts[0].panelURL` (`viewPanel=<ID>` query param) |
| `links.dashboard.panel.url` | `alerts[0].panelURL` (grafana_alerting only) |
| `links.slo.uid` | `commonLabels.grafana_slo_uuid` → `commonLabels.grafana_slo_uid` |
| `links.slo.name` | `commonAnnotations.slo_name` |
| `target.cluster` | `commonLabels.cluster` (AlertGroup) / `alerts[0].labels.cluster` (Alert) |
| `target.service` | `commonLabels.service` / `alerts[0].labels.service` |
| `target.namespace` | `commonLabels.namespace` / `alerts[0].labels.namespace` |
| `severity` | `commonLabels.severity` / `alerts[0].labels.severity` |
| `runbookURL` | `commonAnnotations.runbook_url` / `alerts[0].annotations.runbook_url` |
| `summary` | `commonAnnotations.summary` → `commonAnnotations.description` (only the first non-empty) |

All promoted fields are `omitempty`. Non-Grafana integrations populate fewer of these fields, sometimes zero — that is expected behaviour, not an error.

Three fields from the earlier draft are dropped. `alertGroupUID` is dropped because it has no sound source: the previously-proposed `labels.grafana_folder_uid` does not exist; `grafana_folder` is a folder *name* (string), not a UID. `valueString` is dropped because its content (`var='alert' labels={...} value=1` traces) is too technical to belong in promoted fields aimed at human readers; the data is preserved in `raw.alerts[0]` (when `--include-raw` is passed) for anyone who needs it. `panelID` as a flat scalar is replaced by the nested `dashboard.panel.{id,url}` block — it only makes sense in the context of a dashboard.

#### 2.4 `list-alerts` behaviour

The slim list endpoint matters less than the earlier draft assumed — `alertgroups/<id>/` already inlines `last_alert.raw_request_data` for the typical drilldown — but `list-alerts` still owns the per-alert detail view, and that view has to be rich for the SRE pivot path to work end-to-end.

- **Default**: rich. For each Alert returned by the slim `/alerts/?alert_group_id=X` endpoint, gcx fetches `/alerts/<id>/` (which returns `raw_request_data`) and populates the full shape above.
- **Concurrency**: bounded errgroup, default 10 (matches the gcx convention elsewhere).
- **Cap**: 100 alerts per group with a `warn:` line if exceeded ("retrieved 100 of K alerts; pass `--limit 0` to fetch all"). 100 × ~150ms ≈ 15s upper bound for default behaviour. `--limit 0` removes the cap entirely; `--limit N` sets a different cap.
- **`--slim`**: opt-out flag — skips the N+1 entirely, returning `Alert` objects with no extracted fields and no `status.raw`. Fast (one round trip). Suitable for sorting, counting, or spotting use cases that do not need pivot identifiers.
- **`--include-raw`**: orthogonal opt-in flag — emits `status.raw` with the full Alertmanager-shape webhook for every alert. Fetch behaviour unchanged (the N+1 still runs because the extracted fields require it); this flag only controls what is emitted. See §2.6.
- **Per-alert ordering**: same order as the slim API returns (most-recent-first, matching the `-created_at` ordering on the OnCall model queryset).

#### 2.5 `alerts get <id>` retained

The earlier draft removed `alerts get` on the basis that `list-alerts`'s rich payload made it redundant. That is reconsidered here: under the rich-by-default `list-alerts` model, `alerts get <id>` is the cheapest path to one specific alert without re-fetching the entire group's alert list (and paying the N+1 cost up to the cap). The verb is retained, returns the same `Alert` shape as `list-alerts` items (including the `--include-raw` toggle from §2.6), and inherits `--open` along with the rest of the typed-CRUD `get` family in Decision 3.

#### 2.6 `--include-raw`: opt-in raw passthrough

`AlertGroup.status.raw` (commonLabels + commonAnnotations + groupLabels) and `Alert.status.raw` (full Alertmanager-shape webhook with the nested `alerts[]` array) are hidden by default on `alert-groups get`, `alert-groups list-alerts`, and `alerts get`. The promoted blocks (`status.{target,rule,instance,dashboard,severity,summary,runbookURL,...}`) are the curated view of the same data and cover the typical SRE drilldown.

The `--include-raw` flag opts the raw block back in. Common cases:

- An SRE wants to see a label or annotation that wasn't promoted (e.g. a custom `team_routing_hint`, an integration-specific annotation).
- A multi-cell investigation wants `raw.alerts[*].labels.namespace` to enumerate distinguishing labels per Alertmanager-individual entry — the promoted `target.namespace` only captures `payload.alerts[0]`.
- An agent needs to see the full payload for arbitrary projection (e.g. extracting a label not in the curated set).

The flag does not affect fetch behaviour: the underlying retrieve calls always happen because the extracted fields require the raw payload. `--include-raw` only controls emission. Default-off keeps the typical output ~50% smaller and free of `__values__` / `__value_string__` / `__alertImageToken__` / `__orgId__` and other Grafana-internal noise; the flag is a one-character toggle when that data is genuinely needed.

#### 2.7 Deferred: backend AlertSerializer enrichment

An earlier proposal asked the IRM team to enrich the slim `AlertSerializer` with `raw_request_data` so `list-alerts` could be rich without an N+1. That ask is deferred indefinitely — the AlertGroup-first path covers the 99% drilldown without backend changes, and the N+1 cost is bounded by the 100-cap and the bounded errgroup. If backends are later enriched, gcx drops the N+1 with a one-line code change.

The promoted-fields-not-nested-block choice is preserved from the earlier draft and reinforced by the K8s envelope: agents read scalar JSON fields directly under `status.links.alert.rule.uid`, `status.links.dashboard.uid`, etc., with no need to dereference a `relatedAlerting` sub-object or string-parse URLs to extract IDs.

### 3. `--open` retrofit on provider-tier `get` commands

`--open` is the canonical "show me this in the browser" mechanism (already wired into the generic `gcx resources get`). It is **not** a sub-verb. The shared options helper used by typed-CRUD list/get commands in the IRM OnCall provider gains an `Open` field and uses the project's deeplink resolver/opener against the resource's registered URL template. Every typed-CRUD-built `get` command inherits the flag automatically.

URL template registrations are backfilled where missing (EscalationPolicy, Shift, Route, Team, ResolutionNote, ShiftSwap, User) so the flag works everywhere a `get` command exists.

### 4. `notifications send` (collapses `oncall escalate` + test endpoints)

A single `$NOUN $VERB`-shaped command replaces both `oncall escalate` (which violates the grammar invariant) and the three OnCall test endpoints (`make_test_call`, `send_test_sms`, `send_test_push`):

```bash
gcx irm oncall notifications send "DB primary down" --user-ids me,bob
gcx irm oncall notifications send "DB primary down" --team prod-sre --important
gcx irm oncall notifications send "self-test"      --user-ids me --test --via push
```

Backend dispatch:
- Without `--test`: POST `/direct_paging/` (creates an alert group).
- With `--test`: POST `/users/{id}/{make_test_call|send_test_sms|send_test_push}/` based on `--via`.

`oncall escalate` is removed in the same release (no deprecation alias — gcx is pre-1.0). CHANGELOG documents the rename.

The `notifications` parent noun is reserved for additional notification-related sub-commands.

### 5. `shifts list` adds filters; shape matches the generic tier

The current `shifts list` has been failing the SRE persona because it lacks the filter flags that would let an SRE answer "who is on call right now" or "what's my upcoming on-call load." Shape divergence between the generic and tailored tiers is **not** the right answer — the canonical resource shape from `gcx resources get shifts/<id>` is the same shape humans and agents already work with through GitOps round-trip, and the tailored tier MUST stay shape-compatible.

After this ADR ships, all three commands return the same `Shift` shape:

| Tier | Command | Shape | Differentiator |
|---|---|---|---|
| Generic / GitOps | `gcx resources get shifts/<id>` | `Shift` | Canonical read |
| Tailored / SRE | `gcx irm oncall shifts list` | `Shift` (multiple) | Filter flags + actionable defaults |
| Tailored / SRE | `gcx irm oncall shifts get <id>` | `Shift` (one) | Provider-tier single-shift read for `--open`, agent hints, deeplink |

`shifts list` answers SRE coverage questions through filter composition, not shape transformation:

```bash
gcx irm oncall shifts list                                       # default: --at now
gcx irm oncall shifts list --schedule SCH123 --at now            # who is on this schedule now
gcx irm oncall shifts list --user me --from now --to "+30d"      # my upcoming oncall load
gcx irm oncall shifts list --schedule SCH123 \
    --from 2026-05-04 --to 2026-05-11                            # range view
gcx irm oncall shifts list --team prod-sre                       # team's coverage
```

Filter flags accepted by `shifts list`:
- `--at` (single moment, default `now`)
- `--from` / `--to` (range; mutually exclusive with `--at`)
- `--schedule` (multi)
- `--user` (multi; `me` is the conventional self-reference)
- `--team` (multi)
- `--mine` (shortcut for `--user me`)

The implementation backs these filters by the OnCall internal `filter_events` endpoint where required to resolve "currently on call" and the `oncall_shifts` endpoint for the canonical resource shape — but the **shape returned to the caller is the canonical `Shift`**, with derived/effective fields populated where the underlying event resolution is needed (e.g., a derived `effectiveUsers[]` array for the queried window). Derived field names follow the project convention: `omitempty`, scalar-or-flat-array, no nested `relatedSchedule` block.

Restored / preserved:
- `gcx irm oncall shifts get <id>` is **kept**. The same-shape rule means it's not redundant — the provider tier owns the `--open` flag, agent hints, and any tailored display logic on top of the canonical resource read.

Removed:
- `gcx irm oncall schedules final-shifts <id>` — subsumed by `shifts list --schedule <id>`.

Default-filter convention: `shifts list` defaults to `--at now`. `--all` opts out of the time filter (mirroring the actionable-default behaviour of `alert-groups list`).

### 6. Bulk-by-filter on existing action verbs

Action verbs that today take a single `<id>` positional grow a filter mode:

```bash
# Single (unchanged)
gcx irm oncall alert-groups acknowledge IKFI314W1DEM9

# Bulk via filter — same verb, no positional
gcx irm oncall alert-groups acknowledge --team prod-sre --status firing
# → "About to acknowledge 23 alert groups. Continue? [y/N]"

gcx irm oncall alert-groups acknowledge --team prod-sre --yes   # skip prompt
gcx irm oncall alert-groups resolve     --max-age 24h --yes
```

Same shape applies to `resolve`, `silence`, `unsilence`, `unacknowledge`. With **neither** `<id>` nor any filter flag, the command MUST error with "`<id>` argument or filter flag required" — silent "act on everything" is forbidden.

Filter flags accepted on the action verbs: `--team`, `--integration`, `--status`, `--max-age`, `--mine`. They reuse the same flag definitions as `alert-groups list` so behaviour stays consistent.

The earlier-considered `bulk-acknowledge` / `bulk-resolve` / `stats` sub-verbs are not added — the filter form on the existing verb covers the batch case, and `stats` is covered by `list -o table` aggregation.

`--yes` is the canonical destructive-confirm flag. `--force` is reserved for "override safety guard" semantics elsewhere in the project — bulk action verbs use `--yes`.

### 7. Agent-mode output contract

Every command this ADR touches MUST conform to the agent-mode output contract defined here. The contract has four parts.

#### 7.1 Stream contract

For every mutating and long-running command in this ADR (`alert-groups acknowledge|resolve|silence|unsilence|unacknowledge`, `notifications send`):

- **stdout** = exactly one JSON document — the result envelope (or the fused error envelope on failure).
- **stderr** = zero or more JSON records, one per line, each with a typed `event` or `class` field. For bulk-by-filter operations, one progress event per item (`{"event":"acknowledged","target":{...}}`).
- A timeout / partial failure / total failure fuses into the result envelope on stdout — **never two documents on stdout**.

#### 7.2 MutationResult envelope

`notifications send` and the bulk-by-filter action verbs return:

```json
{
  "action": "acknowledge",
  "target": { "alertGroupIds": ["IKFI...","..."] },
  "changed": true,
  "summary": { "matched": 23, "succeeded": 23, "failed": 0 }
}
```

Single-target invocations return the same shape with `target.alertGroupId` (scalar) and no `summary.matched` (it's always 1). `changed:false` on idempotent re-runs (acknowledge of already-acknowledged group). Inline signal fields are added as needed (e.g., `discovered:bool` if a target is not found by filter).

#### 7.3 DetailedError + suggestions

Every error path in the OnCall commands emits the canonical schema:

```json
{
  "error": {
    "summary": "<one-line summary>",
    "exitCode": 1,
    "details": "<structured multi-line detail>",
    "suggestions": [
      "<runnable command>",
      "<runnable command>"
    ]
  }
}
```

Concrete suggestions for the "id-or-filter required" guardrail (Decision 6):

```json
{
  "error": {
    "summary": "<id> argument or filter flag required",
    "exitCode": 2,
    "details": "Bulk action verbs require either a positional <id> or one or more filter flags to scope the operation. Acting on every alert group is not supported.",
    "suggestions": [
      "Pass an alert-group ID: gcx irm oncall alert-groups acknowledge <id>",
      "Filter by team: gcx irm oncall alert-groups acknowledge --team <name>",
      "Filter by status + age: gcx irm oncall alert-groups resolve --status firing --max-age 24h"
    ]
  }
}
```

Errors from the OnCall plugin proxy are translated into this shape — backend 500s do NOT leak through with raw call chains.

#### 7.4 List envelope, field-select, identity

- `alert-groups list`, `alert-groups list-alerts`, `shifts list`, and every other OnCall list command in scope return `{"items":[...]}` on stdout. Empty → `{"items":[]}`. Never bare `[...]`, never `null`.
- The new `Alert` and `AlertGroup` fields participate in the global `--json` codec: `--json list` enumerates field paths including `status.links.alert.rule.uid`, `status.links.alert.rule.url`, `status.links.alert.instance.id`, `status.links.alert.instance.silenceURL`, `status.links.dashboard.uid`, `status.links.dashboard.url`, `status.links.dashboard.panel.id`, `status.links.dashboard.panel.url`, `status.links.slo.uid`, `status.links.slo.name`, `status.target.cluster`, `status.target.service`, `status.target.namespace`, `status.severity`, `status.summary`, `status.runbookURL`, etc. Paths under `status.raw.*` (commonLabels/commonAnnotations/groupLabels for AlertGroup; the full Alertmanager-shape webhook for Alert) participate when `--include-raw` is in effect — `--json list` enumerates them so projection can reach a label or annotation that wasn't promoted. Unknown fields exit 2 with a structured error and a suggestion to run `--json list`. Empty-list projection preserves the `{"items":[]}` shape, never a phantom `{"field":null}` row.
- Filter flag names on bulk action verbs match `alert-groups list` exactly (`--team`, `--integration`, `--status`, `--max-age`, `--mine`).
- In agent mode (`--agent` or auto-detected), bulk action verbs MUST fail-fast pre-prompt with a structured DetailedError suggesting `--yes` rather than auto-confirming. Agent-mode auto-confirmation of destructive operations is a footgun; an explicit `--yes` from the agent's prompt is the safer default.

### 8. Hint conventions (output-time)

Diagnostic output for every command in this ADR's scope follows the project-wide hint conventions.

#### 8.1 Three classes, plain-text prefixes

Diagnostic output uses three plain-text prefixes, in strict rendering order:

1. `warn:` — something is off but the operation succeeded (or partially succeeded). Always emitted regardless of `--quiet`.
2. `note:` — supplementary information about the result (e.g., "default filter excludes resolved groups; pass --all for full set").
3. `hint:` — concrete next-step suggestions. Each hint MUST be a runnable command the user can copy-paste.

`warn` → `note` → `hint`, never interleaved.

#### 8.2 Channel discipline

The stdout/stderr split is invariant. Hints, notes, and warnings ALWAYS go to **stderr**. Stdout is the result envelope (a single JSON document in agent mode; formatted output in TTY mode). The form of the stderr stream depends on the mode:

- **TTY mode:** stderr is plain prefixed text, dim-styled. ``` hint: See live instances: gcx alert instances list --rule <status.links.alert.rule.uid> hint: Inspect the rule: gcx alert rules get <status.links.alert.rule.uid> note: 0 results — defaults exclude resolved/child groups; try --all warn: Default filter excluded 47 resolved groups ```
- **Agent mode:** stderr is JSONL — one JSON record per line, with a typed `class` field and structured fields: ```jsonl {"class":"warning","summary":"Default filter excluded 47 resolved groups"} {"class":"note","summary":"0 results — defaults exclude resolved/child groups; try --all"} {"class":"hint","summary":"See live instances","command":"gcx alert instances list --rule <status.links.alert.rule.uid>"} {"class":"hint","summary":"Inspect the rule","command":"gcx alert rules get <status.links.alert.rule.uid>"} ```

In agent mode, the JSONL stderr stream is the same channel as the progress events from §7.1. Both are structured records; they coexist on stderr with distinct `event` (progress) and `class` (diagnostic) fields. Stdout in agent mode remains a single JSON document — the result envelope only.

`--quiet` suppresses `note:` + `hint:` (warn always renders); `--no-hints` suppresses only `hint:`. Suppression flags apply identically in TTY and agent modes.

#### 8.3 Post-result hints

Discovery verbs (`list`, `get`, `query`) in this ADR's scope emit a post-result hint suggesting the next logical step, on success only, conditional on result content:

| Command | Result | Post-result hint |
|---|---|---|
| `alert-groups list` | non-empty | `hint: Drill into alerts: gcx irm oncall alert-groups list-alerts <group-id>` |
| `alert-groups list` | empty | `hint: 0 results — defaults exclude resolved/child groups; try --all or --include-child-groups` |
| `alert-groups list-alerts` | non-empty | `hint: See live instances: gcx alert instances list --rule <status.links.alert.rule.uid>; inspect rule: gcx alert rules get <status.links.alert.rule.uid>; open dashboard: gcx resources get dashboards/<status.links.dashboard.uid>` |
| `alert-groups get` | success | `hint: See live instances: gcx alert instances list --rule <status.links.alert.rule.uid>; open dashboard: gcx resources get dashboards/<status.links.dashboard.uid>; per-alert detail: gcx irm oncall alert-groups list-alerts <id>` |
| `shifts list` | non-empty | `hint: View a single shift: gcx irm oncall shifts get <id>; open in browser: --open` |
| `shifts list` | empty + filter set | `hint: 0 results in window; try widening --from/--to or removing --user` |
| `notifications send` | success (`--test` mode) | `hint: Send a real page: gcx irm oncall notifications send "<message>" --user-ids <ids>` |

Hint emission is **conditional on result content** — empty vs non-empty gets different hints.

#### 8.4 Errors carry their own suggestions

Error paths use the DetailedError `suggestions[]` array on stdout (§7.3) — they do NOT emit `hint:` lines on stderr. The `suggestions[]` array IS the error-mode hint mechanism. Output-time `hint:` is for success paths.

### Hint and Cost annotations (registry table)

The agent command-annotations registry is updated for the SRE-side commands touched by this ADR. These annotations are agent-context metadata (Cobra-time), distinct from the output-time `hint:` lines from § 8.

| Command | Hint added | Cost |
|---|---|---|
| `alert-groups list` | "default filter excludes resolved + child groups; pass `--all` for full set." | small (large with `--all`) |
| `alert-groups get` | "populates rich status with `rule.uid` + `dashboard.uid` + target labels — pivot identifiers in one round trip; no need to call `list-alerts` for typical drilldown." | small |
| `alert-groups list-alerts` | "extract `status.links.alert.rule.uid` for `gcx alert rules get` / `gcx alert instances list --rule`; `status.links.dashboard.uid` for `gcx resources get dashboards/<uid>`; `--slim` to skip per-alert N+1." | medium (10 concurrent retrieves up to 100 cap; small with `--slim`) |
| `notifications send` | "use `--test --via push\|call\|sms` to validate notification setup without paging." | small |
| `shifts list` | "default `--at now`; pass `--from`/`--to` for ranges, `--user me` for personal coverage." | small |
| `alert-groups acknowledge` (and siblings) | "bulk via filter flags; `--yes` to skip the count-confirmation prompt; agent mode requires `--yes` explicitly." | small (medium with broad filter) |

## Rejected Alternatives

### `open` as a sub-verb (`alert-groups open <id>`, etc.)

`--open` is the established mechanism on the generic `gcx resources get` command. Adding sibling sub-verbs duplicates the affordance and bloats the command tree.

### Nested `relatedAlerting` block on the alert payload

A research-stage proposal was:

```json
{ "id": "...", "relatedAlerting": { "ruleUID": "...", "ruleURL": "...", ... } }
```

We chose typed sub-blocks grouped under a single `status.links` umbrella by what-they-point-at (`links.alert.{rule.{uid,url},instance.{id,silenceURL}}`, `links.dashboard.{uid,url,panel.{id,url}}`, `links.slo.{uid,name}`) instead. Reasons:
- Pairing the ID with its URL inside the same sub-block reads more naturally than the parallel-flat `alertRuleUID` / `alertRuleURL` arrangement that the original draft proposed.
- Grouping all cross-provider pivots under one `links` umbrella makes the section easy to find in the YAML and easy to reason about ("which other resources is this alert linked to?").
- Agents and codecs still read scalar fields directly — `status.links.alert.rule.uid` is one path traversal, no different in cost from a flat scalar.
- Hierarchy by pivot target lets us evolve each block independently (e.g. add `status.links.dashboard.tags` later, or add new pivot targets like `status.links.synthetic` for synthetic monitoring) without inventing a new top-level surface every time.
- The earlier "stepping stone" framing (one ID is enough to pivot) is preserved — the pivot ID is still a single scalar field, just nested for grouping.

### Keep `gcx irm oncall alerts get <id>`

An earlier draft of this ADR removed the verb on the basis that "`list-alerts` is now rich, so a single-alert deep-dive is redundant." The spike reversed that conclusion. With `list-alerts` rich-by-default and bounded by the 100-cap N+1, `alerts get <id>` becomes the cheapest path to one specific alert without re-fetching the parent group's full alert list — a real workflow when an agent or human already holds an alert ID (from a previous `list-alerts`, a permalink, or an upstream system) and just wants the rich shape for that one record. The verb is retained, returns the same `Alert` shape as `list-alerts` items, and inherits `--open` from Decision 3. The earlier "alert IDs are only meaningful in the context of their group" framing remains true at the conceptual level but does not justify removing the read verb when its cost is one round trip vs N+1 for the full group.

### `bulk-acknowledge` / `bulk-resolve` / `stats` sub-verbs

Bulk operations belong on the existing action verb (with filter flags), not on a parallel `bulk-*` family. `stats` is covered by `list -o table` aggregation.

### `who-on-call-now` / `next-shifts-per-user` / user `upcoming-shifts` sub-verbs

These violate compositional CLI design — they bake one filter into the verb name. Filter flags on `shifts list` (`--at`, `--from`, `--to`, `--user`, `--schedule`) compose all the same use cases without verb proliferation. The "who is on call right now" question is answered by `shifts list --at now` (optionally with `--user me` or `--team <name>`).

### Tier-divergent `Shift` shape (rotation rule vs resolved events)

This ADR's earlier draft had `gcx resources get shifts/<id>` returning the raw rotation rule and `shifts list` returning resolved per-user time-window rows — two different shapes for the same resource name across tiers. That broke the dual-tier mental model: agents writing one decoder per resource would have to special-case OnCall shifts. The chosen direction keeps the shape canonical and uses filter flags + derived `omitempty` fields to deliver the SRE coverage view.

### Removing `gcx irm oncall shifts get <id>`

The earlier draft removed `shifts get <id>` on duplicative-with-generic grounds. With the same-shape requirement the duplication argument no longer holds — the provider tier earns its keep by carrying the `--open` flag, agent hints, and Cobra-time annotations. The verb is restored.

### `users send-test-{call,sms,push}` as three separate commands

The three OnCall test endpoints map to one user-facing concept ("test my notification setup"). Collapsed into `notifications send --test --via <channel>`, which also unifies with the imperative `notifications send` escalation path.

### Keep `gcx irm oncall escalate` as a bare verb

Violates the project's CLI grammar invariant — `escalate` is a verb without a noun. Removed in favour of `notifications send`.

### Ramp rollout for `alert-groups list` defaults (opt-in flag, then flip)

Industry-cautious default but adds release latency. gcx is pre-1.0, the primary consumer is the project author, and the ramp adds no real backward-compat preservation. Flipping in the same release with a CHANGELOG note is sufficient.

### Hints on stdout in agent mode (as fields on the result envelope)

A draft of § 8.2 had agent-mode hints surface as `warnings[]`/`notes[]`/ `hints[]` fields on the stdout result envelope. Rejected because the stdout/stderr split is invariant: stdout is the result, stderr is diagnostics. Moving diagnostics onto stdout would conflate the two and break single-pass parsing for any consumer that treats stdout as the result and stderr as side-information. JSONL on stderr preserves the contract for both human and agent consumers.

### Auto-imply `--yes` in agent mode

Considered for § 7.4. Rejected because agent-mode auto-confirmation of destructive bulk operations is a footgun — an agent that paged the wrong filter set would acknowledge or resolve groups silently. The fail-fast pre-prompt with a structured DetailedError suggesting `--yes` is the safer default; agents pass `--yes` explicitly when the user has authorised it.

## Consequences

### Positive

- The three pain points are resolved end-to-end: actionable list defaults, rich `AlertGroup` and `Alert` shapes (K8s envelope with hierarchical `status.{target,rule,instance,dashboard}` blocks, decoded `state` enum, dropped HTML `render_for_web`), and `alerts get <id>` retained but now returning the same rich shape as `list-alerts` items.
- SRE workflow gains a coherent "investigate → pivot to alerting → act" path through promoted cross-provider IDs.
- The CLI grammar invariant violation (`oncall escalate`) is fixed by collapsing it (and the test-endpoint trio) into `notifications send`.
- The tailored tier no longer diverges in shape from the generic tier — one decoder per resource works across both tiers.
- Agent-mode behaviour for the affected commands is brought into a single contract: stream discipline, MutationResult envelope, DetailedError with suggestions, `{"items":[]}` list envelope, `--json` validator, `--yes` consistency, and structured JSONL diagnostics on stderr.
- Bulk operations are available without a parallel `bulk-*` verb family — the action-verb-with-filter shape composes naturally.

### Negative

- **Breaking change** to `alert-groups list` default behaviour. Anyone with scripts that grepped resolved alert groups out of the default output must add `--all` (or migrate to `--state resolved`). Documented loudly in CHANGELOG.
- **Breaking change** removing `oncall escalate`, `oncall users send-test-*`, `schedules final-shifts`. CHANGELOG covers all.
- **Breaking change** to `AlertGroup` (and `Alert`) output shape: the K8s envelope split (`metadata`/`spec`/`status`), the decoded `status.state` enum string (was integer), the dropped `render_for_web` field, and the renamed/restructured promoted fields (`status.links.alert.rule.uid`, `status.links.dashboard.uid`, etc., grouped hierarchically rather than flat). Scripts that grepped the old flat YAML break. CHANGELOG covers the migration with worked examples.
- **Breaking change** to `shifts list` output: was raw rotation-rule rows, becomes the canonical `Shift` shape with derived fields. Anyone scripted against the rotation-rule-only output must update field paths. Migration documented in CHANGELOG with a worked example.
- **Breaking change** to existing list/action output envelopes: `list` commands now return `{"items":[]}`, action verbs return MutationResult envelopes, errors return DetailedError. Scripts that grepped the old human-readable output break. The migration cost is one-time and documented in CHANGELOG.
- The Alertmanager-shape promotion in `Alert` couples gcx to Grafana-managed alert label conventions. Non-Grafana integrations get partial promotion. Mitigation: `omitempty` keeps the JSON honest; raw `Payload.Labels` and `Payload.Annotations` are always present.
- Internal-API surface dependency is unchanged but expanded — more endpoints are now load-bearing (`filter_events` for `shifts list`, `direct_paging` for `notifications send`), so a Grafana-side tightening of the IRM plugin proxy would have wider blast radius.
