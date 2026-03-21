/// OTLP JSON decoder — parses ExportTraceServiceRequest and produces
/// one AttestationCommand per span.
///
/// Each span becomes an OTLPSpan attestation in ATS. Loom reads these
/// on startup and weaves them into embedding-ready text blocks.
///
/// Attestation structure:
///   subjects:   ["{project}:{agent_name}"]
///   predicates: ["OTLPSpan"]
///   contexts:   ["trace:{trace_id}"]
///   attributes: { span_id, parent_span_id, name, start_time_ns, end_time_ns,
///                 gen_ai.*, tool.*, and all other span attributes }
module ixotlp.otlp;

import ixotlp.proto : AttestationCommand, encodeStructFromStringMap;
import ixotlp.version_ : PLUGIN_NAME, PLUGIN_VERSION;
import ixotlp.log;

import std.json;
import std.conv : to;

struct IngestResult {
    AttestationCommand[] attestations;
    int spanCount;
    int traceCount;
    string lastError;
}

/// Parse OTLP JSON ExportTraceServiceRequest and produce attestation commands.
IngestResult ingestOTLP(string body_) {
    IngestResult result;

    JSONValue json;
    try {
        json = parseJSON(body_);
    } catch (Exception e) {
        result.lastError = "JSON parse error: " ~ e.msg;
        logError("[ix-otlp] %s", result.lastError);
        return result;
    }

    if (json.type != JSONType.object) {
        result.lastError = "expected JSON object at root";
        return result;
    }

    auto resourceSpansPtr = "resourceSpans" in json;
    if (resourceSpansPtr is null || resourceSpansPtr.type != JSONType.array) {
        result.lastError = "missing or invalid resourceSpans";
        return result;
    }

    bool[string] seenTraces;

    foreach (ref rs; resourceSpansPtr.array) {
        // Extract resource attributes
        string[string] resourceAttrs;
        auto resourcePtr = "resource" in rs;
        if (resourcePtr !is null && resourcePtr.type == JSONType.object) {
            auto attrsPtr = "attributes" in *resourcePtr;
            if (attrsPtr !is null && attrsPtr.type == JSONType.array) {
                resourceAttrs = extractOTLPAttrs(*attrsPtr);
            }
        }

        string serviceName = "service.name" in resourceAttrs ? resourceAttrs["service.name"] : "";
        string project = "qntx.project" in resourceAttrs ? resourceAttrs["qntx.project"] : "";

        auto scopeSpansPtr = "scopeSpans" in rs;
        if (scopeSpansPtr is null || scopeSpansPtr.type != JSONType.array) continue;

        foreach (ref ss; scopeSpansPtr.array) {
            auto spansPtr = "spans" in ss;
            if (spansPtr is null || spansPtr.type != JSONType.array) continue;

            foreach (ref span; spansPtr.array) {
                if (span.type != JSONType.object) continue;

                auto cmd = spanToAttestation(span, resourceAttrs, serviceName, project);
                if (cmd.subjects.length > 0) {
                    result.attestations ~= cmd;
                    result.spanCount++;

                    // Count unique traces
                    if (cmd.contexts.length > 0) {
                        seenTraces[cmd.contexts[0]] = true;
                    }
                }
            }
        }
    }

    result.traceCount = cast(int)seenTraces.length;
    return result;
}

/// Extract OTLP attributes array [{key, value}] into flat string map.
/// OTLP values are typed: {stringValue, intValue, boolValue, doubleValue, ...}
private string[string] extractOTLPAttrs(ref JSONValue attrsArray) {
    string[string] result;
    foreach (ref attr; attrsArray.array) {
        if (attr.type != JSONType.object) continue;
        auto keyPtr = "key" in attr;
        if (keyPtr is null || keyPtr.type != JSONType.string_) continue;
        string key = keyPtr.str;

        auto valuePtr = "value" in attr;
        if (valuePtr is null || valuePtr.type != JSONType.object) continue;

        string val = extractOTLPValue(*valuePtr);
        if (val.length > 0) {
            result[key] = val;
        }
    }
    return result;
}

