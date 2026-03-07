/// HPACK header compression codec (RFC 7541).
///
/// The Huffman decode trie is built entirely at compile time using D's CTFE.
/// The static table, integer codec, and string codec are all pure D.
module ixbin.hpack;

// ---------------------------------------------------------------------------
// HPACK static table (RFC 7541, Appendix A)
// ---------------------------------------------------------------------------

struct HeaderField {
    string name;
    string value;
}

// 61-entry static table (index 1-61; index 0 is unused)
private immutable HeaderField[] staticTable = [
    HeaderField("", ""),                         // index 0 (unused)
    HeaderField(":authority", ""),                // 1
    HeaderField(":method", "GET"),               // 2
    HeaderField(":method", "POST"),              // 3
    HeaderField(":path", "/"),                   // 4
    HeaderField(":path", "/index.html"),         // 5
    HeaderField(":scheme", "http"),              // 6
    HeaderField(":scheme", "https"),             // 7
    HeaderField(":status", "200"),               // 8
    HeaderField(":status", "204"),               // 9
    HeaderField(":status", "206"),               // 10
    HeaderField(":status", "304"),               // 11
    HeaderField(":status", "400"),               // 12
    HeaderField(":status", "404"),               // 13
    HeaderField(":status", "500"),               // 14
    HeaderField("accept-charset", ""),           // 15
    HeaderField("accept-encoding", "gzip, deflate"), // 16
    HeaderField("accept-language", ""),          // 17
    HeaderField("accept-ranges", ""),            // 18
    HeaderField("accept", ""),                   // 19
    HeaderField("access-control-allow-origin", ""), // 20
    HeaderField("age", ""),                      // 21
    HeaderField("allow", ""),                    // 22
    HeaderField("authorization", ""),            // 23
    HeaderField("cache-control", ""),            // 24
    HeaderField("content-disposition", ""),      // 25
    HeaderField("content-encoding", ""),         // 26
    HeaderField("content-language", ""),         // 27
    HeaderField("content-length", ""),           // 28
    HeaderField("content-location", ""),         // 29
    HeaderField("content-range", ""),            // 30
    HeaderField("content-type", ""),             // 31
    HeaderField("cookie", ""),                   // 32
    HeaderField("date", ""),                     // 33
    HeaderField("etag", ""),                     // 34
    HeaderField("expect", ""),                   // 35
    HeaderField("expires", ""),                  // 36
    HeaderField("from", ""),                     // 37
    HeaderField("host", ""),                     // 38
    HeaderField("if-match", ""),                 // 39
    HeaderField("if-modified-since", ""),        // 40
    HeaderField("if-none-match", ""),            // 41
    HeaderField("if-range", ""),                 // 42
    HeaderField("if-unmodified-since", ""),      // 43
    HeaderField("last-modified", ""),            // 44
    HeaderField("link", ""),                     // 45
    HeaderField("location", ""),                 // 46
    HeaderField("max-forwards", ""),             // 47
    HeaderField("proxy-authenticate", ""),       // 48
    HeaderField("proxy-authorization", ""),      // 49
    HeaderField("range", ""),                    // 50
    HeaderField("referer", ""),                  // 51
    HeaderField("refresh", ""),                  // 52
    HeaderField("retry-after", ""),              // 53
    HeaderField("server", ""),                   // 54
    HeaderField("set-cookie", ""),               // 55
    HeaderField("strict-transport-security", ""), // 56
    HeaderField("transfer-encoding", ""),        // 57
    HeaderField("user-agent", ""),               // 58
    HeaderField("vary", ""),                     // 59
    HeaderField("via", ""),                      // 60
    HeaderField("www-authenticate", ""),         // 61
];

// ---------------------------------------------------------------------------
// RFC 7541 Appendix B — HPACK Huffman code table
// ---------------------------------------------------------------------------

private struct HuffSym {
    uint code;
    ubyte bits;
}

