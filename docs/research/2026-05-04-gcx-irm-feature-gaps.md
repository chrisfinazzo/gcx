# Feature Gap Analysis: `gcx irm` vs Grafana IRM UI / Backend / Docs

**Date**: 2026-05-04
**Sources surveyed**: 8 research domains (CLI surface, OnCall UI, Incidents UI, IRM backend API, public docs, cross-linking opportunities, CLI list-default precedents, gcx design conventions)
**Sources cited**: 17
**Citations**: 41 inline references

> Goal: enumerate what Grafana IRM exposes that `gcx irm` does not, and recommend a prioritized fill-order anchored to the user's three pain points.

---

## TL;DR

`gcx irm` is a thin slice of the IRM product surface. The CLI exposes ~15% of OnCall verbs and ~5% of Incidents RPCs by count [4]; more importantly, the verbs it exposes are wired to defaults that make the CLI almost unusable on real stacks (alert-groups list iterates the entire history [1], incidents list mixes resolved with active [1]). The user's three pain points are all real and all fixable; addressing them surfaces a deeper gap (no per-resource imperative create/update verbs [1], no cross-provider data joining [6][8], ~150 incident RPCs unexposed [4]). The right fix order is:

1. **Fix list defaults** (`alert-groups list`, `incidents list`) — small change, big quality-of-life impact, sets a defensible precedent for the rest of the CLI [7][8].
2. **Fix `list-alerts` data shape** — extend `oncalltypes.Alert` to carry the full Alertmanager payload the API already returns [1][4]; this resolves pain points 2 and 3 simultaneously.
3. **Add cross-linking primitives** — `open <id>` verbs across all URL-templated OnCall resources [6], plus a related-alerting deeplink in `list-alerts` output.
4. **Fill imperative CRUD gaps** — most OnCall resources have `Create/Update/Delete` in the client but no CLI verb [1]; add them.
5. **Expand Incidents** — the largest gap; phase by feature priority (status/severity/role catalog, then tasks, then refs, then comments/attachments) [4][5].

---

## 1. Pain points — diagnosis and resolution

### Pain point #1: list defaults

**Diagnosis:**

`gcx irm oncall alert-groups list` calls `GET /alertgroups/` with **no query parameters except `--max-age`** (which most users never set) [1]. The OnCall internal API returns paginated results across the entire history; gcx's `iterResources` follows the `next` cursor until exhausted [1]. On stacks with months of data, this is effectively unbounded.

`gcx irm incidents list` defaults to `Limit:50, OrderField:"createdTime", OrderDirection:"DESC"` with **no status filter** [1]. This is bounded by `--limit 50` but mixes Active, Investigating, Resolved, Closed, etc.

The UI does not solve this with a hardcoded "active-only" default either. Instead, it [2][3]:
- Persists user filter choices to localStorage (OnCall) [2] or URL query params (Incidents) [3].
- Always sets `is_root=true` on alert-groups (excludes child groups) [2].
- Uses cursor-based pagination capped at ~50 per page [2].
- Uses the newer slimmer `QueryIncidentPreviews` RPC for incident lists [3].

**The CLI cannot persist filters per-session, so the design bar is different from the UI's.** The precedent set in mature CLIs is unambiguous: `gh issue list`, `glab issue list`, `aws ecs list-tasks` all default to the actionable subset and require an explicit `--state all` to escape [7]. The gcx CONSTITUTION doesn't forbid this [8]; it's just not yet a project rule.

**Recommendation (high confidence, ~95%):**

