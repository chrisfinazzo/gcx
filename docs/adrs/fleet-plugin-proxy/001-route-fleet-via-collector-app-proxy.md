# Route `fleet` and `instrumentation` through the `grafana-collector-app` plugin proxy

**Created**: 2026-06-24
**Status**: proposed
**Supersedes**: none

<!-- Status lifecycle: proposed -> accepted -> deprecated | superseded -->

## Context

`gcx fleet` and `gcx instrumentation` both talk to the Grafana Fleet Management
(FM) API **directly**. They share a base HTTP client (`internal/fleet/`) that
POSTs Connect-style JSON to the FM gRPC endpoint with Basic auth (the FM instance
ID as username, the FM access-policy token as password):

- `internal/fleet/` — the base client and its config loader, which resolves the
  FM instance URL/ID from GCOM stack info and the access-policy token from the
  active context.
- `internal/providers/fleet/` — typed pipeline/collector/tenant methods
  (`ListPipelines`, `Create/Update/DeletePipeline`, `*Collector`, `GetLimits`).
- `internal/providers/instrumentation/` — instrumentation, discovery, and
  pipeline methods layered on the same base client; sets the per-request
  `X-Prom-*` cluster/instance headers and embeds backend datasource push URLs in
  request bodies.

This direct path requires the caller to hold an **FM-scoped access-policy
token**, which gcx uses as the Basic-auth password (username = FM instance ID).

`grafana-collector-app` — the Grafana app plugin the Fleet Management UI already
uses — exposes the same FM routes under `/fleet-management-api/...` and relies on
**Grafana authNZ**. A caller authenticates to Grafana (`cfg.Host`) with its
existing credential and reaches FM without holding an FM token of its own.

**Why now.** FM's direct API doesn't accept Grafana OAuth credentials — it
requires a Cloud access-policy token — and we want OAuth to work across the board
in gcx. Routing through the plugin puts `fleet`/`instrumentation` on the same
Grafana-authenticated transport as every other provider.

The plugin-proxy transport is also the **dominant** HTTP pattern in gcx already:
assistant, aio11y, irm, k6, slo, and kg providers all reach
`/api/plugins/<id>/resources/...` via `rest.HTTPClientFor(&cfg.Config)`
(`internal/assistant/assistanthttp/` is the canonical template). The config
plumbing exists with no new wiring: the same `providers.ConfigLoader` the fleet
providers already instantiate exposes
`LoadGrafanaConfig(ctx) → config.NamespacedRESTConfig` (`Host` + `rest.Config`).

This change is **CONSTITUTION-touching**. `CONSTITUTION.md`'s dependency rule
mandates `httputils.NewDefaultClient(ctx)` (no auth injection) for "APIs outside
the Grafana server" and explicitly names **Fleet** as such a case. Routing through
the proxy moves these calls *to* `cfg.Host`, where the correct client is
`rest.HTTPClientFor()` (which injects the Grafana bearer token) — the inverse of
the current rule. The invariant is descriptive of today's design, not a
prohibition on changing it, but the amendment requires explicit sign-off.

### Route migration plan

Every path gcx calls is reachable. Named routes grant **Viewer**-level reads; an
`/fleet-management-api/*` catch-all route (requiring the **Admin** role) proxies
everything else.

| current RPC | target route | required role |
|---|---|---|
| `pipeline.v1` `ListPipelines` / `GetPipeline` | named | **Viewer** |
| `collector.v1` `ListCollectors` / `GetCollector` | named | **Viewer** |
| `pipeline.v1` `Create` / `Update` / `DeletePipeline` | `/fleet-management-api/*` | **Admin** |
| `collector.v1` `Create` / `Update` / `DeleteCollector` | `/fleet-management-api/*` | **Admin** |
| `tenant.v1` `GetLimits` (only `GetSummary` is named) | `/fleet-management-api/*` | **Admin** |
| `instrumentation.v1` `Get/Set` `App`/`K8S` | `/fleet-management-api/*` | **Admin** |
| `discovery.v1` `SetupK8sDiscovery` / `RunK8sDiscovery` / `RunK8sMonitoring` | `/fleet-management-api/*` | **Admin** |

The wire format is identical (the proxy is transparent), and backend datasource
URLs travel in the request **body**, so the instrumentation `Set*`/`Setup*`
paths pass through unchanged.

### Alternatives considered

- **Keep direct access (status quo).** Self-contained and gcx-controlled, but
  requires every user/CI job to obtain and carry an FM-scoped access-policy token
  in addition to their Grafana credential.
