# Expand `gcx irm oncall` for the GitOps Persona

**Created**: 2026-05-05
**Status**: proposed
**Supersedes**: none

## Context

The GitOps persona stores Grafana IRM configuration in version control and
uses `gcx resources push|pull` to round-trip it against a live stack. A
prior gap analysis of `gcx irm oncall` against the Grafana IRM product
surface flagged that several OnCall surfaces in the UI cannot currently be
expressed declaratively because they have no typed resource registration
in gcx:

- **Notification policies** — per-user notification chains live entirely in
  the UI; no way to source-control them.
- **Labels** — referenced everywhere (alert-groups, integrations, routes)
  but the catalog itself is unmanaged.
- **Heartbeats** — integration heartbeat-as-monitor is UI-only.
- **iCal-imported schedules** — pushable as resources today, but the
  imperative actions needed to refresh / re-tokenise them are missing.

This ADR scopes the GitOps-side OnCall work.

### Personas and themes referenced below

- **GitOps configurator (primary persona of this ADR)** — wants IRM
  configuration in git, round-tripped via the generic-tier declarative push
  and pull commands. Cares about *what* can be declaratively managed.
- **Agentic theme (cross-cutting principle)** — every human-facing
  affordance MUST have a CLI verb. Output MUST be predictable across human
  and agent invocation. Pivot identifiers between providers MUST be
  machine-extractable from structured output without string parsing.

### Tier convention

The gcx project distinguishes two CLI tiers, which is the load-bearing
principle for this ADR:

> `gcx resources` is the **generic + GitOps** tier (canonical resource shape,
> push/pull round-trip).
> `gcx <provider> <resource> <verb>` is the **tailored + action-oriented**
> tier (enriched data, filters, imperative actions).

For each new resource, both tiers get appropriate coverage: a typed
registration unlocks declarative round-trip, and a tailored view in the
provider tier delivers an enriched read shape.

### Out of scope (deferred)

- OnCall ViewSets `tokens`, `telegram_channels`, `live_settings`
  (lower-value GitOps targets).
- Per-resource imperative `create`/`update`/`delete` CLI verbs for
  resources already covered by the generic tier.
- SRE-side OnCall expansion (alert-groups list defaults, Alert payload changes, action-verb redesign, etc.).

## Decision

We will register three new typed resources for OnCall and add two
imperative actions on the existing `Schedule` noun for round-trip
workflows.

### 1. `NotificationPolicy` — full CRUD typed resource

Per-user notification chain step. One row per (user, position, important)
combination. Standard `Create/Update/Delete` operations from the OnCall API
are plumbed through the typed adapter so declarative push/pull works.

Backend endpoint: `/notification_policies/`.

```bash
# GitOps tier — declarative round-trip
gcx resources pull notificationpolicies --output-dir ./irm-config/
gcx resources push ./irm-config/notification-policies/me.yaml

# Tailored tier — enriched, ordered read view
gcx irm oncall notifications policies list --user me
# STEP  IMPORTANT  DELAY    NOTIFY-VIA
# 1     false      0s       slack
# 2     false      5m       phone-call
# 3     true       0s       phone-call
# 4     true       2m       sms
```

The tailored view is nested under a unified `notifications` parent noun —
the parent groups all notification-related affordances under one path.
Other notification-related sub-commands (e.g., imperative escalation /
test-page verbs) hang off the same parent:

```
gcx irm oncall notifications send                       # imperative action
gcx irm oncall notifications policies list|get          # GitOps tier read
```

Write operations stay in the generic tier (declarative push). The agentic
theme is satisfied: every UI-side notification-policy operation has a CLI
path.

### 2. `LabelKey` — aggregated typed resource

Labels are modelled as a single aggregated resource, not as separate
`LabelKey` + `LabelValue` resources. The shape matches the mental model — a
key carries its values:

```go
type LabelKey struct {
    ID     string   `json:"id,omitempty"`
    Name   string   `json:"name"`
    Values []string `json:"values,omitempty"`
}
```

Push semantics are atomic: replacing the `Values` slice **deletes any value
not in the file**. This is intentional and consistent with how kubectl-style
declarative tooling treats list fields. Mitigation: the existing
`gcx resources push --dry-run -o diff` workflow highlights deletions before
they apply.

The underlying client composes the two backend endpoints (`/labels/keys/`
and `/labels/{key_id}/values/`) into a single `LabelKey` shape on read, and
decomposes back to per-value POSTs/DELETEs on write.

```bash
# GitOps tier
gcx resources pull labelkeys --output-dir ./irm-config/
gcx resources push ./irm-config/labels/

# Tailored tier
gcx irm oncall labels list                    # all keys, with values
gcx irm oncall labels list --key environment  # filter to one key
```

### 3. `Heartbeat` — full CRUD with cross-reference enrichment

Heartbeat-as-monitor sub-resource of an integration. Standard CRUD via the
typed adapter for round-trip; the tailored `heartbeats list` enriches the
table view by cross-referencing `alert_receive_channel` IDs to integration
`verbal_name` strings:

```bash
gcx irm oncall heartbeats list
# ID   INTEGRATION         TIMEOUT  LAST_HEARTBEAT        STATUS    INSTANCE_URL
# ..   prod-prom            60s     2026-05-04T12:34:56Z  OK        https://...
# ..   staging-pyrra       300s     2026-05-03T08:11:00Z  EXPIRED   https://...
```

The cross-reference costs one extra `ListIntegrations` call per `list`
invocation; results are joined client-side. Acceptable at typical
integration cardinality.

