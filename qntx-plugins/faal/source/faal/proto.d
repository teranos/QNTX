/// Protobuf wire format codec using D's compile-time function execution (CTFE).
///
/// Struct fields annotated with @Proto(N) generate encode/decode code at compile time.
/// No runtime reflection, no schema registry — the struct layout IS the codec.
module faal.proto;

/// UDA to mark protobuf field numbers on struct fields.
struct Proto {
    int num;
}

/// UDA to mark a field as a protobuf map<string,string>.
struct ProtoMap {}

/// UDA to mark a field as repeated (array of embedded messages or strings).
struct Repeated {}

/// Protobuf wire types.
enum WireType : ubyte {
    Varint = 0,
    Fixed64 = 1,
    LengthDelimited = 2,
    Fixed32 = 5,
}

// ---------------------------------------------------------------------------
// Varint encoding/decoding
// ---------------------------------------------------------------------------

ubyte[] encodeVarint(ulong value) {
    ubyte[] result;
    do {
        ubyte b = cast(ubyte)(value & 0x7F);
        value >>= 7;
        if (value != 0) b |= 0x80;
        result ~= b;
    } while (value != 0);
    return result;
}

ulong decodeVarint(const ubyte[] data, ref size_t pos) {
    ulong result = 0;
    uint shift = 0;
    while (pos < data.length) {
        ubyte b = data[pos++];
        result |= (cast(ulong)(b & 0x7F)) << shift;
        if ((b & 0x80) == 0) return result;
        shift += 7;
        if (shift >= 64) break;
    }
    return result;
}

/// Encode a tag (field number + wire type).
ubyte[] encodeTag(int fieldNum, WireType wt) {
    return encodeVarint((cast(ulong)fieldNum << 3) | wt);
}

// ---------------------------------------------------------------------------
// Skip unknown fields
// ---------------------------------------------------------------------------

void skipField(const ubyte[] data, ref size_t pos, WireType wt) {
    final switch (wt) {
        case WireType.Varint:
            decodeVarint(data, pos);
            break;
        case WireType.Fixed64:
            pos += 8;
            break;
        case WireType.LengthDelimited:
            auto len = cast(size_t)decodeVarint(data, pos);
            pos += len;
            break;
        case WireType.Fixed32:
            pos += 4;
            break;
    }
}

// ---------------------------------------------------------------------------
// CTFE-powered generic encoder
// ---------------------------------------------------------------------------

/// Encode any @Proto-annotated struct to protobuf wire format.
ubyte[] encode(T)(const T msg) {
    ubyte[] result;
    static foreach (i, field; T.tupleof) {{
        alias FT = typeof(field);
        enum hasProto = hasProtoUDA!(T, i);
        static if (hasProto) {
            enum fieldNum = getProtoNum!(T, i);
            auto value = msg.tupleof[i];
            result ~= encodeField!(FT)(fieldNum, value);
        }
    }}
    return result;
}

private ubyte[] encodeField(FT)(int fieldNum, const FT value) {
    ubyte[] result;
    static if (is(FT == string)) {
        if (value.length > 0) {
            result ~= encodeTag(fieldNum, WireType.LengthDelimited);
            result ~= encodeVarint(value.length);
            result ~= cast(const(ubyte)[])value;
        }
    } else static if (is(FT == const(ubyte)[])) {
        if (value.length > 0) {
            result ~= encodeTag(fieldNum, WireType.LengthDelimited);
            result ~= encodeVarint(value.length);
            result ~= value;
        }
    } else static if (is(FT == ubyte[])) {
        if (value.length > 0) {
            result ~= encodeTag(fieldNum, WireType.LengthDelimited);
            result ~= encodeVarint(value.length);
            result ~= value;
        }
    } else static if (is(FT == bool)) {
        if (value) {
            result ~= encodeTag(fieldNum, WireType.Varint);
            result ~= encodeVarint(1);
        }
    } else static if (is(FT == int) || is(FT == uint) || is(FT == long) || is(FT == ulong)) {
        if (value != 0) {
            result ~= encodeTag(fieldNum, WireType.Varint);
            result ~= encodeVarint(cast(ulong)value);
        }
    } else static if (is(FT == double)) {
        if (value != 0.0) {
            result ~= encodeTag(fieldNum, WireType.Fixed64);
            auto bits = *cast(const(ubyte)[8]*)&value;
            result ~= bits[];
        }
    } else static if (is(FT == string[string])) {
        foreach (k, v; value) {
            ubyte[] entry;
            if (k.length > 0) {
                entry ~= encodeTag(1, WireType.LengthDelimited);
                entry ~= encodeVarint(k.length);
                entry ~= cast(const(ubyte)[])k;
            }
            if (v.length > 0) {
                entry ~= encodeTag(2, WireType.LengthDelimited);
                entry ~= encodeVarint(v.length);
                entry ~= cast(const(ubyte)[])v;
            }
            result ~= encodeTag(fieldNum, WireType.LengthDelimited);
            result ~= encodeVarint(entry.length);
            result ~= entry;
        }
    } else static if (is(FT == string[])) {
        foreach (s; value) {
            result ~= encodeTag(fieldNum, WireType.LengthDelimited);
            result ~= encodeVarint(s.length);
            result ~= cast(const(ubyte)[])s;
        }
    } else static if (is(FT == E[], E) && is(E == struct)) {
        foreach (ref elem; value) {
            auto sub = encode(elem);
            result ~= encodeTag(fieldNum, WireType.LengthDelimited);
            result ~= encodeVarint(sub.length);
            result ~= sub;
        }
    }
    return result;
}

