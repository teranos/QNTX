/// Pure D gRPC server and client over HTTP/2.
///
/// Implements just enough HTTP/2 to serve DomainPluginService RPCs
/// and make callback RPCs to QNTX core services. No external dependencies.
module ixnet.grpc;

import ixnet.hpack;
import ixnet.proto;
import std.socket;
import std.conv : to;
import core.stdc.string : memcpy;
import core.time : dur;
import ixnet.log;

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
    // Send empty SETTINGS (use defaults)
    return writeFrame(sock, FrameType.SETTINGS, 0, streamId, []);
}

/// Send a SETTINGS ACK.
bool sendSettingsAck(Socket sock) {
    return writeFrame(sock, FrameType.SETTINGS, FrameFlags.END_HEADERS, 0, []);
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
    // byte 0 = compressed flag (ignore)
    uint len = (cast(uint)data[1] << 24) | (cast(uint)data[2] << 16) |
               (cast(uint)data[3] << 8) | data[4];
    if (5 + len > data.length) return data[5 .. $].dup; // partial
    return data[5 .. 5 + len].dup;
}

// ---------------------------------------------------------------------------
// Request context for a single gRPC stream
// ---------------------------------------------------------------------------

struct StreamContext {
    string method; // e.g., "/protocol.DomainPluginService/Metadata"
    ubyte[] data;  // accumulated DATA payload
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
    RpcHandler[string] handlers; // keyed by full gRPC method path
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

        // Try ports starting from requestedPort
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
        return 0; // failed
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

        // Read HTTP/2 connection preface
        auto preface = readExact(sock, 24);
        if (preface is null) return;
        if (cast(string)preface != H2_PREFACE) return;

        // Send our SETTINGS
        sendSettings(sock);

        // Read client SETTINGS
        auto clientSettings = readFrame(sock);
        if (clientSettings is null) return;
        // ACK client settings
        sendSettingsAck(sock);

        // Send a large window update for connection-level flow control
        sendWindowUpdate(sock, 0, 1_073_741_823); // ~1GB

        DynamicTable dynTable;
        StreamContext[uint] streams;

        // Main frame loop
        while (running) {
            auto frame = readFrame(sock);
            if (frame is null) break; // connection closed

            switch (frame.type) {
                case FrameType.SETTINGS:
                    if ((frame.flags & FrameFlags.END_HEADERS) == 0) {
                        // Not an ACK — acknowledge it
                        sendSettingsAck(sock);
                    }
                    break;

                case FrameType.PING:
                    // Respond with PONG (same payload, ACK flag)
                    writeFrame(sock, FrameType.PING, 0x1, 0, frame.payload);
                    break;

                case FrameType.WINDOW_UPDATE:
                    // Accept and ignore (we don't enforce send-side flow control)
                    break;

                case FrameType.GOAWAY:
                    return; // client is disconnecting

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

                    // If END_STREAM on HEADERS (no body, e.g., Metadata/Health)
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
                    // Send stream-level window update
                    if (frame.length > 0) {
                        sendWindowUpdate(sock, streamId, frame.length);
                    }
                    break;

                case FrameType.RST_STREAM:
                    streams.remove(frame.streamId);
                    break;

                default:
                    break; // ignore unknown frames
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
            // Unknown method — send empty response with error trailer
            responseProto = [];
        }

        // Send HEADERS (response headers)
        auto responseHeaders = encodeResponseHeaders();
        writeFrame(sock, FrameType.HEADERS, FrameFlags.END_HEADERS, streamId, responseHeaders);

        // Send DATA (gRPC-framed protobuf)
        auto grpcData = grpcFrame(responseProto);
        writeFrame(sock, FrameType.DATA, 0, streamId, grpcData);

        // Send HEADERS (trailers with grpc-status: 0)
        auto trailers = encodeGrpcTrailers();
        writeFrame(sock, FrameType.HEADERS,
            FrameFlags.END_STREAM | FrameFlags.END_HEADERS,
            streamId, trailers);
    }
}

// ---------------------------------------------------------------------------
// gRPC Client (for callback services to QNTX core)
// ---------------------------------------------------------------------------

struct GrpcClient {
    string host;
    ushort port;
    Socket sock;
    uint nextStreamId = 1; // client streams are odd-numbered
    DynamicTable dynTable;

