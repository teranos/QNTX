/// JSON field extraction for Claude API payloads.
///
/// Extracts model, image content blocks, and token usage from
/// plaintext HTTP request/response bodies. String-based parsing
/// (no regex, no external JSON library).
module ixnet.extract;

/// Extracted fields from a Claude API request body.
struct RequestInfo {
    string model;
    int imageCount;
    bool hasImages;
    bool streaming;      // "stream": true
}

/// Extracted fields from a Claude API response body.
struct ResponseInfo {
    int inputTokens;
    int outputTokens;
    int statusCode;      // from HTTP status line
}

/// Extract fields from a Claude API request JSON body.
RequestInfo extractRequest(const(ubyte)[] body_) {
    RequestInfo info;
    if (body_.length == 0) return info;

    auto json = cast(string)body_;

    // "model": "claude-opus-4-6"
    info.model = extractStringValue(json, `"model"`);

    // "stream": true
    auto streamVal = extractRawValue(json, `"stream"`);
    info.streaming = (streamVal == "true");

    // Count image content blocks: "type": "image"
    size_t pos = 0;
    while (pos < json.length) {
        auto idx = findSubstring(json, `"type"`, pos);
        if (idx < 0) break;

        auto val = extractRawValue(json[idx .. $], `"type"`);
        if (val == `"image"` || val == `"image_url"`) {
            info.imageCount++;
            info.hasImages = true;
        }
        pos = idx + 6; // skip past `"type"`
    }

    return info;
}

/// Extract usage fields from a Claude API response JSON body.
/// Works for both non-streaming responses and streaming final events.
ResponseInfo extractResponse(const(ubyte)[] body_) {
    ResponseInfo info;
    if (body_.length == 0) return info;

    auto json = cast(string)body_;

    // "input_tokens": 1234
    auto inputStr = extractRawValue(json, `"input_tokens"`);
    if (inputStr.length > 0) info.inputTokens = parsePositiveInt(inputStr);

    // "output_tokens": 567
    auto outputStr = extractRawValue(json, `"output_tokens"`);
    if (outputStr.length > 0) info.outputTokens = parsePositiveInt(outputStr);

    return info;
}

/// Extract usage from streaming response (SSE events).
/// Scans for the last "usage" block in the accumulated event data.
ResponseInfo extractStreamingResponse(const(ubyte)[] body_) {
    ResponseInfo info;
    if (body_.length == 0) return info;

    auto text = cast(string)body_;

    // In streaming, usage appears in message_delta event near the end:
    //   data: {"type":"message_delta","usage":{"output_tokens":123}}
    // And in message_start:
    //   data: {"type":"message_start","message":{"usage":{"input_tokens":456}}}
    //
    // Find the LAST occurrence of "input_tokens" and "output_tokens"
    ptrdiff_t lastInput = -1;
    ptrdiff_t lastOutput = -1;
    size_t pos = 0;

    while (pos < text.length) {
        auto idx = findSubstring(text, `"input_tokens"`, pos);
        if (idx < 0) break;
        lastInput = idx;
        pos = idx + 14;
    }
    pos = 0;
    while (pos < text.length) {
        auto idx = findSubstring(text, `"output_tokens"`, pos);
        if (idx < 0) break;
        lastOutput = idx;
        pos = idx + 15;
    }

    if (lastInput >= 0) {
        auto val = extractRawValue(text[lastInput .. $], `"input_tokens"`);
        info.inputTokens = parsePositiveInt(val);
    }
    if (lastOutput >= 0) {
        auto val = extractRawValue(text[lastOutput .. $], `"output_tokens"`);
        info.outputTokens = parsePositiveInt(val);
    }

    return info;
}

// ---------------------------------------------------------------------------
// String-based JSON field extraction
// ---------------------------------------------------------------------------

/// Extract a quoted string value for a given key.
/// e.g., extractStringValue(`{"model":"claude-3"}`, `"model"`) → "claude-3"
private string extractStringValue(string json, string key) {
    auto raw = extractRawValue(json, key);
    if (raw.length >= 2 && raw[0] == '"' && raw[$ - 1] == '"') {
        return raw[1 .. $ - 1].idup;
    }
    return "";
}

/// Extract the raw value token after "key": in JSON.
/// Returns the value portion (could be string, number, bool, object, array).
private string extractRawValue(string json, string key) {
    auto keyIdx = findSubstring(json, key, 0);
    if (keyIdx < 0) return "";

    // Skip past key and find ':'
    size_t pos = keyIdx + key.length;
    while (pos < json.length && json[pos] == ' ') pos++;
    if (pos >= json.length || json[pos] != ':') return "";
    pos++; // skip ':'
    while (pos < json.length && json[pos] == ' ') pos++;
    if (pos >= json.length) return "";

    // Determine value type and extract
    if (json[pos] == '"') {
        // String value — find closing quote (handle escaped quotes)
        size_t end = pos + 1;
        while (end < json.length) {
            if (json[end] == '\\') { end += 2; continue; }
            if (json[end] == '"') { end++; break; }
            end++;
        }
        return cast(string)json[pos .. end];
    } else {
        // Number, bool, null — read until delimiter
        size_t end = pos;
        while (end < json.length && json[end] != ',' && json[end] != '}'
               && json[end] != ']' && json[end] != ' ' && json[end] != '\n') {
            end++;
        }
        return cast(string)json[pos .. end];
    }
}

/// Find substring starting at offset. Returns index or -1.
private ptrdiff_t findSubstring(string haystack, string needle, size_t offset = 0) {
    if (needle.length == 0) return cast(ptrdiff_t)offset;
    if (offset + needle.length > haystack.length) return -1;
    foreach (i; offset .. haystack.length - needle.length + 1) {
        if (haystack[i .. i + needle.length] == needle) return cast(ptrdiff_t)i;
    }
    return -1;
}

/// Parse a positive integer from a string. Returns 0 on failure.
private int parsePositiveInt(string s) {
    int result = 0;
    foreach (c; s) {
        if (c >= '0' && c <= '9') {
            result = result * 10 + (c - '0');
        } else {
            break;
        }
    }
    return result;
}
