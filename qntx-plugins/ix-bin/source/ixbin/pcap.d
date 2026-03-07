/// PCAP format parser — CTFE-generated from struct layout.
///
/// The struct IS the parser: each field maps to its binary position.
module ixbin.pcap;

import ixbin.detect : parseBinaryStruct, binarySize, byteSwap32;

// ---------------------------------------------------------------------------
// Struct definitions
// ---------------------------------------------------------------------------

/// PCAP global header — 24 bytes, packed.
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

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

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
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    assert(binarySize!PcapGlobalHeader == 24);
    assert(binarySize!PcapPacketHeader == 16);
}
