/// Pure D gRPC server over HTTP/2.
///
/// Server-only fork of ix-net's grpc.d — no client needed since faal
/// doesn't call back into QNTX core services.
module faal.grpc;

import faal.hpack;
import faal.proto;
import std.socket;
import std.conv : to;
import core.stdc.string : memcpy;
import core.time : dur;
import faal.log;

// ---------------------------------------------------------------------------
// HTTP/2 frame types and constants
// ---------------------------------------------------------------------------

enum FrameType : ubyte {
    DATA          = 0x0,
    HEADERS       = 0x1,
    PRIORITY      = 0x2,
    RST_STREAM    = 0x3,
    SETTINGS      = 0x4,
    PUSH_PROMISE  = 0x5,
    PING          = 0x6,
    GOAWAY        = 0x7,
    WINDOW_UPDATE = 0x8,
    CONTINUATION  = 0x9,
}

enum FrameFlags : ubyte {
    NONE         = 0x0,
    END_STREAM   = 0x1,
    ACK          = 0x1,  // SETTINGS/PING ACK (same bit as END_STREAM, different frame types)
    END_HEADERS  = 0x4,
    PADDED       = 0x8,
    PRIORITY_FL  = 0x20,
}

struct Frame {
    uint length;
    FrameType type;
    ubyte flags;
    uint streamId;
    ubyte[] payload;
}

enum H2_PREFACE = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n";
enum SETTINGS_INITIAL_WINDOW_SIZE = 0x4;
enum SETTINGS_MAX_FRAME_SIZE     = 0x5;

// ---------------------------------------------------------------------------
// Frame I/O
// ---------------------------------------------------------------------------

/// Read exactly N bytes from a socket, returns null on failure.
ubyte[] readExact(Socket sock, size_t n) {
    if (n == 0) return [];
    auto buf = new ubyte[n];
    size_t total = 0;
    while (total < n) {
        auto received = sock.receive(buf[total .. n]);
        if (received <= 0) return null;
        total += received;
    }
    return buf;
}

/// Read one HTTP/2 frame.
Frame* readFrame(Socket sock) {
    auto header = readExact(sock, 9);
    if (header is null || header.length < 9) return null;

    auto f = new Frame;
    f.length = (cast(uint)header[0] << 16) | (cast(uint)header[1] << 8) | header[2];
    f.type = cast(FrameType)header[3];
    f.flags = header[4];
    f.streamId = ((cast(uint)header[5] << 24) | (cast(uint)header[6] << 16) |
                  (cast(uint)header[7] << 8) | header[8]) & 0x7FFFFFFF;

    if (f.length > 0) {
        f.payload = readExact(sock, f.length);
        if (f.payload is null) return null;
    } else {
        f.payload = [];
    }
    return f;
}

/// Write one HTTP/2 frame.
bool writeFrame(Socket sock, FrameType type, ubyte flags, uint streamId, const ubyte[] payload) {
    uint len = cast(uint)payload.length;
    ubyte[9] header;
    header[0] = cast(ubyte)(len >> 16);
    header[1] = cast(ubyte)(len >> 8);
    header[2] = cast(ubyte)(len);
    header[3] = cast(ubyte)type;
    header[4] = flags;
    header[5] = cast(ubyte)(streamId >> 24);
    header[6] = cast(ubyte)(streamId >> 16);
    header[7] = cast(ubyte)(streamId >> 8);
    header[8] = cast(ubyte)(streamId);

    auto sent = sock.send(header[]);
    if (sent != 9) return false;
    if (payload.length > 0) {
        sent = sock.send(payload);
        if (sent != payload.length) return false;
    }
    return true;
}

/// Send a SETTINGS frame.
bool sendSettings(Socket sock, uint streamId = 0) {
    return writeFrame(sock, FrameType.SETTINGS, 0, streamId, []);
}

/// Send a SETTINGS ACK.
bool sendSettingsAck(Socket sock) {
    return writeFrame(sock, FrameType.SETTINGS, FrameFlags.ACK, 0, []);
}

/// Send a WINDOW_UPDATE frame.
bool sendWindowUpdate(Socket sock, uint streamId, uint increment) {
    ubyte[4] payload;
    payload[0] = cast(ubyte)(increment >> 24);
    payload[1] = cast(ubyte)(increment >> 16);
    payload[2] = cast(ubyte)(increment >> 8);
    payload[3] = cast(ubyte)(increment);
    return writeFrame(sock, FrameType.WINDOW_UPDATE, 0, streamId, payload[]);
}

// ---------------------------------------------------------------------------
// gRPC message framing
// ---------------------------------------------------------------------------

/// Wrap a protobuf message in gRPC framing: [compressed:1][length:4][data:N]
ubyte[] grpcFrame(const ubyte[] protoBytes) {
    ubyte[] result;
    result.length = 5 + protoBytes.length;
    result[0] = 0; // not compressed
    auto len = cast(uint)protoBytes.length;
    result[1] = cast(ubyte)(len >> 24);
    result[2] = cast(ubyte)(len >> 16);
    result[3] = cast(ubyte)(len >> 8);
    result[4] = cast(ubyte)(len);
    if (protoBytes.length > 0) {
        result[5 .. $] = protoBytes[];
    }
    return result;
}

/// Extract protobuf bytes from gRPC-framed data.
ubyte[] grpcUnframe(const ubyte[] data) {
    if (data.length < 5) return [];
    uint len = (cast(uint)data[1] << 24) | (cast(uint)data[2] << 16) |
               (cast(uint)data[3] << 8) | data[4];
    if (5 + len > data.length) return [];
    return data[5 .. 5 + len].dup;
}

