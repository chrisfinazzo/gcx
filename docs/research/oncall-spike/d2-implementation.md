# D2 — Rich `AlertGroup` and `Alert` shapes — implementation log

**Status**: shipped to the branch. ADR § 2 fully rewritten to match. End-to-end smoke-tested against the `ops` stack.

**Companion docs**:
- [`d2-rich-alert.md`](d2-rich-alert.md) — original spike verdict (YELLOW with caveats; what we knew before iteration).
- [`../../adrs/oncall-feature-expansion/001-sre-expansion.md`](../../adrs/oncall-feature-expansion/001-sre-expansion.md) § 2 — the locked design (single source of truth).

This doc captures the iteration arc — what changed between the spike's verdict and the shipped implementation, and why. Useful as a template for handling other ADR findings (D4, D5, D6 …) and as context if D2 needs re-opening later.

## Final shape (locked)

```yaml
apiVersion: oncall.ext.grafana.app/v1alpha1
kind: AlertGroup
metadata:
  name: IWDIPP8VLKENJ
  namespace: stacks-27821
  creationTimestamp: "2026-05-05T19:29:23.381143Z"
spec:
  integration: {id, name, type}
  team: {id, name}                          # name resolved via cached teams list
  permalinks: {web, slack, slack_app, telegram}
status:
  title: "..."
  summary: "..."                            # commonAnnotations.summary OR description
  severity: warning
  state: acknowledged                       # decoded enum (was integer 0/1/2/3)
  runbookURL: "..."
  target:
    cluster: prod-us-east-0
    service: dashboard-service
    namespace: ""                           # omitempty
  timestamps:
    started: "..."
    acknowledged: "..."
    resolved: ""                            # omitempty
    silenced: ""                            # omitempty
  links:                                    # cross-provider pivots (omitempty when empty)
    alert:
      rule: {uid, url}                      # → gcx alert rules get / instances list --rule
      instance: {id, silenceURL}
    dashboard: {uid, url, panel: {id, url}} # → gcx resources get dashboards/<uid>
    slo: {uid, name}                        # → gcx slo definitions get <uid> (when SLO-driven)
  alertsCount: 3
  raw:                                      # hidden by default — opt in via --include-raw
    commonLabels: {...}
    commonAnnotations: {...}
    groupLabels: {...}
```

`Alert` mirrors the `status` block (state, severity, target, links) and exposes the full Alertmanager-shape webhook under `status.raw` (also opt-in via `--include-raw`).

## How the design evolved

The locked shape is the result of seven user-driven iterations on the original ADR draft. Each round of feedback tightened the design or surfaced a backend reality we hadn't accounted for.

### 1. Architecture: alertgroup-first vs N+1-from-list

ADR draft proposed: rich payload on `Alert`, exposed via `list-alerts <group-id>` doing N+1 retrieves. The spike found:

