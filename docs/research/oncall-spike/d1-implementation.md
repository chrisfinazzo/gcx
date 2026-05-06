# D1 — `alert-groups list` actionable defaults — implementation log

**Status**: shipped to the branch. ADR § 1 unchanged (the locked behaviour matched what the spike already specified). End-to-end smoke-tested against the `ops` stack.

**Companion docs**:
- [`d1d3d6d7d8-client-demo.md`](d1d3d6d7d8-client-demo.md) — original combined spike verdict (GREEN; pure client-side composition).
- [`../../adrs/oncall-feature-expansion/001-sre-expansion.md`](../../adrs/oncall-feature-expansion/001-sre-expansion.md) § 1 — the locked design.

D1 was lifted in the same session as D2 once the user noticed the real `alert-groups list` command still had only `--max-age` while the spike had a working filter set ready to go. The ADR § 1 spec was already locked from the spike phase — no design iteration needed, just implementation.

## What landed

### Flag set (matches ADR § 1 verbatim)

| Flag | Wire param | Notes |
|---|---|---|
| `--state` (multi: `firing\|acknowledged\|resolved\|silenced`) | `status=0&status=1&status=2&status=3` (multi-int) | String input; client edge translates to int |
| `--team` (multi) | `team=<pk>&team=<pk>` | PK only |
| `--integration` (multi) | `integration=<pk>` | PK only |
| `--mine` (bool) | `mine=true` | Backend resolves to authenticated user |
| `--with-resolution-note` (bool) | `with_resolution_note=true` | |
| `--has-related-incident` (bool) | `has_related_incident=true` | |
| `--all` (bool) | (omits status + is_root params) | Bypasses defaults entirely |
| `--include-child-groups` (bool) | (omits is_root param) | Drops is_root, keeps status default |
| `--max-age` (existing) | `started_at=<from>_<to>` | ISO range |

### Defaults (the "actionable" flip)

- `is_root=true` unless `--all` or `--include-child-groups`.
- `status=firing,acknowledged,silenced` (integers `0,1,3`) unless `--all` or `--state` was explicitly user-set.
- `cmd.Flags().Changed("state")` is the canonical "user touched this flag" probe — not `len(opts.States) == 0`, which can't distinguish "default" from "explicitly empty".

### State string → int translation

Centralized in `stateNameToInt` in `oncall_commands_extra.go`. Accepts `firing`/`new`, `acknowledged`/`ack`, `resolved`, `silenced`. Unknown values produce a clean structured error at the command edge.

### SA-token (public-API) path

The OnCall public API (used in SA-token mode) doesn't honor every filter the internal API does. Best-effort passthrough:

- Honored: `state` (string form), `team_id`, `integration_id`, `started_at`.
- Not honored: `is_root`, `mine`, `with_resolution_note`, `has_related_incident`.

When a user passes a non-honored filter under SA-token mode, the command emits a `note: SA-token mode uses the OnCall public API which does not honor: ...` line on stderr. Silent drop would be a footgun.

## Files touched

- `internal/providers/irm/oncalltypes/api.go` — extended `ListConfig` with `Statuses`, `IsRoot`, `Teams`, `Integrations`, `Mine`, `WithResolutionNote`, `HasRelatedIncident`. Added matching `WithStatuses`/`WithIsRoot`/`WithTeams`/`WithIntegrations`/`WithMine`/`WithWithResolutionNote`/`WithHasRelatedIncident` option helpers.
- `internal/providers/irm/oncall_client.go` — refactored `ListAlertGroups` to use new `buildAlertGroupListParams` helper that emits the integer status encoding, repeatable team/integration, scalar bool filters, and the existing `started_at` window.
- `internal/providers/irm/oncallpublic/client.go` — best-effort filter passthrough for SA-token mode.
- `internal/providers/irm/oncall_commands_extra.go` — added the new flags on `alertGroupListOpts`, `stateNameToInt`, `alertGroupListFilters`, `resolveAlertGroupListFilters` (default resolver). Rewired both `listAlertGroupsRaw` (OAuth path) and `listAlertGroupsLegacy` (SA-token path) to consume the resolved filter struct. Added `alertGroupListLong` help text documenting the defaults.

## Smoke tests passing on `ops`

```bash
# default — applies new defaults, returns root non-resolved groups in window
gcx --context=ops irm oncall alert-groups list --max-age 24h          # 153

# --all bypass — returns more (resolved + child groups)
gcx --context=ops irm oncall alert-groups list --max-age 24h --all    # 1000 (capped)

# --state explicit
gcx --context=ops irm oncall alert-groups list --max-age 24h --state firing                  # 146
gcx --context=ops irm oncall alert-groups list --max-age 24h --state firing,acknowledged     # 154

# --include-child-groups drops is_root, keeps status default
gcx --context=ops irm oncall alert-groups list --max-age 24h --include-child-groups          # 154

# scope filters
gcx --context=ops irm oncall alert-groups list --max-age 720h --mine                          # 0 (user has no current groups)
gcx --context=ops irm oncall alert-groups list --max-age 720h --has-related-incident          # 5
gcx --context=ops irm oncall alert-groups list --max-age 720h --integration <pk>              # narrowed
gcx --context=ops irm oncall alert-groups list --max-age 720h --team <pk>                     # narrowed

# error path is structured
gcx --context=ops irm oncall alert-groups list --state bogus                                  # DetailedError envelope on stdout
```

## Open / deferred

| Item | Disposition |
|---|---|
| `--mine` SA-token mode coverage | Public API doesn't honor `mine` — emits `note:` warning; could be implemented in oncallpublic by querying `/users/current/` and filtering client-side, but deferred |
| Internal 1000-cap on `listAlertGroupsRaw` | Inherited from D2 lift; defensive number; revisit if a real workflow hits it |
| CHANGELOG entry for breaking default flip | Release-time concern; not a code change. ADR § 1 / § Consequences flag this loudly |
| Filter flag naming (`--state` vs `--status`) | Locked per ADR; avoids confusion with bulk action verb's existing `--status` flag |

## How to re-open D1

If something needs adjustment:

1. **Add a new filter flag**: add a field to `alertGroupListOpts` + binding in `setup`; thread through `alertGroupListFilters` + `resolveAlertGroupListFilters`; add the wire param in `buildAlertGroupListParams` (and the public-API translation in `oncallpublic/client.go` if the SA-token path can honor it).
2. **Adjust default behaviour**: edit `resolveAlertGroupListFilters`. The full default-resolution logic is centralized there.
3. **Add a state alias**: extend `stateNameToInt`. Currently aliases: `new→firing`, `ack→acknowledged`.
4. **Discover-time field projection** (`--json status.foo.bar`): no extra work — the existing `FieldSelectCodec` handles it via the default `toMap` path.
