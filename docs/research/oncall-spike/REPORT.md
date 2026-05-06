# Spike Report — ADR 001 (oncall-feature-expansion) Buildability

**Date**: 2026-05-05
**ADR**: [`docs/adrs/oncall-feature-expansion/001-sre-expansion.md`](../../adrs/oncall-feature-expansion/001-sre-expansion.md)
**Stack tested against**: `ops` (OAuth context). `dev` is SA-token mode and the spikes type-assert to the OAuth `*OnCallClient` so they don't run there — the public-API (SA token) path is a separate code path the ADR will need to handle, see "Open questions" below.

## TL;DR

| Decision | Spike verdict | Implementation status | Detail |
|---|---|---|---|
| D1 — `alert-groups list` defaults | 🟢 GREEN | ✅ **shipped** (2026-05-06) | [`d1-implementation.md`](d1-implementation.md) |
| D2 — rich `Alert` and `AlertGroup` payload | 🟡 YELLOW | ✅ **shipped** (2026-05-06) | [`d2-implementation.md`](d2-implementation.md) |
| D3 — `--open` retrofit | 🟢 GREEN | open | 7 URL templates to backfill; templates may be stale (`grafana-oncall-app` → `grafana-irm-app`) |
| D4 — `notifications send` | 🟡 YELLOW | open | team is PK-only on input; PK-vs-username needs decision; test mode is OAuth-only |
| D5 — `shifts list` filter composition | 🟡 YELLOW | open | "my upcoming oncall" needs fan-out (44s on 123 schedules); existing `ListShifts` has a silent-ignore bug |
| D6 — bulk-by-filter on action verbs | 🟢 GREEN | open | list-then-iterate composes cleanly |
| D7 — agent-mode output contract | 🟢 GREEN | open | single JSON on stdout + JSONL on stderr verified end-to-end |
| D8 — hint conventions | 🟢 GREEN | open | TTY/agent mode switch driven by existing `agent.IsAgentMode()` |

**Overall verdict: 🟡 YELLOW** — all 8 decisions are buildable. No hard blockers. Two production bugs in existing code surfaced as side findings.

**Iteration model**: each finding moves through `spike → hack/test/iterate → ADR update → ship`. D1 and D2 have shipped (see per-finding `*-implementation.md`); the rest follow the same model in dedicated sessions.

## Per-decision detail

Full per-decision reports with captured JSON, timing data, and per-scenario tables:

- **D2** → [`d2-rich-alert.md`](d2-rich-alert.md)
- **D4** → [`d4-notifications-send.md`](d4-notifications-send.md)
- **D5** → [`d5-shifts-list.md`](d5-shifts-list.md)
- **D1/D3/D6/D7/D8** → [`d1d3d6d7d8-client-demo.md`](d1d3d6d7d8-client-demo.md)

Runnable spike commands (registered as hidden subcommands under `gcx irm oncall spike`):

```bash
gcx --context=ops irm oncall spike d2 <alert-group-id>
gcx --context=ops irm oncall spike d4 --user-ids me --test --via push --dry-run
gcx --context=ops irm oncall spike d5 --schedule <schedule-id> --at now
gcx --context=ops irm oncall spike client-demo --max-age 24h --simulate-bulk-ack
```

Spike implementation lives in `internal/providers/irm/spike*.go` (5 files including the harness). The whole tree is hidden from help output and tagged `[POC]` — delete when the ADR is built.

## Headline findings

### 1. ADR Decision 2 has a better path than the ADR proposes

The ADR's premise — "the OnCall internal API actually returns the full Alertmanager payload that we discard" — is half-true. The list endpoint `/alerts/?alert_group_id=X` uses the slim `AlertSerializer` and does NOT include `raw_request_data`. The retrieve endpoint `/alerts/<id>/` uses `AlertRawSerializer` and does. **However**, `/alertgroups/<id>/` already includes `last_alert.raw_request_data` inline. For the primary SRE use case ("what is this alert about?"), the alertgroup-first path delivers the rich payload in one round-trip. Per-alert N+1 is only needed when the caller explicitly asks for *all* alerts in the group, and the implementation should cap that fan-out.

