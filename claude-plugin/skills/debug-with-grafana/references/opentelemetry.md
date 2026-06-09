# OpenTelemetry investigation checks

General OpenTelemetry checks for application telemetry problems. Use this for
OTel-specific questions before going language-specific: missing or incomplete
traces, suspected export failures, collector/Alloy drops, or span gaps that need
to be separated from uninstrumented application time.

For Java agent specifics, see
[`opentelemetry-java.md`](opentelemetry-java.md).

## 1. Establish the telemetry path

Identify the path a span should take before assuming where it was lost:

```text
application SDK / auto-instrumentation
  -> SDK processor / exporter
  -> optional OpenTelemetry Collector or Alloy
  -> Grafana Cloud OTLP endpoint
  -> Tempo
```

For trace investigations, first fetch representative traces and identify exact
service instances and time windows:

```bash
gcx traces get -d <tempo-uid> <trace-id> --llm -o json
```

Record:

- `traceId`, span IDs, and any missing `parentSpanId`s;
- `service.name`, `service.namespace`, and `service_instance_id` labels;
- the trace start/end timestamps in UTC;
- whether the app exports directly to Grafana Cloud or through a collector.

## 2. Discover available OTel metrics

Metric names vary by SDK, distribution, collector, and version. Discover before
querying exact names:

```bash
# SDK / application-side telemetry metrics for an affected instance.
gcx metrics series -d <prom-uid> \
  '{__name__=~".*(otel|otlp|span|spans|exporter).*",service_instance_id="<instance>"}' \
  --from <from> --to <to> -o json

# Collector / Alloy self-metrics, when traffic passes through a collector.
gcx metrics series -d <prom-uid> \
  '{__name__=~"otelcol_.*"}' \
  --from <from> --to <to> -o json
```

Prefer exact `service_instance_id` and a narrow time window around the trace.
Broad service-level queries can hide instance-local exporter or collector
problems.

## 3. Check application exporter health

Some SDK distributions expose counters for spans seen by the exporter and spans
successfully exported. When present, compare them over the affected window:

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

If these metric names are absent, use the discovery query and inspect available
SDK/exporter counters instead of assuming no failures exist.

## 4. Check collector / Alloy span loss signals

If traffic goes through OpenTelemetry Collector or Alloy, compare common
collector self-metrics when present:

```bash
# Receiver refused spans: loss before processing.
gcx metrics query -d <prom-uid> \
  'sum(increase(otelcol_receiver_refused_spans[10m]))' \
  --from <from> --to <to> --step 1m -o json

# Exporter send failures: loss or retries after processing.
gcx metrics query -d <prom-uid> \
  'sum(increase(otelcol_exporter_send_failed_spans[10m]))' \
  --from <from> --to <to> --step 1m -o json
```

If these metric names are absent, inspect the discovered `otelcol_*` series for
receiver, processor, exporter, queue, retry, memory-limiter, and batch labels.

## 5. Search logs with a control span ID

Search for missing span or parent IDs directly. Always search for a known
present span ID from the same trace as a control to prove span IDs are searchable
in the logs you are querying.

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

Exporter and debug log formats vary by language and distribution. Absence of a
missing parent span ID from local logs means no span with that ID was observed in
the available logs; it does not prove that no remote context with that ID
existed.

## Decision points

- Exporter or collector failures present: investigate endpoint, auth, network,
  batching, retries, queues, memory limiter, and backpressure.
- No export failures, missing parent ID appears in exporter logs: suspect loss
  after local export or backend ingestion/path behavior.
- No export failures, missing parent ID does not appear in local logs: suspect
  upstream remote context, missing instrumentation boundary, or a span that was
  never created/exported locally.
- No missing-parent evidence, but waterfall gaps remain inside present spans:
  suspect uninstrumented application/runtime work and use targeted spans,
  runtime metrics, and wall-clock profiling.
