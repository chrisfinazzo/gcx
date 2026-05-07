# Output Shapes

> Prescriptive rules for the JSON shapes that gcx commands emit. These are project-wide invariants — every provider follows them. **Operation class** (read / mutation single-target / mutation bulk / list / error) drives shape choice; provider boundary does not.

TTY rendering is decoupled from JSON shape — both render to a concise human-readable form in non-agent mode. Agents see the canonical JSON.

---

## 1. Scope

1. **List operations** — `{items:[...]}` envelope.
2. **Read operations** — pass-through resource shape (no wrapper).
3. **Mutation operations** — single-target vs bulk-by-filter (two shapes).
4. **Errors** — canonical DetailedError envelope.
5. **Hints / progress / diagnostics** — stderr-only, TTY plain text vs agent JSONL.
6. **Implementation notes** — codec patterns, `ColumnWidths` semantics, truncation.

## 2. List envelope

Every list command emits exactly one document on stdout:

```json
{"items": [...]}
```

Empty result: `{"items": []}` (never a phantom `{"field": null}` row, never a bare `[]`).

Truncation: when `--limit` truncates the result set, emit a `hint` event on stderr (see § 5) — never mutate the `items` envelope to indicate truncation.

Field ordering inside each item follows the struct declaration (not alphabetical) — see § 2.5 for typed-envelope guidance.

## 3. Mutation envelopes (two shapes)

**Operation class drives shape choice**:

- **Single-target** = positional `<id>` arg, exactly one resource. Per-resource detail.
- **Bulk-by-filter** = `--filter` form, N targets resolved at runtime. Aggregate counts + enumerated failures.

The two shapes are **mutually exclusive**:

- Single-target invocations never emit `summary` or `failures`.
- Bulk invocations never emit top-level `target` or `changed`.

### 3.1 Single-target mutation

```json
{
  "action": "<verb>",
  "target": {<domain-defined scalar fields>},
  "changed": true,
  "fields": [
    {"name": "<field>", "from": "<old>", "to": "<new>"}
  ]
}
```