// All 257 entries (symbols 0-255 + EOS=256)
private immutable HuffSym[257] huffSyms = [
    HuffSym(0x1ff8, 13),       HuffSym(0x7fffd8, 23),     HuffSym(0xfffffe2, 28),    HuffSym(0xfffffe3, 28),   // 0-3
    HuffSym(0xfffffe4, 28),    HuffSym(0xfffffe5, 28),    HuffSym(0xfffffe6, 28),    HuffSym(0xfffffe7, 28),   // 4-7
    HuffSym(0xfffffe8, 28),    HuffSym(0xffffea, 24),     HuffSym(0x3ffffffc, 30),   HuffSym(0xfffffe9, 28),   // 8-11
    HuffSym(0xfffffea, 28),    HuffSym(0x3ffffffd, 30),   HuffSym(0xfffffeb, 28),    HuffSym(0xfffffec, 28),   // 12-15
    HuffSym(0xfffffed, 28),    HuffSym(0xfffffee, 28),    HuffSym(0xfffffef, 28),    HuffSym(0xffffff0, 28),   // 16-19
    HuffSym(0xffffff1, 28),    HuffSym(0xffffff2, 28),    HuffSym(0x3ffffffe, 30),   HuffSym(0xffffff3, 28),   // 20-23
    HuffSym(0xffffff4, 28),    HuffSym(0xffffff5, 28),    HuffSym(0xffffff6, 28),    HuffSym(0xffffff7, 28),   // 24-27
    HuffSym(0xffffff8, 28),    HuffSym(0xffffff9, 28),    HuffSym(0xffffffa, 28),    HuffSym(0xffffffb, 28),   // 28-31
    HuffSym(0x14, 6),          HuffSym(0x3f8, 10),        HuffSym(0x3f9, 10),        HuffSym(0xffa, 12),       // 32-35 ' !"#'
    HuffSym(0x1ff9, 13),       HuffSym(0x15, 6),          HuffSym(0xf8, 8),          HuffSym(0x7fa, 11),       // 36-39 '$%&\''
    HuffSym(0x3fa, 10),        HuffSym(0x3fb, 10),        HuffSym(0xf9, 8),          HuffSym(0x7fb, 11),       // 40-43 '()*+'
    HuffSym(0xfa, 8),          HuffSym(0x16, 6),          HuffSym(0x17, 6),          HuffSym(0x18, 6),         // 44-47 ',-./''
    HuffSym(0x0, 5),           HuffSym(0x1, 5),           HuffSym(0x2, 5),           HuffSym(0x19, 6),         // 48-51 '0123'
    HuffSym(0x1a, 6),          HuffSym(0x1b, 6),          HuffSym(0x1c, 6),          HuffSym(0x1d, 6),         // 52-55 '4567'
    HuffSym(0x1e, 6),          HuffSym(0x1f, 6),          HuffSym(0x5c, 7),          HuffSym(0xfb, 8),         // 56-59 '89:;'
    HuffSym(0x7ffc, 15),       HuffSym(0x20, 6),          HuffSym(0xffb, 12),        HuffSym(0x3fc, 10),       // 60-63 '<=>?'
    HuffSym(0x1ffa, 13),       HuffSym(0x21, 6),          HuffSym(0x5d, 7),          HuffSym(0x5e, 7),         // 64-67 '@ABC'
    HuffSym(0x5f, 7),          HuffSym(0x60, 7),          HuffSym(0x61, 7),          HuffSym(0x62, 7),         // 68-71 'DEFG'
    HuffSym(0x63, 7),          HuffSym(0x64, 7),          HuffSym(0x65, 7),          HuffSym(0x66, 7),         // 72-75 'HIJK'
    HuffSym(0x67, 7),          HuffSym(0x68, 7),          HuffSym(0x69, 7),          HuffSym(0x6a, 7),         // 76-79 'LMNO'
    HuffSym(0x6b, 7),          HuffSym(0x6c, 7),          HuffSym(0x6d, 7),          HuffSym(0x6e, 7),         // 80-83 'PQRS'
    HuffSym(0x6f, 7),          HuffSym(0x70, 7),          HuffSym(0x71, 7),          HuffSym(0x72, 7),         // 84-87 'TUVW'
    HuffSym(0xfc, 8),          HuffSym(0x73, 7),          HuffSym(0xfd, 8),          HuffSym(0x1ffb, 13),      // 88-91 'XYZ['
    HuffSym(0x7fff0, 19),      HuffSym(0x1ffc, 13),       HuffSym(0x3ffc, 14),       HuffSym(0x22, 6),         // 92-95 '\\]^_'
    HuffSym(0x7ffd, 15),       HuffSym(0x3, 5),           HuffSym(0x23, 6),          HuffSym(0x4, 5),          // 96-99 '`abc'
    HuffSym(0x24, 6),          HuffSym(0x5, 5),           HuffSym(0x25, 6),          HuffSym(0x26, 6),         // 100-103 'defg'
    HuffSym(0x27, 6),          HuffSym(0x6, 5),           HuffSym(0x74, 7),          HuffSym(0x75, 7),         // 104-107 'hijk'
    HuffSym(0x28, 6),          HuffSym(0x29, 6),          HuffSym(0x2a, 6),          HuffSym(0x7, 5),          // 108-111 'lmno'
    HuffSym(0x2b, 6),          HuffSym(0x76, 7),          HuffSym(0x2c, 6),          HuffSym(0x8, 5),          // 112-115 'pqrs'
    HuffSym(0x9, 5),           HuffSym(0x2d, 6),          HuffSym(0x77, 7),          HuffSym(0x78, 7),         // 116-119 'tuvw'
    HuffSym(0x79, 7),          HuffSym(0x7a, 7),          HuffSym(0x7b, 7),          HuffSym(0x7fffe, 19),     // 120-123 'xyz{'
    HuffSym(0x7fc, 11),        HuffSym(0x3ffd, 14),       HuffSym(0x1ffd, 13),       HuffSym(0xffffffc, 28),   // 124-127
    HuffSym(0xfffe6, 20),      HuffSym(0x3fffd2, 22),     HuffSym(0xfffe7, 20),      HuffSym(0xfffe8, 20),     // 128-131
    HuffSym(0x3fffd3, 22),     HuffSym(0x3fffd4, 22),     HuffSym(0x3fffd5, 22),     HuffSym(0x7fffd9, 23),    // 132-135
    HuffSym(0x3fffd6, 22),     HuffSym(0x7fffda, 23),     HuffSym(0x7fffdb, 23),     HuffSym(0x7fffdc, 23),    // 136-139
    HuffSym(0x7fffdd, 23),     HuffSym(0x7fffde, 23),     HuffSym(0xffffeb, 24),     HuffSym(0x7fffdf, 23),    // 140-143
    HuffSym(0xffffec, 24),     HuffSym(0xffffed, 24),     HuffSym(0x3fffd7, 22),     HuffSym(0x7fffe0, 23),    // 144-147
    HuffSym(0xffffee, 24),     HuffSym(0x7fffe1, 23),     HuffSym(0x7fffe2, 23),     HuffSym(0x7fffe3, 23),    // 148-151
    HuffSym(0x7fffe4, 23),     HuffSym(0x1fffdc, 21),     HuffSym(0x3fffd8, 22),     HuffSym(0x7fffe5, 23),    // 152-155
    HuffSym(0x3fffd9, 22),     HuffSym(0x7fffe6, 23),     HuffSym(0x7fffe7, 23),     HuffSym(0xffffef, 24),    // 156-159
    HuffSym(0x3fffda, 22),     HuffSym(0x1fffdd, 21),     HuffSym(0xfffe9, 20),      HuffSym(0x3fffdb, 22),    // 160-163
    HuffSym(0x3fffdc, 22),     HuffSym(0x7fffe8, 23),     HuffSym(0x7fffe9, 23),     HuffSym(0x1fffde, 21),    // 164-167
    HuffSym(0x7fffea, 23),     HuffSym(0x3fffdd, 22),     HuffSym(0x3fffde, 22),     HuffSym(0xfffff0, 24),    // 168-171
    HuffSym(0x1fffdf, 21),     HuffSym(0x3fffdf, 22),     HuffSym(0x7fffeb, 23),     HuffSym(0x7fffec, 23),    // 172-175
    HuffSym(0x1fffe0, 21),     HuffSym(0x1fffe1, 21),     HuffSym(0x3fffe0, 22),     HuffSym(0x1fffe2, 21),    // 176-179
    HuffSym(0x7fffed, 23),     HuffSym(0x3fffe1, 22),     HuffSym(0x7fffee, 23),     HuffSym(0x7fffef, 23),    // 180-183
    HuffSym(0xfffea, 20),      HuffSym(0x3fffe2, 22),     HuffSym(0x3fffe3, 22),     HuffSym(0x3fffe4, 22),    // 184-187
    HuffSym(0x7ffff0, 23),     HuffSym(0x3fffe5, 22),     HuffSym(0x3fffe6, 22),     HuffSym(0x7ffff1, 23),    // 188-191
    HuffSym(0x3ffffe0, 26),    HuffSym(0x3ffffe1, 26),    HuffSym(0xfffeb, 20),      HuffSym(0x7fff1, 19),     // 192-195
    HuffSym(0x3fffe7, 22),     HuffSym(0x7ffff2, 23),     HuffSym(0x3fffe8, 22),     HuffSym(0x1ffffec, 25),   // 196-199
    HuffSym(0x3ffffe2, 26),    HuffSym(0x3ffffe3, 26),    HuffSym(0x3ffffe4, 26),    HuffSym(0x7ffffde, 27),   // 200-203
    HuffSym(0x7ffffdf, 27),    HuffSym(0x3ffffe5, 26),    HuffSym(0xfffff1, 24),     HuffSym(0x1ffffed, 25),   // 204-207
    HuffSym(0x7fff2, 19),      HuffSym(0x1fffe3, 21),     HuffSym(0x3ffffe6, 26),    HuffSym(0x7ffffe0, 27),   // 208-211
    HuffSym(0x7ffffe1, 27),    HuffSym(0x3ffffe7, 26),    HuffSym(0x7ffffe2, 27),    HuffSym(0xfffff2, 24),    // 212-215
    HuffSym(0x1fffe4, 21),     HuffSym(0x1fffe5, 21),     HuffSym(0x3ffffe8, 26),    HuffSym(0x3ffffe9, 26),   // 216-219
    HuffSym(0xffffffd, 28),    HuffSym(0x7ffffe3, 27),    HuffSym(0x7ffffe4, 27),    HuffSym(0x7ffffe5, 27),   // 220-223
    HuffSym(0xfffec, 20),      HuffSym(0xfffff3, 24),     HuffSym(0xfffed, 20),      HuffSym(0x1fffe6, 21),    // 224-227
    HuffSym(0x3fffe9, 22),     HuffSym(0x1fffe7, 21),     HuffSym(0x1fffe8, 21),     HuffSym(0x7ffff3, 23),    // 228-231
    HuffSym(0x3fffea, 22),     HuffSym(0x3fffeb, 22),     HuffSym(0x1ffffee, 25),    HuffSym(0x1ffffef, 25),   // 232-235
    HuffSym(0xfffff4, 24),     HuffSym(0xfffff5, 24),     HuffSym(0x3ffffea, 26),    HuffSym(0x7ffff4, 23),    // 236-239
    HuffSym(0x3ffffeb, 26),    HuffSym(0x7ffffe6, 27),    HuffSym(0x3ffffec, 26),    HuffSym(0x3ffffed, 26),   // 240-243
    HuffSym(0x7ffffe7, 27),    HuffSym(0x7ffffe8, 27),    HuffSym(0x7ffffe9, 27),    HuffSym(0x7ffffea, 27),   // 244-247
    HuffSym(0x7ffffeb, 27),    HuffSym(0xffffffe, 28),    HuffSym(0x7ffffec, 27),    HuffSym(0x7ffffed, 27),   // 248-251
    HuffSym(0x7ffffee, 27),    HuffSym(0x7ffffef, 27),    HuffSym(0x7fffff0, 27),    HuffSym(0x3ffffee, 26),   // 252-255
    HuffSym(0x3fffffff, 30),   // 256 = EOS
];