/// Extract a single OTLP typed value to string.
private string extractOTLPValue(ref JSONValue v) {
    auto sv = "stringValue" in v;
    if (sv !is null && sv.type == JSONType.string_) return sv.str;

    auto iv = "intValue" in v;
    if (iv !is null) {
        if (iv.type == JSONType.string_) return iv.str;
        if (iv.type == JSONType.integer) return to!string(iv.integer);
    }

    auto bv = "boolValue" in v;
    if (bv !is null) {
        if (bv.type == JSONType.true_) return "true";
        if (bv.type == JSONType.false_) return "false";
    }

    auto dv = "doubleValue" in v;
    if (dv !is null && dv.type == JSONType.float_) return to!string(dv.floating);

    return "";
}

/// Convert a single OTLP span JSON object to an AttestationCommand.
private AttestationCommand spanToAttestation(
    ref JSONValue span,
    string[string] resourceAttrs,
    string serviceName,
    string project
) {
    AttestationCommand cmd;

    // Extract basic span fields
    string traceId = jsonStr(span, "traceId");
    string spanId = jsonStr(span, "spanId");
    string parentSpanId = jsonStr(span, "parentSpanId");
    string name = jsonStr(span, "name");
    string startTimeNs = jsonStr(span, "startTimeUnixNano");
    string endTimeNs = jsonStr(span, "endTimeUnixNano");

    if (traceId.length == 0) return cmd; // skip spans without trace ID

    // Extract span attributes
    string[string] spanAttrs;
    auto attrsPtr = "attributes" in span;
    if (attrsPtr !is null && attrsPtr.type == JSONType.array) {
        spanAttrs = extractOTLPAttrs(*attrsPtr);
    }

    // Extract span events (gen_ai.content.prompt, gen_ai.content.completion)
    auto eventsPtr = "events" in span;
    if (eventsPtr !is null && eventsPtr.type == JSONType.array) {
        foreach (ref evt; eventsPtr.array) {
            if (evt.type != JSONType.object) continue;
            string evtName = jsonStr(evt, "name");
            auto evtAttrsPtr = "attributes" in evt;
            if (evtAttrsPtr is null || evtAttrsPtr.type != JSONType.array) continue;
            auto evtAttrs = extractOTLPAttrs(*evtAttrsPtr);

            // Store event content as span-level attributes with event prefix
            if (evtName == "gen_ai.content.prompt") {
                auto p = "gen_ai.prompt" in evtAttrs;
                if (p !is null) spanAttrs["gen_ai.prompt"] = *p;
            } else if (evtName == "gen_ai.content.completion") {
                auto c = "gen_ai.completion" in evtAttrs;
                if (c !is null) spanAttrs["gen_ai.completion"] = *c;
            }
        }
    }

    // Derive agent name from span or resource attributes
    string agentName = "gen_ai.agent.name" in spanAttrs ? spanAttrs["gen_ai.agent.name"] : "";
    if (agentName.length == 0) {
        agentName = "gen_ai.agent.name" in resourceAttrs ? resourceAttrs["gen_ai.agent.name"] : "";
    }

    // Derive branch/subject
    string branchPrefix = project.length > 0 ? project :
                          serviceName.length > 0 ? serviceName : "agno";
    string branchSuffix = agentName.length > 0 ? agentName : "agent";
    string branch = branchPrefix ~ ":" ~ branchSuffix;

    // Build attestation attributes
    string[string] attrs;
    attrs["span_id"] = spanId;
    attrs["parent_span_id"] = parentSpanId;
    attrs["name"] = name;
    attrs["start_time_ns"] = startTimeNs;
    attrs["end_time_ns"] = endTimeNs;

    // Include all span attributes (gen_ai.*, tool.*, etc.)
    foreach (k, v; spanAttrs) {
        attrs[k] = v;
    }

    // Include resource attributes that aren't already present
    foreach (k, v; resourceAttrs) {
        if (k !in attrs) {
            attrs["resource." ~ k] = v;
        }
    }

    cmd.subjects = [branch];
    cmd.predicates = ["OTLPSpan"];
    cmd.contexts = ["trace:" ~ traceId];
    cmd.source = PLUGIN_NAME;
    cmd.sourceVersion = PLUGIN_VERSION;
    cmd.attributes = encodeStructFromStringMap(attrs);

    return cmd;
}

/// Safe string extraction from JSON object.
private string jsonStr(ref JSONValue obj, string key) {
    auto ptr = key in obj;
    if (ptr is null) return "";
    if (ptr.type == JSONType.string_) return ptr.str;
    if (ptr.type == JSONType.integer) return to!string(ptr.integer);
    return "";
}
