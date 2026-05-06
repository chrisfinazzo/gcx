# Spike D2: Rich Alert Payload — YELLOW

## What we tested

We ran the spike against a live Grafana ops stack (`ops.grafana-ops.net`) using an OAuth-authenticated context, hitting the OnCall internal API via the IRM plugin proxy. We probed three endpoints for alert group `IT328XLNNBI23` (1 alert, Alertmanager integration, Grafana SLO origin): the list endpoint `alerts/?alert_group_id=IT328XLNNBI23`, the retrieve endpoint `alerts/AXDF6HWZXHFJX/`, and the alertgroup endpoint `alertgroups/IT328XLNNBI23/`. All calls were made with `gcx irm oncall spike d2` via `DoRequest` on `*OnCallClient`. The retrieve call was repeated 3 times to get a stable timing sample. Note: the spike requires an OAuth context (`auth-method: oauth`) — SA-token contexts route through the public API client which does not support raw internal API calls.

## Endpoint shapes (real captured JSON)

### LIST `/alerts/?alert_group_id=...`

```json
{
  "id": "AXDF6HWZXHFJX",
  "link_to_upstream_details": null,
  "render_for_web": {
    "title": "AlertingEnricherEnrichment - Error Budget Burn Rate is Very High (prod-eu-west-2, grafana-alertmanager)",
    "message": "<p>Status: firing ...</p>",
    "image_url": null,
    "source_link": "https://ops.grafana-ops.net/alerting/grafana/dfh3ygs5lr2tcd/view"
  },
  "created_at": "2026-05-05T15:53:42.945550Z",
  "rule_name": "AlertingEnricherEnrichment - Error Budget Burn Rate is Very High",
  "generator_url": "https://ops.grafana-ops.net/alerting/grafana/dfh3ygs5lr2tcd/view"
}
```

Observation: no labels, no annotations, no fingerprint. The `render_for_web` is a pre-rendered HTML blob. `rule_name` and `generator_url` are extracted by `AlertSerializer.get_rule_name()` / `get_generator_url()` from `raw_request_data` server-side.

### RETRIEVE `/alerts/<id>/`

```json
{
  "id": "AXDF6HWZXHFJX",
  "raw_request_data": {
    "alerts": [
      {
        "endsAt": "0001-01-01T00:00:00Z",
        "labels": {
          "team": "alerting",
          "cluster": "prod-eu-west-2",
          "service": "alerting-enricher",
          "severity": "warning",
          "alertname": "AlertingEnricherEnrichment - Error Budget Burn Rate is Very High",
          "namespace": "grafana-alertmanager",
          "service_name": "alerting",
          "grafana_folder": "Grafana SLO",
          "__grafana_origin": "plugin/grafana-slo-app",
          "grafana_slo_uuid": "63v1halth7vbup43dm0ss",
          "__alert_rule_uid__": "dfh3ygs5lr2tcd",
          "grafana_slo_severity": "critical",
          "__grafana_managed_route__": "deployment-tools",
          "__bypass_imported_global_am_allowlist": "true"
        },
        "status": "firing",
        "startsAt": "2026-05-05T15:53:00Z",
        "annotations": {
          "name": "FastErrorBudgetBurn",
          "slo_name": "AlertingEnricherEnrichment",
          "__orgId__": "1",
          "__panelId__": "1",
          "description": "Alerting Enricher burns its alert enrichment error budget too fast over the last 5m and 1h.",
          "runbook_url": "https://github.com/grafana/deployment_tools/blob/master/docs/alerting-enricher/runbooks.md#AlertingEnricherErrorBudgetBurn",
          "dashboard_url": "https://ops.grafana-ops.net/d/34d6798a0fe7cbf8fac99b6a005299ac/alerting-enricher-enrichment?...",
          "__dashboardUid__": "grafana_slo_app-63v1halth7vbup43dm0ss"
        },
        "fingerprint": "fe3164fa91641147",
        "generatorURL": "https://ops.grafana-ops.net/alerting/grafana/dfh3ygs5lr2tcd/view"
      }
    ],
    "status": "firing",
    "version": "4",
    "groupKey": "{...}:{alertname=\"...\", cluster=\"prod-eu-west-2\", namespace=\"grafana-alertmanager\"}",
    "receiver": "grafana-oncall-am-alerting-prod",
    "numFiring": 1,
    "externalURL": "https://ops.grafana-ops.net/",
    "groupLabels": { "cluster": "prod-eu-west-2", "alertname": "AlertingEnricherEnrichment...", "namespace": "grafana-alertmanager" },
    "numResolved": 0,
    "commonLabels": { "__alert_rule_uid__": "dfh3ygs5lr2tcd", ... },
    "truncatedAlerts": 0,
    "commonAnnotations": { "__dashboardUid__": "grafana_slo_app-63v1halth7vbup43dm0ss", "__panelId__": "1", ... }
  }
}
```

Observation: full Alertmanager webhook payload is present. `raw_request_data` is the verbatim Alertmanager POST body stored by OnCall on receipt. Every Grafana-managed alert carries `__alert_rule_uid__` in labels and `__dashboardUid__` / `__panelId__` in annotations (not labels).

### ALERTGROUP `/alertgroups/<id>/` (bonus check)

The alertgroup endpoint returns an inline `last_alert` field that includes `raw_request_data` — the same full payload as the retrieve endpoint. For groups with a single alert this is identical to calling `alerts/<id>/`. For multi-alert groups it provides only the most-recent alert's payload. Additionally, the alertgroup response includes an `alerts` array but each entry in it uses `AlertSerializer` (truncated, no `raw_request_data`).