// ---------------------------------------------------------------------------
// CTFE-generated Huffman decode trie
// ---------------------------------------------------------------------------

private struct TrieNode {
    short left  = -1; // child for bit 0 (-1 = none)
    short right = -1; // child for bit 1 (-1 = none)
    short sym   = -1; // decoded symbol (-1 = internal node, 0-256 = symbol)
}

/// Build the Huffman decode trie at compile time.
private TrieNode[] buildHuffTrie() {
    TrieNode[] nodes;
    nodes.length = 1; // root = index 0
    nodes[0] = TrieNode(-1, -1, -1);

    foreach (sym; 0 .. 257) {
        uint code = huffSyms[sym].code;
        ubyte bits = huffSyms[sym].bits;
        int cur = 0;

        foreach (b; 0 .. bits) {
            int bit = (code >> (bits - 1 - b)) & 1;
            if (bit == 0) {
                if (nodes[cur].left == -1) {
                    nodes ~= TrieNode(-1, -1, -1);
                    nodes[cur].left = cast(short)(cast(int)nodes.length - 1);
                }
                cur = nodes[cur].left;
            } else {
                if (nodes[cur].right == -1) {
                    nodes ~= TrieNode(-1, -1, -1);
                    nodes[cur].right = cast(short)(cast(int)nodes.length - 1);
                }
                cur = nodes[cur].right;
            }
        }
        nodes[cur].sym = cast(short)sym;
    }
    return nodes;
}

