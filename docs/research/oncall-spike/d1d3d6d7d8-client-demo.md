# Spike D1/D3/D6/D7/D8 client-side composition — GREEN

## What we tested

We exercised ADR 001 Decisions 1, 3, 6, 7, and 8 in a single combined `gcx irm oncall spike client-demo` command built against the `ops` context (OAuth, `https://ops.grafana-ops.net`). The spike listed alert groups with the proposed D1 defaults, probed the deeplink registry for all OnCall resource kinds (D3), simulated bulk-by-filter acknowledge without mutation (D6), emitted a single MutationResult JSON document on stdout (D7), and emitted plain-text hints in TTY mode and JSONL diagnostics in agent mode (D8). All five decisions were validated end-to-end in a single run.

## D1 — list defaults

Default query (no flags, `--max-age 720h`): `?is_root=true&started_at=<range>&status=0&status=1&status=3`. The status values use the integer encoding the backend requires: `0=New(firing)`, `1=Acknowledged`, `3=Silenced`. The backend accepts these and returns only active groups. Using `status=firing` (string) produced HTTP 400 `"Select a valid choice. firing is not one of the available choices."` — the internal API only accepts integer choices. This is an important implementation note: the ADR says `status=firing,acknowledged,silenced` in human-readable terms, but the wire encoding must use integers.

Backend respects the filters: **yes, confirmed**. With `--max-age 1h` on the ops stack: default filter returned 13 groups (firing/acked/silenced, root-only); `--all` (no status/is_root) returned 25 groups on first page. The difference confirms both `status` and `is_root` params are applied by the backend.

`--all` produces an empty filter set: **yes** — only `started_at` appears in the query URL.

Sample: matched 13 items with default vs 25 with `--all` (1h window, ops stack).

## D3 — --open inventory

URL templates registered today for OnCall resources (from the deeplink registry, probed at runtime via `deeplink.Resolve`):

- AlertGroup: **yes** — `/a/grafana-oncall-app/alert-groups/{name}` (registered in `oncall_adapter.go:270`)
- Schedule: **yes** — `/a/grafana-oncall-app/schedules/{name}`
- Integration: **yes** — `/a/grafana-oncall-app/integrations/{name}`
- EscalationChain: **yes** — `/a/grafana-oncall-app/escalation-chains/{name}`
- Webhook: **yes** — `/a/grafana-oncall-app/outgoing-webhooks/{name}`
- EscalationPolicy: **MISSING** — no URLTemplate in `oncall_adapter.go` registration block 3
- Shift: **MISSING** — no URLTemplate in registration block 5
- Route: **MISSING** — no URLTemplate in registration block 6
- User: **MISSING** — no URLTemplate in registration block 9
- Team: **MISSING** — no URLTemplate in registration block 10
- ResolutionNote: **MISSING** — not registered as adapter resource (client-only)
- ShiftSwap: **MISSING** — not registered as adapter resource (client-only)

ADR backfill list (must add URLTemplate): EscalationPolicy, Shift, Route, User, Team, ResolutionNote, ShiftSwap.

`--open` with a matched group resolved the URL via the registry and printed it to stderr (channel discipline: `--open` output is a diagnostic, not a result document). The URL correctly included the alert group PK. Note: in the ops context with a proxy-endpoint configured, `c.Host` returns the proxy path (`https://assistant-ops.../api/cli/v1/proxy`) rather than the bare Grafana server URL. In production implementation, the deeplink should use `restCfg.Config.Host` (the canonical Grafana server URL) rather than the OnCallClient's internal host, since deeplinks are for browser navigation. This is a D3 implementation note, not a blocker.

## D6 — bulk-by-filter simulation

The spike listed all groups matching the D1 defaults, then iterated and emitted a "would POST" record for each — no `DoRequest` was called.

Sample stderr trace (truncated):

```
# D1 query: alertgroups/?is_root=true&started_at=...&status=0&status=1&status=3
{"event":"matched","class":"info","alertGroupId":"IZ8GQKDTTFUC2","status":0}
{"event":"matched","class":"info","alertGroupId":"IT328XLNNBI23","status":0}
...
# D6 would POST: alertgroups/IZ8GQKDTTFUC2/acknowledge/ body={}
{"event":"would_acknowledge","class":"info","alertGroupId":"IZ8GQKDTTFUC2"}
# D6 would POST: alertgroups/IT328XLNNBI23/acknowledge/ body={}
{"event":"would_acknowledge","class":"info","alertGroupId":"IT328XLNNBI23"}
```

