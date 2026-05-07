# D2 Mining Round 14 — `--limit` flag for alert-groups list and list-alerts

Captured: 2026-05-07. Binary: feat-missing-oncall-features worktree. Stack: ops.

---

## API Pagination Support

### alertgroups (`/api/internal/v1/alertgroups/`)

Tested via `gcx --context=ops api '/api/plugins/grafana-irm-app/resources/alertgroups/?<param>=N'`.

| param | behavior |
|-------|----------|
| `perpage=N` | **Accepted.** Response `page_size` and `page_length` reflect N exactly. |
| `page_size=N` | Ignored — always returns default 25. Response `page_size` stays at 25. |
| `limit=N` | Ignored — always returns default 25. |

**Response shape** (alertgroups):
```json
{
  "next": "https://oncall-ops-eu-south-0.../alertgroups?cursor=<base64>&perpage=N",
  "previous": null,
  "page_size": N,
  "page_length": N,
  "results": [...]
}
```

- **Pagination scheme**: cursor-based (`?cursor=<base64>`). The `next` URL contains both `cursor` and `perpage`.
- **No `count` field** in alertgroups response. Only `page_size`, `page_length`, `next`, `previous`, `results`.
- **Default page size**: 25 (when no `perpage` param).
- **Max ceiling observed**: `perpage=500` works and returns 500 items (page_size=500). `perpage=250` returns 250. Did not find hard server ceiling in tests — ceiling is not the typical 100. The existing code cap of 1000 is client-side, not server-side.
- **Cursor format**: opaque base64 in `next` URL param; the `ExtractNextPath` function in the client strips the oncall-backend host and reconstructs the proxy path correctly.

### alerts (`/api/internal/v1/alerts/`)

Tested via `gcx --context=ops api '/api/plugins/grafana-irm-app/resources/alerts/?alert_group_id=<ID>&perpage=N'`.

| param | behavior |
|-------|----------|
| `perpage=N` | **Accepted.** Response reflects the requested N in `page_size`. |

**Response shape** (alerts):
```json
{
  "count": 8,
  "current_page_number": 1,
  "total_pages": 3,
  "next": "...",
  "previous": null,
  "page_size": 3,
  "page_length": 3,
  "results": [...]
}
```

- **`count` IS present** in the alerts response. It reflects the total number of alerts for the group across all pages.
- **Additional fields vs alertgroups**: `current_page_number`, `total_pages` — richer pagination metadata.
- **Default page size**: 10 (when no `perpage` param).
- Pagination also cursor/page-based; `next` URL present when more pages exist.

---

## Total / Count Affordance

| Endpoint | count in response | notes |
|----------|-----------------|-------|
| `alertgroups/` | **No.** | Response has `page_size`, `page_length`, `next`, `previous` only. No total count. |
| `alerts/` | **Yes.** | `response.count` = total alerts for the group across all pages. Available on first page. |

**For alertgroups list**: No `count` in the response. The only way to get total would be to drain all pages (expensive). There is no cheap HEAD request or count endpoint identified. The post-result hint for alertgroups list cannot show a reliable total and must fall back to `"limited to N — pass --limit 0 to disable"` style.

**For list-alerts**: `count` is in the first-page response (`listAlertIDs` already reads it and returns it as the second return value). The existing hint at `oncall_commands_extra.go:593` already uses this: `"retrieved %d of %d alerts; pass --limit 0 to fetch all"`. The total is reliably available.

---

## Current List Code Today

### alertgroups: `listAlertGroupsRaw`

- **File**: `internal/providers/irm/oncall_commands_extra.go:346`
- **Current cap**: `const hardCap = 1000` (line 388)
- **Loop shape**: Manual pagination loop — fetches page, appends `page.Results` to `out []json.RawMessage`, checks `len(out) >= hardCap` to break, follows `page.Next` via `ExtractNextPath`. Does NOT pass `perpage` to the API — the server uses its default page size of 25, so reaching 1000 items requires 40 round trips.
- **Filters**: Built via `buildAlertGroupListParams` into `url.Values`, composed with `alertGroupListFilters` (statuses, is_root, teams, integrations, mine, with_resolution_note, has_related_incident, started_at). Filters are passed as query params on the first page URL; subsequent pages follow the cursor URL from `next` which already encodes filters.
- **Call site**: `newAlertGroupListCommand` (line 238) calls `listAlertGroupsRaw` only when `client.(*OnCallClient)` succeeds (OAuth proxy path). SA-token path falls through to `listAlertGroupsLegacy`.
- **Passing limit**: To support `--limit`, the caller needs to pass a cap into `listAlertGroupsRaw` (replacing the hardcoded `hardCap = 1000`), AND pass `perpage=min(limit, 500)` to the API to reduce round trips.