// The trie is built at compile time and embedded as static immutable data.
private immutable TrieNode[] huffTrie = buildHuffTrie();

/// Decode a Huffman-encoded byte string.
string huffmanDecode(const ubyte[] data, size_t encodedLen) {
    char[] result;
    int node = 0; // start at root

    foreach (i; 0 .. encodedLen) {
        if (i >= data.length) break;
        ubyte b = data[i];
        foreach (bit; 0 .. 8) {
            int direction = (b >> (7 - bit)) & 1;
            if (direction == 0) {
                node = huffTrie[node].left;
            } else {
                node = huffTrie[node].right;
            }
            if (node < 0) return cast(string)result.idup; // invalid
            if (huffTrie[node].sym >= 0) {
                int sym = huffTrie[node].sym;
                if (sym == 256) return cast(string)result.idup; // EOS
                result ~= cast(char)sym;
                node = 0; // back to root
            }
        }
    }
    return cast(string)result.idup;
}

// ---------------------------------------------------------------------------
// HPACK integer codec (RFC 7541, Section 5.1)
// ---------------------------------------------------------------------------

/// Decode an HPACK integer with N-bit prefix.
ulong decodeInt(const ubyte[] data, ref size_t pos, int prefixBits) {
    if (pos >= data.length) return 0;
    ubyte mask = cast(ubyte)((1 << prefixBits) - 1);
    ulong value = data[pos] & mask;
    pos++;
    if (value < mask) return value;
    // Multi-byte integer
    uint shift = 0;
    while (pos < data.length) {
        ubyte b = data[pos++];
        value += cast(ulong)(b & 0x7F) << shift;
        shift += 7;
        if ((b & 0x80) == 0) break;
    }
    return value;
}