Sample stdout (the MutationResult envelope, ONE JSON document):

```json
{"action":"acknowledge","target":{"alertGroupIds":["IZ8GQKDTTFUC2","IT328XLNNBI23"]},"changed":false,"summary":{"matched":25,"succeeded":0,"failed":0,"dryRun":true}}
```

## D7 — stream contract

stdout was single JSON document: **yes** — verified by parsing the file with `json.JSONDecoder` and confirming exactly 1 document. With `--open`, the resolved URL is emitted to stderr (not stdout), so stdout remains a single parseable JSON object in all invocation modes.

stderr was JSONL records: **yes** — progress events (`event` field) and diagnostic records (`class` field) all emitted as line-delimited JSON. The `# --open URL:` line also goes to stderr (plain text, prefixed with `#`).

No interleaving: **yes** — stdout is the result envelope only; progress, diagnostics, and `--open` URL all go to stderr.

## D8 — hint emission

TTY mode stderr (sample, `GCX_AGENT_MODE=false`):

```
hint: Drill into alerts: gcx irm oncall alert-groups list-alerts IZ8GQKDTTFUC2
hint: Simulate bulk-ack: rerun with --simulate-bulk-ack
```

Agent mode stderr (sample, `GCX_AGENT_MODE=true`):

```jsonl
{"class":"hint","summary":"Drill into alerts","command":"gcx irm oncall alert-groups list-alerts IFNI2FKWPKV8H"}
```

The form changes correctly between modes. `agent.IsAgentMode()` (from `internal/agent`) drives the switch — no env-var fallback needed. Note: when running under Claude Code the `CLAUDECODE=1` env var is always set, so default runs without `GCX_AGENT_MODE=false` emit JSONL. This is correct behaviour.

## Verdict: GREEN

- D1: **GREEN** — backend accepts status integer codes and `is_root`; defaults produce the right filter. Key implementation note: wire encoding is integers (0,1,2,3), not strings. The ADR text must clarify this.
- D3: **GREEN** — registry lookup works end-to-end; `--open` resolves registered templates correctly. 7 resource kinds need URLTemplate backfill (see inventory above).
- D6: **GREEN** — list-then-iterate loop works; no mutation risk; progress events emit per-item on stderr as specified.
- D7: **GREEN** — single JSON doc on stdout confirmed; JSONL on stderr confirmed; no interleaving.
- D8: **GREEN** — `warn:`/`note:`/`hint:` in TTY mode, `{"class":...}` JSONL in agent mode; mode switch works correctly via `agent.IsAgentMode()`.

## Implementation effort estimate (per decision)

- D1: **trivial** — change the default params in the existing `ListAlertGroups` path; update the opts struct to carry status and is_root booleans. The integer-encoding gotcha must be documented.
- D3: **trivial** — add 7 `URLTemplate` strings to `oncall_adapter.go` registration blocks; ResolutionNote and ShiftSwap also need adapter registrations if `--open` is to work for them.
- D6: **moderate** — requires the confirmation prompt, `--yes` flag, agent-mode fail-fast, and error aggregation per item. The list-then-iterate pattern is proven; the prompt and error envelope are the real work.
- D7: **trivial** — the MutationResult struct and single-doc stdout pattern are straightforward. The only complexity is the partial-failure case (not exercised in this spike).
- D8: **trivial** — the `emitStderr` helper pattern is clean and generic; wiring hint content per command is mechanical.

## Surprises

The internal API uses **integer status codes** (0=New/firing, 1=Acknowledged, 2=Resolved, 3=Silenced), not the human-readable strings `firing/acknowledged/resolved/silenced`. Passing strings produces HTTP 400. The ADR text should be updated to note that the public-facing flag accepts strings but the wire encoding uses integers, and the client layer translates. This is not a decision change — just an implementation clarification.

`c.Host` on `*OnCallClient` in OAuth-proxy mode is set to the proxy endpoint path rather than the canonical Grafana server URL (because OAuth contexts configure `proxy-endpoint` in the gcx config). For D3 deeplink URL generation, the implementation should use the Grafana server URL from the context config directly, not the `OnCallClient.Host` field.