    /// Connect to the gRPC server. Returns true on success.
    /// Logs errors to stderr with endpoint context.
    bool connect(string endpoint) {
        // Parse host:port
        auto colonIdx = lastIndexOf(endpoint, ':');
        if (colonIdx < 0) {
            logError("[ix-bin] gRPC client: invalid endpoint %s (no port)", endpoint);
            return false;
        }
        host = endpoint[0 .. colonIdx];
        port = to!ushort(endpoint[colonIdx + 1 .. $]);

        sock = new TcpSocket();

        // Set read timeout so handshake can't block forever (5 seconds)
        sock.setOption(SocketOptionLevel.SOCKET, SocketOption.RCVTIMEO, dur!"seconds"(5));

        try {
            sock.connect(new InternetAddress(host, port));
        } catch (SocketOSException e) {
            logError("[ix-bin] gRPC client: TCP connect to %s failed: %s", endpoint, e.msg);
            sock = null;
            return false;
        }

        // Send connection preface
        sock.send(cast(const(ubyte)[])H2_PREFACE);
        sendSettings(sock);

        // Read server SETTINGS
        auto serverSettings = readFrame(sock);
        if (serverSettings is null) {
            logError("[ix-bin] gRPC client: no SETTINGS from %s (timeout or connection closed)", endpoint);
            sock.close();
            sock = null;
            return false;
        }
        sendSettingsAck(sock);

        // Read SETTINGS ACK from server
        auto settingsAck = readFrame(sock);
        // May also get WINDOW_UPDATE — read frames until we get SETTINGS ACK
        int maxReads = 10;
        while (settingsAck !is null && maxReads > 0) {
            if (settingsAck.type == FrameType.SETTINGS &&
                (settingsAck.flags & FrameFlags.END_HEADERS) != 0) {
                break; // Got ACK
            }
            settingsAck = readFrame(sock);
            maxReads--;
        }

        if (maxReads == 0) {
            logError("[ix-bin] gRPC client: SETTINGS ACK not received from %s after 10 frames", endpoint);
            sock.close();
            sock = null;
            return false;
        }

        logInfo("[ix-bin] gRPC client: connected to %s", endpoint);
        return true;
    }

    /// Make a unary RPC call. Returns empty on failure.
    ubyte[] call(string method, const ubyte[] requestProto) {
        if (sock is null) {
            logError("[ix-bin] gRPC client: call %s failed — not connected", method);
            return [];
        }

        uint streamId = nextStreamId;
        nextStreamId += 2;

        // Encode request headers
        ubyte[] headerBlock;
        // :method POST (static index 3)
        headerBlock ~= encodeIndexedHeader(3);
        // :scheme http (static index 6)
        headerBlock ~= encodeIndexedHeader(6);
        // :path
        headerBlock ~= encodeIndexedNameHeader(4, method);
        // content-type: application/grpc
        headerBlock ~= encodeIndexedNameHeader(31, "application/grpc");
        // te: trailers
        headerBlock ~= encodeLiteralHeader("te", "trailers");

        // Send HEADERS
        writeFrame(sock, FrameType.HEADERS, FrameFlags.END_HEADERS, streamId, headerBlock);

        // Send DATA with gRPC framing
        auto grpcData = grpcFrame(requestProto);
        writeFrame(sock, FrameType.DATA, FrameFlags.END_STREAM, streamId, grpcData);

        // Read response frames
        ubyte[] responseData;
        int maxFrames = 20;
        while (maxFrames > 0) {
            auto frame = readFrame(sock);
            if (frame is null) break;
            maxFrames--;

            if (frame.type == FrameType.DATA && frame.streamId == streamId) {
                responseData ~= frame.payload;
            } else if (frame.type == FrameType.HEADERS && frame.streamId == streamId) {
                if ((frame.flags & FrameFlags.END_STREAM) != 0) {
                    break; // trailers received, done
                }
            } else if (frame.type == FrameType.WINDOW_UPDATE) {
                continue; // ignore flow control
            } else if (frame.type == FrameType.SETTINGS) {
                if ((frame.flags & FrameFlags.END_HEADERS) == 0) {
                    sendSettingsAck(sock);
                }
            } else if (frame.type == FrameType.PING) {
                writeFrame(sock, FrameType.PING, 0x1, 0, frame.payload);
            }
        }

        return grpcUnframe(responseData);
    }

    void close() {
        if (sock !is null) {
            sock.close();
            sock = null;
        }
    }
}

/// Find last occurrence of a character in a string.
private ptrdiff_t lastIndexOf(string s, char c) {
    for (ptrdiff_t i = cast(ptrdiff_t)s.length - 1; i >= 0; i--) {
        if (s[i] == c) return i;
    }
    return -1;
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    // Test gRPC frame/unframe round-trip
    auto data = cast(ubyte[])[1, 2, 3, 4, 5];
    auto framed = grpcFrame(data);
    assert(framed.length == 10); // 5 header + 5 data
    assert(framed[0] == 0);     // not compressed
    auto unframed = grpcUnframe(framed);
    assert(unframed == data);

    // Test empty grpc frame
    auto emptyFramed = grpcFrame([]);
    assert(emptyFramed.length == 5);
    auto emptyUnframed = grpcUnframe(emptyFramed);
    assert(emptyUnframed.length == 0);
}
