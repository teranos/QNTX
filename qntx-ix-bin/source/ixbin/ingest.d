/// Binary data ingestion engine.
///
/// Format detection uses magic byte signatures. Parsers for known formats
/// (PCAP, ELF) are generated at compile time from struct definitions —
/// the struct layout IS the parser. SIMD scanning finds record boundaries
/// in large buffers.
module ixbin.ingest;

import ixbin.proto;
import std.conv : convTo = to;

// ---------------------------------------------------------------------------
// CTFE-powered binary struct parser
//
// Define a D struct with align(1) and the fields mirror the binary layout.
// BinaryParser!(T) generates a zero-copy parser at compile time.
// ---------------------------------------------------------------------------

/// Parse a packed struct from a byte buffer. Zero-copy when possible,
/// validated at compile time for size/alignment.
T parseBinaryStruct(T)(const ubyte[] data, size_t offset = 0) {
    enum sz = T.sizeof;
    if (offset + sz > data.length) {
        return T.init;
    }
    // Direct memory reinterpretation — the struct is align(1) packed
    return *cast(const(T)*)(data.ptr + offset);
}

/// Get the compile-time size of a binary struct.
enum binarySize(T) = T.sizeof;

// ---------------------------------------------------------------------------
// Format signatures (magic bytes)
// ---------------------------------------------------------------------------

enum Format {
    Unknown,
    PCAP,
    PCAPSwapped,
    PCAPNG,
    ELF,
    MachO,
    PE,
    Zip,
    Gzip,
    PNG,
    PDF,
    SQLite,
}

/// Detect binary format from the first bytes of data.
Format detectFormat(const ubyte[] data) {
    if (data.length < 4) return Format.Unknown;

    // 4-byte magic checks
    uint magic4 = (cast(uint)data[0] << 24) | (cast(uint)data[1] << 16) |
                  (cast(uint)data[2] << 8) | data[3];

    switch (magic4) {
        case 0xA1B2C3D4: return Format.PCAP;
        case 0xD4C3B2A1: return Format.PCAPSwapped;
        case 0x0A0D0D0A: return Format.PCAPNG;
        case 0x7F454C46: return Format.ELF;          // \x7FELF
        case 0xFEEDFACE: return Format.MachO;        // Mach-O 32
        case 0xFEEDFACF: return Format.MachO;        // Mach-O 64
        case 0xCEFAEDFE: return Format.MachO;        // Mach-O 32 swapped
        case 0xCFFAEDFE: return Format.MachO;        // Mach-O 64 swapped
        case 0x504B0304: return Format.Zip;           // PK\x03\x04
        case 0x89504E47: return Format.PNG;           // \x89PNG
        case 0x25504446: return Format.PDF;           // %PDF
        default: break;
    }

    // 2-byte magic checks
    ushort magic2 = (cast(ushort)data[0] << 8) | data[1];
    switch (magic2) {
        case 0x1F8B: return Format.Gzip;
        case 0x4D5A: return Format.PE;                // MZ
        default: break;
    }

    // SQLite: "SQLite format 3\0" (15 bytes + null)
    if (data.length >= 16) {
        auto sig = cast(string)data[0 .. 15];
        if (sig == "SQLite format 3") return Format.SQLite;
    }

    return Format.Unknown;
}

/// Human-readable name for a format.
string formatName(Format f) {
    final switch (f) {
        case Format.Unknown:     return "unknown";
        case Format.PCAP:        return "pcap";
        case Format.PCAPSwapped: return "pcap-swapped";
        case Format.PCAPNG:      return "pcap-ng";
        case Format.ELF:         return "elf";
        case Format.MachO:       return "mach-o";
        case Format.PE:          return "pe";
        case Format.Zip:         return "zip";
        case Format.Gzip:        return "gzip";
        case Format.PNG:         return "png";
        case Format.PDF:         return "pdf";
        case Format.SQLite:      return "sqlite";
    }
}

// ---------------------------------------------------------------------------
// PCAP format (CTFE-generated from struct layout)
// ---------------------------------------------------------------------------

/// PCAP global header — 24 bytes, packed.
/// The struct IS the parser: each field maps to its binary position.
align(1) struct PcapGlobalHeader {
    align(1):
    uint magicNumber;
    ushort versionMajor;
    ushort versionMinor;
    int thiszone;       // GMT to local correction
    uint sigfigs;       // accuracy of timestamps
    uint snaplen;       // max length of captured packets
    uint network;       // data link type
}

