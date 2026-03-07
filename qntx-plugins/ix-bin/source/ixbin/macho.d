/// Mach-O format parser — CTFE-generated from struct layout.
///
/// Handles single-arch (FEEDFACE/FEEDFACF) and fat/universal (CAFEBABE) binaries.
module ixbin.macho;

import ixbin.detect : parseBinaryStruct, readBE32, byteSwap32;
import std.conv : convTo = to;

// ---------------------------------------------------------------------------
// Struct definitions
// ---------------------------------------------------------------------------

/// Mach-O 64-bit header — 32 bytes, packed.
align(1) struct MachO64Header {
    align(1):
    uint magic;
    uint cpuType;
    uint cpuSubtype;
    uint fileType;
    uint ncmds;         // number of load commands
    uint sizeOfCmds;    // total size of load commands
    uint flags;
    uint reserved;      // 64-bit only
}

static assert(MachO64Header.sizeof == 32, "Mach-O 64 header must be exactly 32 bytes");

/// Mach-O 32-bit header — 28 bytes, packed.
align(1) struct MachO32Header {
    align(1):
    uint magic;
    uint cpuType;
    uint cpuSubtype;
    uint fileType;
    uint ncmds;
    uint sizeOfCmds;
    uint flags;
}

static assert(MachO32Header.sizeof == 28, "Mach-O 32 header must be exactly 28 bytes");

/// Mach-O fat (universal) header — 8 bytes, big-endian.
align(1) struct FatHeader {
    align(1):
    uint magic;
    uint nfatArch;      // number of architecture slices
}

static assert(FatHeader.sizeof == 8, "Fat header must be exactly 8 bytes");

/// Mach-O fat architecture entry — 20 bytes, big-endian.
align(1) struct FatArch {
    align(1):
    uint cpuType;
    uint cpuSubtype;
    uint offset;        // offset to the Mach-O header for this slice
    uint size;
    uint alignment;
}

static assert(FatArch.sizeof == 20, "Fat arch entry must be exactly 20 bytes");

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

struct MachOSummary {
    bool is64bit;
    bool swapped;
    bool fat;
    uint fatSlices;
    string[] sliceArchs;
    string cpuType;
    string fileType;
    uint loadCommands;
    uint loadCommandsSize;
    uint flags;
}

MachOSummary parseMachO(const ubyte[] data) {
    MachOSummary result;
    if (data.length < 4) return result;

    uint magic = (cast(uint)data[0]) | (cast(uint)data[1] << 8) |
                 (cast(uint)data[2] << 16) | (cast(uint)data[3] << 24);

    // Fat/universal binary — big-endian header, contains multiple arch slices
    bool isFat = (magic == 0xBEBAFECA || magic == 0xCAFEBABE);
    if (isFat) {
        result.fat = true;
        uint nArch = readBE32(data, 4);
        result.fatSlices = nArch;

        size_t pos = FatHeader.sizeof;
        uint firstSliceOffset = 0;
        foreach (i; 0 .. nArch) {
            if (pos + FatArch.sizeof > data.length) break;
            uint archCpu = readBE32(data, pos);
            uint archOffset = readBE32(data, pos + 8);
            result.sliceArchs ~= cpuTypeName(archCpu);
            if (i == 0) firstSliceOffset = archOffset;
            pos += FatArch.sizeof;
        }

        // Parse the first slice's Mach-O header for detailed info
        if (firstSliceOffset > 0 && firstSliceOffset + 32 <= data.length) {
            auto sliceResult = parseMachOSingle(data[firstSliceOffset .. $]);
            result.is64bit = sliceResult.is64bit;
            result.cpuType = sliceResult.cpuType;
            result.fileType = sliceResult.fileType;
            result.loadCommands = sliceResult.loadCommands;
            result.loadCommandsSize = sliceResult.loadCommandsSize;
            result.flags = sliceResult.flags;
        }
        return result;
    }

    return parseMachOSingle(data);
}