Status (`OK` / `EXPIRED`) is computed from the API-provided
`last_heartbeat_time` against the configured `timeout` — no extra calls
required for the core status field.

### 4. Schedule round-trip imperative actions

Two action verbs hanging off the `Schedule` noun for iCal-imported
schedules. These are imperative actions, not new typed resources:

```bash
gcx irm oncall schedules reload-ical SCH123     # POST /schedules/{id}/reload_ical/
gcx irm oncall schedules export-token SCH123    # GET  /schedules/{id}/export_token/
```

`reload-ical` triggers a refresh of the iCal-imported shifts; `export-token`
returns the read-only iCal export URL/token. Both are needed for end-to-end
GitOps workflows where the source of truth is an external calendar.

### Hint annotations

The agent command-annotations registry is updated for the GitOps-side
commands introduced here:

| Command | Hint added |
|---|---|
| `notifications policies list` | "ordered by step; pass `--user <id>` (or `me`) to scope. Write via `gcx resources push`." |
| `labels list` | "aggregated view of label catalog. Push semantics for `LabelKey.values` are atomic — values not in the YAML are deleted. Use `--dry-run -o diff` to verify." |
| `heartbeats list` | "STATUS=EXPIRED means the integration has stopped sending heartbeats — upstream is silent." |
| `schedules reload-ical` | "force-refreshes iCal-imported schedule shifts; safe to run anytime." |
| `schedules export-token` | "returns the read-only iCal export URL/token used by external calendar consumers." |

## Rejected Alternatives

### Two-resource (`LabelKey` + `LabelValue`) label model

```go
type LabelKey   struct { ID, Name string }
type LabelValue struct { ID, KeyID, Value string }
```

Most idiomatically declarative — each row gets stable identity. Rejected
because it produces verbose stable-identity surface for what is logically a
single concept ("a label key with its values"). The aggregated shape carries
the same data with one resource and a more obvious mental model.

### Flat `Label{Key, Value}` model

One resource, one row per (key, value) pair. Rejected because the composite
key (`Key + Value`) is awkward for declarative push — there is no single
field that uniquely identifies a label, so adapter natural-key registration
becomes contrived.

### Read-only `NotificationPolicy` (list/get without write ops)

A smaller scope option. Rejected because it undermines the GitOps persona —
pull would work but push would not. A GitOps resource that cannot be
declaratively applied is not a GitOps resource.

### Separate top-level `notification-policies` parent

A `notification-policies list` command parallel to other imperative
notification verbs (e.g. an escalation/test-page verb under `notifications
send`). Rejected because two top-level noun siblings sharing a
`notification-` prefix duplicate surface for a single conceptual area. The
unified parent noun (`notifications`) groups action and policy access
together.

### Per-resource imperative `create` / `update` / `delete` CLI verbs

Originally surfaced for Integration, EscalationChain, Schedule, Webhook,
Route, Shift, ResolutionNote, ShiftSwap. Rejected because the generic tier
already covers declarative round-trip, and one-off creation flows go
through the UI. Adding imperative CRUD verbs would duplicate the surface
without serving a clear persona need. This reaffirms the dual-tier
principle.

### Adding `Tokens`, `TelegramChannels`, `LiveSettings` ViewSets

Lower-value GitOps targets that the gap analysis flagged but were not
requested by the project author. Out of scope for this ADR; can be added in
a follow-up if demand surfaces.

### Read-only `Heartbeat` (no write ops)

A heartbeat-as-monitor without push-able config is observable but not
manageable. Rejected for the same reason as read-only `NotificationPolicy`.

## Consequences

### Positive

- Three previously UI-only resources (`NotificationPolicy`, `LabelKey`,
  `Heartbeat`) gain full GitOps round-trip.
- Schedule iCal lifecycle becomes scriptable end-to-end.
- The agentic theme is preserved — every UI affordance for these resources
  has a corresponding CLI path.
- The unified `notifications` parent reduces surface duplication for
  notification-related concerns (action verb + policies sub-resource).
- The dual-tier principle is reinforced: write via generic tier, read via
  tailored tier with enrichment.

### Negative

- The aggregated `LabelKey` shape has unforgiving push semantics — removing
  a value from the YAML deletes it server-side. Mitigation: `--dry-run -o
  diff` highlights deletions; documented in the help text.
- The heartbeat list enrichment costs an extra `ListIntegrations` call per
  invocation. Acceptable at typical integration cardinality but should be
  noted for very large stacks.
- More backend endpoints are now load-bearing (notification policies,
  labels, heartbeats), so a Grafana-side tightening of the IRM plugin
  proxy has wider blast radius.
- The aggregated `LabelKey` shape requires custom client-side composition
  on read and decomposition on write — slightly more complex adapter code
  than a 1:1 typed resource.
- New typed resources land alongside the existing typed-CRUD pattern used
  by the OnCall provider; no new structural complexity, but the
  registration list grows.

### Follow-up

- **Implementation.** Self-contained — single PR per resource is feasible:
  - PR A: `NotificationPolicy` typed resource + `notifications policies
    list/get` tailored view.
  - PR B: `LabelKey` aggregated resource + `labels list` tailored view +
    client-side composition layer.
  - PR C: `Heartbeat` typed resource + `heartbeats list` enriched view +
    integration name cross-reference.
  - PR D: `schedules reload-ical` + `schedules export-token` action verbs.
- **`Tokens`, `TelegramChannels`, `LiveSettings`** can be added later if
  demand surfaces; same pattern as the three resources here.
- **Internal-vs-public API audit.** The OnCall plugin proxy is not a
  stable contract. Long-term, consider mirroring the public OnCall API
  where feasible.