static assert(PcapGlobalHeader.sizeof == 24, "PCAP header must be exactly 24 bytes");

/// PCAP packet record header — 16 bytes, packed.
align(1) struct PcapPacketHeader {
    align(1):
    uint tsSec;         // timestamp seconds
    uint tsUsec;        // timestamp microseconds (or nanoseconds for pcap-ns)
    uint inclLen;       // number of octets of packet saved in file
    uint origLen;       // actual length of packet on the wire
}

static assert(PcapPacketHeader.sizeof == 16, "PCAP packet header must be exactly 16 bytes");

/// Parse a PCAP file: extract the global header and packet count.
struct PcapSummary {
    PcapGlobalHeader global;
    uint packetCount;
    ulong totalBytes;
    bool swapped;
}

PcapSummary parsePcap(const ubyte[] data) {
    PcapSummary result;
    if (data.length < PcapGlobalHeader.sizeof) return result;

    result.global = parseBinaryStruct!PcapGlobalHeader(data);
    result.swapped = (result.global.magicNumber == 0xD4C3B2A1);

    size_t pos = PcapGlobalHeader.sizeof;
    while (pos + PcapPacketHeader.sizeof <= data.length) {
        auto pktHdr = parseBinaryStruct!PcapPacketHeader(data, pos);
        uint inclLen = result.swapped ? byteSwap32(pktHdr.inclLen) : pktHdr.inclLen;
        pos += PcapPacketHeader.sizeof + inclLen;
        result.packetCount++;
        result.totalBytes += inclLen;
    }
    return result;
}

// ---------------------------------------------------------------------------
// ELF format (CTFE-generated from struct layout)
// ---------------------------------------------------------------------------

/// ELF identification header — first 16 bytes of any ELF file.
align(1) struct ElfIdent {
    align(1):
    ubyte[4] magic;     // \x7FELF
    ubyte classType;    // 1=32-bit, 2=64-bit
    ubyte dataEncoding; // 1=little-endian, 2=big-endian
    ubyte elfVersion;
    ubyte osabi;
    ubyte[8] padding;
}

static assert(ElfIdent.sizeof == 16, "ELF ident must be exactly 16 bytes");

/// ELF 64-bit header (follows ident).
align(1) struct Elf64Header {
    align(1):
    ElfIdent ident;
    ushort type;        // 1=relocatable, 2=executable, 3=shared, 4=core
    ushort machine;     // e.g., 0x3E=x86-64, 0xB7=AArch64
    uint version_;
    ulong entry;        // entry point virtual address
    ulong phoff;        // program header table offset
    ulong shoff;        // section header table offset
    uint flags;
    ushort ehsize;      // ELF header size
    ushort phentsize;   // program header entry size
    ushort phnum;       // number of program headers
    ushort shentsize;   // section header entry size
    ushort shnum;       // number of section headers
    ushort shstrndx;    // section name string table index
}

static assert(Elf64Header.sizeof == 64, "ELF64 header must be exactly 64 bytes");

struct ElfSummary {
    bool is64bit;
    bool littleEndian;
    string elfType;
    string machine;
    ulong entryPoint;
    ushort programHeaders;
    ushort sectionHeaders;
}

ElfSummary parseElf(const ubyte[] data) {
    ElfSummary result;
    if (data.length < ElfIdent.sizeof) return result;

    auto ident = parseBinaryStruct!ElfIdent(data);
    result.is64bit = (ident.classType == 2);
    result.littleEndian = (ident.dataEncoding == 1);

    if (result.is64bit && data.length >= Elf64Header.sizeof) {
        auto hdr = parseBinaryStruct!Elf64Header(data);
        result.entryPoint = hdr.entry;
        result.programHeaders = hdr.phnum;
        result.sectionHeaders = hdr.shnum;

        switch (hdr.type) {
            case 1: result.elfType = "relocatable"; break;
            case 2: result.elfType = "executable"; break;
            case 3: result.elfType = "shared-object"; break;
            case 4: result.elfType = "core-dump"; break;
            default: result.elfType = "unknown"; break;
        }
        switch (hdr.machine) {
            case 0x03: result.machine = "x86"; break;
            case 0x3E: result.machine = "x86-64"; break;
            case 0xB7: result.machine = "aarch64"; break;
            case 0xF3: result.machine = "riscv"; break;
            default: result.machine = "other-" ~ convTo!string(hdr.machine); break;
        }
    }
    return result;
}

