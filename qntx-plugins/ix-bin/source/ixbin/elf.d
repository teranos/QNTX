/// ELF format parser — CTFE-generated from struct layout.
module ixbin.elf;

import ixbin.detect : parseBinaryStruct;
import std.conv : convTo = to;

// ---------------------------------------------------------------------------
// Struct definitions
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

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

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
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    auto elfData = new ubyte[64];
    elfData[0] = 0x7F; elfData[1] = 0x45; elfData[2] = 0x4C; elfData[3] = 0x46;
    elfData[4] = 2; // 64-bit
    elfData[5] = 1; // little-endian
    elfData[16] = 3; elfData[17] = 0; // shared object (LE)
    elfData[18] = 0x3E; elfData[19] = 0; // x86-64 (LE)
    auto info = parseElf(elfData);
    assert(info.is64bit);
    assert(info.littleEndian);
    assert(info.elfType == "shared-object");
    assert(info.machine == "x86-64");
}