/// Encode an HPACK integer with N-bit prefix.
ubyte[] encodeInt(ulong value, int prefixBits, ubyte flagBits) {
    ubyte mask = cast(ubyte)((1 << prefixBits) - 1);
    ubyte[] result;
    if (value < mask) {
        result ~= cast(ubyte)(flagBits | (value & 0xFF));
    } else {
        result ~= cast(ubyte)(flagBits | mask);
        value -= mask;
        while (value >= 128) {
            result ~= cast(ubyte)((value & 0x7F) | 0x80);
            value >>= 7;
        }
        result ~= cast(ubyte)value;
    }
    return result;
}

// ---------------------------------------------------------------------------
// HPACK string codec (RFC 7541, Section 5.2)
// ---------------------------------------------------------------------------

/// Decode an HPACK string (possibly Huffman-encoded).
string decodeString(const ubyte[] data, ref size_t pos) {
    if (pos >= data.length) return "";
    bool isHuffman = (data[pos] & 0x80) != 0;
    auto len = cast(size_t)decodeInt(data, pos, 7);
    if (pos + len > data.length) return "";
    if (isHuffman) {
        auto result = huffmanDecode(data[pos .. pos + len], len);
        pos += len;
        return result;
    } else {
        auto result = cast(string)data[pos .. pos + len].idup;
        pos += len;
        return result;
    }
}