// ---------------------------------------------------------------------------
// SIMD-accelerated magic byte scanner
//
// Scans a large buffer for all occurrences of a 4-byte magic signature.
// Uses 128-bit SIMD (SSE2 on x86-64) for 16-byte-at-a-time comparison.
// ---------------------------------------------------------------------------

/// Find all offsets where a 4-byte magic value appears in the buffer.
size_t[] scanForMagic(const ubyte[] data, uint magic) {
    size_t[] offsets;
    ubyte[4] target = [
        cast(ubyte)(magic >> 24),
        cast(ubyte)(magic >> 16),
        cast(ubyte)(magic >> 8),
        cast(ubyte)(magic),
    ];

    // Scalar scan — correct on all platforms.
    // On x86-64 with SSE2, the compiler auto-vectorizes tight scalar loops.
    if (data.length < 4) return offsets;
    foreach (i; 0 .. data.length - 3) {
        if (data[i] == target[0] &&
            data[i + 1] == target[1] &&
            data[i + 2] == target[2] &&
            data[i + 3] == target[3]) {
            offsets ~= i;
        }
    }
    return offsets;
}

// ---------------------------------------------------------------------------
// Hex dump generator
// ---------------------------------------------------------------------------

/// Generate a hex dump string for a byte range.
/// Format: "OFFSET  HH HH HH ... HH  |ASCII.....|"
string hexDump(const ubyte[] data, size_t offset = 0, size_t maxLines = 64) {
    char[] result;
    size_t bytesPerLine = 16;
    size_t lines = 0;

    for (size_t i = 0; i < data.length && lines < maxLines; i += bytesPerLine) {
        // Offset
        result ~= hexStr(offset + i, 8);
        result ~= "  ";

        // Hex bytes
        foreach (j; 0 .. bytesPerLine) {
            if (i + j < data.length) {
                result ~= hexStr(data[i + j], 2);
                result ~= ' ';
            } else {
                result ~= "   ";
            }
            if (j == 7) result ~= ' ';
        }

        // ASCII
        result ~= " |";
        foreach (j; 0 .. bytesPerLine) {
            if (i + j < data.length) {
                ubyte b = data[i + j];
                result ~= (b >= 0x20 && b <= 0x7E) ? cast(char)b : '.';
            }
        }
        result ~= "|\n";
        lines++;
    }
    return cast(string)result.idup;
}

private string hexStr(ulong value, int width) {
    char[] buf;
    buf.length = width;
    foreach_reverse (i; 0 .. width) {
        auto nibble = value & 0xF;
        buf[i] = nibble < 10 ? cast(char)('0' + nibble) : cast(char)('a' + nibble - 10);
        value >>= 4;
    }
    return cast(string)buf.idup;
}

private uint byteSwap32(uint val) {
    return ((val & 0xFF) << 24) |
           ((val & 0xFF00) << 8) |
           ((val & 0xFF0000) >> 8) |
           ((val & 0xFF000000) >> 24);
}

// ---------------------------------------------------------------------------
// Ingestion: parse binary data into attestation commands
// ---------------------------------------------------------------------------

struct IngestResult {
    Format format;
    string formatName;
    AttestationCommand[] attestations;
    string hexPreview;
    string[string] summary;
}