// ---------------------------------------------------------------------------
// Request context for a single gRPC stream
// ---------------------------------------------------------------------------

struct StreamContext {
    string method;
    ubyte[] data;
    bool headersReceived;
    bool endStream;
}

// ---------------------------------------------------------------------------
// RPC handler delegate type
// ---------------------------------------------------------------------------

alias RpcHandler = ubyte[] delegate(const ubyte[] requestData);

// ---------------------------------------------------------------------------
// gRPC Server
// ---------------------------------------------------------------------------

struct GrpcServer {
    RpcHandler[string] handlers;
    ushort port;
    Socket listener;
    bool running = false;

    void registerHandler(string method, RpcHandler handler) {
        handlers[method] = handler;
    }

    /// Bind and start listening. Returns the actual port.
    ushort bind(ushort requestedPort) {
        listener = new TcpSocket();
        listener.setOption(SocketOptionLevel.SOCKET, SocketOption.REUSEADDR, true);

        foreach (attempt; 0 .. 64) {
            ushort tryPort = cast(ushort)(requestedPort + attempt);
            try {
                listener.bind(new InternetAddress("127.0.0.1", tryPort));
                listener.listen(5);
                port = tryPort;
                running = true;
                return tryPort;
            } catch (SocketOSException) {
                continue;
            }
        }
        return 0;
    }

    /// Accept and serve connections. Blocks.
    void serve() {
        while (running) {
            auto client = listener.accept();
            if (client !is null) {
                serveConnection(client);
            }
        }
    }

    void stop() {
        running = false;
        if (listener !is null) {
            listener.close();
        }
    }

    /// Handle a single HTTP/2 connection.
    private void serveConnection(Socket sock) {
        scope(exit) sock.close();

        auto preface = readExact(sock, 24);
        if (preface is null) return;
        if (cast(string)preface != H2_PREFACE) return;

        sendSettings(sock);

        auto clientSettings = readFrame(sock);
        if (clientSettings is null) return;
        sendSettingsAck(sock);

        sendWindowUpdate(sock, 0, 1_073_741_823);

        DynamicTable dynTable;
        StreamContext[uint] streams;

        while (running) {
            auto frame = readFrame(sock);
            if (frame is null) break;

            switch (frame.type) {
                case FrameType.SETTINGS:
                    if ((frame.flags & FrameFlags.ACK) == 0) {
                        sendSettingsAck(sock);
                    }
                    break;

                case FrameType.PING:
                    writeFrame(sock, FrameType.PING, 0x1, 0, frame.payload);
                    break;

                case FrameType.WINDOW_UPDATE:
                    break;

                case FrameType.GOAWAY:
                    return;

                case FrameType.HEADERS:
                    auto streamId = frame.streamId;
                    auto headers = decodeHeaders(frame.payload, dynTable);

                    if (streamId !in streams) {
                        streams[streamId] = StreamContext.init;
                    }
                    foreach (hf; headers) {
                        if (hf.name == ":path") {
                            streams[streamId].method = hf.value;
                        }
                    }
                    streams[streamId].headersReceived = true;
                    if ((frame.flags & FrameFlags.END_STREAM) != 0) {
                        streams[streamId].endStream = true;
                    }

                    if (streams[streamId].endStream) {
                        handleStream(sock, streamId, streams[streamId]);
                        streams.remove(streamId);
                    }
                    break;

                case FrameType.DATA:
                    auto streamId = frame.streamId;
                    if (streamId in streams) {
                        streams[streamId].data ~= frame.payload;
                        if ((frame.flags & FrameFlags.END_STREAM) != 0) {
                            streams[streamId].endStream = true;
                            handleStream(sock, streamId, streams[streamId]);
                            streams.remove(streamId);
                        }
                    }
                    if (frame.length > 0) {
                        sendWindowUpdate(sock, streamId, frame.length);
                    }
                    break;

                case FrameType.RST_STREAM:
                    streams.remove(frame.streamId);
                    break;

                default:
                    break;
            }
        }
    }

    /// Process a complete request stream and send the response.
    private void handleStream(Socket sock, uint streamId, ref StreamContext ctx) {
        auto handler = ctx.method in handlers;
        ubyte[] responseProto;

        if (handler !is null) {
            auto requestProto = grpcUnframe(ctx.data);
            responseProto = (*handler)(requestProto);
        } else {
            responseProto = [];
        }

        auto responseHeaders = encodeResponseHeaders();
        writeFrame(sock, FrameType.HEADERS, FrameFlags.END_HEADERS, streamId, responseHeaders);

        auto grpcData = grpcFrame(responseProto);
        writeFrame(sock, FrameType.DATA, 0, streamId, grpcData);

        auto trailers = encodeGrpcTrailers();
        writeFrame(sock, FrameType.HEADERS,
            FrameFlags.END_STREAM | FrameFlags.END_HEADERS,
            streamId, trailers);
    }
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    auto data = cast(ubyte[])[1, 2, 3, 4, 5];
    auto framed = grpcFrame(data);
    assert(framed.length == 10);
    assert(framed[0] == 0);
    auto unframed = grpcUnframe(framed);
    assert(unframed == data);

    auto emptyFramed = grpcFrame([]);
    assert(emptyFramed.length == 5);
    auto emptyUnframed = grpcUnframe(emptyFramed);
    assert(emptyUnframed.length == 0);
}
