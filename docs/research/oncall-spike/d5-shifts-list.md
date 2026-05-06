# Spike D5: shifts list filter composition — YELLOW

## What we tested

We ran a Go spike command (`gcx irm oncall spike d5`) against the `ops` Grafana Cloud stack (OAuth proxy mode) to validate whether the four SRE coverage questions from ADR 001 Decision 5 can be answered by composing `--at`, `--from/--to`, `--schedule`, `--user`, `--team`, and `--mine` filter flags on `shifts list`. We tested all four scenarios, captured real API responses and timings, identified a filter-parameter bug in the existing `ListShifts` client (wrong query param name), and measured the fan-out cost for the "my upcoming shifts" scenario.

## Endpoint shapes captured (real JSON, truncated)

### `schedules/<id>/` (relevant fields)
```json
{
  "id": "SFF4JZKPZUQX2",
  "name": "Platform Infrastructure Primary",
  "type": 0,
  "time_zone": "Etc/UTC",
  "team": "TSWYWYCY7ZBEH",
  "on_call_now": [
    {
      "pk": "UQDZL7K6STK6P",
      "username": "patrick.nelsen@grafana.com",
      "avatar": "/avatar/ff76...",
      "avatar_full": "https://ops.grafana-ops.net/avatar/ff76..."
    }
  ]
}
```

### `schedules/<id>/filter_events/?starting_date=...&days=...&user_tz=UTC&type=final`
```json
{
  "id": "SFF4JZKPZUQX2",
  "name": "Platform Infrastructure Primary",
  "type": 0,
  "events": [
    {
      "start": "2026-05-03T03:00:00Z",
      "end": "2026-05-04T03:00:00Z",
      "users": [
        { "display_name": "eloy.morenogarcia@grafana.com", "pk": "U48I13JWMGPWV", "email": "eloy.morenogarcia@grafana.com" }
      ]
    },
    {
      "start": "2026-05-05T07:00:00Z",
      "end": "2026-05-05T15:00:00Z",
      "is_override": true,
      "users": [
        { "display_name": "niko.smeds@grafana.com", "pk": "U5KALMMH9N8BS", "email": "niko.smeds@grafana.com" }
      ]
    },
    {
      "start": "2026-05-05T15:00:00Z",
      "end": "2026-05-06T01:00:00Z",
      "users": [
        { "display_name": "patrick.nelsen@grafana.com", "pk": "UQDZL7K6STK6P", "email": "patrick.nelsen@grafana.com" }
      ]
    }
  ]
}
```

### `oncall_shifts/?schedule_id=<id>` (canonical Shift shape)

**Important bug found:** the existing `ListShifts` call in `oncall_client.go:424` uses no filter at all, and a `?schedule=` (no `_id`) param is silently ignored by the backend. The correct query param is `?schedule_id=` as documented in the backend source (`apps/api/views/on_call_shifts.py` line 138: `request.query_params.get("schedule_id", None)`). Using `?schedule=` returns unrelated shifts from other schedules.

```json
{
  "results": [
    {
      "id": "O6CMLHE4YS5B7",
      "name": "Platform Infrastructure EMEA Weekday Primary Shift",
      "type": 2,
      "schedule": "SFF4JZKPZUQX2",
      "priority_level": 0,
      "shift_start": "2024-01-08T03:00:00Z",
      "shift_end": "2024-01-08T15:00:00Z",
      "rotation_start": "2022-02-28T03:00:00Z",
      "until": "2025-01-31T23:59:59Z",
      "frequency": 1,
      "interval": 1,
      "by_day": ["MO", "TU", "WE", "TH", "FR"],
      "week_start": "MO",
      "rolling_users": [["UFCZEWUQ3WAJ2"], ["UANZPCBAMMFGQ"], ["UV44DY1UFFS74"], ["UX3U2QS5BDV7R"], ["UI53IQZFCQT14"], ["UHJRZRKZM1HRJ"], ["UJUQGNICY5TSC"]],
      "start_rotation_from_user_index": 3,
      "updated_shift": null
    }
  ],
  "next": null
}
```

## How well does each scenario compose?

| Scenario | Endpoint(s) | Works? | Notes |
|---|---|---|---|
| who-on-call-now `--schedule X --at now` | `schedules/<id>/` `on_call_now` field | ✓ | One RTT (~1s), instant answer. `on_call_now` is a flat user-pk+username array on the schedule object. Alternatively: `filter_events` with 2-day backward window + `start <= now < end` derivation also works (~650ms) and provides richer event context (is_override, etc.). Both paths confirmed live. |
| schedule range `--schedule X --from A --to B` | `filter_events?starting_date&days&user_tz=UTC&type=final` | ✓ | 31-day window returned 55 events in 1 RTT (~1s). Derived `effectiveUsers[]` can be populated by filtering events where `start <= now < end` (point-in-time) or collecting all events in the window (range). The `is_override` flag is present per event, so override events can be flagged in the derived array. |
| my upcoming `--user me --from now --to +30d` | iterate all schedules + `filter_events` per schedule | ✓ with caveat | Fan-out across 123 schedules took **44 seconds** (123 RTTs, ~350ms avg each). Found 4 matching events for user `igor.suleymanov@grafana.com` on 2 schedules. There is no `/users/{id}/upcoming_shifts/` or `/oncall_shifts/?user_id=` endpoint — fan-out is the only path. For production use this is O(N schedules) and will be slow on large orgs. See "Surprises" for mitigation options. |
| team coverage `--team T` | `schedules/?team=<team_id>` (API-side) | ✓ | The backend supports `?team=<team_id>` on the schedules list endpoint. Tested live: `schedules/?team=TSWYWYCY7ZBEH` returned 2 schedules in 1.4s. Once schedules are filtered by team, `filter_events` per schedule gives full coverage with O(N_team_schedules) RTTs (typically small). |