// ---------------------------------------------------------------------------
// CTFE-powered generic decoder
// ---------------------------------------------------------------------------

/// Decode a @Proto-annotated struct from protobuf wire format.
T decode(T)(const ubyte[] data) {
    T result;
    size_t pos = 0;
    while (pos < data.length) {
        auto tag = decodeVarint(data, pos);
        int fieldNum = cast(int)(tag >> 3);
        WireType wt = cast(WireType)(tag & 0x7);

        bool matched = false;
        static foreach (i, field; T.tupleof) {{
            static if (hasProtoUDA!(T, i)) {
                enum expectedNum = getProtoNum!(T, i);
                if (fieldNum == expectedNum) {
                    alias FT = typeof(field);
                    decodeFieldInto!(FT)(data, pos, wt, result.tupleof[i]);
                    matched = true;
                }
            }
        }}
        if (!matched) {
            skipField(data, pos, wt);
        }
    }
    return result;
}

private void decodeFieldInto(FT)(const ubyte[] data, ref size_t pos, WireType wt, ref FT target) {
    static if (is(FT == string)) {
        if (wt == WireType.LengthDelimited) {
            auto len = cast(size_t)decodeVarint(data, pos);
            if (pos + len <= data.length) {
                target = cast(string)data[pos .. pos + len].idup;
                pos += len;
            }
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == ubyte[])) {
        if (wt == WireType.LengthDelimited) {
            auto len = cast(size_t)decodeVarint(data, pos);
            if (pos + len <= data.length) {
                target = data[pos .. pos + len].dup;
                pos += len;
            }
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == bool)) {
        if (wt == WireType.Varint) {
            target = decodeVarint(data, pos) != 0;
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == int)) {
        if (wt == WireType.Varint) {
            target = cast(int)decodeVarint(data, pos);
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == uint)) {
        if (wt == WireType.Varint) {
            target = cast(uint)decodeVarint(data, pos);
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == long)) {
        if (wt == WireType.Varint) {
            target = cast(long)decodeVarint(data, pos);
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == ulong)) {
        if (wt == WireType.Varint) {
            target = decodeVarint(data, pos);
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == double)) {
        if (wt == WireType.Fixed64) {
            if (pos + 8 <= data.length) {
                target = *cast(const(double)*)data[pos .. pos + 8].ptr;
                pos += 8;
            }
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == string[string])) {
        if (wt == WireType.LengthDelimited) {
            auto len = cast(size_t)decodeVarint(data, pos);
            auto end = pos + len;
            string k, v;
            while (pos < end) {
                auto entryTag = decodeVarint(data, pos);
                int entryNum = cast(int)(entryTag >> 3);
                WireType entryWt = cast(WireType)(entryTag & 0x7);
                if (entryNum == 1 && entryWt == WireType.LengthDelimited) {
                    auto slen = cast(size_t)decodeVarint(data, pos);
                    k = cast(string)data[pos .. pos + slen].idup;
                    pos += slen;
                } else if (entryNum == 2 && entryWt == WireType.LengthDelimited) {
                    auto slen = cast(size_t)decodeVarint(data, pos);
                    v = cast(string)data[pos .. pos + slen].idup;
                    pos += slen;
                } else {
                    skipField(data, pos, entryWt);
                }
            }
            target[k] = v;
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == string[])) {
        if (wt == WireType.LengthDelimited) {
            auto len = cast(size_t)decodeVarint(data, pos);
            if (pos + len <= data.length) {
                target ~= cast(string)data[pos .. pos + len].idup;
                pos += len;
            }
        } else {
            skipField(data, pos, wt);
        }
    } else static if (is(FT == E[], E) && is(E == struct)) {
        if (wt == WireType.LengthDelimited) {
            auto len = cast(size_t)decodeVarint(data, pos);
            if (pos + len <= data.length) {
                target ~= decode!E(data[pos .. pos + len]);
                pos += len;
            }
        } else {
            skipField(data, pos, wt);
        }
    } else {
        skipField(data, pos, wt);
    }
}

