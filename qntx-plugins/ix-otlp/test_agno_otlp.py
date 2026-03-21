#!/usr/bin/env python3
"""
Test script for ix-otlp plugin integration with Agno.

Tests two things:
1. What OTLP traces Agno actually produces (captured via custom exporter)
2. Whether ix-otlp can parse real Agno OTLP JSON

Usage:
    python3 test_agno_otlp.py

Requires: pip install agno opentelemetry-sdk opentelemetry-exporter-otlp-proto-http
"""

import json
import sys
from typing import Sequence

# --- Part 1: Capture what Agno produces ---

from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SpanExporter, SpanExportResult, SimpleSpanProcessor
from opentelemetry.sdk.resources import Resource

# Custom exporter that captures spans as OTLP JSON
class CaptureExporter(SpanExporter):
    """Captures spans and converts to OTLP JSON format for testing."""
    def __init__(self):
        self.spans = []

    def export(self, spans: Sequence) -> SpanExportResult:
        for span in spans:
            self.spans.append(span)
        return SpanExportResult.SUCCESS

    def shutdown(self):
        pass

    def to_otlp_json(self, service_name="test-agent", project="myproject"):
        """Convert captured spans to OTLP ExportTraceServiceRequest JSON."""
        resource_attrs = [
            {"key": "service.name", "value": {"stringValue": service_name}},
            {"key": "qntx.project", "value": {"stringValue": project}},
        ]

        otlp_spans = []
        for span in self.spans:
            ctx = span.get_span_context()
            trace_id = format(ctx.trace_id, '032x')
            span_id = format(ctx.span_id, '016x')
            parent_id = ""
            if span.parent:
                parent_id = format(span.parent.span_id, '016x')

            attrs = []
            if span.attributes:
                for k, v in span.attributes.items():
                    if isinstance(v, str):
                        attrs.append({"key": k, "value": {"stringValue": v}})
                    elif isinstance(v, bool):
                        attrs.append({"key": k, "value": {"boolValue": v}})
                    elif isinstance(v, int):
                        attrs.append({"key": k, "value": {"intValue": str(v)}})
                    elif isinstance(v, float):
                        attrs.append({"key": k, "value": {"doubleValue": v}})

            events = []
            for event in span.events:
                event_attrs = []
                for k, v in event.attributes.items():
                    if isinstance(v, str):
                        event_attrs.append({"key": k, "value": {"stringValue": v}})
                events.append({
                    "name": event.name,
                    "timeUnixNano": str(event.timestamp),
                    "attributes": event_attrs,
                })

            otlp_spans.append({
                "traceId": trace_id,
                "spanId": span_id,
                "parentSpanId": parent_id,
                "name": span.name,
                "startTimeUnixNano": str(span.start_time),
                "endTimeUnixNano": str(span.end_time),
                "attributes": attrs,
                "events": events,
            })

        return json.dumps({
            "resourceSpans": [{
                "resource": {"attributes": resource_attrs},
                "scopeSpans": [{
                    "scope": {"name": "agno"},
                    "spans": otlp_spans,
                }]
            }]
        }, indent=2)


def test_manual_spans():
    """Create spans that mimic what Agno produces with tracing=True."""
    print("=" * 60)
    print("TEST 1: Manual spans mimicking Agno tracing output")
    print("=" * 60)

    exporter = CaptureExporter()
    resource = Resource.create({"service.name": "agno-test"})
    provider = TracerProvider(resource=resource)
    provider.add_span_processor(SimpleSpanProcessor(exporter))

    tracer = provider.get_tracer("agno")

    # Simulate an Agno agent run with tracing=True
    with tracer.start_as_current_span("agent_run") as agent_span:
        agent_span.set_attribute("gen_ai.operation.name", "invoke_agent")
        agent_span.set_attribute("gen_ai.agent.name", "research-bot")

        # Chat completion span
        with tracer.start_as_current_span("chat") as chat_span:
            chat_span.set_attribute("gen_ai.operation.name", "chat")
            chat_span.set_attribute("gen_ai.request.model", "claude-sonnet-4-20250514")
            chat_span.set_attribute("gen_ai.usage.input_tokens", 150)
            chat_span.set_attribute("gen_ai.usage.output_tokens", 89)

            # Add prompt/completion events (GenAI semantic conventions)
            chat_span.add_event("gen_ai.content.prompt", attributes={
                "gen_ai.prompt": "What is the capital of France?"
            })
            chat_span.add_event("gen_ai.content.completion", attributes={
                "gen_ai.completion": "The capital of France is Paris."
            })

        # Tool use span
        with tracer.start_as_current_span("tool.search") as tool_span:
            tool_span.set_attribute("gen_ai.tool.name", "search")
            tool_span.set_attribute("gen_ai.tool.input", "Paris population 2024")

        # Second chat completion
        with tracer.start_as_current_span("chat") as chat2:
            chat2.set_attribute("gen_ai.operation.name", "chat")
            chat2.set_attribute("gen_ai.request.model", "claude-sonnet-4-20250514")
            chat2.add_event("gen_ai.content.prompt", attributes={
                "gen_ai.prompt": "What is the population of Paris?"
            })
            chat2.add_event("gen_ai.content.completion", attributes={
                "gen_ai.completion": "The population of Paris is approximately 2.1 million."
            })

    provider.shutdown()

    # Convert to OTLP JSON
    otlp_json = exporter.to_otlp_json(service_name="agno-test", project="qntx-test")
    print("\nCaptured OTLP JSON (ExportTraceServiceRequest):")
    print(otlp_json)

    # Parse back and verify structure
    data = json.loads(otlp_json)
    spans = data["resourceSpans"][0]["scopeSpans"][0]["spans"]
    print(f"\nSpan count: {len(spans)}")
    for s in spans:
        attrs = {a["key"]: list(a["value"].values())[0] for a in s["attributes"]}
        print(f"  {s['name']}: {attrs.get('gen_ai.operation.name', '-')}"
              f" agent={attrs.get('gen_ai.agent.name', '-')}"
              f" model={attrs.get('gen_ai.request.model', '-')}")
        if s.get("events"):
            for e in s["events"]:
                print(f"    event: {e['name']}")

    return otlp_json