- **Hybrid: plugin-proxy when the plugin is detected, else fall back to direct.**
  Rejected as unnecessary. Because the commands are Cloud-only and the plugin is
  guaranteed on all Cloud stacks, the fallback branch would serve no environment
  that exists for these commands. It would add a probe + two transports to
  maintain for a dead path. Revisit only if FM becomes reachable outside Grafana
  Cloud (i.e., a non-GCOM way to set the FM URL is introduced).

## Decision

Route all `gcx fleet` and `gcx instrumentation` FM traffic through the
`grafana-collector-app` plugin proxy, replacing direct FM access. Specifically:

1. **Transport.** Swap the `internal/fleet/` base client from the direct
   Basic-auth POST (to the FM endpoint) to the plugin-proxy pattern: build the
   HTTP client with `rest.HTTPClientFor(&cfg.Config)` and target
   `cfg.Host + /api/plugins/grafana-collector-app/resources/fleet-management-api`
   + the existing service/method path. Grafana auth is injected by the k8s
   round-tripper.
2. **Config.** Resolve connection details via
   `providers.ConfigLoader.LoadGrafanaConfig(ctx)` (`NamespacedRESTConfig`)
   instead of `LoadCloudConfig(ctx)`'s `AgentManagementInstanceURL` + FM token.
   gcx no longer needs an FM-scoped access-policy token for these commands, nor
   `AgentManagementInstanceURL`/`ID` from GCOM.
3. **Drop now-redundant request decoration.** The instrumentation client no
   longer sets the `X-Prom-Cluster-ID`/`X-Prom-Instance-ID` headers itself.
   Backend datasource URLs remain in the request body (still gcx's
   responsibility).
4. **Clean cutover, no hybrid.** No fallback to direct FM access (see
   Alternatives).
5. **Amend the CONSTITUTION.** Update the dependency rule in `CONSTITUTION.md`
   to remove **Fleet** from the "outside the Grafana server" list and document
   that `fleet`/`instrumentation` reach FM via the collector-app proxy at
   `cfg.Host`, using `rest.HTTPClientFor()` like the other plugin-proxied
   providers.

Explicitly **rejected**: a hybrid plugin-or-direct transport; keeping the
FM-scoped token requirement.

## Consequences

### Positive

- **Enables OAuth support.** Direct FM access needs a Cloud access-policy token,
  so interactive OAuth users cannot drive `fleet`/`instrumentation` today.
  Because the proxy authenticates the caller to Grafana rather than to FM, any
  Grafana credential — OAuth included — now works.
- **Consistency with the other providers.** `fleet`/`instrumentation` join the
  plugin-proxy transport family (assistant, aio11y, irm, k6, slo, kg) instead of
  being a Basic-auth special case. Behaviour converges too: errors, status
  codes, and auth all come from Grafana now, so these commands fail and
  authenticate the same way as the rest of gcx.
- **One less provider to require Cloud access-policy tokens.** gcx stops
  requiring an FM-scoped token for these commands and relies on the Grafana
  credential the active context already holds. Dropping the two-tier "Grafana
  credential *and* FM token" requirement removes real friction.
- **Opens the door to proper RBAC.** With traffic flowing through Grafana and the
  plugin, fine-grained authorization becomes achievable as a future enhancement
  (once the Fleet/Instrumentation teams implement RBAC in the plugin) rather than
  the all-or-nothing authority an FM token grants today.

### Negative / trade-offs

- **Breaking change.** This is a clean cutover with no hybrid fallback: callers
  that relied on direct FM access now authenticate to Grafana instead of carrying
  an FM token. Nothing else of note — the wire format, typed method surfaces, and
  request bodies are unchanged.

### Implementation plan

Once this ADR is accepted, realizing the decision involves the following work —
all in scope of executing this decision, not deferred beyond it:

- Implement the transport + config swap in `internal/fleet/` (base client) and
  adjust the `internal/providers/fleet/` and `internal/providers/instrumentation/`
  call sites; the per-method typed surfaces are unchanged.
- Amend the `CONSTITUTION.md` dependency rule and set this ADR's status to
  `accepted` on sign-off.
- Update `gcx fleet` / `gcx instrumentation` command docs to state the Grafana
  `Admin` role requirement for service-account tokens.
- Review error mapping for proxy/RBAC responses so messages stay actionable
  (e.g. distinguishing "missing RBAC" from "FM rejected the request").
- Update `ARCHITECTURE.md` (Instrumentation/Fleet sections + ADR index) and
  `internal/fleet` package docs to reflect the proxy transport.
- Reconcile tests in `internal/fleet`, `internal/providers/fleet`, and
  `internal/providers/instrumentation` (auth assertions move from Basic-auth to
  plugin-proxy path/URL expectations).