// ---------------------------------------------------------------------------
// Compile-time UDA helpers
// ---------------------------------------------------------------------------

private template hasProtoUDA(T, size_t i) {
    import std.traits : getUDAs;
    alias udas = getUDAs!(T.tupleof[i], Proto);
    enum hasProtoUDA = udas.length > 0;
}

private template getProtoNum(T, size_t i) {
    import std.traits : getUDAs;
    alias udas = getUDAs!(T.tupleof[i], Proto);
    enum getProtoNum = udas[0].num;
}

// ---------------------------------------------------------------------------
// Protocol message types (mirrors domain.proto)
// ---------------------------------------------------------------------------

struct Empty {}

struct MetadataResponse {
    @Proto(1) string name;
    @Proto(2) string version_;
    @Proto(3) string qntxVersion;
    @Proto(4) string description;
    @Proto(5) string author;
    @Proto(6) string license;
}

struct InitializeRequest {
    @Proto(1) string atsStoreEndpoint;
    @Proto(2) string queueEndpoint;
    @Proto(3) string authToken;
    @Proto(4) string[string] config;
    @Proto(5) string scheduleEndpoint;
    @Proto(6) string fileServiceEndpoint;
}

struct ScheduleInfo {
    @Proto(1) string handlerName;
    @Proto(2) int intervalSeconds;
    @Proto(3) bool enabledByDefault;
    @Proto(4) string description;
    @Proto(5) string atsCode;
}

struct InitializeResponse {
    @Proto(1) string[] handlerNames;
    @Proto(2) ScheduleInfo[] schedules;
}

struct HTTPRequest {
    @Proto(1) string method;
    @Proto(2) string path;
    @Proto(3) HTTPHeader[] headers;
    @Proto(4) ubyte[] body_;
}

struct HTTPHeader {
    @Proto(1) string name;
    @Proto(2) string[] values;
}

struct HTTPResponse {
    @Proto(1) int statusCode;
    @Proto(2) HTTPHeader[] headers;
    @Proto(3) ubyte[] body_;
}

struct HealthResponse {
    @Proto(1) bool healthy;
    @Proto(2) string message;
    @Proto(3) string[string] details;
}

struct ConfigSchemaResponse {
    @Proto(1) string[string] fields;
}

struct GlyphDef {
    @Proto(1) string symbol;
    @Proto(2) string title;
    @Proto(3) string label;
    @Proto(4) string contentPath;
    @Proto(5) string cssPath;
    @Proto(6) int defaultWidth;
    @Proto(7) int defaultHeight;
    @Proto(8) string modulePath;
}

struct GlyphDefResponse {
    @Proto(1) GlyphDef[] glyphs;
}

struct ExecuteJobRequest {
    @Proto(1) string jobId;
    @Proto(2) string handlerName;
    @Proto(3) ubyte[] payload;
    @Proto(4) long timeoutSecs;
}

struct JobLogEntry {
    @Proto(1) string timestamp;
    @Proto(2) string level;
    @Proto(3) string message;
    @Proto(4) string stage;
    @Proto(5) string metadata;
}

struct ExecuteJobResponse {
    @Proto(1) bool success;
    @Proto(2) string error;
    @Proto(3) ubyte[] result;
    @Proto(4) int progressCurrent;
    @Proto(5) int progressTotal;
    @Proto(6) double costActual;
    @Proto(7) JobLogEntry[] logEntries;
    @Proto(8) string pluginVersion;
}
