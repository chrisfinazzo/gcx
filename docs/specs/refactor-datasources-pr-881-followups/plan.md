---
type: feature-plan
title: "Datasources K8s transport refactor and PR #881 review remediation"
status: draft
spec: spec.md
created: 2026-07-07
---

# Plan: Datasources K8s transport refactor and PR #881 review remediation

## Pipeline Architecture

The rewrite is confined to the K8s half of the dual transport. The command
layer, the `datasourceAdapter`, the `dualTransport` fallback dispatcher, and the
legacy REST `Client` are unchanged. The rewritten `k8sTransport` composes two
existing gcx subsystems and keeps only the genuinely datasource-specific glue.

```
 gcx datasources CLI            gcx resources (datasourceAdapter)
        │                                  │
        └───────── dsclient.NewTransport(cfg) ─────────┐
                                                        ▼
                    dualTransport.k8sThenREST[T]  (unchanged seam)
                     errK8sNotServed → REST ; else propagate
                          │                         │
                          ▼                         ▼
              ┌── k8sTransport (rewritten) ──┐   REST Client (unchanged)
              │                              │   /api/datasources
   Tier 1 ────┤ discovery.NewDefaultRegistry │   dsclient.APIError
   discovery  │  (disk cache, per-plugin     │
              │   DataSource descriptors,    │
              │   LookupPreferredPerGroup)   │
              │                              │
   Tier 2 ────┤ dynamic.NewDefaultNamespaced │
   dynamic    │  Client(cfg): L/G/C/U/D over │
   client     │  per-plugin GVR              │
              │   └─ ParseStatusError ──► normalize to dsclient.APIError
              │                              │
   glue ──────┤ per-plugin GVR enumeration · UID→pluginID index ·
   (kept)     │ secure/spec mapping · Health subresource · fallback trigger ·
              │ uniform 404-only→errK8sNotServed policy · demotion debug log
              └──────────────────────────────┘

 discovery pipeline (global, shared): drop empty-Kind resources so
 `resources pull datasources` never emits a malformed `<type>/v0alpha1`/empty-kind manifest.
```

**Fallback decision (uniform):** `errK8sNotServed` is emitted **only** for
404 / absent API group / zero served datasource groups. A 5xx or network error
at `/apis` or a per-plugin collection is a real error surfaced on both read and
write paths. This preserves the sentinel-keyed dispatcher while removing the
5xx-masking and the read/write asymmetry.

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Tier 1: replace `servedGroupCache.discover()` with `discovery.NewDefaultRegistry` / `LookupPreferredPerGroup` | Deletes the clearest client-go duplication and gains the `~/.cache/gcx/discovery/` disk cache the hand-rolled version lacked (Lead-Major; process-scoped → cross-invocation). |
| Tier 2: rebuild per-op CRUD on `dynamic.NewDefaultNamespacedClient` (the client dashboards ride) | Removes raw `net/http` CRUD duplication; verified sufficient because the transport uses no server-side content negotiation (`do()` sets only `Content-Type`, no `Accept: as=Table`) (Lead-Major). |
| Normalize dynamic-client `StatusError` → `datasources.APIError` | Keeps the `Transport` typed-error contract transport-agnostic so `IsNotFound`, 409-surfacing, health row messages, and `fail/convert.go` classification (incl. the 403 fix) behave identically on K8s and REST paths (Invariant; enables Lead-Minor). |
| Uniform fallback: 404/absent/zero-served → `errK8sNotServed`; 5xx/network → real error, both paths; debug log on demotion | Fixes 5xx masking and read/write asymmetry while preserving the sentinel dispatcher and REST result-correctness (Major #1, m8). |
| Keep Health as thin datasource-specific glue (or extend `NamespacedClient.Get` with subresource variadic) | `NamespacedClient.Get` does not expose the subresource variadic, so `.../health` cannot ride the shared client unchanged; named as a decision so Tier-2 is executable. |
| Retain the top-level `secure` block + `ManifestFromDatasource`/`ToDatasource` mapping as datasource-specific | The `secure` sibling of `spec` cannot fit TypedCRUD's spec-only `TypedObject[T]` envelope — this is exactly the D21 driver the ADR codifies (Critical #1). |
| Fix the pull-double-listing bug in the global discovery pipeline (drop empty-Kind resources) with the narrowest predicate | An empty-`Kind` resource can never produce a valid `apiVersion`+`kind` and breaks `writer.go:40` (`""+"s"`); dropping it at ingestion is the root-cause fix. Confirm the malformed group on a live `/apis` first (pre-existing bug). |
| Validate `spec.type` (`^[a-z0-9][a-z0-9-]*$`) at manifest load; bound stdin with `io.LimitReader` | Prevents pluginID path injection into `/apis/{group}/...` and unbounded stdin memory use (m1, m2), consistent with the 10 MB response cap. |
| Split 403 from 401 in `fail/convert.go` | A 403 RBAC denial is authorization, not authentication; `gcx login` cannot fix it (Lead-Minor). |
| Codify the TypedCRUD exception via ADR-021 + CONSTITUTION clause + ARCHITECTURE row (no code rework) | An un-waived hard-invariant deviation is blocking per the compliance hierarchy; the constitution requires the ADR mechanism, not a research doc. Approval is granted; this step codifies it (Critical #1, Major #3). |

## Compatibility

**Continues unchanged:**
- `Transport` interface and `NewTransport(cfg)` construction semantics.
- `dualTransport` fallback dispatcher and `errK8sNotServed` keying.
- Legacy REST `Client` (`client.go`) and its typed `APIError`.
- Provider `datasourceAdapter` / router integration, natural-key + GVK-normalizer
  registration.
- Top-level `secure` block, secret hygiene, dry-run redaction, value-free diffs.
- Optimistic-concurrency 409 surfacing; 10 MB response cap; exit-code discipline
  (partial failure → 4).
- `resources pull` output for dashboards, folders, and all non-datasource types.

**Changes (intentional):**
- 5xx / network errors at discovery now surface instead of silently falling back
  to REST (both read and write paths).
- 403 renders as permission-denied + RBAC suggestion (was auth-failure +
  `gcx login`); 401 unchanged.
- `resources pull datasources` no longer emits the duplicate malformed-GVK
  manifests.
- New debug log when `List` demotes to REST.
- Flag-help / warning wording (`--limit` display-cap note; suppressed basic-auth
  round-trip warning; `health` UID+`--type` conflict).

**Newly available:**
- Disk-cached datasource discovery (`~/.cache/gcx/discovery/`, 10-min TTL) shared
  with the resources pipeline.
- A single, principled K8s→REST fallback policy that distinguishes "not served"
  from "server broken".

## Commit Strategy

This is a large PR (transport rewrite + eight fixes + docs). Commit per
workstream in spec order — **WS0 governance first** (clears the compliance
gate), then WS1 Tier-1, WS2 Tier-2 + fallback/error-normalization, WS3 security,
WS4 UX/error, WS5 fidelity, WS6 pre-existing bug — keeping `dualtransport.go`,
`client.go`, and the provider adapter untouched to bound the diff. If the
governance docs drift from the reuse-based end state during the rewrite, add a
final doc touch-up commit so the ADR and code land coherently.