### alerts: `listAlertIDs` (used by list-alerts)

- **File**: `internal/providers/irm/oncall_rich_client.go:279`
- **Current behavior**: Takes `limit int`, passes it as `perpage=N` to the API (line 283). Only fetches one page.
- **Count return**: Returns `(ids []string, total int, error)` — total comes from `response.count`. When `page.Count == 0`, falls back to `len(ids)`.
- **Cap constant** (for the command): `alertGroupsListAlertsCap = 100` at `oncall_commands_extra.go:42`.
- **`--limit` flag already exists**: `alertGroupListAlertsOpts.Limit` with default `alertGroupsListAlertsCap=100` (line 540). Flag is bound at `oncall_commands_extra.go:541`.
- **Hint already implemented**: `oncall_commands_extra.go:592-594` — `"warn: retrieved %d of %d alerts; pass --limit 0 to fetch all"` emitted to stderr when `total > len(ids)`.
- **Call site for limit**: `limit` from `opts.Limit` is passed directly to `listAlertIDs` (line 585).

---

## Precedents

### synth/checks list

- **File**: `internal/providers/synth/checks/commands.go:64`
- **Flag**: `--limit int64`, default=50, description: `"Maximum number of items to return (0 for all)"`
- **Applied at**: client (via `adapter.LimitedListFn` in resource_adapter, then `TruncateSlice`).
- **Hint**: None — no post-result stderr message about truncation or how to get more.
- **Shows total**: No.

### slo/reports list

- **File**: `internal/providers/slo/reports/commands.go:64`
- **Flag**: `--limit int64`, default=50, description: `"Maximum number of items to return (0 for all)"`
- **Applied at**: codec-truncate (`adapter.TruncateSlice(rpts, opts.Limit)` at line 94) — after full fetch.
- **Hint**: None — no post-result message.
- **Shows total**: No.

Both precedents: flag name is `--limit`, default is 50, type is int64 (not int), no hint text, no total-display, applied via TruncateSlice after full client fetch rather than early-termination pagination.

**Notable contrast with IRM**: `list-alerts` already has `--limit int` (not int64) with a post-result hint using `count` from the API response. The alertgroups list does not yet have `--limit` at all.

---

## Edge Cases Observed

1. **`perpage` param on alertgroups, NOT `page_size`**: The internal OnCall API uses `perpage` for server-side page size control. Passing `page_size=2` is silently ignored and the server returns the default 25 items. The code's current `listAlertGroupsRaw` passes no `perpage` param, so each page is 25 items — 40 pages to reach the 1000 cap.

2. **alertgroups has NO `count` field, alerts has one**: The two endpoints diverge here. `count` on alertgroups is absent entirely, while alerts always includes it. This asymmetry means alertgroups list hints must be "limited to N" style without showing total, while list-alerts can already show "N of M" total — and does.

3. **`list-alerts --limit` already exists and works** with the count-aware hint. The orchestrator's task description referenced a "1000 cap" for alertgroups, but for alerts the cap is `alertGroupsListAlertsCap = 100`. The 1000 hardcap is specific to `listAlertGroupsRaw` only.

4. **`perpage` up to at least 500** confirmed working for alertgroups. Server returns exactly the requested number (no silent ceiling at 25 or 100). Setting `perpage=min(limit, 500)` in the request would reduce pagination round trips proportionally.

5. **`listAlertIDs` fetches only one page** — it does not follow pagination. This means if `limit > perpage` (which never happens since `perpage=limit`), alerts beyond the first page are silently omitted. For `--limit 0` (no cap), the caller would need to handle multi-page fetch in `listAlertIDs` or fall back to the cursor-following `iterResources` path.