**Implication**: ship D2 without backend changes. Update the ADR to describe the alertgroup-first path; raise an enrichment of `AlertSerializer` to the IRM team as a separate follow-up.

### 2. Two ADR text bugs in D2's promoted-field extraction

ADR claims `dashboardUID` and `panelID` live in `alerts[*].labels`. They actually live in `annotations.__dashboardUid__` and `annotations.__panelId__` (Grafana-managed alerts only). ADR also claims `alertGroupUID` is extractable from `labels.grafana_folder_uid` — that label does not exist; `grafana_folder` is the folder *name* (string), not a UID. These are documentation bugs only — the extraction logic itself works once aimed at the right keys.

### 3. ADR Decision 5's `effectiveUsers[]` is misplaced

The ADR proposes `effectiveUsers[]` as a derived field on `Shift` (the rotation rule). But `filter_events` returns events with `users[]` and a time window — there is **no `shift_id` back-reference** on the event, so events cannot be joined back to individual rotation-rule `Shift` objects. The right home for `effectiveUsers[]` is at the per-schedule-window level, not per `Shift`. ADR Decision 5 should be revised; the SRE coverage scenarios still all work, just with a different shape on the response.

### 4. D5 "my upcoming oncall" is expensive without optimization

There is no `/users/{id}/upcoming-shifts/` endpoint, and `oncall_shifts/` does not accept `?user=`. To answer `shifts list --user me --from now --to "+30d"` we must fan-out: `schedules/` → `schedules/<id>/filter_events/` per schedule → filter events by user. **44 seconds** on the `ops` stack with 123 schedules. Mitigations:
- Bounded-concurrency fan-out (existing `errgroup` pattern in gcx) — cuts to roughly serial-time / concurrency.
- Cache schedule membership so only schedules containing the user are queried.
- Stream a `note:` progress hint to stderr while fanning out.

`shifts list --schedule X --at now` is one round-trip via `schedules/<id>/on_call_now`. `shifts list --schedule X --from A --to B` is one `filter_events` call. Only the user-pivoted form is expensive.

### 5. D4 ships with three caveats, not one

a. `DirectPagingInput.Team` accepts a PK only — slug/name is a separate `team_name` field on the backend not currently in the Go type. Adding `TeamName string \`json:"team_name,omitempty"\`` to `oncalltypes.DirectPagingInput` covers the ADR's `--team prod-sre` case.

b. `--user-ids me,bob` needs a PK-vs-username disambiguation strategy. `me` resolves cleanly via `GET user/`; arbitrary entries need either (i) eager username→PK lookup, (ii) a heuristic (`U`-prefixed = PK), or (iii) two flags (`--user-pks`, `--usernames`). Pick one and document.

c. The three test endpoints (`make_test_call`, `send_test_sms`, `send_test_push`) are on the IRM plugin proxy — only reachable from OAuth contexts. SA-token contexts use the public OnCall API which doesn't expose these. Document in `--test`'s help and CHANGELOG that `--test` requires an OAuth context.

### 6. D3's URL templates may already be stale

Existing OnCall URL templates in `internal/providers/irm/oncall_adapter.go` use `/a/grafana-oncall-app/...`. The live alertgroup permalink in `dev` returned `https://dev.grafana-dev.net/a/grafana-irm-app/alert-groups/...`. Either:
- The legacy `grafana-oncall-app` route still resolves (modern Grafana redirects it) — verify before shipping; or
- The templates are stale and need updating to `grafana-irm-app` alongside the D3 backfills.

D3's backfill list — EscalationPolicy, Shift, Route, Team, ResolutionNote, ShiftSwap, User — was confirmed (none have `URLTemplate` set in `oncall_adapter.go`).

## Cross-provider pivot — receiving end (D2 follow-on)

Verified that the cross-provider pivots D2 enables actually land somewhere useful:

- `gcx alert rules get <alertRuleUID>` — accepts plain UIDs; returns rule definition with labels and annotations.
- `gcx alert instances list --rule <alertRuleUID>` — arguably *better* drill-target for an SRE; shows live state (`Alerting`, `Normal`, `Pending`), `activeAt`, value, full label set per instance. Recommend promoting this in D2's post-result hint:
  ```
  hint: See live instances: gcx alert instances list --rule <alertRuleUID>
  hint: Inspect rule:        gcx alert rules get <alertRuleUID>
  ```
