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

## How to re-open D2

If something needs re-litigating later, the entry points are:

1. **Add a promoted field**: extend `extractRule`/`extractDashboard`/`extractSLO` in `oncall_extract.go`. Update the fallback-chain table in ADR § 2.3.
2. **Reshape an existing block**: edit the type in `oncalltypes/rich.go`. The envelope conversion (`alertGroupRichToEnvelope` / `alertRichToEnvelope`) auto-picks up the new shape. Update ADR § 2.1 / § 2.2 YAML examples.
3. **Adjust default visibility** (e.g. show `raw` by default): flip the default of `--include-raw` in the three command opts setup functions. Update ADR § 2.6.
4. **Restore field ordering elsewhere** (e.g. on `alert-groups list`): convert that command's emission path to use a typed envelope + `orderedYAMLCodec`. Will require either keeping unstructured for the table codec via a second branch, or porting the table codec to read from envelope structs.
5. **Adjust column choice or width on a table view**: edit the `headers`/`rows` slice and `colWidths` argument inside `alertGroupTableCodec.Encode` (or `alertTableCodec.Encode`) in `internal/providers/irm/oncall_commands_extra.go`. The list/wide column set is the only place changing — there's no separate registry to update. To swap a row-builder helper, search for `r.format` calls inside `Encode`. Pattern reference: `internal/providers/traces/adaptive/commands.go:110` (`recommendationTableCodec`) and `internal/query/tempo/formatter.go:343` (`formatTrace` — the original `ColumnWidths` consumer).
