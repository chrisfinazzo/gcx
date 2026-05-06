# Cross-cutting design rules

Prescriptive rules that apply to commands and resources across all providers. New commands and providers MUST comply with each rule that fits their shape.

These rules were first codified by the OnCall feature-expansion ADR ([`../adrs/oncall-feature-expansion/001-sre-expansion.md`](../adrs/oncall-feature-expansion/001-sre-expansion.md)). Future ADRs that introduce new cross-cutting rules SHOULD append them here rather than re-litigating them per provider.

## List defaults

> List commands SHOULD default to the "actionable" subset of resources when the underlying API supports the relevant filter. Help text MUST document the default explicitly. An `--all` shortcut MUST be provided to escape the default. Agent mode MUST NOT change the default — the same flag set produces the same data semantics whether invoked by a human or an agent.

**Why:** state-bearing list commands that return their full history (e.g. including resolved alert groups, child rollups, archived items) are unusable on real stacks. The default needs to be the answer to the question a human or agent typically asks ("what's open / what's actionable right now"), not the answer that requires the most flags to refine.

**Applies to (examples):**
- `gcx irm oncall alert-groups list` — defaults exclude `resolved` status and child groups; `--all` is the escape hatch.
- `gcx irm oncall shifts list` — defaults to `--at now`; `--all` opts out of the time filter.

**Future application:** any state-bearing list command in any provider (incidents, fleet pipelines, synthetic checks, etc.).

## Cross-provider fields

> When a resource carries identifiers usable by another provider's command (alert rule UIDs, dashboard UIDs, datasource names, alert-group IDs, etc.), those identifiers SHOULD be promoted to first-class fields on the resource alongside the raw payload. Field names follow the convention `<provider><Noun>UID` or `<noun>ID` (e.g., `alertRuleUID`, `dashboardUID`, `panelID`). Promoted fields MUST be `omitempty` — empty/missing when not extractable. Cross-provider data is NOT carried in a nested `related<Provider>` block; promotion to first-class fields keeps consumers simpler and avoids inventing a new top-level surface.

**Why:** agents and codecs read first-class fields directly. A nested `relatedAlerting` sub-object forces every consumer to dereference one more layer for the most common pivot. The "stepping stone" pattern (one ID is enough to pivot to another command) maps cleanly to scalar fields and awkwardly to a typed sub-object that wants URL/command suggestions as well.

**Applies to (examples):**
- `Alert.{AlertRuleUID, AlertGroupUID, AlertInstanceID, DashboardUID, PanelID}` — best-effort extracted from `Payload.Labels` / `Payload.Annotations`.

**Future application:** any provider whose resources surface cross-pivot identifiers (synthetic checks pointing at SLOs; fleet pipelines pointing at metrics streams; k6 runs pointing at dashboards; etc.).

## Tailored-tier shape parity

> The tailored tier (`gcx <provider> <resource> <verb>`) MUST NOT diverge in resource shape from the generic tier (`gcx resources get <resource>/<id>`). The tailored tier's value-add is filtering, defaults, and imperative actions — never type reshaping. If a use case requires a different shape (e.g., resolved time-window events vs rotation rule templates), it MUST be modelled either as derived `omitempty` fields on the canonical type or as a separate canonical resource — never as a tier-divergent shape on the same resource name.

**Why:** the dual-tier mental model only works if a single decoder per resource is sufficient. Shape divergence forces consumers to special-case per-tier, which scales poorly and breaks GitOps round-trip when the generic-tier shape can no longer represent everything the tailored tier shows.

**Applies to (examples):**
- `Shift` — `gcx resources get shifts/<id>`, `gcx irm oncall shifts list`, and `gcx irm oncall shifts get <id>` all return the same canonical shape; filtering and derived fields are the tailored tier's value-add.

## Hint conventions

> Diagnostic output uses three plain-text prefix classes (`warn:` / `note:` / `hint:`), rendered in that order. The stdout/stderr split is invariant: stdout MUST be pure data (a single JSON document in agent mode, formatted output in TTY mode); diagnostics MUST emit on stderr in all modes. In TTY mode stderr is plain prefixed text (dim styled). In agent mode stderr is JSONL — one JSON record per line, with a typed `class` field (`warning`, `note`, `hint`) and structured fields. Each hint MUST be a runnable command. `--quiet` suppresses `note:` + `hint:` (warn always renders); `--no-hints` suppresses only `hint:`. Discovery verbs (`list`, `get`, `query`) SHOULD emit a post-result hint suggesting the next logical step on success, conditional on result content (empty vs non-empty distinct hints). Errors carry suggestions via the canonical DetailedError `suggestions[]` array on stdout, not via `hint:`.

**Why:** the stdout/stderr split is the contract that makes `gcx <anything> | jq` work unconditionally. Hints, notes, and warnings are diagnostics — they belong on stderr. Agents need them in a structured form, but moving them onto stdout would conflate diagnostics with the result and break single-pass parsing. Emitting JSONL on stderr keeps the contract intact for both human and agent consumers.

**Wire format examples:**

TTY mode (stderr):
```
hint: Drill into alerts: gcx irm oncall alert-groups list-alerts <group-id>
note: 0 results — defaults exclude resolved/child groups; try --all
warn: Default filter excluded 47 resolved groups
```

Agent mode (stderr, one JSON per line):
```jsonl
{"class":"warning","summary":"Default filter excluded 47 resolved groups"}
{"class":"note","summary":"0 results — defaults exclude resolved/child groups; try --all"}
{"class":"hint","summary":"Drill into alerts","command":"gcx irm oncall alert-groups list-alerts <group-id>"}
```

Render order is invariant: `warn` → `note` → `hint`. Hints SHOULD be conditional on result content (different hint for empty vs non-empty results).

**Applies to (examples):**
- Every command modified by the OnCall feature-expansion ADR.

**Future application:** every command in every provider; this rule is the project-wide standard for diagnostic output.
