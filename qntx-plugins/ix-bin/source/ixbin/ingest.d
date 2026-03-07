/// Binary data ingestion engine.
///
/// Orchestrates format detection and parsing. Format-specific parsers
/// live in their own modules (pcap, elf, macho). Detection and shared
/// binary utilities live in detect.
module ixbin.ingest;

import ixbin.proto;
import ixbin.version_ : PLUGIN_VERSION;
import ixbin.detect;
import ixbin.pcap;
import ixbin.elf;
import ixbin.macho;

import std.conv : convTo = to;

// ---------------------------------------------------------------------------
// Ingestion result
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
    result.summary["size_bytes"] = convTo!string(data.length);

    switch (result.format) {
        case Format.PCAP:
        case Format.PCAPSwapped:
            ingestPcap(data, source, result);
            break;

        case Format.ELF:
            ingestElf(data, source, result);
            break;

        case Format.MachO:
            ingestMachO(data, source, result);
            break;

        case Format.Shebang:
            // Extract interpreter from first line
            size_t lineEnd = 0;
            while (lineEnd < data.length && data[lineEnd] != '\n') lineEnd++;
            if (lineEnd > 2) {
                result.summary["interpreter"] = cast(string)data[2 .. lineEnd];
            }
            goto case;

        case Format.BPlist:
        case Format.USDC:
        case Format.BOM:
        case Format.SQLite:
        case Format.PNG:
        case Format.PDF:
        case Format.Gzip:
        case Format.PE:
        case Format.PCAPNG:
            // Detected format, no deep parser yet
            result.attestations ~= makeAttestation(source, result.formatName, result.summary);
            break;

        default:
            result.attestations ~= makeAttestation(source, "binary", result.summary);
            break;
    }

    return result;
}

// ---------------------------------------------------------------------------
// Format-specific ingestion
// ---------------------------------------------------------------------------

private void ingestPcap(const ubyte[] data, string source, ref IngestResult result) {
    auto pcap = parsePcap(data);
    result.summary["packets"] = convTo!string(pcap.packetCount);
    result.summary["total_captured_bytes"] = convTo!string(pcap.totalBytes);
    result.summary["snaplen"] = convTo!string(
        result.format == Format.PCAPSwapped ?
            byteSwap32(pcap.global.snaplen) : pcap.global.snaplen);
    result.summary["link_type"] = convTo!string(
        result.format == Format.PCAPSwapped ?
            byteSwap32(pcap.global.network) : pcap.global.network);
    result.attestations ~= makeAttestation(source, "pcap", result.summary);
}

private void ingestElf(const ubyte[] data, string source, ref IngestResult result) {
    auto elf = parseElf(data);
    result.summary["class"] = elf.is64bit ? "64-bit" : "32-bit";
    result.summary["endianness"] = elf.littleEndian ? "little-endian" : "big-endian";
    result.summary["type"] = elf.elfType;
    result.summary["machine"] = elf.machine;
    result.summary["entry_point"] = "0x" ~ hexStr(elf.entryPoint, 16);
    result.summary["program_headers"] = convTo!string(elf.programHeaders);
    result.summary["section_headers"] = convTo!string(elf.sectionHeaders);
    result.attestations ~= makeAttestation(source, "elf", result.summary);
}

private void ingestMachO(const ubyte[] data, string source, ref IngestResult result) {
    auto macho = parseMachO(data);
    if (macho.fat) {
        result.summary["universal"] = "true";
        result.summary["slices"] = convTo!string(macho.fatSlices);
        string archs;
        foreach (i, a; macho.sliceArchs) {
            if (i > 0) archs ~= ", ";
            archs ~= a;
        }
        result.summary["architectures"] = archs;
    }
    result.summary["class"] = macho.is64bit ? "64-bit" : "32-bit";
    result.summary["cpu"] = macho.cpuType;
    result.summary["type"] = macho.fileType;
    result.summary["load_commands"] = convTo!string(macho.loadCommands);
    result.summary["load_commands_size"] = convTo!string(macho.loadCommandsSize);
    result.summary["flags"] = "0x" ~ hexStr(macho.flags, 8);
    result.attestations ~= makeAttestation(source, "mach-o", result.summary);
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

private AttestationCommand makeAttestation(string source, string context, string[string] summary) {
    AttestationCommand cmd;
    cmd.subjects = [source];
    cmd.predicates = ["ingested"];
    cmd.contexts = [context];
    cmd.source = "ix-bin";
    cmd.sourceVersion = PLUGIN_VERSION;
    cmd.attributes = encodeStructFromStringMap(summary);
    return cmd;
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    // Format detection
    ubyte[8] pcap = [0xA1, 0xB2, 0xC3, 0xD4, 0, 0, 0, 0];
    assert(detectFormat(pcap[]) == Format.PCAP);

    ubyte[8] elf = [0x7F, 0x45, 0x4C, 0x46, 0, 0, 0, 0];
    assert(detectFormat(elf[]) == Format.ELF);

    ubyte[8] png = [0x89, 0x50, 0x4E, 0x47, 0, 0, 0, 0];
    assert(detectFormat(png[]) == Format.PNG);

    ubyte[3] empty = [0, 0, 0];
    assert(detectFormat(empty[]) == Format.Unknown);

    // String-based format detection
    ubyte[8] bplist = [0x62, 0x70, 0x6C, 0x69, 0x73, 0x74, 0x30, 0x30];
    assert(detectFormat(bplist[]) == Format.BPlist);

    ubyte[8] shebang = [0x23, 0x21, 0x2F, 0x75, 0x73, 0x72, 0x2F, 0x62];
    assert(detectFormat(shebang[]) == Format.Shebang);

    ubyte[8] usdc = [0x50, 0x58, 0x52, 0x2D, 0x55, 0x53, 0x44, 0x43];
    assert(detectFormat(usdc[]) == Format.USDC);

    ubyte[8] bom = [0x42, 0x4F, 0x4D, 0x53, 0x74, 0x6F, 0x72, 0x65];
    assert(detectFormat(bom[]) == Format.BOM);

    // Mach-O detection
    ubyte[8] macho64 = [0xCF, 0xFA, 0xED, 0xFE, 0, 0, 0, 0];
    assert(detectFormat(macho64[]) == Format.MachO);

    ubyte[8] fatBE = [0xCA, 0xFE, 0xBA, 0xBE, 0, 0, 0, 0];
    assert(detectFormat(fatBE[]) == Format.MachO);
}
