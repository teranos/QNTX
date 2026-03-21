# qntx-ix-otlp

OpenTelemetry trace ingestion plugin. Receives OTLP/HTTP JSON trace exports and persists each span as an `OTLPSpan` attestation in ATS. Designed for Agno with `tracing=True`, works with any OTLP-compatible agent framework.

**HTTP endpoint**: `POST /api/ix-otlp/v1/traces` (standard OTLP/HTTP path)

## Architecture

```
Agno Agent (Python, tracing=True)
  └─ OTLP HTTP exporter
     └─ POST /api/ix-otlp/v1/traces (JSON)
        └─ ix-otlp plugin (D)
           ├─ Parse ExportTraceServiceRequest JSON
           ├─ Walk resourceSpans → scopeSpans → spans
           └─ Per span → ATSStoreService.GenerateAndCreateAttestation
              subjects:   ["{project}:{agent_name}"]
              predicates: ["OTLPSpan"]
              contexts:   ["trace:{trace_id}"]
              attributes: { span_id, parent_span_id, name,
                            start_time_ns, end_time_ns,
                            gen_ai.*, resource.*, ... }
```

Loom reads `OTLPSpan` attestations from ATS and weaves them into embedding-ready text blocks. Traces persist as attestations regardless of whether loom is running.

## Agno configuration

```sh
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:877/api/ix-otlp
```

Or in Python:

```python
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

exporter = OTLPSpanExporter(endpoint="http://127.0.0.1:877/api/ix-otlp/v1/traces")
```

## Build

```sh
make build    # requires LDC2
make install  # copies to ~/.qntx/plugins/
make test     # runs unit tests
```

## Configuration

Add to `am.toml`:

```toml
[plugin]
enabled = ["ix-otlp"]
```