/// Encode an HPACK string without Huffman (plain).
ubyte[] encodeString(string value) {
    ubyte[] result;
    // No Huffman: H bit = 0, prefix = 7 bits
    result ~= encodeInt(value.length, 7, 0x00);
    result ~= cast(const(ubyte)[])value;
    return result;
}

// ---------------------------------------------------------------------------
// HPACK header block decoder
// ---------------------------------------------------------------------------

/// Dynamic table for incremental indexing.
struct DynamicTable {
    HeaderField[] entries;
    size_t maxSize = 4096;
    size_t currentSize = 0;

    void add(string name, string value) {
        auto entrySize = name.length + value.length + 32; // per RFC 7541
        // Evict entries to make room
        while (currentSize + entrySize > maxSize && entries.length > 0) {
            auto last = entries[$ - 1];
            currentSize -= last.name.length + last.value.length + 32;
            entries = entries[0 .. $ - 1];
        }
        if (entrySize <= maxSize) {
            // Prepend (newest first)
            entries = [HeaderField(name, value)] ~ entries;
            currentSize += entrySize;
        }
    }

    HeaderField get(size_t index) const {
        // Dynamic table indices start after static table (62+)
        auto dynamicIndex = index - 62;
        if (dynamicIndex < entries.length) {
            return entries[dynamicIndex];
        }
        return HeaderField("", "");
    }
}

/// Lookup a header field by index (static or dynamic table).
HeaderField lookupIndex(size_t index, ref const DynamicTable dyn) {
    if (index >= 1 && index <= 61) {
        return staticTable[index];
    }
    return dyn.get(index);
}