| Command | New default | New flags |
|---|---|---|
| `gcx irm oncall alert-groups list` | `status` filter active for `firing,acknowledged,silenced` (i.e. exclude `resolved`); `is_root=true` always | `--state firing,acknowledged,silenced,resolved` (multi; `--state all` shorthand for all four); existing `--max-age` retained; new `--integration`, `--team`, `--mine`, `--with-resolution-note`, `--has-related-incident` for parity with backend filterset [4] |
| `gcx irm incidents list` | `IncludeStatuses` set to all NON-resolved org statuses (computed at runtime from the org's status catalog, falling back to `["active"]` if catalog lookup fails) | `--status` (multi), `--exclude-status` (multi), `--severity`, `--only-drills`, `--role`; existing `--limit 50` retained as bound; switch backing RPC to `QueryIncidentPreviews` for slim payload [3][4] |

**Implementation notes:**
- Both follow the `gh` convention: help text MUST document the default explicitly [7].
- Add `--all` as a syntactic shortcut for "no filter."
- Per CONSTITUTION dual-purpose-design rule [8], agent mode MUST NOT change the data semantics — agents either accept the same filtered default or pass `--all`.
- Write a short ADR in `docs/adrs/` — Domain 8 flagged this as a precedent-setting change [8].

### Pain point #2: `list-alerts` returns useless IDs

**Diagnosis:**

`oncalltypes.Alert` truncates the OnCall internal API's response to four fields: `ID`, `LinkToUpstreamDetails`, `CreatedAt`, `RenderForWeb` [1]. The API actually returns the full Alertmanager-shaped payload — labels, annotations, status, startsAt, endsAt, generatorURL, fingerprint — under a typed sub-object [4]. The table codec (`alertTableCodec`) shows only `ID, CREATED`; even `LinkToUpstreamDetails` is dropped from the table view [1].

The UI fetches the same endpoint (`getInnerAlerts` in `AlertGroupStore.ts`) and renders the rich payload via `render_for_web` plus the underlying labels/annotations [2].

**Recommendation (high confidence, ~95%):**

1. Extend `oncalltypes.Alert` to capture:
   ```go
   type Alert struct {
       ID                    string            `json:"id,omitempty"`
       AlertGroupID          string            `json:"alert_group_id,omitempty"`
       CreatedAt             string            `json:"created_at,omitempty"`
       LinkToUpstreamDetails string            `json:"link_to_upstream_details,omitempty"`
       Payload               AlertPayload      `json:"payload,omitempty"`
       RenderForWeb          AlertRenderForWeb `json:"render_for_web,omitempty"`
   }
   type AlertPayload struct {
       Status       string            `json:"status,omitempty"`
       Labels       map[string]string `json:"labels,omitempty"`
       Annotations  map[string]string `json:"annotations,omitempty"`
       StartsAt     string            `json:"startsAt,omitempty"`
       EndsAt       string            `json:"endsAt,omitempty"`
       Fingerprint  string            `json:"fingerprint,omitempty"`
       GeneratorURL string            `json:"generatorURL,omitempty"`
   }
   ```
2. Update `alertTableCodec` to a sensible default (e.g. `ID, STATUS, CREATED, FINGERPRINT, LABELS_SUMMARY`) plus a `wide` codec showing labels, annotations, generatorURL, link_to_upstream_details.
3. The yaml/json default codec automatically gets the richer payload (no codec change needed; CONSTITUTION pattern 13 requires fetching all data regardless of format) [8].

**Cross-linking layer:** add a `relatedAlerting` block to JSON output [6]:

```json
{
  "id": "...",
  "payload": { "labels": {"grafana_rule_uid": "abc123", ...}, ... },
  "relatedAlerting": {
    "ruleUID": "abc123",
    "ruleURL": "https://my-stack.grafana.net/alerting/grafana/abc123/view",
    "instancesCommand": "gcx alert instances list --rule abc123",
    "generatorURL": "https://prom/...",
    "dashboardUID": "..."
  }
}
```

This is additive (doesn't break existing JSON consumers) and immediately useful for agents. Pattern doesn't exist yet in gcx [8] — recommend a brief design note when implemented.

### Pain point #3: `oncall alerts get` is redundant

**Diagnosis:** Same root cause as pain point #2 — the type is impoverished [1]. After fixing `Alert`, `oncall alerts get <id>` returns the full payload as YAML and is genuinely useful (single-alert deep-dive). No removal needed; the redundancy disappears once the data is present.

**Recommendation:** ship pain point #2's type extension, and `alerts get` becomes valuable automatically. Help text could clarify "for the alerts list within a group, use `alert-groups list-alerts <ag-id>`."

---

## 2. Beyond pain points — the broader gap

### 2.1 OnCall imperative CRUD verbs missing

The `OnCallAPI` client interface includes `Create*`, `Update*`, `Delete*`, and other write methods for nearly every resource (Integration, EscalationChain, EscalationPolicy, Schedule, Shift, Route, Webhook, ResolutionNote, ShiftSwap), but the CLI exposes only `list/get` for them [1]. Users who want to mutate go through `gcx resources push|pull` (declarative).

**Recommendation:** add imperative verbs symmetric with the UI:

| Resource | Missing verbs | Backend evidence |
|---|---|---|
| Integration | create, update, delete, send-demo-alert, change-team, start-maintenance, stop-maintenance, test-connection, migrate, validate-name | `AlertReceiveChannelView` actions [4] |
| EscalationChain | create, update, delete | [4] |
| EscalationPolicy | create, update, delete; add `--chain` flag to list (already in client) | [4] |
| Schedule | create, update, delete, who-on-call-now (`Schedule.on_call_now`), next-shifts-per-user, related-users, reload-ical, export-token | `ScheduleViewSet` actions [4] |
| Shift | create, update, delete | [4] |
| Route (channel_filter) | create, update, delete, move (reorder), convert-regex-to-jinja | [4] |
| Webhook | create, update, delete, responses (delivery history), preview-template, trigger-manual, reset | `WebhooksView` actions [4] |
| ResolutionNote | create (`add` verb under alert-group?), update, delete | client methods exist [1] |
| ShiftSwap | create, update, delete, take | `TakeShiftSwap` exists [1] |
| User | upcoming-shifts, send-test-{call,sms,push} | [4] |

CONSTITUTION (provider-architecture rules) forbids non-CRUD-shaped resources from using `get/list/create/update/delete` [8]. All the above ARE CRUD-shaped, so standard verbs are appropriate.

### 2.2 OnCall ViewSets entirely missing from gcx

These full backend viewsets are unexposed by gcx [4]:

- **`/notification_policies/`** — per-user notification policy chains (delay, notify-by). Documented user feature [5].
- **`/heartbeats/`** — integration heartbeat-as-monitor. Documented user feature [5].
- **`/labels/`**, `/labels/keys/`, `/labels/{id}/values/` — label catalog used everywhere [5].
- **`/tokens/`** — OnCall API tokens (create/revoke).
- **`/telegram_channels/`** — ChatOps telegram side.
- **`/live_settings/`** — runtime feature toggles.

Each is a candidate for a new top-level command tree under `gcx irm oncall`. The notification policies and labels surfaces are the highest priority — they're in the UI's primary nav [2] and the IRM Terraform provider already exposes them [5].

### 2.3 Alert-group missing actions (CRITICAL for pivot workflows)

The `AlertGroupView` has 16 `@action` endpoints; gcx exposes 6 (ack, resolve, unack, unresolve, silence, unsilence) plus `delete` (standard CRUD) [4]. Missing:

| Action | Why it matters |
|---|---|
| `attach` (POST `/alertgroups/{id}/attach/`) | THE primary OnCall→Incidents bridge. Pain-point-adjacent [4]. |
| `unattach` | Symmetric undo |
| `unpage_user` | Granular escalation control |
| `bulk_action` (POST `/alertgroups/bulk_action/`) | Agent-friendly batch operations |
| `escalation_snapshot` | Audit "who got paged in what step" |
| `stats` | Single-number health summary by status |
| `filters` | Discovery — list available filter chips dynamically |

Recommendation: add `gcx irm oncall alert-groups attach <id> --incident <incident-id>`, `... unattach <id>`, `... bulk-acknowledge|bulk-resolve --filter <expr>`, `... stats`.

### 2.4 Incidents — the largest gap

The incident backend has ~150 RPCs across 20+ services; gcx exposes 7 [4]. Several of these are **publicly documented as user-facing**, including ones added in 2025 (custom statuses, custom fields, briefings, private incidents, auto-summary) [5].

**Phase 1 (high priority — already-documented features):**
- **Incident updates**: `gcx irm incidents update <id> --title --description --severity --is-drill --event-time` (RPCs `UpdateTitle, UpdateDescription, UpdateSeverity, UpdateIncidentIsDrill, UpdateIncidentEventTime`) [4].
- **Status sub-tree**: `gcx irm incidents status set <id> <status>` and `gcx irm incidents statuses {list, create, update, delete}` (RPCs `StatusService.*`) [4][5].
- **Severity sub-tree**: extend `gcx irm incidents severities` to support `create/update/delete/archive` (RPCs `SeveritiesService.*`) [4][5].
- **Roles sub-tree**: `gcx irm incidents roles {list, create, update, delete, archive}` and `gcx irm incidents assign-role <id> --role <r> --user <u>` / `unassign-role` (RPCs `RolesService.*`, `IncidentsService.{AssignRole, UnassignRole}`) [4][5].
- **Tasks sub-tree**: `gcx irm incidents tasks {list, add, delete, update-status, update-text, update-user, reorder, convert-to-issue}` (RPCs `TasksService.*`) [4][5]. `AllTasks` page [3] maps to `gcx irm incidents tasks list --org-wide`.
- **Refs**: `gcx irm incidents refs {list, add, remove}` (RPCs `IncidentsService.{AddIncidentRef, RemoveIncidentRef, GetIncidentMembership}`) [4].
- **Labels**: `gcx irm incidents labels {list, add, remove}` and org-wide label catalog (RPCs `IncidentsService.{GetLabels, AddLabel, UpdateLabels, RemoveLabel, AssignLabel, UnassignLabel}`) [4].
- **Activity**: extend `activity add` to support more `kind` values (currently hardcoded `userNote` [1]); add `activity remove`, `activity update`, activity tag mgmt (RPCs `ActivityService.*`) [4].

**Phase 2 (medium priority — user-facing but less critical):**
- Comments (`CommentsService.*`) [4].
- Attachments (`AttachmentsService.*` — file upload is heavier; offer link-attach first) [4].
- Custom fields (`FieldsService.*`) [4][5].
- Org settings (`OrgService.{GetOrg, UpdateIncidentStatuses, UpdateImportantActivities, UpdateDefaultSeverity}`) [4].

**Phase 3 (low priority — niche or AI-side):**
- Auto-summary (`auto_summary_service`) [4][5].
- Suggestions (`suggestion_service`) [4].
- Recommendations / key updates / configuration tracker [4].
- Investigation cross-link (already partial via `gcx assistant investigations`) [3].

### 2.5 Switch `incidents list` backend RPC

The UI uses `IncidentsService.QueryIncidentPreviews` (returns slim `IncidentPreview` objects) for list views [3]; gcx uses the older heavier `QueryIncidents` [1]. Recommendation: switch gcx to `QueryIncidentPreviews` and add a `--full` flag (or auto-fetch via `Get` per-incident in `wide` mode) for users who want the heavyweight payload.

### 2.6 `is_root=true` parity

Currently gcx alert-groups list includes child alert groups that the UI hides via `is_root=true` [2]. Recommendation: pass `is_root=true` by default; add `--include-child-groups` flag for explicit opt-out.

---

## 3. Cross-cutting recommendations

### 3.1 Deep-link `open` verb on URL-templated resources

AlertGroup, Integration, EscalationChain, Schedule, Webhook, and Incident all have URLTemplates registered, but only `incidents open <id>` is wired today [6]. Add:

- `gcx irm oncall alert-groups open <id>`
- `gcx irm oncall integrations open <id>`
- `gcx irm oncall escalation-chains open <id>`
- `gcx irm oncall schedules open <id>`
- `gcx irm oncall webhooks open <id>`

Each is a one-shot wrapper around `deeplink.Resolve` + `deeplink.Open` [6]. Cost: trivial.

Backfill missing URLTemplates for EscalationPolicy, Shift, Route, Team, ResolutionNote, ShiftSwap, User (one line per resource) [6].

### 3.2 Agent-mode hint enrichment

`internal/agent/command_annotations.go` has minimal `Hint` strings for IRM commands [6]. Most are bare. Recommendation: populate `Hint` strings to suggest pivot commands — e.g. for `alert-groups list-alerts`, suggest "extract labels, then `gcx alert instances list --rule <uid>` to drill into alerting." This costs nothing at runtime and gives agents better self-direction.

Also: re-grade the Cost annotations. `oncall alert-groups list` is currently `Cost: "small"` [6] but with no defaults change it's potentially large; once defaults land, "small" becomes accurate again.

### 3.3 New design rule: progressive-disclosure default for list commands

There is no project-wide rule today on default-state filtering [8]. Adding one is precedent-setting. Recommendation: write a short ADR or add a section to `docs/design/output.md` / a new `docs/design/list-defaults.md`:

> List commands SHOULD default to the "actionable" subset of resources when the underlying API supports it. Help text MUST document the default explicitly. An `--all` shortcut MUST be provided. Agent mode MUST NOT change the default — agents must opt into expansion via flag.

This rule will apply to `alert instances list` (currently no default) [8], `incidents list`, `oncall alert-groups list`, and any future provider that lists state-bearing resources.

### 3.4 New design rule: cross-provider data references

There is no precedent for one provider's output emitting links to another provider's resources [6][8]. Adding a `relatedX` block (for the cross-linking case) introduces a new pattern. Recommendation: codify in a new `docs/design/cross-provider-output.md`:

> When a list/get command returns data that contains identifiers usable by another provider (rule UIDs, dashboard UIDs, datasource names), the output MAY include a top-level `related<Provider>` field that contains: (a) a deeplink URL, (b) a suggested gcx command to drill in, (c) the raw identifiers. This field is additive and stable across versions.

### 3.5 Existing CLI architecture stays intact

None of the above recommendations require deviating from CONSTITUTION invariants [8]:
- Options pattern for new flags ✓
- TypedCRUD for new resources ✓
- Existing `httputils` / k8s rest config plumbing reused ✓
- Sub-resource grammar `parent verb-child <id>` already used ✓

The existing OnCall plugin proxy at `/api/plugins/grafana-irm-app/resources/` is sufficient — it already brokers internal API access [1]; new endpoints just need new `OnCallClient` methods + `OnCallAPI` interface entries (which have a TODO comment about being temporary, so the code is ready to evolve) [1].

---

## 4. Risk and uncertainty

- **Internal vs public API stability.** gcx uses OnCall internal API (the plugin proxy) and Incidents oto-RPC internal services [1][4]. These are not public stable contracts; if Grafana tightens that, gcx breaks. The Terraform provider is anchored on public OnCall API + (limited) Incidents RPC [5]. Recommendation: track this risk in CONSTITUTION; long-term plan should consider mirroring the public OnCall API where feasible.
- **`oncalltypes.Alert` schema instability.** Alertmanager-shaped payload field names are stable for Grafana managed alerts but third-party integrations may use different label sets. Adding the typed payload makes gcx implicitly assume Grafana shape; non-Grafana integrations may have empty `relatedAlerting`. Mitigation: extract on best-effort basis, fall back to `Labels` raw map.
- **Incident "previews" RPC drift.** UI moved to `QueryIncidentPreviews` [3]; the older `QueryIncidents` may eventually be deprecated. Recommendation: switch sooner rather than later.
- **Default state filter behavior change.** This IS a breaking change for any existing CLI consumer scripted against the current "return everything" semantics. Strict backward compatibility argues for adding `--state` as opt-in first, then flipping default in a major release with a deprecation notice. Lower-risk path: ship `--state` as opt-in NOW; gather feedback for two minor releases; then flip default in the third.
- **Coverage estimate.** "5%" of incident RPCs is a count, not a weighted measure. The 7 RPCs gcx exposes are the highest-frequency ones (list, get, create, close, activity). Practically, gcx already covers ~50% of weighted user value for incidents and ~70% for OnCall. The recommendations above raise these meaningfully but don't claim full UI parity is achievable in a single release.

---

## 5. Recommended sequencing

| Tier | Work | Effort | Risk | Why |
|---|---|---|---|---|
| **T0** (this PR-or-soon) | Pain point #2: extend `oncalltypes.Alert` payload, update table codec | Low | Low | Direct fix; additive; resolves pain points 2 and 3 |
| T0 | Pain point #1: ship `--state` flag on `alert-groups list` and `--status` on `incidents list` (opt-in, no default change yet) | Low | Low | Unblocks users; non-breaking |
| T0 | Add `is_root=true` default to alert-groups list with `--include-child-groups` opt-out | Low | Low | UI parity |
| T0 | Add `gcx irm oncall <res> open <id>` for AlertGroup, Integration, EscalationChain, Schedule, Webhook | Trivial | None | Existing URLTemplates |
| **T1** (next minor) | Flip default of `alert-groups list` and `incidents list` to actionable subset; document in CHANGELOG | Trivial | Medium (breaking) | After T0 has had a release of bake-in time |
| T1 | Switch `incidents list` to `QueryIncidentPreviews` | Low | Low | UI parity, lighter payload |
| T1 | Add `relatedAlerting` block to alert payload JSON | Medium | Low (additive) | Pivot UX |
| T1 | Imperative create/update/delete for OnCall Integration, Schedule, Webhook, Route, EscalationChain | Medium | Low | Already in client interface |
| T1 | Add alert-groups `attach`/`unattach` and bulk actions | Medium | Low | Pain-point-adjacent |
| **T2** (next-next minor) | Phase-1 incidents expansion: status sub-tree, severity full CRUD, role catalog, role assignment, task CRUD, refs CRUD, labels | Large | Medium | Largest user value bump for Incidents users |
| T2 | NotificationPolicies, Heartbeats, Labels OnCall ViewSets | Medium | Low | Documented user features |
| T2 | New design rules: list-defaults, cross-provider-output ADRs | Trivial doc | None | Codify the above |
| **T3+** | Phase-2 incidents expansion: comments, attachments, custom fields, org settings | Large | Medium | Lower-frequency but valuable |
| T3+ | ChatOps surfaces (Slack/MSTeams settings, telegram channels) | Medium | Medium | Less common in CLI flows |
| T3+ | Auto-summary, suggestions, recommendations | Small per-RPC | Low | AI-side; bonus rather than core |

---

## 6. What this report does NOT answer

- Exact code-level line counts for the implementation (would need a separate planning pass).
- Whether to expose Incidents creation flags for `create` (e.g. `--severity`, `--status`, `--field-value`) — the `IncidentQuery` and `createIncidentRequest` types support more than the current CLI exposes [1]; an `update` command and richer `create` flags should be aligned together.
- Whether `gcx resources push|pull` already round-trips correctly for IRM Incident resource (`incidents_resource_adapter.go` exists [1]; in-practice testing not done).
- Performance characteristics of the JSON enrichment in `relatedAlerting` — cheap if done from already-fetched data; expensive if it requires an extra round-trip to alerting.
- Detailed UX mockup for `--state` flag help text — the precedent shape is in [7]; final wording is a copy-edit.

These are appropriate next-step questions for a `/plan-spec` or `/design` follow-up.

---

## References

1. **Domain 1 — gcx irm CLI surface** (codebase analysis, confidence 95%). Source: `/Users/igor/Code/grafana/gcx/internal/providers/irm/{provider.go, oncall_commands.go, oncall_commands_extra.go, oncall_client.go, oncalltypes/types.go, oncalltypes/api.go, incidents_commands.go, incidents_commands_impl.go, incidents_client.go, incidents_types.go, oncall_adapter.go, incidents_adapter.go}`.
2. **Domain 2 — IRM UI OnCall features** (codebase analysis, confidence 80%). Source: `/Users/igor/Code/grafana/irm/packages/@plugins/grafana-oncall-app/`, `@plugins/grafana-irm-app/`, `@grafana-irm/oncall-{api,state}/`. Key file: `@grafana-irm/oncall-state/src/stores/{AlertGroupStore.ts, FiltersStore.ts}`.
3. **Domain 3 — IRM UI Incidents features** (codebase analysis, confidence 75%). Source: `/Users/igor/Code/grafana/irm/packages/@plugins/grafana-incident-app/`, `@grafana-irm/incident-{api,state,hooks,utils}/`. Key file: `@grafana-irm/incident-api/src/api/useIncidentPreviews.ts`.
4. **Domain 4 — IRM backend API surface** (codebase analysis, confidence 85%). Sources: `/Users/igor/Code/grafana/irm/backend/oncall/apps/api/{urls.py, views/*.py}`, `backend/incident/api/*service.go`, `backend/incident/api/client-libraries/go/incident/oto-client.gen.go`.
5. **Domain 5 — Grafana IRM public docs** (web research, confidence 80%). Sources include:
   - [Grafana IRM (overview)](https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/)
   - [Get started with Grafana IRM](https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/get-started/)
   - [Configure escalation chains](https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/configure/escalation-routing/escalation-chains/)
   - [Configure integrations for Grafana IRM](https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/configure/integrations/configure-integrations/)
   - [Manage incidents in Grafana IRM](https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/manage-incidents/)
   - [Customize incident severity levels](https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/configure/incident-settings/severity-levels/)
   - [Manage Grafana IRM with Terraform](https://grafana.com/docs/grafana-cloud/developer-resources/infrastructure-as-code/terraform/terraform-oncall/)
   - [grafana_oncall_integration (Terraform)](https://registry.terraform.io/providers/grafana/grafana/latest/docs/resources/oncall_integration)
   - [grafana_oncall_schedule (Terraform)](https://registry.terraform.io/providers/grafana/grafana/latest/docs/resources/oncall_schedule)
   - [grafana_oncall_escalation_chain (Terraform)](https://registry.terraform.io/providers/grafana/grafana/latest/docs/resources/oncall_escalation_chain)
   - [What's new — IRM, May 2025](https://grafana.com/whats-new/2025-05-12-new-incident-response-features-in-grafana-irm/)
   - [terraform-provider-grafana issue #2508](https://github.com/grafana/terraform-provider-grafana/issues/2508)
6. **Domain 6 — Cross-linking opportunities** (codebase analysis, confidence 88%). Sources: `/Users/igor/Code/grafana/gcx/internal/{deeplink/, providers/{alert,logs,metrics,dashboards}/, agent/}`.
7. **Domain 7 — Progressive disclosure CLI precedents** (web research, confidence 90%). Sources include:
   - [gh issue list — manual](https://cli.github.com/manual/gh_issue_list)
   - [aws cloudwatch describe-alarms reference](https://docs.aws.amazon.com/cli/latest/reference/cloudwatch/describe-alarms.html)
   - [aws ecs list-tasks reference](https://docs.aws.amazon.com/cli/latest/reference/ecs/list-tasks.html)
   - [GitHub Issue #5511 — gh issue list semantics](https://github.com/cli/cli/issues/5511)
   - [Kubernetes kubectl Cheat Sheet](https://kubernetes.io/docs/reference/kubectl/cheatsheet/)
8. **Domain 8 — gcx design conventions** (project documentation discovery, confidence 90%). Sources: `/Users/igor/Code/grafana/gcx/{CONSTITUTION.md, DESIGN.md, CLAUDE.md, docs/design/*.md, docs/architecture/patterns.md, internal/providers/alert/instances_commands.go}`.