- The alert *list* endpoint uses a slim serializer; rich data only on retrieve.
- **`alertgroups/<id>/` already includes `last_alert.raw_request_data` inline** — one round trip gets the rich payload for the typical drilldown.
- For multi-cell debugging (each alert's distinguishing labels), `last_alert` isn't enough — N+1 across all Alert records *is* needed.

Both paths matter. Final decision: **rich shape on AlertGroup AND Alert**; `list-alerts` does N+1 by default with `--slim` opt-out and a 100-cap. `alerts get <id>` retained (the ADR's original "remove" plan was reversed) as the cheapest single-alert path.

### 2. AlertGroup ergonomics (separate concern from rich payload)

The user's first encounter with the new `alert-groups get` output exposed problems the ADR hadn't framed:

- `status: 1` opaque integer enum.
- `team: TKH52TW6TH7UE` PK-only, no name.
- `render_for_web` was a wall of HTML markup of data already structured elsewhere — pure noise.

Resolution: decode `status` to string at the client edge; resolve `team.name` via a cached `ListTeams` call (one extra GET per command); drop `render_for_web` entirely (extract just `.title` text).

### 3. Promoted-field set — three text bugs in the ADR

The ADR's promoted-field source paths were wrong:

| ADR claimed | Reality | Where it actually lives |
|---|---|---|
| `dashboardUID` ← `labels.dashboard_uid_label_name` | doesn't exist | `annotations.__dashboardUid__` |
| `panelID` ← `labels.panel_id` | doesn't exist | `annotations.__panelId__` |
| `alertGroupUID` ← `labels.grafana_folder_uid` | doesn't exist | nowhere — dropped from the field set |

`valueString` (proposed) was dropped: `var='alert' labels={...} value=1` traces are too technical for human readers.

`lastAlert` (proposed) was dropped: a single arbitrary record is not useful at the AlertGroup level — callers wanting per-alert data run `list-alerts`.

### 4. Two integration shapes, one extraction logic

Mining 4 alertgroups across 4 integration types (alertmanager, grafana_alerting, formatted_webhook, webhook) revealed:

- **`grafana_alerting`** (native): pivot identifiers first-class on `alerts[].ruleUID`, `dashboardURL`, `panelURL`, `silenceURL`, `valueString`. Bonus fields like `silenceURL` only available here.
- **`alertmanager`** (Grafana-managed routed via AM): identifiers buried in `alerts[].labels.__alert_rule_uid__`, `alerts[].annotations.__dashboardUid__`, `alerts[].annotations.__panelId__`.
- **`formatted_webhook`, `webhook`**: little to no structured data — promoted fields all `omitempty` and degrade gracefully.

Extraction needs ordered fallback chains per field. Documented in ADR § 2.3.

### 5. K8s envelope + hierarchical status

User feedback: the flat status block was getting long (15+ keys). Group hierarchically.

- K8s envelope (`metadata` / `spec` / `status`) was added consistently across both AlertGroup and Alert.
- Status grouped semantically by what-it-points-at: `target` (where), `timestamps` (when), `rule` / `instance` / `dashboard` (pivot identifiers).

Subsequent iteration on the user's "spec vs status" intuition: pivot UIDs are *observed* runtime data, not configured intent → keep in `status`, not `spec`. Field ordering in YAML emission can put pivots near the top of `status` for visibility.

### 6. SLO lift + `links` umbrella

User noticed the test alertgroup was driven by an SLO. Proposed lifting `slo.{uid, name}` and grouping all cross-provider pivots under a single `status.links` block. Final shape:

```yaml
status:
  links:
    alert:
      rule: {uid, url}
      instance: {id, silenceURL}
    dashboard: {uid, url, panel: {id, url}}
    slo: {uid, name}
```

SLO sources: `commonLabels.grafana_slo_uuid` (uid), `commonAnnotations.slo_name` (name). Verified end-to-end on `IWDIPP8VLKENJ`. Naming conversation (`pivots` vs `links` vs `refs` vs `subject`) settled on `links` (user's call); same for `target` (the affected cluster/service/namespace block).

### 7. `--include-raw` flag + field rename

After a round on the rich shape, the user flagged the `raw`/`payload` blocks as noise (`__values__`, `__alertImageToken__`, etc.) for typical drilldown.

- `AlertGroup.status.raw` (3-key subset) and `Alert.status.payload` (full webhook) hidden by default.
- New `--include-raw` flag on `alert-groups get`, `alert-groups list-alerts`, `alerts get`. Fetch behavior unchanged (extraction needs the payload); flag controls emission only.
- `Alert.status.payload` renamed to `Alert.status.raw` for uniform field naming with AlertGroup.
- Empty `{}` blocks for missing sub-fields fixed by converting struct values to pointer fields with `omitempty`.

### 8. Field ordering — typed envelope

After all of the above, the user noticed alphabetical YAML field ordering. Diagnosis: the default YAML codec in `internal/format/codec.go` does `json.Marshal → sigs.k8s.io/yaml.JSONToYAML`, and `JSONToYAML` alphabetizes object keys.

Fix:
- Replaced the `unstructured.Unstructured` → map-of-any path with typed envelope structs (`alertGroupEnvelope`, `alertEnvelope`, `k8sMetadata`).
- Custom `orderedYAMLCodec` registered on the three GET commands' opts, using `goccy/go-yaml` directly with `UseJSONMarshaler` — preserves struct field declaration order.
- JSON output preserves order naturally via `encoding/json`.

Verified: status block now reads top-to-bottom `title → summary → severity → state → runbookURL → target → timestamps → links → alertsCount`.

### 9. Round 9 — `alerts get <id>` axed (reversed from round 1)

A re-look at the verb landed on a removal. The `AlertRawSerializer` endpoint omits `alert_group_pk`, so `spec: {}` was empty and the resource was orphan from group context. Workflow analysis showed all real entry points (`list-alerts`, web/Slack permalinks, upstream notification webhooks) start from group-level data — bare alert IDs without group context are not a real flow. The verb, the parent `alerts` cobra group, and the verb-only command annotation entry were removed in commit `c3e978ab`. The shared rich-shape surface (`GetAlertRich`, `AlertRich` and dependent types, `alertEnvelope`/`alertRichToEnvelope`/`slimAlertEnvelope`) was retained because `alert-groups list-alerts` still uses it for the rich-by-default N+1 fan-out. ADR § 2.5 reframed; § 1 bullet 3, § 2 heading, § 2.6 raw-block list, "Rejected Alternatives" entry, and Consequences updated.

### 10. Round 10 — action verbs stay separate (no `--undo` collapse)

Considered collapsing `acknowledge`/`unacknowledge`, `resolve`/`unresolve`, `silence`/`unsilence` into three verbs with a `--undo` flag. Rejected. (a) `silence` is a TTL'd silence record — `silence --for=1h` and a hypothetical `silence --undo` would share a help block with disjoint flag sets, and the resource model becomes implicit rather than visible. (b) Agents and humans both read `unacknowledge` faster than `acknowledge --undo`. (c) Verb count is not the right thing to minimise — each of the six is a real domain action, not a flag toggle. kubectl precedent (`cordon`/`uncordon`, `suspend`/`resume`) supports the keep-separate shape. ADR § 7.1 already lists the six verbs explicitly, so no ADR text edit was required.

### 11. Round 11 — custom table codecs + typed envelope on list path

The `alert-groups` family had three latent issues round 11 closed out:

1. **`alert-groups list` YAML/JSON keys were alphabetical** while `get` and `list-alerts` were struct-declared. The d2-implementation log had blamed `unstructured.Unstructured`-for-table-codec-compat. The actual cause turned out to be a missing `orderedYAMLCodec` registration on `alertGroupListOpts`. Registered now; the unstructured path was lifted out as a side effect of the typed codec rework.
2. **`list-alerts -o table` was advertised in help but errored at runtime** with `Invalid data type for table codec, expected []unstructured.Unstructured` because list-alerts emits typed envelopes, not unstructured maps. Fixed by introducing a typed `alertTableCodec`.
3. **`alert-groups get -o, --output` help listed `yaml` twice** — duplicate registration drift. Fixed by a one-line dedupe in `internal/output/format.go::allowedCodecs()` (out-of-package fix, surfaced explicitly because the duplication couldn't be resolved from inside the irm package without intercepting `BindFlags`).

The shape we landed on for table/wide column sets follows the `recommendationTableCodec` pattern at `internal/providers/traces/adaptive/commands.go:110` and uses the `style.TableBuilder.ColumnWidths([]int)` API introduced by PR #610 (`feat(traces): render trace tree as table for traces get`):

```yaml
# alert-groups list -o table (TTY default)
columns: [ID, TITLE, SEVERITY, STATE, TEAM, STARTED]
column_widths: [14, 0, 10, 14, 0, 12]   # 0 = flexible

# alert-groups list -o wide
columns: [ID, TITLE, SEVERITY, STATE, TEAM, INTEGRATION, TARGET.CLUSTER, TARGET.SERVICE, ALERTS, LINKS.SLO.NAME, STARTED]
column_widths: [14, 0, 10, 14, 0, 0, 0, 0, 8, 0, 12]

# alert-groups list-alerts -o table
columns: [NAME, STATE, SEVERITY, TARGET.SERVICE, STARTED]
column_widths: [14, 14, 10, 0, 12]

# alert-groups list-alerts -o wide
columns: [NAME, STATE, SEVERITY, TARGET.CLUSTER, TARGET.SERVICE, LINKS.ALERT.RULE.UID, LINKS.DASHBOARD.UID, LINKS.ALERT.INSTANCE.SILENCEURL, STARTED]
column_widths: [14, 14, 10, 0, 0, 14, 14, 0, 12]
```

Column ordering follows reading order (`what-is-it → human-label → severity → state → who-owns → when`). Predictable-width columns (PK IDs, severity, state, age) get fixed widths; long / variable-width columns (TITLE, TEAM, INTEGRATION, TARGET.*, SLO.NAME) stay flexible so they expand into terminal width without forcing TITLE to a truncated mid-width.

Codec registration: `alertGroupTableCodec` and `alertTableCodec` are registered for both `table` and `wide` formats on the respective commands' `IO` options. SA-token fallback paths (`listAlertGroupsLegacy`, list-alerts public-API) also build typed envelopes now — fields stay empty (`omitempty`) since the public API doesn't return `raw_request_data`.

Commit `43db5743`; smoke 17/17 passing across diversity (alert-groups list/get/list-alerts × default/wide/yaml/json, three integration shapes, slim/include-raw paths, sibling-provider help-text safety).

### 12. Round 12 — `acknowledge` action verb vanguard + two-shape MutationResult

Built `gcx irm oncall alert-groups acknowledge <id>` end-to-end as the vanguard for the action-verb pattern. Single-target + bulk-by-filter forms; MutationResult on stdout per the project two-shape rule (single = `{action, target, changed}`; bulk = `{action, summary, failures}`). Idempotent re-runs surface as `changed:false` (single) or `summary.skipped` (bulk). `--yes` confirmation contract; agent mode requires `--yes` when targets > 1. Live-verified on `ops`: bulk `--max-age 1h --yes` ran with `matched:31 succeeded:27 skipped:4 failed:0` on first run, `matched:28 succeeded:0 skipped:28 failed:0` on the smoke re-run (idempotent edge case — sum invariant holds either way).

The first lift (commit `6a87f655`) shipped a single-shape `targets[]` envelope that conflated single and bulk. After reconciling against PR #597 (which establishes the scalar `target` + top-level `changed` shape for the instrumentation provider) and issue #264 (which specifies aggregate-success + enumerated-failure for bulk push/pull/delete), the refactor (commit `110adb57`) split the shapes: per-resource detail for single-target, aggregate counts + enumerated failures for bulk. Operation class — not provider boundary — drives shape choice. Pattern reference for the remaining five action verbs (resolve / unresolve / silence / unsilence / unacknowledge) which become mechanical clones in `/plan-spec`.

Type names landed: `singleMutationResult`, `bulkMutationResult`, `irmTarget`, `mutationSummary`, `mutationFailure`, `mutationTargetError`. `Changed *bool` with `omitempty` distinguishes "false-but-present" (idempotent) from "absent-on-failure" (error path).

### 13. Round 13 — `--open` flag on `alert-groups get` (mini-D3 cherry-pick)

Pulled the `--open` flag from D3 forward onto `alert-groups get` since round-11 smoke caught its absence. Uses the existing `deeplink.Open(url)` helper and reads the AlertGroup permalink directly from the typed envelope's `Spec.Permalinks.Web` (no need to consult `oncall_adapter.go`'s URL template — the OnCall server already provides the rendered permalink in the response). In agent mode, `--open` is a no-op that emits a `{"class":"note","summary":"--open is ignored in agent mode","url":"..."}` JSONL event on stderr.

Scoped narrowly: wired on `alert-groups get` only. The full D3 URL-template backfill and the `grafana-oncall-app` → `grafana-irm-app` migration (the existing template at `oncall_adapter.go:270` is still on the legacy plugin path; modern Grafana redirects but it's stale) remain open for `/plan-spec`.

### 14. Round 14 — `--limit` flag on `alert-groups list` with cursor-aware hint

Added `--limit int` (default 50) to `alert-groups list`. Default matches synth/slo project precedent. The `listAlertGroupsRaw` request now passes `perpage=min(limit,100)` to the OnCall internal API on the first page (NOT `page_size` — the data-miner caught that the OnCall backend silently ignores `page_size` and only honours `perpage`). Subsequent cursor-paginated pages echo the perpage value. Default `--limit 50` is one round trip; previously the default 25/page would have required two.

Hint emission is gated by three conditions: `limit > 0` (caller accepted truncation), `len(envs) == limit` (we hit the limit), and the server's `next` cursor URL is non-empty (more available). When all three: `hint: showing first N results — pass --limit M to fetch more or --limit 0 for all` on stderr (TTY plain dim, agent JSONL with `class:hint`). Suggested next-limit doubles current. Otherwise silent.

`list-alerts` already had its own `--limit` with a count-aware hint (the OnCall `/alerts/?...` endpoint does return a total `count` field, unlike `/alertgroups/?...`); left untouched.

### 15. Round 15 — table rendering bug fixes

Five bugs surfaced from real `--limit=3 -o table` / `-owide` output:

1. **ID column wrapped to two lines** — root cause: lipgloss `Style.Width()` includes `Padding(0,1)` (confirmed via lipgloss issue #298), so the round-11-locked `colWidths[0]=14` left only 12 chars of content width; 13-char OnCall PKs wrapped. Fix: bumped to 16 in `alertGroupTableCodec`; same arithmetic fix applied to `alertTableCodec` NAME and to LINKS.* UID columns.
2. **TITLE concatenated `(cluster, namespace)` into the alert name** — root cause: OnCall server-renders `render_for_web.title` as `"AlertName (cluster, namespace)"`, and the round-11 extraction copied this verbatim. Fix: added `stripTitleTargetSuffix` regex (conservative — strips only 1-3 comma-separated identifiers, leaves prose-style parens alone) inside `extractTitleFromRenderForWeb`.
3. **TITLE wrapped instead of ellipsis-truncating** when narrow — added `truncateRunes` helper (rune-aware, single-char `…` ellipsis) with a width budget computed from `terminal.StdoutWidth()` minus the sum of fixed-width columns and lipgloss border/padding overhead.
4. **SEVERITY column was `-` for all rows** — root cause: the list endpoint omits `last_alert.raw_request_data` (per the round-2 D2 finding), so the structured-payload extraction in `extractSeverity` short-circuits before setting the field. Fix: added `extractSeverityFromRenderForWeb` HTML fallback that parses `<li>severity: VALUE</li>` out of the OnCall server-rendered message body. Get-path Severity remains structured (it has `raw_request_data` inline); list-path Severity is best-effort HTML-parsed. Both produce the same string output.
5. **`-o wide` "missing columns" was NOT a regression** — the codec correctly declares all 11 columns; lipgloss compresses (not drops) when terminal width is narrow. Verified at 200-col TTY: TARGET.CLUSTER, TARGET.SERVICE, LINKS.SLO.NAME all render. UX call still open: should narrow-mode behaviour emit a hint, or accept silent compression?

Implementation note: lipgloss `ColumnWidths()` semantics — passed-in widths INCLUDE the cell padding, not just content. Worth codifying alongside the codec pattern doc; doc-editor will surface this in the design-rule pass.

### 16. Round 16 — column-order tweak + `name (id)` for TEAM

After live-testing rounds 12–15, the user manually iterated on the column order and TEAM rendering:

- TEAM moved from position 5 (default) / 5 (wide) to position 3 in both — semantically pairs "what is this" (`TITLE`) with "whose is it" (`TEAM`) before showing severity / state / age.
- TEAM cell now renders `name (id)` (e.g. `Alerting (T7BX6FGR3Y9IP)`) instead of just `name`. ID disambiguation matters when team names collide or when an agent needs the PK for a follow-up call.
- Bug caught + fixed: the manual edit reordered the headers + colWidths in both branches and the wide-branch row args, but the **default-branch `t.Row(...)` call still passed args in the old order**. Result: severity rendered under the TEAM column, state under SEVERITY, etc. One-line fix to bring the default row args into header order.

Updated final-shape column lock:

```yaml
# alert-groups list -o table
columns: [ID, TITLE, TEAM, SEVERITY, STATE, STARTED]
column_widths: [16, 0, 0, 10, 14, 12]

# alert-groups list -o wide
columns: [ID, TITLE, TEAM, SEVERITY, STATE, INTEGRATION, TARGET.CLUSTER, TARGET.SERVICE, ALERTS, LINKS.SLO.NAME, STARTED]
column_widths: [16, 0, 0, 10, 14, 0, 0, 0, 8, 0, 12]
```

`list-alerts` column order unchanged.

### 17. Round 17 — post-result hints + link-focused list-alerts table

Final polish before closeout. Three changes:

**a. `alert-groups list` post-result hints** — two emissions on stderr (TTY plain, agent JSONL):

- *Filter summary* — emit when any filter is active. Default-only state renders as `default (excludes resolved + child groups)`. Explicit non-status filters prefix with `default + ` (e.g. `default + team=prod-sre`). Explicit `--state` overrides the default. `> 80 chars` collapses to `<N filters>`. Silent when `--all` is passed and no other filter is active.
- *Drill-in navigation* — two hints emitted when result count > 0, using literal `<id>` placeholder (template-shaped, not row-specific): `gcx irm oncall alert-groups get <id>` and `gcx irm oncall alert-groups list-alerts <id>`.

Both fire after the round-14 `--limit` truncation hint, not in place of it. All three may emit in one invocation.

**b. `list-alerts` table column rework** — drop redundant-with-parent fields, emphasize links:

```yaml
# alert-groups list-alerts -o table (4 cols)
columns: [NAME, STATE, RULE, DASHBOARD]
column_widths: [16, 14, 0, 0]
# RULE cell: status.links.alert.rule.url || status.links.alert.rule.uid || "-"
# DASHBOARD cell: status.links.dashboard.url || status.links.dashboard.uid || "-"

# alert-groups list-alerts -o wide (8 cols)
columns: [NAME, STATE, RULE.UID, RULE.URL, INSTANCE.SILENCEURL, DASHBOARD.UID, DASHBOARD.URL, DASHBOARD.PANEL.URL]
column_widths: [16, 14, 16, 0, 0, 0, 0, 0]
```

Dropped from both default and wide: SEVERITY, TARGET.CLUSTER, TARGET.SERVICE, STARTED — all live at the parent AlertGroup so showing them per alert was redundant noise. JSON/YAML emission unchanged (typed envelope already carries `{uid, url}` for each link).

**c. `list-alerts` conditional link hints** — emit when any alert in the result has `status.links.alert.rule.uid` populated. First occurrence wins (avoids per-rule noise on multi-rule groups):

```
hint: inspect rule:        gcx alert rules get <first-rule-uid>
hint: see live instances:  gcx alert instances list --rule <first-rule-uid>
```

SLO and dashboard hints deferred — SLO data is at parent-group level (would require an extra round trip), dashboard `gcx resources get` syntax for typed CRUD wasn't confirmed in scope.

Helper functions added (worth knowing for doc maintenance):

- `stringifyAlertGroupListFilters(opts)` + `alertGroupListHasExplicitFilter(opts)` — filter-summary serialization.
- `emitAlertGroupListFilterHint`, `emitAlertGroupListNavHints`, `emitListAlertsLinkHints` — emission helpers.
- `firstAlertRuleUID(envs)` in `oncall_extract.go` — linear scan, first non-empty wins.
- `alertRuleCellPreferURL`, `alertDashboardCellPreferURL` — per-cell URL-over-UID precedence.

Hint pattern variants surfaced (for `docs/design/output-shapes.md` codification):

- Filter-summary hint diverges from the standard `hint: <summary>: <command>` form — uses inline prose (`pass --all to... or --help for...`) with the suggested command embedded separately in the agent-mode JSONL `command` field. Permitted variant when the suggestion needs prose alternatives.
- Nav hints use literal `<id>` placeholder so the JSONL `command` stays row-agnostic. Link hints embed a concrete UID since one is canonical across the result. Both are valid; choice depends on whether there's a single canonical value or many.

Live-verified on ops with grafana_alerting integration: all hint emissions on stderr (TTY + agent), URL preferred over UID in default RULE / DASHBOARD cells, ellipsis truncation at 16 chars on RULE.UID in wide mode, YAML rule `{uid, url}` pair preserved.

## What landed

### Code

- **`internal/providers/irm/oncalltypes/rich.go`** (new) — `AlertGroupRich`, `AlertGroupSpec`, `AlertGroupStatus`, `AlertRich`, `AlertSpec`, `AlertStatus`, `AlertLinks`, `AlertLinkAlert`, `AlertLinkSLO`, `AlertTarget`, `AlertTimestamps`, `AlertRule`, `AlertInstance`, `AlertDashboard`, `AlertPanel`, `AlertGroupRaw`, `AlertPayload`, `AlertmanagerAlert`, `IntegrationRef`, `TeamRef`, `AlertGroupLinks`.
- **`internal/providers/irm/oncall_extract.go`** (new) — extraction helpers with dual-shape fallback chains: `extractRule`, `extractDashboard`, `extractTarget`, `extractSLO`, `buildAlertLinks`, `parseDashboardUIDFromURL`, `parsePanelIDFromURL`, `decodeAlertGroupState`, `extractIntegrationRef`, `extractTeamID`, `extractAlertGroupLinks`, `extractRawRequestDataFromLastAlert`, `extractTitleFromRenderForWeb`.
- **`internal/providers/irm/oncall_rich_client.go`** (new) — `*OnCallClient.GetAlertGroupRich`, `GetAlertRich`, `listAlertIDs`, `resolveTeams` (lazy per-client cache), `buildAlertGroupRich`, `buildAlertRich`, `listAlertGroupRichFromBytes`.
- **`internal/providers/irm/oncall_commands_extra.go`** — added `orderedYAMLCodec`, envelope types (`k8sMetadata`, `alertGroupEnvelope`, `alertEnvelope`), converters (`alertGroupRichToEnvelope`, `alertRichToEnvelope`), `slimAlertEnvelope`. Rewrote `alert-groups get` and `list-alerts` commands. Kept `alertGroupRichToUnstructured` / `alertRichToUnstructured` for legacy paths.
- **`internal/providers/irm/oncall_commands.go`** — rewrote `alerts get` (round 1–8) and then removed it along with the empty `alerts` cobra parent group (round 9, commit `c3e978ab`).
- **`internal/providers/irm/oncall_commands_extra.go`** (round 11) — replaced `alertGroupTableCodec`/`alertTableCodec` unstructured-consuming bodies with typed envelope readers; added `formatRelativeAge` helper for STARTED column; registered `orderedYAMLCodec` on `alertGroupListOpts` (was missing — root cause of the alphabetical-YAML-on-list issue); deleted dead helpers `alertGroupRichToUnstructured`, `alertRichToUnstructured`, `slimAlertObject`.
- **`internal/providers/irm/oncall_commands.go`** (round 11) — list path lifted off `unstructured.Unstructured` onto typed envelope; SA-token fallback paths build typed envelopes too.
- **`internal/output/format.go`** (round 11) — one-line dedupe in `allowedCodecs()` so a custom codec keyed on a builtin name (e.g. `"yaml"`) doesn't appear twice in `-o, --output` help.
- **`internal/providers/irm/oncall_actions.go`** (round 12 + 12-refactor) — new file housing the action-verb vanguard. Shipped `acknowledge` end-to-end with single-target + bulk-by-filter forms, MutationResult envelopes, `--yes` confirmation, idempotent change detection. Refactor (`110adb57`) split into `singleMutationResult` / `bulkMutationResult` types with shared `irmTarget`, `mutationSummary`, `mutationFailure`. Pattern reference for the remaining five action verbs.
- **`internal/providers/irm/oncall_actions_test.go`** (round 12) — unit tests for target resolution, MutationResult builder, idempotent-vs-changed detection, confirmation prompt skip-on-yes. Refactor added shape invariants (single/bulk mutual exclusivity, summary sum check via `assertSummaryAddsUp`).
- **`internal/providers/irm/oncall_commands_extra.go`** (round 13 + 14 + 15) — `--open` flag on `alertGroupGetRichOpts` with `handleAlertGroupOpen` deeplink helper (round 13); `--limit` flag on `alertGroupListOpts` with cursor-aware hint emission and `perpage=min(limit,100)` plumbing through `listAlertGroupsRaw` (round 14); ColumnWidths bumped from 14→16 to fit lipgloss padding (round 15).
- **`internal/providers/irm/oncall_extract.go`** (round 15) — `stripTitleTargetSuffix` regex helper to strip `(cluster, namespace)` parentheticals from server-rendered titles; `extractSeverityFromRenderForWeb` HTML fallback for list-path severity (since list endpoint omits `raw_request_data`).
- **`internal/providers/irm/oncall_extract_test.go`** (round 15) — unit tests for the title strip and severity HTML fallback.
- **`internal/providers/irm/oncall_rich_client.go`** (round 14 + 15) — `WithLimit` plumbed through to the SA-token public-API list path; minor adjustments for the rendered-title strip.
- **`internal/providers/irm/spike_d1d3d6d7d8_demo.go`** (round 12-refactor) — local types renamed (`mutationResult` → `spikeMutationResult` etc.) to resolve symbol collision with the new production types in `oncall_actions.go`. Throwaway POC; will be deleted in `/plan-spec` cleanup.

### ADR § 2

Full rewrite. Sub-sections 2.1 (AlertGroup), 2.2 (Alert), 2.3 (extraction fallback chains — documented per-field source paths), 2.4 (list-alerts behaviour), 2.5 (alerts get removed — reversed from round 1, reflects round 9), 2.6 (`--include-raw` flag), 2.7 (deferred backend AlertSerializer enrichment). Downstream sections updated: registry table for hint+cost annotations, post-result hint table, rejected-alternatives entry on `alerts get` (reversed), Consequences (positive + negative).

### Spike artifacts (preserved)

- `internal/providers/irm/spike.go`, `spike_d2_rich_alert.go`, `spike_d4_notifications_send.go`, `spike_d5_shifts_list.go`, `spike_d1d3d6d7d8_demo.go` — kept as-is. Hidden under `gcx irm oncall spike`.
- `docs/research/oncall-spike/d2-rich-alert.md` etc. — original spike verdicts, untouched.

## Smoke tests passing on `ops`

```bash
gcx --context=ops irm oncall alert-groups get IWDIPP8VLKENJ        # rich shape, ordered, no {} blocks, slo populated
gcx --context=ops irm oncall alert-groups get ILILSGFC6RB9W        # grafana_alerting — silenceURL populated
gcx --context=ops irm oncall alert-groups get I8H7WGN185K18        # alertmanager-via-Grafana extraction
gcx --context=ops irm oncall alert-groups get INNXAABB8Y2R1        # formatted_webhook — minimal, no links block

gcx --context=ops irm oncall alert-groups get IWDIPP8VLKENJ --include-raw  # 47 → 79 lines

gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ          # rich, N+1
gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ --slim   # fast, no fetch
gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ --limit 1

gcx --context=ops irm oncall alert-groups list --max-age 24h -o table       # table codec still works
gcx --context=ops irm oncall spike d2 IWDIPP8VLKENJ                          # spike untouched

# round 11 additions
gcx --context=ops irm oncall alert-groups list --max-age 24h -o table          # 6 cols, ID|TITLE|SEVERITY|STATE|TEAM|STARTED
gcx --context=ops irm oncall alert-groups list --max-age 24h -o wide           # 11 cols incl. TARGET.*, ALERTS, LINKS.SLO.NAME, INTEGRATION
gcx --context=ops irm oncall alert-groups list --max-age 24h -o yaml | head -25  # struct-declared order under status (was alphabetical)
gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ --limit 3 -o table  # 5 cols (was broken)
gcx --context=ops irm oncall alert-groups list-alerts IWDIPP8VLKENJ --limit 3 -o wide   # 9 cols incl. RULE.UID, DASHBOARD.UID, SILENCEURL
gcx --context=ops irm oncall alert-groups get --help | grep -A2 "output"        # yaml listed once (was twice)

# round 12-15 combined coverage (commit 1a7ca0b2 onwards):
gcx --context=ops irm oncall alert-groups list --limit 3 -o table          # IDs single-line, titles stripped, severity populated
gcx --context=ops irm oncall alert-groups list --limit 3 -owide            # all 11 columns including TARGET.* + LINKS.SLO.NAME
gcx --context=ops irm oncall alert-groups list --limit 100                 # cursor hint when truncated
gcx --context=ops irm oncall alert-groups list --limit 0                   # no truncation, no hint
gcx --context=ops irm oncall alert-groups get IWDIPP8VLKENJ --open         # TTY browser open / agent-mode JSONL note
gcx --context=ops irm oncall alert-groups acknowledge IWDIPP8VLKENJ        # single-target shape: {action, target, changed}
gcx --context=ops irm oncall alert-groups acknowledge --max-age 1h --yes   # bulk shape: {action, summary, failures}; sum invariant
gcx --context=ops irm oncall alert-groups acknowledge ZZZINVALIDID         # canonical DetailedError on stderr; exit 1
gcx --context=ops irm oncall alert-groups acknowledge                      # exit 2; usage error message
```

## Open / deferred

| Item | Disposition |
|---|---|
| Backend AlertSerializer enrichment | Deferred indefinitely — the alertgroup-first path covers 99% drilldown without backend changes |
| List-pagination cap of 1000 in `listAlertGroupsRaw` | Defensive; revisit if real workflows hit it |
| Teams resolution leaks across commands within a single CLI process | Acceptable for CLI; flag if anything embeds gcx as a library |
| `render_for_web` extraction takes only `.title`, ignores `message`/`image_url`/`source_link` | Matches locked design; reconsider if a use case surfaces |
| Severity falls back to `groupLabels.severity` (defensible addition not in original spec) | Intentional; documented in code |
| Multi-entry `payload.alerts[]` lose per-entry distinguishing data in promoted view | Caveat documented in ADR § 2.2; `--include-raw` reveals the array without re-fetch |
| Off-package edit to `internal/output/format.go::allowedCodecs()` | One-line dedupe shipped in round 11 to fix `yaml`-twice in help. Affects all providers that register a custom codec on a builtin name; behaviour-neutral (lookup unchanged), but worth flagging for future codec-registry work |
| Narrow-terminal `-o wide` UX | lipgloss compresses columns silently when terminal width < sum of declared widths. Round 15 confirmed all columns are declared correctly. UX call open: emit a hint, or accept silent compression. |
| URL template migration `grafana-oncall-app` → `grafana-irm-app` | Existing template at `oncall_adapter.go:270` is on the legacy plugin path; modern Grafana redirects but it's stale. Defer to full D3 in `/plan-spec`. |
| 4 spike token-cost test failures | `gcx_irm_oncall_spike_*` POC commands lack `agent.AnnotationTokenCost` annotations. Self-resolves when spike POCs are deleted in `/plan-spec` cleanup. |
| Lint vendoring drift | `mise run lint` blocked by go.mod inconsistency. Pre-existing; surface during pre-flight, may need a `go mod tidy` pass. |

## How to re-open D2

If something needs re-litigating later, the entry points are:

1. **Add a promoted field**: extend `extractRule`/`extractDashboard`/`extractSLO` in `oncall_extract.go`. Update the fallback-chain table in ADR § 2.3.
2. **Reshape an existing block**: edit the type in `oncalltypes/rich.go`. The envelope conversion (`alertGroupRichToEnvelope` / `alertRichToEnvelope`) auto-picks up the new shape. Update ADR § 2.1 / § 2.2 YAML examples.
3. **Adjust default visibility** (e.g. show `raw` by default): flip the default of `--include-raw` in the three command opts setup functions. Update ADR § 2.6.
4. **Restore field ordering elsewhere** (e.g. on `alert-groups list`): convert that command's emission path to use a typed envelope + `orderedYAMLCodec`. Will require either keeping unstructured for the table codec via a second branch, or porting the table codec to read from envelope structs.
5. **Adjust column choice or width on a table view**: edit the `headers`/`rows` slice and `colWidths` argument inside `alertGroupTableCodec.Encode` (or `alertTableCodec.Encode`) in `internal/providers/irm/oncall_commands_extra.go`. The list/wide column set is the only place changing — there's no separate registry to update. To swap a row-builder helper, search for `r.format` calls inside `Encode`. Pattern reference: `internal/providers/traces/adaptive/commands.go:110` (`recommendationTableCodec`) and `internal/query/tempo/formatter.go:343` (`formatTrace` — the original `ColumnWidths` consumer).
6. **Add a new action verb** (resolve / silence / etc.): clone `runAcknowledge` + `executeAcknowledge` in `internal/providers/irm/oncall_actions.go`. The two-shape MutationResult + filter-resolution + confirmation logic is verb-agnostic and shared via `runAcknowledgeSingle` / `runAcknowledgeBulk`. Per-verb idempotency check varies (resolve = state==2; silence = non-idempotent so always POST). Register the new subcommand under `alert-groups`. Pattern reference: `oncall_actions.go` round-12-refactor (`110adb57`).
7. **Adjust `--limit` defaults or hint wording**: edit `alertGroupListOpts.setup()` (default value) and `emitAlertGroupListLimitHint` (hint string template) in `internal/providers/irm/oncall_commands_extra.go`. The three gating conditions (`limit > 0 && len == limit && serverHasMore`) are the cheapest reliable signal absent a `count` field on the OnCall alertgroups endpoint.
