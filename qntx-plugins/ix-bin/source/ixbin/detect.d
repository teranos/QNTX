/// Format detection via magic byte signatures.
///
/// Single source of truth for binary format identification.
/// Also provides shared binary parsing utilities used by format parsers.
module ixbin.detect;

import std.conv : convTo = to;

// ---------------------------------------------------------------------------
// Format enum + detection
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
    BPlist,
    Shebang,
    USDC,
    BOM,
}

/// Detect binary format from the first bytes of data.
Format detectFormat(const ubyte[] data) {
    if (data.length < 4) return Format.Unknown;

    uint magic4 = (cast(uint)data[0] << 24) | (cast(uint)data[1] << 16) |
                  (cast(uint)data[2] << 8) | data[3];

    switch (magic4) {
        case 0xA1B2C3D4: return Format.PCAP;
        case 0xD4C3B2A1: return Format.PCAPSwapped;
        case 0x0A0D0D0A: return Format.PCAPNG;
        case 0x7F454C46: return Format.ELF;
        case 0xFEEDFACE: return Format.MachO;
        case 0xFEEDFACF: return Format.MachO;
        case 0xCEFAEDFE: return Format.MachO;
        case 0xCFFAEDFE: return Format.MachO;
        case 0xCAFEBABE:
            // CAFEBABE is shared by Mach-O fat binaries and Java class files.
            // Mach-O fat: BE uint32 at offset 4 is nfat_arch (realistically 1-4).
            // Java class: BE uint32 at offset 4 is (minor_version << 16 | major_version),
            // always >= 45 (0x2D) since JDK 1.1. Safe threshold: nfat_arch > 30 is Java.
            if (data.length >= 8) {
                uint nfatOrVersion = readBE32(data, 4);
                if (nfatOrVersion > 30) return Format.Unknown;
            }
            return Format.MachO;
        case 0xBEBAFECA: return Format.MachO;
        case 0x504B0304: return Format.Zip;
        case 0x89504E47: return Format.PNG;
        case 0x25504446: return Format.PDF;
        default: break;
    }

    ushort magic2 = (cast(ushort)data[0] << 8) | data[1];
    switch (magic2) {
        case 0x1F8B: return Format.Gzip;
        case 0x4D5A: return Format.PE;
        default: break;
    }

    if (data[0] == '#' && data[1] == '!') return Format.Shebang;

    if (data.length >= 15) {
        if (cast(string)data[0 .. 15] == "SQLite format 3") return Format.SQLite;
    }
    if (data.length >= 8) {
        auto s8 = cast(string)data[0 .. 8];
        if (s8 == "bplist00" || s8 == "bplist01") return Format.BPlist;
        if (s8 == "PXR-USDC") return Format.USDC;
        if (s8 == "BOMStore") return Format.BOM;
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
        case Format.BPlist:      return "bplist";
        case Format.Shebang:     return "script";
        case Format.USDC:        return "usdc";
        case Format.BOM:         return "bom";
    }
}

// ---------------------------------------------------------------------------
// Binary parsing utilities (shared by format parsers)
// ---------------------------------------------------------------------------

/// Parse a packed struct from a byte buffer. Zero-copy via memory reinterpretation.
T parseBinaryStruct(T)(const ubyte[] data, size_t offset = 0) {
    enum sz = T.sizeof;
    if (offset + sz > data.length) return T.init;
    return *cast(const(T)*)(data.ptr + offset);
}

/// Compile-time size of a binary struct.
enum binarySize(T) = T.sizeof;

string hexStr(ulong value, int width) {
    char[] buf;
    buf.length = width;
    foreach_reverse (i; 0 .. width) {
        auto nibble = value & 0xF;
        buf[i] = nibble < 10 ? cast(char)('0' + nibble) : cast(char)('a' + nibble - 10);
        value >>= 4;
    }
    return cast(string)buf.idup;
}

uint byteSwap32(uint val) {
    return ((val & 0xFF) << 24) |
           ((val & 0xFF00) << 8) |
           ((val & 0xFF0000) >> 8) |
           ((val & 0xFF000000) >> 24);
}

uint readBE32(const ubyte[] data, size_t offset) {
    if (offset + 4 > data.length) return 0;
    return (cast(uint)data[offset] << 24) | (cast(uint)data[offset+1] << 16) |
           (cast(uint)data[offset+2] << 8) | data[offset+3];
}

string hexDump(const ubyte[] data, size_t offset = 0, size_t maxLines = 64) {
    char[] result;
    size_t bytesPerLine = 16;
    size_t lines = 0;

    for (size_t i = 0; i < data.length && lines < maxLines; i += bytesPerLine) {
        result ~= hexStr(offset + i, 8);
        result ~= "  ";
        foreach (j; 0 .. bytesPerLine) {
            if (i + j < data.length) {
                result ~= hexStr(data[i + j], 2);
                result ~= ' ';
            } else {
                result ~= "   ";
            }
            if (j == 7) result ~= ' ';
        }
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

size_t[] scanForMagic(const ubyte[] data, uint magic) {
    size_t[] offsets;
    ubyte[4] target = [
        cast(ubyte)(magic >> 24),
        cast(ubyte)(magic >> 16),
        cast(ubyte)(magic >> 8),
        cast(ubyte)(magic),
    ];
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
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    // Detection
    assert(detectFormat([0xA1, 0xB2, 0xC3, 0xD4, 0, 0, 0, 0]) == Format.PCAP);
    assert(detectFormat([0x7F, 0x45, 0x4C, 0x46, 0, 0, 0, 0]) == Format.ELF);
    assert(detectFormat([0x89, 0x50, 0x4E, 0x47, 0, 0, 0, 0]) == Format.PNG);
    assert(detectFormat([0xCF, 0xFA, 0xED, 0xFE, 0, 0, 0, 0]) == Format.MachO);
    assert(detectFormat([0xCA, 0xFE, 0xBA, 0xBE, 0, 0, 0, 0]) == Format.MachO);
    assert(detectFormat([0x62, 0x70, 0x6C, 0x69, 0x73, 0x74, 0x30, 0x30]) == Format.BPlist);
    assert(detectFormat([0x23, 0x21, 0x2F, 0x75, 0x73, 0x72, 0x2F, 0x62]) == Format.Shebang);
    assert(detectFormat([0x50, 0x58, 0x52, 0x2D, 0x55, 0x53, 0x44, 0x43]) == Format.USDC);
    assert(detectFormat([0x42, 0x4F, 0x4D, 0x53, 0x74, 0x6F, 0x72, 0x65]) == Format.BOM);
    assert(detectFormat([0, 0, 0]) == Format.Unknown);

    // Hex dump
    assert(hexDump([0x48, 0x65, 0x6C, 0x6C, 0x6F], 0, 1).length > 0);

    // Magic scanner
    ubyte[12] buf = [0, 0, 0x7F, 0x45, 0x4C, 0x46, 0, 0, 0x7F, 0x45, 0x4C, 0x46];
    auto hits = scanForMagic(buf[], 0x7F454C46);
    assert(hits.length == 2);
    assert(hits[0] == 2);
    assert(hits[1] == 8);
}