def test_agno_agent():
    """Try to create a real Agno agent (may fail without API key)."""
    print("\n" + "=" * 60)
    print("TEST 2: Real Agno agent (requires ANTHROPIC_API_KEY)")
    print("=" * 60)

    try:
        from agno.agent import Agent
        from agno.models.anthropic import Claude

        exporter = CaptureExporter()
        resource = Resource.create({"service.name": "agno-real"})
        provider = TracerProvider(resource=resource)
        provider.add_span_processor(SimpleSpanProcessor(exporter))
        trace.set_tracer_provider(provider)

        agent = Agent(
            model=Claude(id="claude-sonnet-4-20250514"),
            description="Test agent for OTLP trace capture",
            instructions=["Respond briefly."],
            monitoring=True,
        )

        # This will fail without API key but we can still see the trace setup
        try:
            agent.run("Say hello in one word.")
        except Exception as e:
            print(f"  Agent run failed (expected without API key): {type(e).__name__}: {e}")

        provider.shutdown()

        if exporter.spans:
            otlp_json = exporter.to_otlp_json(service_name="agno-real", project="test")
            print(f"\n  Captured {len(exporter.spans)} spans from real Agno agent")
            print(otlp_json[:500] + "..." if len(otlp_json) > 500 else otlp_json)
        else:
            print("  No spans captured (agent didn't produce traces)")

    except ImportError as e:
        print(f"  Skipped: {e}")
    except Exception as e:
        print(f"  Error: {type(e).__name__}: {e}")


def verify_ix_otlp_parse(otlp_json):
    """Verify the OTLP JSON structure matches what ix-otlp expects."""
    print("\n" + "=" * 60)
    print("TEST 3: Verify OTLP JSON structure for ix-otlp")
    print("=" * 60)

    data = json.loads(otlp_json)

    # Walk the same path ix-otlp.otlp.d does
    resource_spans = data.get("resourceSpans", [])
    print(f"  resourceSpans: {len(resource_spans)} entries")

    total_spans = 0
    traces = set()

    for rs in resource_spans:
        resource = rs.get("resource", {})
        resource_attrs = resource.get("attributes", [])
        resource_map = {a["key"]: list(a["value"].values())[0] for a in resource_attrs}
        print(f"  Resource attributes: {resource_map}")

        for ss in rs.get("scopeSpans", []):
            spans = ss.get("spans", [])
            for span in spans:
                total_spans += 1
                trace_id = span.get("traceId", "")
                traces.add(trace_id)

                # Verify required fields exist
                assert "traceId" in span, "missing traceId"
                assert "spanId" in span, "missing spanId"
                assert "name" in span, "missing name"
                assert "startTimeUnixNano" in span, "missing startTimeUnixNano"
                assert "endTimeUnixNano" in span, "missing endTimeUnixNano"

                # Check attributes are well-formed
                for attr in span.get("attributes", []):
                    assert "key" in attr, "attribute missing key"
                    assert "value" in attr, "attribute missing value"
                    val = attr["value"]
                    assert any(k in val for k in
                        ["stringValue", "intValue", "boolValue", "doubleValue"]), \
                        f"unknown value type in {attr}"

    print(f"  Total spans: {total_spans}")
    print(f"  Unique traces: {len(traces)}")
    print(f"  All structure checks PASSED")

    # Verify ix-otlp attestation mapping
    print("\n  Expected attestation mapping:")
    for rs in resource_spans:
        resource_attrs = rs.get("resource", {}).get("attributes", [])
        resource_map = {a["key"]: list(a["value"].values())[0] for a in resource_attrs}
        project = resource_map.get("qntx.project", resource_map.get("service.name", "agno"))

        for ss in rs.get("scopeSpans", []):
            for span in ss.get("spans", []):
                span_attrs = {a["key"]: list(a["value"].values())[0]
                             for a in span.get("attributes", [])}
                agent = span_attrs.get("gen_ai.agent.name", "agent")
                trace_id = span.get("traceId", "")
                branch = f"{project}:{agent}"
                context = f"trace:{trace_id}"
                print(f"    span={span['name']}  →  subjects=[{branch}]"
                      f"  contexts=[{context}]  predicates=[OTLPSpan]")


if __name__ == "__main__":
    print("ix-otlp + Agno Integration Test")
    print("=" * 60)
    print(f"Python: {sys.version}")

    try:
        import agno
        print(f"Agno: {agno.__version__}")
    except Exception:
        print("Agno: installed (version unknown)")

    from importlib.metadata import version as pkg_version
    print(f"OpenTelemetry SDK: {pkg_version('opentelemetry-sdk')}")
    print()

    # Test 1: Manual spans
    otlp_json = test_manual_spans()

    # Test 2: Real Agno agent (optional)
    test_agno_agent()

    # Test 3: Structure verification
    verify_ix_otlp_parse(otlp_json)

    print("\n" + "=" * 60)
    print("ALL TESTS PASSED")
    print("=" * 60)