```json
{
  "pk": "IT328XLNNBI23",
  "alerts_count": 1,
  "last_alert": {
    "id": "AXDF6HWZXHFJX",
    "raw_request_data": { "...full Alertmanager payload..." }
  },
  "alerts": [
    { "id": "AXDF6HWZXHFJX", "render_for_web": "...", "rule_name": "...", "generator_url": "..." }
  ],
  "render_for_web": { "title": "...", "message": "...<rendered HTML>..." }
}
```

Observation: `last_alert.raw_request_data` is a no-cost shortcut for the "last alert's labels/annotations" use case. This is the most important discovery from the bonus check: **for the most common SRE use case (what is this alert firing about, what are its labels) a single `alertgroups/<id>/` call already delivers the rich payload via `last_alert`**.

## Promoted-field extraction results

Tested against a Grafana SLO Alertmanager alert. Labels and annotation key names are Grafana-managed conventions.

| Promoted field | Found? | Source path | Notes |
|---|---|---|---|
| alertRuleUID | yes | `raw_request_data.alerts[0].labels.__alert_rule_uid__` | Present in both `alerts[0].labels` and `commonLabels`. Stable Grafana convention. |
| dashboardUID | no | `raw_request_data.alerts[0].labels.dashboard_uid` | NOT in labels. Found in `annotations.__dashboardUid__` (double-underscore). ADR extraction path is wrong label name. |
| panelID | no | `raw_request_data.alerts[0].labels.panel_id` | NOT in labels. Found in `annotations.__panelId__` (double-underscore). ADR extraction path is wrong label name. |
| alertGroupUID | no | `raw_request_data.alerts[0].labels.grafana_folder_uid` | Not present. `grafana_folder` label has the folder name (string), not a UID. There is no stable `alertGroupUID` in the payload — the OnCall alert group PK is the closest analog. |
| alertInstanceID (fingerprint) | yes | `raw_request_data.alerts[0].fingerprint` | Present. Standard Alertmanager field. |

**Corrected extraction paths:**
- `dashboardUID` → `raw_request_data.alerts[0].annotations.__dashboardUid__` (or `commonAnnotations.__dashboardUid__`)
- `panelID` → `raw_request_data.alerts[0].annotations.__panelId__` (or `commonAnnotations.__panelId__`)
- `alertGroupUID` → not extractable from alert payload; best candidate is `alertgroups/<id>/` PK which gcx already has as the alert group ID

## Timing

- List call: 425ms (single call to `alerts/?alert_group_id=IT328XLNNBI23`)
- Per-retrieve call: 147ms mean (3 iterations, same alert ID — lower bound due to server caching)
- N+1 total for a group with N=1 alert: ~573ms (list + 1 retrieve)
- Alertgroup single call: 208ms (returns `last_alert.raw_request_data` inline)

For a group with K alerts the N+1 cost is approximately `425ms + K × 147ms`. At K=10 that is ~1.9s, at K=37 (a group seen in the test stack) it is ~5.9s. The alertgroup path (208ms) does not scale with K but delivers only the last alert's payload, not all K payloads.

## Verdict: YELLOW

**YELLOW** — ADR claim partially holds. The internal API does store and expose the full Alertmanager payload, confirming the ADR's core assumption. However, three caveats require the ADR to be updated before implementation:

1. **N+1 is the only path to per-alert rich payloads from the list endpoint.** `alerts/?alert_group_id=X` uses `AlertSerializer` which strips `raw_request_data`. Getting rich data for all K alerts in a group requires K retrieve calls. For typical groups (1-5 alerts) this is acceptable (< 1s); for large groups (37-92 alerts observed) it is not. **gcx should NOT do a blind N+1 on list.** Instead, gcx should fetch rich data lazily: call `alertgroups/<id>/` (which provides `last_alert.raw_request_data` for free), and only fan out to per-alert retrieves when the caller explicitly requests all alerts.

2. **`dashboardUID` and `panelID` are in annotations, not labels.** The ADR's `AlertPayload` struct extraction comment says to look in `labels.dashboard_uid_label_name` and `labels.panel_id` — both wrong. The actual Grafana convention uses `annotations.__dashboardUid__` and `annotations.__panelId__` (double-underscore prefix). The extraction code must be updated.

3. **`alertGroupUID` is not a real field in the Alertmanager payload.** The ADR proposes promoting `grafana_folder_uid` — that label does not exist. `grafana_folder` (the folder name, not UID) is present. The promoted field should be removed from the ADR or re-scoped to a folder-name field. The OnCall alert group PK (`AlertGroup.PK` / `AlertGroup.ID`) already serves as the group identity anchor and is available from the parent call.

## Recommended next step

The N+1 problem is the most important caveat. The recommended implementation strategy avoids it by exploiting the `alertgroups/<id>/` response: (1) call `alertgroups/<id>/` instead of `alerts/?alert_group_id=<id>` when the user wants rich context for a specific group — `last_alert.raw_request_data` gives promoted-field data in one call; (2) when the user explicitly calls `alert-groups list-alerts <group-id>` for ALL alerts, do the N+1 but cap it at a configurable page size (e.g. first 10 alerts) and document the limit clearly. The backend fix that would eliminate N+1 entirely would be to add `raw_request_data` to `AlertSerializer` for the list action — a small Django change that should be proposed to the IRM backend team as a follow-up. The corrected extraction paths (`annotations.__dashboardUid__`, `annotations.__panelId__`) must be reflected in the `AlertPayload` extraction code before shipping.

## Implementation effort estimate

moderate — the type changes and extraction logic are straightforward, but the N+1 avoidance strategy (alertgroup-first path, lazy fan-out cap) requires a small rethink of the `alert-groups list-alerts` command flow. Backend change to `AlertSerializer` is optional and can be done as a separate follow-up.