- `target` is a scalar object with domain-defined fields (e.g. `{cluster, namespace, service}` for instrumentation; `{alertGroupId}` for IRM).
- `changed` is `true` when the operation actually changed state, `false` on idempotent re-runs (target already in destination state).
- `fields` is **optional** — only emitted by declarative-config writes (PR #597 instrumentation pattern). State-machine verbs like `acknowledge` / `silence` omit `fields` entirely.
- On API failure, replace `changed` with `error: {code, message, suggestion}`. The canonical DetailedError envelope (see § 4) is also emitted on stderr.

### 3.2 Bulk-by-filter mutation

```json
{
  "action": "<verb>",
  "summary": {
    "matched": 23,
    "succeeded": 18,
    "skipped": 5,
    "failed": 0
  },
  "failures": [
    {"target": {<scalar>}, "error": {"code": "...", "message": "...", "suggestion": "..."}}
  ]
}
```

- `summary.matched` = count of targets resolved by filter.
- `summary.succeeded` = count whose state actually changed this run.
- `summary.skipped` = count already in target state (idempotent no-op).
- `summary.failed` = count whose API call errored.
- **Invariant**: `matched == succeeded + skipped + failed` (always; assert in tests).
- `failures` is empty `[]` (not omitted) when `failed == 0`. Always present for predictable parsing.
- Successes and skips are **counted, not enumerated** (per issue #264). If you need per-success detail, run with single-target form.
- Each `failures[]` entry is shaped like a single-target failure: scalar `target` + `error` object.

### 3.3 Why two shapes

- Single-target carries the caller's exact resource and what changed (down to field-level diff for declarative-config writes). The caller already knows the ID; the response confirms the outcome and the diff.
- Bulk operates at scale where enumerating every successful target is noise (a 1000-item ack would emit a 1000-element array; agents and humans both want counts there).
- Operation class — **not** provider boundary — drives shape choice. A single provider may emit both shapes (IRM `acknowledge <id>` vs `acknowledge --filter`).

### 3.4 TTY rendering

Decoupled from JSON shape. Recommended one-liners:

```
acknowledge "I...": done
acknowledge "I...": no changes              # idempotent
acknowledge "I...": failed — <error summary>
acknowledge: 18/23 succeeded (5 already-acked, 0 failed)
acknowledge: all 23 succeeded
acknowledge: 18/23 succeeded (5 already-acked, 1 failed) — see stderr for details
```

### 3.5 Field ordering inside the typed envelope

Struct field declaration order, not alphabetical. Implementation: use a typed envelope struct and either:

- The custom `orderedYAMLCodec` (see `internal/providers/irm/oncall_commands_extra.go`) using `goccy/go-yaml` with `UseJSONMarshaler`, OR
- `encoding/json` directly (preserves struct order natively).

`sigs.k8s.io/yaml.JSONToYAML` alphabetises keys — do **not** use it on outputs where order matters.

## 4. Read envelopes (typed CRUD `get`)

Read operations emit the canonical resource shape directly — no wrapper:

```yaml
apiVersion: ...
kind: ...
metadata: {name, namespace, creationTimestamp}
spec: {...}
status: {...}
```

This is the K8s envelope shape (see [`../architecture/resource-model.md`](../architecture/resource-model.md)). Same field-ordering rule as § 3.5 applies.

## 5. Error envelope

Every error path emits the canonical DetailedError on stderr:

```json
{
  "error": {
    "summary": "<one-line summary>",
    "exitCode": 1,
    "details": "<structured multi-line detail>",
    "suggestions": [
      "<runnable command>",
      "<runnable command>"
    ]
  }
}
```

- Agent mode: single JSON document on stderr.
- TTY mode: rendered as plain text with suggestions prefixed by `→`.

Exit codes:

| Code | Meaning |
|------|---------|
| 0    | success |
| 1    | operation failed |
| 2    | usage error (invalid args, missing required input, agent-mode-without-required-flag) |

Single-target mutation failures: the inline `error` field on stdout MutationResult (§ 3.1) records *what* failed; the canonical envelope on stderr records *why* and suggests retry. They are complementary, not redundant.

## 6. Hints / progress / diagnostics

**Stderr only**. Never stdout.

### 6.1 TTY mode

Plain text, prefixed by class:

```
hint: showing first 50 results — pass --limit 100 to fetch more or --limit 0 for all
note: --open is ignored in agent mode
warn: default filter excluded 47 resolved groups
```

### 6.2 Agent mode (`GCX_AGENT_MODE=true` or auto-detected)

JSONL — one record per line, with a `class` field:

```jsonl
{"class":"hint","summary":"showing first 50 results","command":"gcx ... --limit 100"}
{"class":"note","summary":"--open is ignored in agent mode","url":"..."}
{"class":"warning","summary":"default filter excluded 47 resolved groups"}
```

### 6.3 Bulk progress

Bulk-by-filter operations may emit one progress event per item between command start and final stdout document:

```jsonl
{"event":"acknowledged","target":{"alertGroupId":"I..."}}
```

### 6.4 Hint shape variants

The default `hint:` shape is `<summary>: <command>` — a one-line summary followed by the suggested command. Two permitted variants:

**Inline-prose hint** — when the suggestion needs prose alternatives (e.g. *"pass X or Y"*). The TTY rendering is a single sentence; the agent JSONL keeps `summary` as the prose and embeds the most actionable single command in the `command` field. Example:

```
TTY:    hint: listing alert groups with filter default (excludes resolved + child groups); pass --all to include resolved + child groups, or --help for more filters
agent:  {"class":"hint","summary":"listing alert groups with filter default (excludes resolved + child groups); pass --all to include resolved + child groups, or --help for more filters","command":"gcx irm oncall alert-groups list --all"}
```

**Placeholder vs concrete UID** — choose based on whether the suggestion has a single canonical value:

- **`<id>` placeholder** when the hint is template-shaped (the agent should know to substitute their own ID). Used for nav hints emitted from list outputs where there are N rows:
  ```
  hint: drill into a group: gcx irm oncall alert-groups get <id>
  ```
- **Concrete UID** when there's exactly one canonical value (e.g. all alerts in a group share the same parent rule). Used for cross-provider pivot hints derived from a list result:
  ```
  hint: inspect rule: gcx alert rules get cfh3yivaya4n5a
  ```

Both forms are valid. Default to placeholders for nav-shaped hints; default to concrete UIDs when there's a single uniquely-correct value.

## 7. Implementation notes

### 7.1 Custom table codecs

Per-command table column choice should use a custom codec struct, **not** the heuristic auto-picker over `unstructured.Unstructured`. Reference patterns:

- `internal/providers/traces/adaptive/commands.go:110` (`recommendationTableCodec`) — full struct shape with `Wide bool` field, `Format()` / `Encode(w, v)` / `Decode` methods, registered on the command's `cmdio.Options`.
- `internal/query/tempo/formatter.go:343` (`formatTrace`) — the original `ColumnWidths` consumer.

The codec owns column choice; `style.TableBuilder` handles width allocation via `ColumnWidths`.

### 7.2 `ColumnWidths` semantics

`style.TableBuilder.ColumnWidths(widths []int)` widths **include** lipgloss `Padding(0, 1)` (2 chars total — 1 char on each side). A column with `width = 14` renders **12 chars of content**. Confirmed via lipgloss issue [#298](https://github.com/charmbracelet/lipgloss/issues/298).

Set widths accordingly:

| Content | Recommended width |
|---------|-------------------|
| 13-char OnCall PK (`IXXXXXXXXXXXX`) | 16 (14 content + 2 padding) |
| Severity (`warning` / `critical`) | 10 |
| State (`firing` / `acknowledged`) | 14 |
| Relative age (`2h ago`) | 12 |
| Counter | 8 |
| Variable / wide content (TITLE / TEAM / TARGET / SLO) | 0 (flexible) |

A `0` width is "flexible" — lipgloss allocates the remaining terminal width across the flex columns proportionally to content length.

### 7.3 Truncation in tables

For variable-width columns that risk overflow (e.g. TITLE), use **rune-aware** truncation with the `…` ellipsis (single Unicode char, 1 column wide). Compute the available width budget from `terminal.StdoutWidth()` minus the sum of fixed-width columns + lipgloss border/padding overhead.

Wrap is **not allowed** for human-readable list tables — truncate or shorten. Wrapping makes rows hard to scan and breaks `awk`-style consumption.

## See Also

- [`../architecture/resource-model.md`](../architecture/resource-model.md) — K8s envelope shape for reads.
- [`../architecture/patterns.md`](../architecture/patterns.md) — pattern catalog including format-agnostic data fetching (Pattern 13).
- [`../adrs/oncall-feature-expansion/001-sre-expansion.md`](../adrs/oncall-feature-expansion/001-sre-expansion.md) § 7.2 — IRM-specific application of the two-shape mutation rule.
- `cmd/gcx/instrumentation/output/result.go` — single-target `MutationResult` reference implementation (PR #597 instrumentation provider).
- `internal/providers/irm/oncall_actions.go` — two-shape mutation implementation (`singleMutationResult` / `bulkMutationResult`).
- `internal/resources/remote/summary.go` — `OperationSummary` for issue #264-style bulk `push`/`pull`/`delete` results.
- `internal/style/table.go` — `TableBuilder` and `ColumnWidths`.