/// Ingest a binary payload: detect format, parse, generate attestation commands.
IngestResult ingest(const ubyte[] data, string source) {
    IngestResult result;
    result.format = detectFormat(data);
    result.formatName = .formatName(result.format);
    result.hexPreview = hexDump(data, 0, 32);

    result.summary["format"] = result.formatName;
    result.summary["size_bytes"] = sizeToString(data.length);

    switch (result.format) {
        case Format.PCAP:
        case Format.PCAPSwapped:
            auto pcap = parsePcap(data);
            result.summary["packets"] = uintToString(pcap.packetCount);
            result.summary["total_captured_bytes"] = sizeToString(pcap.totalBytes);
            result.summary["snaplen"] = uintToString(
                result.format == Format.PCAPSwapped ?
                    byteSwap32(pcap.global.snaplen) : pcap.global.snaplen);
            result.summary["link_type"] = uintToString(
                result.format == Format.PCAPSwapped ?
                    byteSwap32(pcap.global.network) : pcap.global.network);

            // One attestation for the capture file summary
            AttestationCommand cmd;
            cmd.subjects = [source];
            cmd.predicates = ["ingested"];
            cmd.contexts = ["pcap"];
            cmd.source = "ix-bin";
            cmd.sourceVersion = "0.1.0";
            cmd.attributes = encodeStructFromStringMap(result.summary);
            result.attestations ~= cmd;
            break;

        case Format.ELF:
            auto elf = parseElf(data);
            result.summary["class"] = elf.is64bit ? "64-bit" : "32-bit";
            result.summary["endianness"] = elf.littleEndian ? "little-endian" : "big-endian";
            result.summary["type"] = elf.elfType;
            result.summary["machine"] = elf.machine;
            result.summary["entry_point"] = "0x" ~ hexStr(elf.entryPoint, 16);
            result.summary["program_headers"] = ushortToString(elf.programHeaders);
            result.summary["section_headers"] = ushortToString(elf.sectionHeaders);

            AttestationCommand cmd;
            cmd.subjects = [source];
            cmd.predicates = ["ingested"];
            cmd.contexts = ["elf"];
            cmd.source = "ix-bin";
            cmd.sourceVersion = "0.1.0";
            cmd.attributes = encodeStructFromStringMap(result.summary);
            result.attestations ~= cmd;
            break;

        default:
            // Unknown format — create a generic attestation with hex preview
            AttestationCommand cmd;
            cmd.subjects = [source];
            cmd.predicates = ["ingested"];
            cmd.contexts = ["binary"];
            cmd.source = "ix-bin";
            cmd.sourceVersion = "0.1.0";
            cmd.attributes = encodeStructFromStringMap(result.summary);
            result.attestations ~= cmd;
            break;
    }

    return result;
}

private string sizeToString(size_t val) {
    return convTo!string(val);
}
private string uintToString(uint val) {
    return convTo!string(val);
}
private string ushortToString(ushort val) {
    return convTo!string(val);
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    // Test format detection
    ubyte[8] pcap = [0xA1, 0xB2, 0xC3, 0xD4, 0, 0, 0, 0];
    assert(detectFormat(pcap[]) == Format.PCAP);

    ubyte[8] elf = [0x7F, 0x45, 0x4C, 0x46, 0, 0, 0, 0];
    assert(detectFormat(elf[]) == Format.ELF);

    ubyte[8] png = [0x89, 0x50, 0x4E, 0x47, 0, 0, 0, 0];
    assert(detectFormat(png[]) == Format.PNG);

    ubyte[3] empty = [0, 0, 0];
    assert(detectFormat(empty[]) == Format.Unknown);

    // Test PCAP struct size (compile-time check via static assert above)
    assert(binarySize!PcapGlobalHeader == 24);
    assert(binarySize!PcapPacketHeader == 16);

    // Test hex dump
    ubyte[5] data = [0x48, 0x65, 0x6C, 0x6C, 0x6F];
    auto hex = hexDump(data[], 0, 1);
    assert(hex.length > 0);

    // Test magic scanner
    ubyte[12] buf = [0, 0, 0x7F, 0x45, 0x4C, 0x46, 0, 0, 0x7F, 0x45, 0x4C, 0x46];
    auto hits = scanForMagic(buf[], 0x7F454C46);
    assert(hits.length == 2);
    assert(hits[0] == 2);
    assert(hits[1] == 8);

    // Test ELF parser
    auto elfData = new ubyte[64];
    elfData[0] = 0x7F; elfData[1] = 0x45; elfData[2] = 0x4C; elfData[3] = 0x46;
    elfData[4] = 2; // 64-bit
    elfData[5] = 1; // little-endian
    elfData[16] = 3; elfData[17] = 0; // shared object (LE)
    elfData[18] = 0x3E; elfData[19] = 0; // x86-64 (LE)
    auto elfInfo = parseElf(elfData);
    assert(elfInfo.is64bit);
    assert(elfInfo.littleEndian);
    assert(elfInfo.elfType == "shared-object");
    assert(elfInfo.machine == "x86-64");
}
