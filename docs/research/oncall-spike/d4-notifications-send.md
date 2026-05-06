# Spike D4: notifications send — YELLOW

## What we tested

We built a throwaway Cobra command (`gcx irm oncall spike d4`) registered via the `spike.go` harness and ran five smoke tests against the `ops` Grafana Cloud stack (OAuth mode). The command hits `GET user/` (singular) to resolve `me` to a user PK, then dispatches to either the three test endpoints (`POST users/<pk>/send_test_push|send_test_sms|make_test_call/`) or builds a `DirectPagingInput` body for `POST direct_paging` (dry-run only — the live POST path is guarded behind `--dry-run=false` which no smoke test invokes). The spike also surfaces both `team` (PK field) and `team_name` (name/slug field) variants for team escalations, since `DirectPagingInput` only models the former.

## /user/ shape (for "me" resolution)

```json
{
  "avatar": "/avatar/b0ebe217495e61d99220ca8982d4c222dc6fd186ac254cd77d32b3788b323134",
  "avatar_full": "https://ops.grafana-ops.net/avatar/b0ebe217...",
  "cloud_connection_status": null,
  "current_team": "TKH52TW6TH7UE",
  "email": "igor.suleymanov@grafana.com",
  "google_calendar_settings": null,
  "google_oauth2_token_is_missing_scopes": false,
  "has_google_oauth2_connected": false,
  "hide_phone_number": false,
  "irm_working_hours": null,
  "is_currently_oncall": true,
  "messaging_backends": {
    "EMAIL": { "email": "igor.suleymanov@grafana.com" },
    "MOBILE_APP": { "connected": true, "device_count": 1, "max_devices": 10 },
    "MOBILE_APP_CRITICAL": { "connected": true, "device_count": 1, "max_devices": 10 },
    "MSTEAMS": null,
    "WEBHOOK": null
  },
  "name": "Igor Suleymanov",
  "notification_chain_verbal": {
    "default": "Slack - 5 min - Mobile push",
    "important": "5 min - Mobile push - 5 min - Mobile push important"
  },
  "organization": { "name": "raintank", "pk": "OYQ71QGV9MYR6" },
  "pk": "UDDXE78B9BQE2",
  "rbac_permissions": [
    { "action": "grafana-irm-app.alert-groups:direct-paging" },
    { "action": "grafana-irm-app.notification-settings:read" },
    { "action": "grafana-irm-app.notification-settings:write" },
    { "action": "grafana-irm-app.user-settings:read" },
    { "action": "grafana-irm-app.user-settings:write" }
  ],
  "role": 2,
  "slack_user_identity": {
    "display_name": "igor",
    "name": "Igor Suleymanov",
    "slack_id": "U01HR2SF5JS",
    "slack_login": "igor.suleymanov"
  },
  "telegram_configuration": null,
  "timezone": "Europe/Helsinki",
  "unverified_phone_number": "+358406864448",
  "username": "igor.suleymanov@grafana.com",
  "verified_phone_number": "+358406864448",
  "working_hours": { "monday": [...], "tuesday": [...], ... }
}
```

Key fields for implementation: `pk` is the public primary key to use everywhere (PKs look like `U[A-Z0-9]+`). `username` is the email-style login. `messaging_backends.MOBILE_APP.connected` tells whether push tests will work. `is_currently_oncall` and `current_team` are useful SRE-persona display fields. `notification_chain_verbal` gives a human-readable summary of the user's notification policy — good for `--wide` table column. The `rbac_permissions` array contains the RBAC actions the calling user holds; `grafana-irm-app.alert-groups:direct-paging` confirms the caller can send pages.

## Test endpoints

### push (`POST users/<id>/send_test_push/`)

- Status: 200
- Body: (empty)
- Notes: Delivered to the mobile app immediately. The backend code (`user.py:628`) reads an optional `critical` query param (`?critical=true`) but ignores it for our empty POST; no body is required. The endpoint is throttled (`TestPushThrottler`). `MOBILE_APP.connected: true` in the `/user/` response confirmed the device was set up — if it weren't, the backend would return `400 "Mobile device not connected"`.

### sms (`POST users/<id>/send_test_sms/`)

- Status: 200
- Body: (empty)
- Notes: Delivered successfully. `verified_phone_number` was populated in the `/user/` response, which is the prerequisite. Backend returns `400 "Phone number is not verified"` when unverified (see `user.py:606`). Also throttled.

### call (`POST users/<id>/make_test_call/`)

- Status: 200
- Body: (empty)
- Notes: Backend accepted it. Whether the physical call goes through depends on the configured phone provider. The `ProviderNotSupports` error path (`user.py:601`) returns `500 "Phone provider not supports phone calls"` when the provider doesn't support voice — that's a known failure mode for environments without Twilio/similar configured.

## /direct_paging/ contract

### Request body (built by spike) — user mode

```json
{
  "title": "DRY-RUN escalation",
  "message": "DRY-RUN escalation",
  "users": [
    {
      "id": "UDDXE78B9BQE2",
      "important": true
    }
  ]
}
```

### Request body — team mode

```json
{
  "title": "DRY-RUN team page",
  "message": "DRY-RUN team page",
  "team": "some-slug",
  "important_team_escalation": true
}
```

### Did the backend accept the field as we built it?

