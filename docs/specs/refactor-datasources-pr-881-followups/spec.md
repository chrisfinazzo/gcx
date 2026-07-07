---
type: refactor-spec
title: "Datasources K8s transport refactor and PR #881 review remediation"
status: approved
created: 2026-07-07
---

# Datasources K8s transport refactor and PR #881 review remediation

## Context

PR #881 (merged, commit `d1b8a61c`) completed the `gcx datasources` CRUD
lifecycle and registered datasources as a first-class resource. A validated
code review (`docs/reviews/2026-07-01-pr-881.md`, 0% hallucination rate)
produced one Critical, three Major, and eight Minor findings plus two lead
follow-ups. The single post-merge commit (#893) touched an unrelated query
command and fixed none of them, so every finding is confirmed present on
current `main`.

This spec remediates that finding set in **one follow-up PR**. The centerpiece
is a Tier-1 + Tier-2 rewrite of the hand-rolled datasources K8s transport
(`internal/datasources/k8stransport.go` + `servedgroups.go`) to reuse existing
gcx client-go machinery, plus targeted correctness, security, UX, fidelity, and
documentation fixes, plus codification of the required CONSTITUTION exception.

Human approval for the CONSTITUTION TypedCRUD-exception waiver is **granted** —
the governance workstream *codifies* it (ADR + clause amendment + ADR-table
row); it does not seek approval.

## Scope

### In Scope

- Rewrite of the K8s half of the dual transport to reuse the cached discovery
  Registry (Tier 1) and the shared dynamic namespaced client (Tier 2), confined
  to `internal/datasources/`.
- A single, uniform K8s→REST fallback policy (Major #1 + m8).
- Security hardening: pluginID path-injection validation (m1), bounded stdin
  read (m2).
- UX / error fixes: 403 reclassification (Lead-Minor), basic-auth warning
  false-positive (m3), `health --type`+UID conflict (m4), `--limit` help (m6).
- Round-trip fidelity note/passthrough for unmodeled fields (m5).
- Documentation: ADR codification (Critical #1 / Major #3), CONSTITUTION
  exception-clause amendment, ARCHITECTURE ADR-table row, `docs/architecture/`
  provider-shape + GVK-normalizer description, stale package-doc fix, research
  doc §9.6 refresh (m7).
- Fix of the pre-existing `resources pull datasources` malformed/duplicate-GVK
  bug, folded into the discovery/normalizer cleanup.
- Transport-package test updates plus M1 regression tests.

### Out of Scope / Non-Goals

- **M2 (`TestUpdateDryRunDoesNotWrite`) is DEFERRED** — deselected by the
  requester. The behavior was empirically verified in the review's live smoke
  test; only the standalone command-layer test is missing. Tracked as a deferred
  follow-up. This does NOT waive the transport rewrite's own tests or the M1
  regression tests, which are in scope.
- No change to the provider ResourceAdapter / router integration
  (`internal/providers/datasources/`) — the review confirmed it is correct.
- No new datasource features or commands.
- No change to the legacy REST `Client` (`client.go`) or the `dualTransport`
  fallback dispatcher (`dualtransport.go`) beyond what error-normalization
  requires.

## Current Structure

```
gcx datasources {list,get,create,update,      gcx resources {get,pull,push,delete} datasources
  delete,health}                                 datasourceAdapter (providers/datasources)
      │ dsclient.NewTransport(cfg)                     │ dsclient.NewTransport(cfg)
      └──────────────────┬──────────────────────────────┘
                         ▼
              dualTransport (dualtransport.go) — k8sThenREST[T], keyed on errK8sNotServed
                         │
      ┌──────────────────┴───────────────────────┐
      ▼                                           ▼
k8sTransport (k8stransport.go)             REST Client (client.go)
 hand-rolled net/http do(GET/POST/PUT/DEL)  /api/datasources  (permission-aware,
 ├─ servedGroupCache.discover()                complete — the fallback target)
 │    GET /apis, parse groups[].name           returns typed dsclient.APIError
 │    process-scoped cache only (no disk)
 │    non-200 incl. 5xx → (nil,nil)   ◄── masks 5xx discovery as "not served"  (Major #1)
 │    network err: writes swallow → REST, reads propagate  ◄── asymmetric      (Major #1)
 ├─ collectionPath(pluginID) = fmt.Sprintf(.../%s/...)  ◄── pluginID unescaped  (m1)
 ├─ listAll(): loop served groups, build UID→pluginID index
 │    any group non-200 → incomplete → errK8sNotServed   ◄── silent demotion, no log (m8)
 ├─ GetByUID / Delete / Health: resolveGroup(uid) via index; health = raw subresource GET
 ├─ Create/Update: served() gate, POST/PUT; 409 surfaced as typed APIError
 └─ fromK8s(): closed DataSourceSpec struct   ◄── drops unmodeled top-level fields (m5)

manifest.go readBytes("-"): io.ReadAll(stdin)  ◄── unbounded                   (m2)
cmd/gcx/fail/convert.go: 401 AND 403 → "Authentication failed" + `gcx login`   (Lead-Minor)
secrets.go secretLikelyRequired: BasicAuth && no SecureJSONData → warn         (m3)
```

**Pain points / technical debt:**

- `servedGroupCache.discover()` hand-GETs `/apis` and parses `groups[].name`,
  duplicating the cached discovery client in `internal/resources/discovery`
  (which already models per-plugin datasource groups via
  `LookupPreferredPerGroup`) and lacking its `~/.cache/gcx/discovery/` disk
  cache (process-scoped only).
- Per-op CRUD is raw `net/http` `do()`, duplicating
  `internal/resources/dynamic.NamespacedClient` — the same client the dashboards
  provider rides (`internal/providers/dashboards/crud.go`, 5 call sites).
- The fallback policy conflates "not served" (404/absent) with "server broken"
  (5xx/network), masking real app-platform failures with zero signal.
- The K8s-path typed-error contract is hand-produced (`NewAPIError`); after a
  dynamic-client rewrite this becomes non-trivial and must be preserved
  explicitly (see Invariants).
- Governance: the provider's TypedCRUD carve-out is documented only in a
  research doc + code comment — not the ADR/CONSTITUTION/ARCHITECTURE mechanism
  the constitution requires.

## Target Structure

Command and adapter layers are **unchanged** — same `dsclient.NewTransport(cfg)`
entry, same `dualTransport` seam. Only the K8s transport internals change.

```
(command layer + datasourceAdapter — UNCHANGED)
                         ▼
              dualTransport (UNCHANGED — k8sThenREST[T], errK8sNotServed keying)
                         │
      ┌──────────────────┴───────────────────────┐
      ▼                                           ▼
k8sTransport (REWRITTEN)                     REST Client (client.go, UNCHANGED)
 ├─ Tier 1 — discovery Registry              /api/datasources (fallback target)
 │    discovery.NewDefaultRegistry(ctx,cfg)
 │    LookupPreferredPerGroup → served per-plugin DataSource descriptors
 │    disk-cached ~/.cache/gcx/discovery/ (10-min TTL)
 │    Fallback policy (uniform, both read+write):
 │      404 / absent group / zero served groups → errK8sNotServed → REST
 │      5xx / network error at /apis or a group  → REAL error, surfaced (not fallback)
 ├─ Tier 2 — dynamic.NewDefaultNamespacedClient(cfg)
 │    List/Get/Create/Update/Delete over a per-plugin Descriptor (GVR)
 │    StatusError → ParseStatusError → NORMALIZED to dsclient.APIError
 │      (typed-error contract preserved: IsNotFound, 404→errK8sNotServed,
 │       409 surfaced, fail/convert.go classification — all transport-agnostic)
 ├─ datasource-specific glue (RETAINED — the genuinely non-generic part):
 │    per-plugin GVR enumeration · UID→pluginID index · secure/spec mapping
 │    · Health subresource (no NamespacedClient wrapper — kept as thin glue)
 │    · errK8sNotServed / REST fallback trigger
 └─ debug log emitted when the REST demotion fires (m8)

manifest.go readBytes("-"): io.LimitReader(stdin, maxManifestBytes)             (m2 fixed)
ReadManifestFile: validate spec.type against ^[a-z0-9][a-z0-9-]*$              (m1 fixed)
cmd/gcx/fail/convert.go: 401 → auth-failure+`gcx login`; 403 → permission-denied+RBAC (Lead-Minor fixed)
secrets.go: warning evaluated against pre-resolution secure block              (m3 fixed)

discovery pipeline: malformed empty-Kind datasource group no longer produces a descriptor
  → `resources pull datasources` emits one manifest per type, no `s.v0alpha1.*` dirs (pre-existing bug fixed)
```

`servedgroups.go` is mostly deleted; its role (served-plugin set + UID→pluginID
index) is split between the discovery Registry (served set, disk-cached) and the
Tier-2 list pass (UID→pluginID index).

**Improvements:** removes the clearest client-go duplication; gains a
cross-invocation disk cache; makes app-platform breakage observable; confines
the K8s-vs-REST decision to a single, principled seam; and codifies the
architecture that was previously undocumented.

## Behavioral Contract

### Invariants (MUST hold — no behavior change)

- **Transport interface contract is preserved.** `Transport`
  (List/GetByUID/Create/Update/Delete/Health) and `NewTransport(cfg)` keep the
  same signatures and construction semantics (no I/O at construction). The
  methods MUST continue to return the wire `*Datasource` type; any
  `unstructured.Unstructured` obtained from the dynamic client MUST be mapped
  back to `*Datasource` at the transport boundary and never leaked upward, so
  the command and adapter layers always render an identical shape.
- **Fallback dispatcher unchanged.** `dualTransport.k8sThenREST[T]` keeps keying
  strictly on the `errK8sNotServed` sentinel; the K8s-vs-REST decision MUST NOT
  leak into command or adapter layers.
- **Typed-error contract is transport-agnostic.** K8s-path errors (k8s
  `StatusError` from the dynamic client) MUST be normalized to
  `datasources.APIError` so `IsNotFound`, `dualTransport`'s `errK8sNotServed`
  mapping, 409-surfacing, `health` row messages, and `cmd/gcx/fail/convert.go`
  classification behave identically on the K8s and REST paths.
- **Optimistic-concurrency 409 surfacing.** `Update` MUST fetch the current
  `resourceVersion`, apply it, and surface a 409 as a typed error (no silent
  re-fetch-retry). `TestDualUpdateConflictSurfacesError` MUST stay green.
- **Fallback result-correctness.** When the K8s surface reports true "not
  served", results returned via the legacy REST path MUST match the pre-refactor
  output (permission-aware, complete).
- **Output parity (dual-path AND dual-surface).** Rendered output MUST be
  invariant across transports and consistent across command surfaces:
  (a) `gcx datasources list/get` renders the k8s-shaped `DataSourceManifest`
  regardless of whether a row was served by the app-platform or the REST
  fallback — both paths return `*Datasource`, rendered via
  `ManifestFromDatasource` (transport is invisible in output);
  (b) `gcx resources get/pull datasources` renders the same k8s-shaped manifest
  via the shared `ManifestFromDatasource`/`ToDatasource` mapping
  (`adapter.go` converges on `ManifestFromDatasource`, same as the commands);
  (c) for the same object set, the `-o yaml` / `-o json` output of
  `gcx datasources list` and `gcx resources get datasources` is byte-identical
  (table / wide / text views MAY differ). The refactor MUST NOT regress any of
  (a)–(c).
- **Secure block + secret hygiene.** The top-level `secure` block
  (`create`/`fromEnv`/`fromFile`/`remove` + name-only read-back placeholders),
  write-only sourcing, dry-run redaction, value-free diffs, and
  output-derived-from-server-response all remain unchanged.
- **Per-plugin GVK groups + UID→pluginID index + canonical descriptor routing**
  remain the datasource-specific model.
- **Response cap + exit codes.** The 10 MB `ReadResponseBody` cap remains;
  exit-code discipline is unchanged (delete/health partial failure → 4; usage →
  2; auth/permission → 3).
- **Provider adapter/router integration unchanged** (`internal/providers/
  datasources/` ResourceAdapter, natural-key + GVK-normalizer registration).
- **No performance regression** on the common path; discovery gains a disk cache
  (net improvement across invocations).

### Intentional Changes (deliberate behavior change — each justified)

1. **Major #1 — 5xx / network errors at discovery now surface.** A non-404
   failure of `/apis` (or a per-group collection), and network errors, are no
   longer collapsed into "not served"/silent REST fallback; they surface as real
   errors consistently on both read and write paths. *Justification:* genuine
   app-platform breakage was previously indistinguishable from "not installed",
   an observability defect. Read/write asymmetry in network-error handling is
   removed.
2. **Lead-Minor — 403 reclassified.** A 403 on a datasource operation renders as
   a permission-denied / insufficient-privileges message with a role/RBAC
   suggestion, distinct from the 401 re-authentication guidance (`gcx login`).
   *Justification:* re-authenticating does not fix an RBAC denial; the prior
   headline mis-directed users and agents. (401 behavior unchanged.)
3. **m8 — new debug log on REST demotion.** When `List` demotes to the legacy
   REST path because a served group is inaccessible/incomplete, a debug log is
   emitted. *Justification:* the mode switch was previously silent and
   un-debuggable. Default output is unchanged (debug level only).
4. **Pre-existing pull bug — malformed manifests no longer emitted.** `resources
   pull datasources` no longer writes the duplicate manifest with a broken GVK
   (`apiVersion: <type>/v0alpha1`, empty `kind` → `s.v0alpha1.<type>` dirs); it
   emits exactly one apply-ready manifest per type
   (`<type>.datasource.grafana.app/v0alpha1`, `kind: DataSource`).
   *Justification:* the malformed output cannot round-trip on push; it was
   pre-existing on `main` but is fixed here as it is native to the
   discovery/normalizer cleanup.
5. **User-visible wording / warning behavior (m3, m4, m6).**
   - m3: the "password is write-only and will not be set" warning is suppressed
     on a `basicAuth:true` round-trip that carries a name-only secure
     placeholder (the stored secret is preserved). *Justification:* the prior
     warning was a false positive contradicting the round-trip-preservation
     guarantee.
   - m4: `health` errors (or documents precedence) when both a UID argument and
     `--type` are supplied, instead of silently ignoring `--type`.
     *Justification:* silent flag-ignore misleads agents.
   - m6: `--limit` flag help states it is a display cap applied after fetch and
     `--type` filtering, not server-side paging. *Justification:* prevents a
     false fetch-time-limiting expectation.

## Migration Steps

Ordered so each step is independently verifiable and governance lands first to
clear the compliance gate. Grouped into workstreams; each SHOULD be a separate
commit in the PR (see Risks — large blast radius).

### WS0 — Governance & documentation (unblocks the compliance hierarchy)

- Add ADR `docs/adrs/datasources-provider/001-datasources-provider-design.md`
  (next ARCHITECTURE.md ADR-table number **021**) matching the ADR-016 format.
  It MUST capture: the D21 rationale (top-level `secure` is a sibling of `spec`
  and cannot fit TypedCRUD's spec-only `TypedObject[T]` envelope); that provider
  commands and the adapter share `ManifestFromDatasource`/`ToDatasource` for
  output parity; the datasources provider shape (custom `ResourceAdapter`,
  dual-mode transport, K8s-vs-Cloud tier placement); the GVK-normalizer
  mechanism (`RegisterGVKNormalizer`/`NormalizeGVK`, init-time self-registration);
  and the **post-refactor** (reuse-based) transport as the described end state.
  Cite ADR-020 (SM dual-mode transport) as the analogous precedent.
- Amend the CONSTITUTION.md "TypedCRUD for provider commands" exception clause to
  list datasources as a second documented exception citing ADR-021.
- Add the ADR-021 row to the ARCHITECTURE.md ADR table.
- Update `docs/architecture/` (provider-system / project-structure) to describe
  the provider shape and the GVK-normalizer mechanism.
- Fix the stale package doc at `internal/providers/datasources/
  resource_adapter.go:4-6` (it wrongly claims datasources are not on the `/apis`
  surface).
- Refresh `docs/research/2026-06-26-datasources-management.md` §9.6 to match
  shipped reality (m7).
- **Verify:** ADR file exists and renders in the table; `CONSTITUTION.md` lists
  the second exception; `docs/reference/doc-maintenance.md` structural checks
  pass; the package doc and research §9.6 no longer describe REST-only.

### WS1 — Tier 1: discovery Registry swap

- **First, capture an output-parity baseline** (golden files, before any
  transport change): `gcx datasources list -o yaml` and `-o json`, and
  `gcx resources get datasources -o yaml` and `-o json`, in both served (k8s)
  and not-served (REST) modes. These become the regression fixtures the rewrite
  must match (Output-parity invariant). Confirm the current `datasources` vs
  `resources` YAML/JSON already match; if they do NOT match today, record the
  delta and treat closing it as in-scope.
- Replace `servedGroupCache.discover()` with the served-datasource-group set
  derived from `discovery.NewDefaultRegistry(ctx, cfg)` /
  `LookupPreferredPerGroup`. Retain the UID→pluginID index (built by the Tier-2
  list pass). Delete the now-dead discovery code in `servedgroups.go`.
- **Verify:** served-group discovery is disk-cached (cache dir populated under
  `~/.cache/gcx/discovery/`); `List`/`Get` succeed in both served and not-served
  modes; updated transport unit tests green.

### WS2 — Tier 2: dynamic-client CRUD + fallback policy (Major #1, m8) + error normalization

- Rebuild `List/GetByUID/Create/Update/Delete` on
  `dynamic.NewDefaultNamespacedClient(cfg)` keyed on a per-plugin `Descriptor`.
  Keep Health as thin datasource-specific glue (no `NamespacedClient` subresource
  wrapper) OR extend `NamespacedClient.Get` with a subresource variadic —
  implementer's choice, documented in the ADR/decision.
- Normalize dynamic-client `StatusError`s to `datasources.APIError` so the typed
  contract is preserved (map 404→`errK8sNotServed` where appropriate; surface
  409/403/5xx as typed errors).
- Implement the uniform fallback policy: only 404 / absent group / zero served
  groups yield `errK8sNotServed`; 5xx / network errors surface on both read and
  write paths. Emit a debug log when `List` demotes to REST (m8).
- **Verify:** regression tests assert a 500 at `/apis` and a 5xx on a group
  collection surface (not silent REST fallback); a K8s-path 403/404/409 renders
  identically to the REST path; `dualtransport_test.go` /
  `TestDualUpdateConflictSurfacesError` green; debug log fires on demotion;
  **the WS1 output-parity baseline still matches** — `datasources list` and
  `resources get datasources` YAML/JSON are byte-identical, and identical across
  served vs not-served modes (the `Transport` still returns `*Datasource`).

### WS3 — Security hardening (m1, m2)

- Validate `spec.type` against `^[a-z0-9][a-z0-9-]*$` in `ReadManifestFile`
  (manifest.go).
- Apply `io.LimitReader(stdin, maxManifestBytes)` (e.g. 1 MB) on the `"-"` path
  in `readBytes` (manifest.go).
- **Verify:** unit tests reject a `spec.type` containing `/` or `..`; an
  oversized piped manifest is bounded/rejected.

### WS4 — UX / error fixes (Lead-Minor, m3, m4, m6)

- Split the 403 case in `cmd/gcx/fail/convert.go` (`datasourceErrorSummary` /
  `datasourceErrorSuggestions`, ~line 494): 403 → permission-denied message +
  RBAC/role suggestion; 401 keeps auth-failure + `gcx login`.
- m3: evaluate `WarnIfSecretMissing` against the pre-resolution `secure` block
  (suppress when a name-only placeholder is present, or thread a "had a secure
  entry" signal from `ResolveSecrets`).
- m4: `health` errors on the UID+`--type` conflict (or documents precedence in
  flag help).
- m6: update the `--limit` flag help (list.go) to state display-cap semantics.
- **Verify:** `convert_test.go` asserts a 403 summary omitting "Authentication
  failed" and a role/RBAC suggestion; a `basicAuth:true` round-trip emits no
  false warning; `health <uid> --type X` errors or documents precedence; `--help`
  reflects the `--limit` wording.

### WS5 — Fidelity note / passthrough (m5)

- Add a comment documenting the modeled-field boundary in `fromK8s`/`client.go`,
  OR add a passthrough map for unknown spec keys **inside** the wire type
  (`DataSourceSpec`/`Datasource`) — NOT by returning `unstructured` from the
  `Transport` (see the Output-parity invariant). If the Tier-2 read path holds
  `unstructured` transiently, capture unknown top-level keys into the passthrough
  map while still returning a `*Datasource`, so the improvement does not change
  the rendered shape or break `datasources`↔`resources` parity.
- **Verify:** either the comment exists, or a round-trip test shows an unmodeled
  top-level field is preserved AND the output-parity baseline still matches.

### WS6 — Pre-existing pull-double-listing bug

- **First**, confirm the root cause on a live stack: run
  `gcx api /apis` (or reproduce `gcx resources pull datasources`) and identify
  the malformed datasource group (hypothesis: discovery indexes a legacy group
  `<type>/v0alpha1` whose `datasources` resource has an empty `Kind`; the
  empty kind flows to `internal/resources/local/writer.go:40`
  `strings.ToLower(gvk.Kind)+"s"` → `"s"` → `s.v0alpha1.<type>` dirs).
- Apply the **narrowest** predicate that drops the malformed group from
  descriptor resolution — preferred: skip discovery resources with an empty
  `Kind` in `RegistryIndex.Update` / `FilterDiscoveryResults`, since such a
  resource can never produce a valid `apiVersion`+`kind` and always breaks the
  writer.
- **Verify:** `resources pull datasources` writes exactly one manifest per type
  (`<type>.datasource.grafana.app/v0alpha1`, `kind: DataSource`), with no
  `s.v0alpha1.*` directories; the pulled manifests round-trip on push; **and**
  `resources pull` output for `dashboards` and `folders` is unchanged (no
  descriptors dropped for other types).

### Final gate

- `GCX_AGENT_MODE=false mise run all` passes; reference docs regenerated if any
  command/flag/config/env changed (m4/m6 flag help).

## Acceptance Criteria

### Transport rewrite (Lead-Major, Tier 1 + Tier 2)

- GIVEN a stack that serves per-plugin datasource groups
  WHEN `gcx datasources list` runs
  THEN the served-group set is obtained from the discovery Registry (disk-cached
  under `~/.cache/gcx/discovery/`) and CRUD is issued via the shared dynamic
  namespaced client, and the returned datasources match the pre-refactor output.
- The `k8sTransport` MUST NOT contain hand-rolled `/apis` discovery parsing or
  per-op raw `net/http` CRUD; `servedgroups.go`'s `discover()` is removed.
- GIVEN the app-platform surface reports true "not served" (404 / absent group /
  zero served groups)
  WHEN any datasource operation runs
  THEN it falls back to the legacy REST path via the unchanged `errK8sNotServed`
  keying and returns correct results.

### Fallback observability (Major #1, m8)

- IF `/apis` (or a per-plugin collection) returns a 5xx, or a network error
  occurs, THEN the transport SHALL surface a real error on BOTH read and write
  paths (it SHALL NOT silently fall back to REST), verified by a regression test
  asserting a 500 at `/apis` surfaces rather than returning a REST result.
- WHEN `List` demotes to the legacy REST path because a served group is
  inaccessible THEN the system SHALL emit a debug-level log recording the
  demotion.

### Typed-error contract (Invariant enabler)

- GIVEN the app-platform path returns a 403 / 404 / 409
  WHEN the operation completes
  THEN the surfaced error is a `datasources.APIError` with the corresponding
  status code, such that `IsNotFound`, 409-surfacing, and `fail/convert.go`
  classification behave identically to the REST path.

### Output parity (dual-path, dual-surface)

- GIVEN one datasource served by the app-platform and one served only by the
  REST fallback
  WHEN `gcx datasources list -o yaml` renders both
  THEN each is emitted as the same k8s-shaped `DataSourceManifest` (which
  transport served the row is invisible in the output).
- GIVEN the same stack and object set
  WHEN `gcx datasources list -o yaml|json` and `gcx resources get datasources
  -o yaml|json` are captured
  THEN the two outputs are byte-identical (table / wide / text MAY differ),
  asserted by a regression test comparing against a captured pre-refactor
  baseline in BOTH served (k8s) and not-served (REST) modes.
- The `Transport` methods MUST return `*Datasource`, never a raw
  `unstructured.Unstructured` — verified by the interface signature remaining
  unchanged.

### Security (m1, m2)

- GIVEN a manifest with `spec.type` = `"foo/bar"` (or containing `..`)
  WHEN `ReadManifestFile` parses it
  THEN it returns a validation error and no request path is constructed.
- GIVEN a piped manifest on stdin larger than `maxManifestBytes`
  WHEN `create -f -` reads it
  THEN the read is bounded by `io.LimitReader` and does not consume unbounded
  memory.

### UX / error (Lead-Minor, m3, m4, m6)

- GIVEN a 403 RBAC denial on `gcx datasources create`
  WHEN the error is rendered
  THEN the summary MUST NOT contain "Authentication failed", MUST convey
  permission/authorization denial, and the suggestion MUST reference roles/RBAC
  rather than `gcx login`. (A 401 MUST still render auth-failure + `gcx login`.)
- GIVEN a `basicAuth: true` datasource fetched via `get -o yaml` and re-applied
  via `update -f -` (its `secure` block carrying only name-only placeholders)
  WHEN the update runs
  THEN no "password is write-only and will not be set" warning is emitted.
- GIVEN both a UID argument and `--type`
  WHEN `gcx datasources health <uid> --type X` runs
  THEN the command errors on the conflict (or the precedence is documented in
  `--type` flag help).
- The `--limit` flag help on `datasources list` SHALL state it is a display cap
  applied after fetch and `--type` filtering.

### Fidelity (m5)

- GIVEN a datasource whose server response carries a top-level field gcx does
  not model
  WHEN it is read and re-serialized
  THEN either the modeled-field boundary is documented in code, or the unmodeled
  field is preserved through the round-trip.

### Governance / docs (Critical #1, Major #3, m7)

- GIVEN the merged PR
  WHEN the compliance hierarchy is checked
  THEN ADR-021 exists under `docs/adrs/datasources-provider/`, the CONSTITUTION
  TypedCRUD exception clause lists datasources citing ADR-021, the ARCHITECTURE
  ADR table has the 021 row, `docs/architecture/` describes the provider shape +
  GVK-normalizer, `resource_adapter.go`'s package doc no longer claims REST-only,
  and research §9.6 matches shipped reality.

### Pre-existing pull bug

- GIVEN a stack serving loki/prometheus/tempo/mysql/clickhouse/pyroscope/infinity
  WHEN `gcx resources pull datasources` runs
  THEN each type is written exactly once as
  `<type>.datasource.grafana.app/v0alpha1` / `kind: DataSource`, no
  `s.v0alpha1.*` directories are produced, and the manifests round-trip on push.
- GIVEN the same discovery fix
  WHEN `gcx resources pull dashboards` and `pull folders` run
  THEN their output is unchanged (no descriptors are dropped for other types).

### Whole-PR gate

- `GCX_AGENT_MODE=false mise run all` passes; the existing datasources test
  suites (`internal/datasources`, `cmd/gcx/datasources`,
  `internal/providers/datasources`) stay green with transport tests updated and
  M1 regression tests added.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Tier-2 unstructured ↔ manifest conversion diverges from the hand-rolled `fromK8s`/`toK8s` mapping (spec/secure round-trip) | Silent fidelity or secret-hygiene regression | Keep `secure`/`spec` mapping as datasource-specific glue; add round-trip tests asserting `get -o yaml \| update -f -` parity and value-free secret handling before deleting old code |
| Output-parity regression from the `unstructured`-based read path — k8s-vs-REST or `datasources`-vs-`resources` YAML/JSON diverge | Silent, user-visible output drift that no single-path test catches | Keep `Transport` returning `*Datasource` (map `unstructured` back at the boundary); capture the WS1 pre-refactor golden baseline (both modes, both surfaces) and assert byte-identical YAML/JSON post-refactor |
| K8s-path errors escape as raw k8s `StatusError` instead of `datasources.APIError` | 403/404/409 UX + `IsNotFound` create-vs-update routing regress on the K8s path only (no REST-path test catches it) | Make error normalization an explicit invariant + AC; add a test that a K8s-path 403/404/409 renders identically to REST |
| Health subresource has no `NamespacedClient` wrapper | Tier-2 step not executable if hand-waved | Named decision in WS2/ADR: keep health as thin glue or extend `NamespacedClient.Get` with a subresource variadic |
| Per-group RBAC / incomplete-list edges under concurrency | Partial-view or index-race regressions | Preserve the incomplete→REST demotion semantics; add the m8 debug log; retain per-group skip-on-inaccessible behavior |
| Pull-bug fix lands in the **global** discovery pipeline (wider than `internal/datasources`) | Dropping empty-Kind resources could suppress legitimate types | Confirm the malformed group on a live `/apis` first; use the narrowest predicate; add a Verify that dashboards/folders pull output is unchanged |
| ADR + CONSTITUTION amendment must land coherently with the reuse-based end state | Governance describes pre-refactor transport if committed first | ADR/docs describe the post-refactor end state forward-looking; add a final doc touch-up commit after Tier-2 if wording drifts |
| Large blast radius (~transport rewrite + 8 fixes + docs in one PR) | Hard to review, risky rollback | Commit per workstream (WS0…WS6), governance first; keep `dualtransport.go` / `client.go` / provider adapter untouched to bound the diff |
