---
generated_by: /iterate-spike
last_updated: 2026-05-06
---

# Iteration state — oncall-feature-expansion (ADR 001)

**Mode**: per-finding
**Current finding**: D2 — Rich `AlertGroup` / `Alert` shapes (re-opened for two sub-decisions)
**Current round**: 9 lifter committed `c3e978ab`; 10 shape locked (no-op)
**Status**: round 9 awaiting smoke-tester then doc-editor; round 10 closed (status-quo confirmed)

## Locked shape so far (from shipped D2)

```yaml
# AlertGroup — shipped, see ADR § 2.1
metadata: {name, namespace, creationTimestamp}
spec:
  integration: {id, name, type}
  team: {id, name}
  permalinks: {web, slack, slack_app, telegram}
status:
  title: ...
  summary: ...
  severity: ...
  state: ...                 # decoded enum
  runbookURL: ...
  target: {cluster, service, namespace}
  timestamps: {started, acknowledged, resolved, silenced}
  links:
    alert: {rule: {uid,url}, instance: {id, silenceURL}}
    dashboard: {uid, url, panel: {id, url}}
    slo: {uid, name}
  alertsCount: N
  raw: {commonLabels, commonAnnotations, groupLabels}   # opt-in via --include-raw

# Alert — shipped, see ADR § 2.2
metadata: {name, namespace, creationTimestamp}
spec:
  alertGroupID: ""           # ⚠ EMPTY for `alerts get` (backend AlertRawSerializer omits alert_group_pk)
status:
  # mirrors AlertGroup.status (target, severity, state, links, ...)
  raw: {fullAlertmanagerWebhook}   # opt-in via --include-raw
```

## Sub-decisions

| ID | Question | Status | Answer |
|----|----------|--------|--------|
| 2.A | AlertGroup-first vs N+1 from list | answered (round 1) | both — alertgroup-first for primary; list-alerts N+1 with `--slim` opt-out and 100-cap |
| 2.B | AlertGroup ergonomics (status enum, team PK, render_for_web) | answered (round 2) | decode state, resolve team name via cached ListTeams, drop render_for_web wall, extract `.title` |
| 2.C | Promoted-field source paths (3 ADR text bugs) | answered (round 3) | dashboardUID ← annotations.__dashboardUid__; panelID ← annotations.__panelId__; alertGroupUID dropped |
| 2.D | Two integration shapes (grafana_alerting vs alertmanager) | answered (round 4) | ordered fallback chains per field; documented ADR § 2.3 |
| 2.E | K8s envelope + hierarchical status | answered (round 5) | metadata/spec/status; `target`, `timestamps`, `links` sub-blocks |
| 2.F | SLO lift + `links` umbrella | answered (round 6) | `status.links.{alert,dashboard,slo}` |
| 2.G | `--include-raw` flag + raw rename | answered (round 7) | hide raw by default; opt-in flag; rename `payload`→`raw` for symmetry |
| 2.H | Field ordering (typed envelope + ordered YAML codec) | answered (round 8) | `goccy/go-yaml` with `UseJSONMarshaler`; struct field order preserved |
| 2.I | Should `alerts get <id>` exist at all? | answered (round 9) | **AXE** — empty `spec` orphans the resource from group context; all real entry points already have group ID. Lifter committed `c3e978ab`. Awaiting smoke + ADR doc-edit. |
| 2.J | Collapse `acknowledge`/`resolve`/`silence` + `un*` counterparts under `--undo`? | answered (round 10) | **NO** — keep all six separate. silence is a TTL'd resource, not a toggle, so `--undo` story breaks at silence; agent/human readability of `unacknowledge` beats `acknowledge --undo`. ADR § 7.1 already lists six verbs; no edit needed. |

## Last user feedback (verbatim)

> let's continue our iteration on D2. Next item on the list - should we axe `irm oncall alerts get`?

## Last subagent summaries

None yet for round 9 — entering brainstorm with prior-round shipped state as the anchor.

## Findings done

- D1: shipped — see `d1-implementation.md` (commit `b8ca8c2f`)
- D2 rounds 1–8: shipped in same commit; D2 itself remains "open" pending sub-decision 2.I

## Findings remaining

- D3: not started (URL template backfill + `grafana-oncall-app` → `grafana-irm-app` migration)
- D4: not started (`notifications send`)
- D5: not started (`shifts list` filter composition)
- D6: not started (bulk-by-filter on action verbs)
- D7: not started (agent-mode output contract)
- D8: not started (hint conventions)