/// Parse a single-arch Mach-O header (not fat).
private MachOSummary parseMachOSingle(const ubyte[] data) {
    MachOSummary result;
    if (data.length < 4) return result;

    uint magic = (cast(uint)data[0]) | (cast(uint)data[1] << 8) |
                 (cast(uint)data[2] << 16) | (cast(uint)data[3] << 24);

    result.is64bit = (magic == 0xFEEDFACF || magic == 0xCFFAEDFE);
    result.swapped = (magic == 0xCEFAEDFE || magic == 0xCFFAEDFE);

    uint cpuType, fileType, ncmds, sizeOfCmds, flags;

    if (result.is64bit && data.length >= MachO64Header.sizeof) {
        auto hdr = parseBinaryStruct!MachO64Header(data);
        cpuType = hdr.cpuType;
        fileType = hdr.fileType;
        ncmds = hdr.ncmds;
        sizeOfCmds = hdr.sizeOfCmds;
        flags = hdr.flags;
    } else if (!result.is64bit && data.length >= MachO32Header.sizeof) {
        auto hdr = parseBinaryStruct!MachO32Header(data);
        cpuType = hdr.cpuType;
        fileType = hdr.fileType;
        ncmds = hdr.ncmds;
        sizeOfCmds = hdr.sizeOfCmds;
        flags = hdr.flags;
    } else {
        return result;
    }

    if (result.swapped) {
        cpuType = byteSwap32(cpuType);
        fileType = byteSwap32(fileType);
        ncmds = byteSwap32(ncmds);
        sizeOfCmds = byteSwap32(sizeOfCmds);
        flags = byteSwap32(flags);
    }

    result.cpuType = cpuTypeName(cpuType);
    result.fileType = fileTypeName(fileType);
    result.loadCommands = ncmds;
    result.loadCommandsSize = sizeOfCmds;
    result.flags = flags;

    return result;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

string cpuTypeName(uint cpuType) {
    uint baseCpu = cpuType & 0x00FFFFFF;
    switch (baseCpu) {
        case 7:    return (cpuType & 0x01000000) ? "x86-64" : "x86";
        case 12:   return (cpuType & 0x01000000) ? "arm64" : "arm";
        case 18:   return "powerpc";
        default:   return "other-" ~ convTo!string(cpuType);
    }
}

string fileTypeName(uint ft) {
    switch (ft) {
        case 1:  return "object";
        case 2:  return "executable";
        case 3:  return "fvmlib";
        case 4:  return "core";
        case 5:  return "preload";
        case 6:  return "dylib";
        case 7:  return "dylinker";
        case 8:  return "bundle";
        case 9:  return "dylib-stub";
        case 10: return "dsym";
        case 11: return "kext";
        default: return "unknown-" ~ convTo!string(ft);
    }
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    // 64-bit arm64 executable (native Apple Silicon layout)
    auto machoData = new ubyte[32];
    machoData[0] = 0xCF; machoData[1] = 0xFA; machoData[2] = 0xED; machoData[3] = 0xFE;
    machoData[4] = 0x0C; machoData[5] = 0x00; machoData[6] = 0x00; machoData[7] = 0x01; // arm64
    machoData[12] = 0x02; // executable
    machoData[16] = 0x0F; // 15 load commands
    auto info = parseMachO(machoData);
    assert(info.is64bit);
    assert(!info.swapped);
    assert(info.cpuType == "arm64");
    assert(info.fileType == "executable");
    assert(info.loadCommands == 15);

    // Fat/universal: 2 slices (x86-64 + arm64)
    auto fatData = new ubyte[8 + 2*20 + 32];
    fatData[0] = 0xCA; fatData[1] = 0xFE; fatData[2] = 0xBA; fatData[3] = 0xBE;
    fatData[4] = 0x00; fatData[5] = 0x00; fatData[6] = 0x00; fatData[7] = 0x02; // 2 arches
    fatData[8]  = 0x01; fatData[9]  = 0x00; fatData[10] = 0x00; fatData[11] = 0x07; // x86-64
    fatData[16] = 0x00; fatData[17] = 0x00; fatData[18] = 0x00; fatData[19] = 0x30; // offset=48
    fatData[28] = 0x01; fatData[29] = 0x00; fatData[30] = 0x00; fatData[31] = 0x0C; // arm64
    size_t o = 48;
    fatData[o]   = 0xCF; fatData[o+1] = 0xFA; fatData[o+2] = 0xED; fatData[o+3] = 0xFE;
    fatData[o+4] = 0x07; fatData[o+5] = 0x00; fatData[o+6] = 0x00; fatData[o+7] = 0x01; // x86-64 LE
    fatData[o+12] = 0x02; // executable
    fatData[o+16] = 0x0A; // 10 load commands
    auto fatInfo = parseMachO(fatData);
    assert(fatInfo.fat);
    assert(fatInfo.fatSlices == 2);
    assert(fatInfo.sliceArchs.length == 2);
    assert(fatInfo.sliceArchs[0] == "x86-64");
    assert(fatInfo.sliceArchs[1] == "arm64");
    assert(fatInfo.fileType == "executable");
    assert(fatInfo.loadCommands == 10);
}