/// Decode an HPACK header block into a list of name-value pairs.
HeaderField[] decodeHeaders(const ubyte[] data, ref DynamicTable dyn) {
    HeaderField[] headers;
    size_t pos = 0;

    while (pos < data.length) {
        ubyte b = data[pos];

        if ((b & 0x80) != 0) {
            // Indexed header field (Section 6.1): 1xxxxxxx
            auto index = cast(size_t)decodeInt(data, pos, 7);
            headers ~= lookupIndex(index, dyn);
        } else if ((b & 0xC0) == 0x40) {
            // Literal with incremental indexing (Section 6.2.1): 01xxxxxx
            auto index = cast(size_t)decodeInt(data, pos, 6);
            string name;
            if (index > 0) {
                name = lookupIndex(index, dyn).name;
            } else {
                name = decodeString(data, pos);
            }
            string value = decodeString(data, pos);
            dyn.add(name, value);
            headers ~= HeaderField(name, value);
        } else if ((b & 0xF0) == 0x00) {
            // Literal without indexing (Section 6.2.2): 0000xxxx
            auto index = cast(size_t)decodeInt(data, pos, 4);
            string name;
            if (index > 0) {
                name = lookupIndex(index, dyn).name;
            } else {
                name = decodeString(data, pos);
            }
            string value = decodeString(data, pos);
            headers ~= HeaderField(name, value);
        } else if ((b & 0xF0) == 0x10) {
            // Literal never indexed (Section 6.2.3): 0001xxxx
            auto index = cast(size_t)decodeInt(data, pos, 4);
            string name;
            if (index > 0) {
                name = lookupIndex(index, dyn).name;
            } else {
                name = decodeString(data, pos);
            }
            string value = decodeString(data, pos);
            headers ~= HeaderField(name, value);
        } else if ((b & 0xE0) == 0x20) {
            // Dynamic table size update (Section 6.3): 001xxxxx
            auto newSize = cast(size_t)decodeInt(data, pos, 5);
            dyn.maxSize = newSize;
            // Evict excess entries
            while (dyn.currentSize > dyn.maxSize && dyn.entries.length > 0) {
                auto last = dyn.entries[$ - 1];
                dyn.currentSize -= last.name.length + last.value.length + 32;
                dyn.entries = dyn.entries[0 .. $ - 1];
            }
        } else {
            // Unknown, skip byte
            pos++;
        }
    }
    return headers;
}

// ---------------------------------------------------------------------------
// HPACK header block encoder (simple: literal without indexing, no Huffman)
// ---------------------------------------------------------------------------

/// Encode a single header as literal without indexing.
ubyte[] encodeLiteralHeader(string name, string value) {
    ubyte[] result;
    // 0000xxxx with index 0 (new name)
    result ~= 0x00;
    result ~= encodeString(name);
    result ~= encodeString(value);
    return result;
}

/// Encode a header using indexed name (static table) + literal value.
ubyte[] encodeIndexedNameHeader(size_t nameIndex, string value) {
    ubyte[] result;
    // 0000xxxx with index N
    result ~= encodeInt(nameIndex, 4, 0x00);
    result ~= encodeString(value);
    return result;
}

/// Encode a fully indexed header reference.
ubyte[] encodeIndexedHeader(size_t index) {
    return encodeInt(index, 7, 0x80);
}

/// Encode a set of response headers for gRPC.
ubyte[] encodeResponseHeaders() {
    ubyte[] result;
    // :status 200 → static table index 8
    result ~= encodeIndexedHeader(8);
    // content-type: application/grpc → name index 31, literal value
    result ~= encodeIndexedNameHeader(31, "application/grpc");
    return result;
}

/// Encode gRPC trailers (grpc-status: 0).
ubyte[] encodeGrpcTrailers() {
    ubyte[] result;
    result ~= encodeLiteralHeader("grpc-status", "0");
    return result;
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    // Test HPACK integer codec round-trip
    foreach (val; [0UL, 1, 30, 31, 127, 128, 1000, 65535]) {
        foreach (prefix; [4, 5, 6, 7]) {
            auto encoded = encodeInt(val, prefix, 0);
            size_t pos = 0;
            auto decoded = decodeInt(encoded, pos, prefix);
            assert(decoded == val, "HPACK int round-trip failed");
        }
    }

    // Test HPACK string codec round-trip (non-Huffman)
    auto encoded = encodeString("hello");
    size_t pos = 0;
    auto decoded = decodeString(encoded, pos);
    assert(decoded == "hello", "HPACK string round-trip failed");

    // Test indexed header lookup
    DynamicTable dyn;
    auto field = lookupIndex(3, dyn); // :method POST
    assert(field.name == ":method");
    assert(field.value == "POST");

    // Test Huffman decode (the trie is built at compile time)
    assert(huffTrie.length > 0, "Huffman trie should have nodes");
}