- `users[].id` accepts **PK only** (via `public_primary_key` lookup in `UserReferenceSerializer.validate`, `direct_paging.py:38`). The type also accepts `username` as a mutually-exclusive alternative field. The current `DirectPagingInput.Users[].ID` field maps to the `id` (PK) path. To send username instead, callers must populate `UserReference.Username` and leave `ID` empty — the backend validates exactly one of the two is set.
- `team` accepts **PK only**. The serializer uses `TeamPrimaryKeyRelatedField` (`direct_paging.py:52`). To look up by name/slug, callers must use the separate `team_name` field (`direct_paging.py:53`), which is not modelled in `DirectPagingInput`. The implementation will need to either add a `TeamName` field to `DirectPagingInput` or resolve team name → PK client-side before building the body.
- `--important` is **per-user** when `--user-ids` is used (each `UserReference.Important = true`) and **top-level** when `--team` is used (`important_team_escalation = true` per `BasePagingSerializer` line 54). The two modes are mutually exclusive — `users` and `team` cannot both be set in the same request (`BasePagingSerializer.validate` line 98–99).

### Key serializer constraint: users and team are mutually exclusive

`BasePagingSerializer.validate` (line 98–99) raises a validation error if both `users` and `team` are set. The `notifications send` implementation must enforce this on the CLI side and emit a structured error when both `--user-ids` and `--team` are given.

### Spike requirement for team_name support

`DirectPagingInput` only has `Team string` (mapped to the `team` PK field). The backend also exposes `team_name` (string, by team name) as a separate field on the same serializer. The spike demonstrated both request body variants. For the real implementation, either: (a) add a `TeamName string` field to `DirectPagingInput`, or (b) add a client-side `ListTeams` lookup to resolve name → PK before building the body. Option (a) is simpler and keeps the client thin; option (b) avoids a serializer-schema dependency.

### OAuth-only constraint

The spike type-asserts the `OnCallAPI` interface to `*OnCallClient` for raw `DoRequest` access to the test endpoints (which are not on the `OnCallAPI` interface). This works only in OAuth mode (`ops` context). With an SA token the loader returns `*oncallpublic.Client`, which does not implement `DoRequest`. The test endpoints (`make_test_call`, `send_test_sms`, `send_test_push`) are internal-API-only endpoints that are not exposed through the OnCall public API, so the `--test` mode of the real `notifications send` command will be OAuth-only. This is consistent with the existing limitation documented in `docs/adrs/oncall-feature-expansion/` — SA token mode uses the public API, which lacks internal-API-only actions.

## Verdict: YELLOW

**YELLOW** = ships with caveats — three things the ADR specifies that require implementation decisions beyond what the spike exercised:

1. **`team` field is PK-only; the ADR example `--team prod-sre` uses a name/slug**: The serializer (`direct_paging.py:52`) uses `TeamPrimaryKeyRelatedField` for `team`, accepting only the PK form (e.g. `TKH52TW6TH7UE`). Slug/name lookup requires the separate `team_name` field on the same serializer (`direct_paging.py:53`), which `DirectPagingInput` does not model. The real implementation must either add `TeamName string` to `DirectPagingInput` or perform client-side `ListTeams` resolution before building the body. This is a spec-level gap in the current type, not a backend blocker.

2. **`--user-ids me,bob` — `bob` as a username is supported by the serializer but not by the spike harness**: `UserReferenceSerializer.validate` accepts either `id` (PK) or `username` as mutually exclusive fields (`direct_paging.py:30–43`). The spike only populates `UserReference.ID` (the PK path). Passing a non-PK identifier like `bob` via `--user-ids` would need detection logic (heuristic: if the value matches the OnCall PK pattern `[A-Z][A-Z0-9]{9,}`, use `id`; otherwise use `username`), or the command can expose separate `--user-pks` / `--user-names` flags. The ADR implies both work transparently under `--user-ids`; the implementation must decide which disambiguation strategy to use.

3. **`--test` mode is OAuth-only**: The test endpoints (`make_test_call`, `send_test_sms`, `send_test_push`) are internal-API-only and are not exposed through the OnCall public API. SA token users (`auth-method: token`) cannot use `--test`. This is a capability gap that should be documented in command help and in the CHANGELOG. Real pages via `direct_paging` in non-dry-run mode are not affected — the public API client supports `CreateDirectPaging`.

## Implementation effort estimate

trivial — the spike is already ~200 lines and covers the full dispatch logic. The real implementation adds: proper error wrapping, MutationResult envelope, DetailedError, agent-mode output contract, and the `team_name` field addition to `DirectPagingInput`. No blocked-on-backend items.

## Surprises

- The `dev` context uses SA token auth (`auth-method: token`), so `rawClient.(*OnCallClient)` type assertion fails there. The spike errors clearly with the mode name. The real `notifications send` command must handle both auth modes (internal API for OAuth, reject `--test` for SA token with a structured error).
- `/user/` returns `rbac_permissions` as an array of `{action: string}` objects — this is a flat list, not a map. The spec writer should note this if RBAC checks are added to client-side validation.
- `messaging_backends.MOBILE_APP.connected` in the `/user/` response is a useful pre-flight signal: if `connected: false`, skip push test and tell the user to install the mobile app. This is a `note:` the implementation can emit on the success path.
- `verified_phone_number` vs `unverified_phone_number` are separate fields. The implementation can check `verified_phone_number != ""` before attempting SMS/call and emit a `warn:` if unverified.
- The `notification_chain_verbal` field is not documented in the existing `User` type in `oncalltypes/types.go`. It's worth adding to the type for the `--wide` table codec.
- `current_team` in the `/user/` response is a team PK, not a slug — this is directly usable in the `team` field of `DirectPagingInput` without a lookup, useful as a "page my team" default.
