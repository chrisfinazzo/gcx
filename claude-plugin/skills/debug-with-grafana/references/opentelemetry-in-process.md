# OpenTelemetry in-process stage

Use this stage to verify what happens inside the application process:
instrumentation creates spans, the SDK span processor handles them, and the
exporter attempts to send them to the next hop.

```text
application SDK / auto-instrumentation
  -> SDK processor / exporter
```

## What this stage can prove

- The application created a span with the expected trace ID and span ID.
- The span had the expected service/resource attributes.
- The SDK/exporter attempted to export spans and whether that export succeeded.
- The data was never created locally, which points to instrumentation, sampling,
  context propagation, or code-path coverage rather than downstream loss.

## Verify data is present in process

Use the narrowest affected time window and exact `service_instance_id` from the
trace when possible.

```bash
# Discover SDK/exporter/span metrics exposed by the application.
gcx metrics series -d <prom-uid> \
  '{__name__=~".*(otel|otlp|span|spans|exporter|processor).*",service_instance_id="<instance>"}' \
  --from <from> --to <to> -o json
```

Search application logs for the trace ID, a missing parent span ID, and a known
present span ID from the same trace as a control:

```bash
# Missing parent ID under investigation.
gcx logs query -d <loki-uid> \
  '{job="<service>"} |= "<missing-parent-span-id>"' \
  --from <from> --to <to> --limit 20 -o raw

# Control: a known present span ID from the same trace.
gcx logs query -d <loki-uid> \
  '{job="<service>"} |= "<known-present-span-id>"' \
  --from <from> --to <to> --limit 5 -o raw
```

The control matters: it proves the selected logs actually contain exporter or
trace-correlated output and that the query can find span IDs.

## Debug options

- Add a temporary console/logging exporter, if the SDK or auto-instrumentation
  supports it, to mirror spans to application logs while still exporting to the
  normal destination.
- Enable SDK or agent debug logging for a bounded reproduction window.
- Confirm the actual runtime configuration: service name, resource attributes,
  OTLP endpoint, protocol, headers, sampler, propagators, batch processor, and
  exporter type.
- Generate a fresh slow request with a known timestamp and trace ID, then compare
  local log output with Tempo output.
- For language-specific auto-instrumentation, check the language reference. For
  Java, see [`opentelemetry-java.md`](opentelemetry-java.md).

Debug logs and console exporters can be verbose and may affect performance; keep
them time-bounded.

## Metrics that suggest drops or export failure

Some SDK distributions expose exporter counters. When present, compare spans
seen by the exporter with spans successfully exported:

```bash
gcx metrics query -d <prom-uid> \
  'increase(otlp_exporter_seen_total{service_instance_id="<instance>",type="span"}[10m])' \
  --from <from> --to <to> --step 1m -o json

gcx metrics query -d <prom-uid> \
  'increase(otlp_exporter_exported_total{service_instance_id="<instance>",type="span",success="true"}[10m])' \
  --from <from> --to <to> --step 1m -o json

gcx metrics query -d <prom-uid> \
  'increase(otlp_exporter_exported_total{service_instance_id="<instance>",type="span",success="false"}[10m])' \
  --from <from> --to <to> --step 1m -o json
```

Interpretation:

- `seen` materially greater than `exported{success="true"}` suggests local
  exporter loss, retry backlog, or backpressure.
- non-zero `exported{success="false"}` indicates failed exports in that window.
- small differences can be Prometheus scrape / `increase()` extrapolation
  artifacts; look for sustained gaps or failures.

If these exact metrics are absent, use metric discovery and look for equivalents
such as exported spans, failed exports, dropped spans, queue size/capacity,
processor drops, or retry/backoff counters.

## Common conclusions

- **Span appears in local exporter logs and export succeeds**: move to the next
  stage (collector/Alloy or Grafana Cloud).
- **Span appears locally but export fails**: investigate endpoint, auth, network,
  protocol, batching, retry, or backpressure.
- **Span never appears locally**: investigate instrumentation coverage, sampling,
  async context propagation, disabled instrumentation, or remote upstream context
  that was propagated but never exported by this process.