## Canonical Shift shape vs FlatShift

The ADR (Decision 5) requires the same `Shift` shape across both tiers. Currently `shifts list` returns `FlatShift` (`oncalltypes/types.go:295`), which is a derived per-user row from `filter_events`. The ADR proposes keeping the canonical `Shift` shape and adding a derived `effectiveUsers[]` field populated from `filter_events` when a time window is queried.

Can we populate `effectiveUsers[]` from `filter_events` events while keeping the canonical `Shift` shape? **Yes.** The `Shift` object (rotation rule) has an `ID` field that matches shifts returned by `oncall_shifts/?schedule_id=`. The `filter_events` endpoint returns resolved events with `start`, `end`, `users[]{pk, email, display_name}`, and `is_override`. Joining on the event users back to the rotation-rule `Shift` requires knowing which shift rule generated each event — but the `filter_events` response does not include a `shift_id` back-reference per event. This means `effectiveUsers[]` must be attached to the `Schedule` or emitted as a standalone derived list, not joined back to individual `Shift` rotation rules. The cleanest shape is: when filters are present, return `Shift` objects annotated with a derived `effectiveUsers[]` populated from all `filter_events` events that overlap the requested window, scoped to the schedule (not per rotation rule). This is slightly looser than "which rotation rule is active" but gives the right answer for the SRE coverage use case.

What does an `effectiveUsers[]` element need to carry to be useful? At minimum: `start` (RFC3339), `end` (RFC3339), `userPK` (string), `userEmail` (string), `isOverride` (bool). The `display_name` from the event is also useful. This is exactly what `FinalShiftEvent.Users` provides today.

Does this conflict with anything existing? No — `effectiveUsers` is an `omitempty` field not present on the `Shift` struct today. Adding it additively is non-breaking. The existing `FlatShift` type can be removed once `effectiveUsers[]` covers its use cases.

## Verdict: YELLOW

All four SRE scenarios compose correctly against the real API. The "who is on call now" and team/schedule-range scenarios are fast (1-2 RTTs, sub-2s). The "my upcoming shifts" fan-out is the headline gap: 123 RTTs and 44 seconds on a 123-schedule org is not production-viable for a default `shifts list --user me` invocation. Everything else works as designed.

## Surprises

**`?schedule=` is silently wrong.** The `ListShifts` method in `oncall_client.go` accepts no schedule filter, and the existing spike used `?schedule=` which the backend silently ignores (returns unrelated shifts from other schedules). The correct parameter is `?schedule_id=`, verified against the IRM backend source (`on_call_shifts.py:138`). This needs to be fixed in the production `ListShifts` implementation.

**`filter_events` `starting_date` must precede atTime.** For `--at now` (point-in-time), the `starting_date` must be set 1-2 days before `atTime`, not at `atTime` itself. Events that started before the `starting_date` are excluded from the response even if they extend past `atTime`. The spike compensates by setting `startDate = atTime - 48h` for point-in-time queries.

**`on_call_now` in `Schedule` type is `any`.** The `Schedule.OnCallNow` field is typed as `any` in `oncalltypes/types.go:87`, which is correct for JSON flexibility but means consumers must type-assert or re-unmarshal to get `pk`/`username` fields. The `filter_events` derived path gives a more strongly typed result via `FinalShiftEvent.Users`.

**API-side team filter on schedules works.** We expected this might require client-side filtering (the `Schedule.Team` field is `any`), but `schedules/?team=<team_id>` returns correctly filtered results from the API. No client-side filtering needed for the `--team` flag.

**`filter_events` does not include a `shift_id` back-reference.** Events in the response have `start`, `end`, `users`, `is_gap`, `is_override`, `all_day` but no reference back to the canonical shift rotation rule that produced them. This means `effectiveUsers[]` can be attached at the schedule level (per-window), not at the per-shift-rule level. For the SRE use cases this is fine — the SRE wants "who is on call" not "which rotation rule fires when".

## Implementation effort estimate

moderate — the four filter scenarios work but require: (1) fixing the `?schedule_id=` bug in `ListShifts`, (2) adding a `FilterShifts` method that calls `oncall_shifts/?schedule_id=` then joins with `filter_events` to populate `effectiveUsers[]`, (3) implementing fan-out logic for `--user` with a progress hint about latency, and (4) adding pagination/caching consideration for large orgs on the fan-out path. The `effectiveUsers[]` design is straightforward given the data available. No backend changes needed.