- **Naming overlap to flag**: `gcx alert groups list` returns rule *groups* (folder buckets), unrelated to OnCall's *alert* group (Alertmanager group). Hint copy should pivot to `alert rules` / `alert instances`, never to `alert groups`, to avoid confusing agents.

## Production bugs found in existing code (not part of the ADR)

These are pre-existing issues that surfaced during the spike:

1. **`ListShifts` uses the wrong query param**. `internal/providers/irm/oncall_client.go` passes `?schedule=<id>` to `oncall_shifts/` which the backend silently ignores (correct param is `?schedule_id=` per `apps/api/views/on_call_shifts.py:138`). Result: passing a schedule filter currently returns shifts from unrelated schedules. Worth a separate fix.
2. **`alerts get <id>` returns "not found"** on `dev` even when the ID was just returned by `list-alerts`. Worth investigating — likely path-construction bug in the public-API client. (`gcx irm oncall alerts get AF7HQFU6SGVPH` failed on `dev` against an ID `list-alerts` had just yielded.)

Both should land as their own beads tasks separate from the ADR work.

## Implementation effort summary

| Decision | Effort | Blockers? |
|---|---|---|
| D1 | trivial | none — backend filters already work |
| D2 | moderate | none — alertgroup-first sidesteps the N+1; backend AlertSerializer enrichment is nice-to-have follow-up |
| D3 | trivial | none — registry pattern already proven; backfill is mechanical |
| D4 | moderate | needs `DirectPagingInput.TeamName` field + decision on PK-vs-username for `--user-ids` |
| D5 | moderate-significant | needs bounded-concurrency fan-out + ADR shape revision for `effectiveUsers[]` + fix to `ListShifts` |
| D6 | trivial | confirmation prompt + `--yes` + agent-mode fail-fast are pure CLI work |
| D7 | trivial | already enforced by single `json.Encoder` on stdout + JSONL writer on stderr |
| D8 | trivial | wraps existing `agent.IsAgentMode()` |

No backend changes are *required* to ship the ADR. They would *improve* D2 (richer list endpoint) and *enable* D5's user-pivoted view (a `/users/{id}/upcoming-shifts/` endpoint). Both are good follow-up asks for the IRM team.

## Open questions for the ADR author

1. **SA-token contexts**: the spikes only validated the OAuth/plugin-proxy path. The ADR doesn't explicitly address whether all new commands work in SA-token mode (which routes through the public OnCall API via `oncallpublic.Client`). At minimum: D4's test endpoints don't exist on the public API. Decision needed: gate them on context type and document, or add public-API equivalents.
2. **`--user-ids` PK-vs-username strategy**: pick one of the three options listed in finding #5b.
3. **D5 `effectiveUsers[]` location**: confirm willingness to attach at schedule-window level rather than per-`Shift`. Update the ADR table accordingly.
4. **D3 plugin route**: confirm whether `/a/grafana-oncall-app/...` redirects to `/a/grafana-irm-app/...` on modern Grafana, or whether the existing templates should be migrated wholesale.

## Appendix — ADR text edits required

Sections of the ADR that need updating to match observed reality:

- **§ 2 (Decision 2) — promoted field source paths.** Replace `Payload.Labels / Payload.Annotations` with the corrected paths: `dashboardUID` ← `annotations.__dashboardUid__`, `panelID` ← `annotations.__panelId__`, `alertGroupUID` ← (no sound source — drop or pivot to `groupKey`), `alertRuleUID` ← `labels.__alert_rule_uid__`, `alertInstanceID` ← `fingerprint`.
- **§ 5 (Decision 5) — `effectiveUsers[]` location.** Move from per-`Shift` to per-schedule-window in the response shape. Acknowledge the absence of `/users/{id}/upcoming-shifts/` and the fan-out cost.
- **§ 4 (Decision 4) — team field shape.** Note that the underlying API uses `team` (PK) and `team_name` (slug), and that the Go type needs `TeamName string`.
- **§ 4 (Decision 4) — `--test` precondition.** Note OAuth-only.
- **§ 7.4 — `--user-ids` resolution.** Add a sentence on PK-vs-username strategy.
