/// HTTPS proxy for Claude Code API traffic capture.
///
/// Accepts HTTP CONNECT tunnels, terminates TLS with a local CA cert,
/// captures request/response payloads, and forwards to api.anthropic.com.
///
/// Architecture:
///   Claude Code  --HTTPS-->  ix-net proxy (localhost)  --HTTPS-->  api.anthropic.com
///                                  |
///                                  v
///                            Capture buffer (ring)
///                            → Attest to QNTX
///
/// Phase 1: Stub — accepts connections, logs them, passes through without TLS interception.
/// Phase 2: TLS termination with local CA cert — intercept plaintext HTTP.
/// Phase 3: Payload extraction (JSON body, base64 images).
module ixnet.proxy;

import ixnet.log;
import ixnet.tls;
import std.conv : to;
import std.socket;
import core.thread;

// ---------------------------------------------------------------------------
// Capture ring buffer
// ---------------------------------------------------------------------------

/// A single captured API exchange.
struct Capture {
    long timestamp;        // unix epoch seconds
    string method;         // HTTP method (POST)
    string path;           // /v1/messages
    size_t requestSize;    // bytes
    size_t responseSize;   // bytes
    bool hasImages;        // request contained base64 image blocks
    int imageCount;        // number of image content blocks
    string model;          // model field from request JSON
    int inputTokens;       // from response usage
    int outputTokens;      // from response usage
    int statusCode;        // HTTP response status
    bool streaming;        // was this a streaming response?
}

enum MAX_CAPTURES = 256;

// ---------------------------------------------------------------------------
// Proxy state
// ---------------------------------------------------------------------------

struct ProxyState {
    ushort proxyPort;
    int captureCount;
    Socket listener;
    bool running;
    bool tlsEnabled;                 // true if TLS interception is active
    string certFile;                 // path to leaf cert
    string keyFile;                  // path to leaf key
    Capture[MAX_CAPTURES] captures;  // ring buffer
    int captureHead;                 // next write index
    Thread listenerThread;
}

/// Start the HTTPS proxy on the given port.
/// certFile/keyFile enable TLS interception (MITM). Pass empty strings for passthrough mode.
bool startProxy(ref ProxyState state, ushort port,
                string certFile = "", string keyFile = "") {

    // Initialize TLS if cert paths provided
    if (certFile.length > 0 && keyFile.length > 0) {
        if (tlsInit(certFile, keyFile)) {
            state.tlsEnabled = true;
            state.certFile = certFile;
            state.keyFile = keyFile;
            logInfo("[ix-net] proxy: TLS interception enabled");
        } else {
            logWarn("[ix-net] proxy: TLS init failed, falling back to passthrough");
        }
    }

    state.listener = new TcpSocket();
    state.listener.setOption(SocketOptionLevel.SOCKET, SocketOption.REUSEADDR, true);

    // Try binding
    foreach (attempt; 0 .. 16) {
        ushort tryPort = cast(ushort)(port + attempt);
        try {
            state.listener.bind(new InternetAddress("127.0.0.1", tryPort));
            state.listener.listen(32);
            state.proxyPort = tryPort;
            state.running = true;

            // Accept connections in a background thread
            state.listenerThread = new Thread(() {
                acceptLoop(&state);
            });
            state.listenerThread.isDaemon = true;
            state.listenerThread.start();

            return true;
        } catch (SocketOSException) {
            continue;
        }
    }

    logError("[ix-net] proxy: failed to bind to port %d (tried 16 ports)", port);
    return false;
}

/// Stop the proxy.
void stopProxy(ref ProxyState state) {
    state.running = false;
    if (state.listener !is null) {
        state.listener.close();
        state.listener = null;
    }
    if (state.tlsEnabled) {
        tlsCleanup();
        state.tlsEnabled = false;
    }
}

/// Get recent captures as JSON.
string getRecentCaptures(ref ProxyState state) {
    string json = `{"captures":[`;
    bool first = true;
    int count = state.captureCount < MAX_CAPTURES ? state.captureCount : MAX_CAPTURES;
    int start = state.captureCount < MAX_CAPTURES ? 0 : state.captureHead;

    foreach (i; 0 .. count) {
        int idx = (start + i) % MAX_CAPTURES;
        auto c = &state.captures[idx];
        if (!first) json ~= ",";
        json ~= `{"timestamp":` ~ c.timestamp.to!string ~
                `,"method":"` ~ c.method ~
                `","path":"` ~ c.path ~
                `","request_bytes":` ~ c.requestSize.to!string ~
                `,"response_bytes":` ~ c.responseSize.to!string ~
                `,"has_images":` ~ (c.hasImages ? "true" : "false") ~
                `,"image_count":` ~ c.imageCount.to!string ~
                `,"model":"` ~ c.model ~
                `","input_tokens":` ~ c.inputTokens.to!string ~
                `,"output_tokens":` ~ c.outputTokens.to!string ~
                `,"status_code":` ~ c.statusCode.to!string ~
                `,"streaming":` ~ (c.streaming ? "true" : "false") ~ `}`;
        first = false;
    }
    json ~= `],"total":` ~ state.captureCount.to!string ~ `}`;
    return json;
}

// ---------------------------------------------------------------------------
// Connection handling
// ---------------------------------------------------------------------------

private void acceptLoop(ProxyState* state) {
    while (state.running) {
        try {
            auto client = state.listener.accept();
            if (client !is null) {
                // Handle each connection in a new thread
                auto t = new Thread(() {
                    handleConnection(state, client);
                });
                t.isDaemon = true;
                t.start();
            }
        } catch (SocketOSException) {
            if (!state.running) break; // expected on shutdown
        }
    }
}

/// Handle a single proxy connection.
///
/// With TLS enabled: terminates TLS from client (using leaf cert),
/// reads plaintext HTTP, forwards over TLS to upstream, captures payload.
/// Without TLS: blind byte relay (passthrough).
private void handleConnection(ProxyState* state, Socket client) {
    scope(exit) client.close();

    // Read the initial HTTP request line (CONNECT host:port HTTP/1.1)
    auto requestLine = readLine(client);
    if (requestLine.length == 0) return;

    // Parse CONNECT method
    if (!startsWith(requestLine, "CONNECT ")) {
        logWarn("[ix-net] proxy: non-CONNECT request: %s", requestLine);
        sendResponse(client, "HTTP/1.1 405 Method Not Allowed\r\n\r\n");
        return;
    }

    // Extract host:port from "CONNECT api.anthropic.com:443 HTTP/1.1"
    auto hostPort = extractHostPort(requestLine);
    if (hostPort.length == 0) {
        sendResponse(client, "HTTP/1.1 400 Bad Request\r\n\r\n");
        return;
    }

    // Drain remaining headers
    while (true) {
        auto line = readLine(client);
        if (line.length == 0) break;
    }

    logInfo("[ix-net] proxy: CONNECT tunnel to %s", hostPort);

    // Connect to upstream
    auto upstream = connectUpstream(hostPort);
    if (upstream is null) {
        sendResponse(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n");
        logError("[ix-net] proxy: failed to connect to %s", hostPort);
        return;
    }
    scope(exit) upstream.close();

    // Tell client the tunnel is established
    sendResponse(client, "HTTP/1.1 200 Connection Established\r\n\r\n");

    if (state.tlsEnabled) {
        // Phase 2: TLS interception
        handleTLSInterception(state, client, upstream, hostPort);
    } else {
        // Phase 1: blind relay
        recordCapture(state, hostPort, "CONNECT", 0, 0);
        relay(client, upstream);
    }
}

/// TLS MITM: terminate client TLS, connect upstream TLS, relay plaintext.
private void handleTLSInterception(ProxyState* state, Socket client,
                                    Socket upstream, string hostPort) {
    // TLS handshake with client (we present the leaf cert)
    auto clientTLS = tlsAccept(client);
    if (!clientTLS.established) {
        logError("[ix-net] proxy: TLS accept failed for %s", hostPort);
        return;
    }
    scope(exit) tlsClose(clientTLS);

    // Extract hostname from host:port for SNI
    auto colonIdx = lastIndexOfChar(hostPort, ':');
    string hostname = colonIdx > 0 ? hostPort[0 .. colonIdx] : hostPort;

    // TLS handshake with upstream (real api.anthropic.com)
    auto upstreamTLS = tlsConnect(upstream, hostname);
    if (!upstreamTLS.established) {
        logError("[ix-net] proxy: TLS connect to upstream %s failed", hostPort);
        return;
    }
    scope(exit) tlsClose(upstreamTLS);

    logInfo("[ix-net] proxy: TLS interception active for %s", hostPort);

    // Relay plaintext between the two TLS connections, capturing traffic
    tlsRelay(state, clientTLS, upstreamTLS, hostPort);
}

/// HTTP-aware relay: reads headers, accumulates bodies, extracts JSON fields.
/// Handles multiple request/response cycles on the same connection (HTTP keep-alive).
private void tlsRelay(ProxyState* state, ref TLSConn clientTLS,
                      ref TLSConn upstreamTLS, string hostPort) {
    import ixnet.extract;

    auto clientBuf = TLSBufReader(&clientTLS);
    auto upstreamBuf = TLSBufReader(&upstreamTLS);

    // Handle one or more HTTP request/response pairs
    while (true) {
        // ---- Read request from client ----
        logInfo("[ix-net] relay: reading request headers from client...");
        string reqHeaders = readHTTPHeaders(clientBuf);
        if (reqHeaders.length == 0) { logInfo("[ix-net] relay: client closed"); break; }

        // Parse request line
        string method, path;
        parseRequestLine(cast(const ubyte[])reqHeaders, method, path);
        logInfo("[ix-net] relay: got %s %s (%d header bytes)", method, path, reqHeaders.length);

        // Strip accept-encoding so upstream sends plaintext (not gzip/br)
        // — we need readable response bodies for token/image extraction
        reqHeaders = stripHeader(reqHeaders, "accept-encoding");

        // Forward request headers to upstream
        if (tlsWrite(upstreamTLS, cast(const ubyte[])reqHeaders) <= 0) break;

        // Get content-length from request headers
        size_t reqContentLen = parseContentLength(reqHeaders);
        size_t totalReqBytes = reqHeaders.length;

        // Read and forward request body
        ubyte[] reqBody;
        if (reqContentLen > 0) {
            logInfo("[ix-net] relay: reading %d byte request body...", reqContentLen);
            reqBody = readAndForward(clientBuf, upstreamTLS, reqContentLen);
            totalReqBytes += reqBody.length;
            logInfo("[ix-net] relay: forwarded %d/%d request body bytes", reqBody.length, reqContentLen);
        } else {
            logInfo("[ix-net] relay: no request body (content-length=0)");
        }

        // Extract request fields
        auto reqInfo = extractRequest(reqBody);

        logInfo("[ix-net] relay: reading response from upstream for %s...", path);
        // ---- Read response from upstream ----
        string respHeaders = readHTTPHeaders(upstreamBuf);
        if (respHeaders.length == 0) { logInfo("[ix-net] relay: upstream closed (no resp headers for %s)", path); break; }
        logInfo("[ix-net] relay: got response headers (%d bytes) for %s", respHeaders.length, path);

        // Forward response headers to client
        if (tlsWrite(clientTLS, cast(const ubyte[])respHeaders) <= 0) break;

        // Parse response status
        int statusCode = parseStatusCode(respHeaders);

        // Determine response body handling
        bool isChunked = containsHeader(respHeaders, "transfer-encoding", "chunked");
        size_t respContentLen = parseContentLength(respHeaders);
        size_t totalRespBytes = respHeaders.length;

        logInfo("[ix-net] relay: resp status=%d chunked=%s content-len=%d",
                statusCode, isChunked ? "yes" : "no", respContentLen);

        ResponseInfo respInfo;
        ubyte[] respBody;
        if (isChunked) {
            // Streaming/chunked: read chunks, forward, accumulate for extraction
            respBody = readAndForwardChunked(upstreamBuf, clientTLS);
            totalRespBytes += respBody.length;
            respInfo = extractStreamingResponse(respBody);
        } else if (respContentLen > 0) {
            respBody = readAndForward(upstreamBuf, clientTLS, respContentLen);
            totalRespBytes += respBody.length;
            respInfo = extractResponse(respBody);
        }

        // Debug: dump first /v1/messages exchange to /tmp/ix-net-dump/
        if (startsWith(path, "/v1/messages")) {
            import std.file : mkdirRecurse, write_ = write, exists;
            enum DUMP_DIR = "/tmp/ix-net-dump";
            if (!exists(DUMP_DIR ~ "/req-body.json")) {
                try {
                    mkdirRecurse(DUMP_DIR);
                    write_(DUMP_DIR ~ "/req-headers.txt", cast(const(void)[])reqHeaders);
                    if (reqBody.length > 0)
                        write_(DUMP_DIR ~ "/req-body.json", cast(const(void)[])reqBody);
                    write_(DUMP_DIR ~ "/resp-headers.txt", cast(const(void)[])respHeaders);
                    if (respBody.length > 0)
                        write_(DUMP_DIR ~ "/resp-body.txt", cast(const(void)[])respBody);
                    logInfo("[ix-net] dumped /v1/messages exchange to %s", DUMP_DIR);
                } catch (Exception) {}
            }
        }

        // Record the capture with all extracted info
        Capture cap;
        cap.method = method;
        cap.path = path.length > 0 ? path : hostPort;
        cap.requestSize = totalReqBytes;
        cap.responseSize = totalRespBytes;
        cap.model = reqInfo.model;
        cap.hasImages = reqInfo.hasImages;
        cap.imageCount = reqInfo.imageCount;
        cap.streaming = reqInfo.streaming || isChunked;
        cap.statusCode = statusCode;
        cap.inputTokens = respInfo.inputTokens;
        cap.outputTokens = respInfo.outputTokens;
        {
            import core.stdc.time : time;
            cap.timestamp = cast(long)time(null);
        }

        auto idx = state.captureHead % MAX_CAPTURES;
        state.captures[idx] = cap;
        state.captureHead = (state.captureHead + 1) % MAX_CAPTURES;
        state.captureCount++;

        // Only log actual API calls, not telemetry/health noise
        if (startsWith(path, "/v1/messages")) {
            logInfo("[ix-net] %s %s model=%s status=%d req=%dB resp=%dB images=%d in_tok=%d out_tok=%d",
                    method, path, reqInfo.model, statusCode,
                    totalReqBytes, totalRespBytes, reqInfo.imageCount,
                    respInfo.inputTokens, respInfo.outputTokens);
        }
    }
}

// ---------------------------------------------------------------------------
// Buffered TLS reader
// ---------------------------------------------------------------------------

/// Buffered reader over a TLS connection for line-oriented HTTP parsing.
private struct TLSBufReader {
    TLSConn* conn;
    ubyte[32768] buf;
    size_t pos = 0;
    size_t len = 0;
    bool eof = false;

    /// Read a single byte. Returns -1 on EOF/hard error.
    /// Retries on would-block (essential for streaming responses).
    int readByte() {
        if (eof) return -1;

        if (pos >= len) {
            // Refill buffer — retry on would-block
            int retries = 0;
            while (true) {
                int n = tlsRead(*conn, buf[]);
                if (n > 0) {
                    pos = 0;
                    len = n;
                    break;
                } else if (n == 0) {
                    eof = true;
                    return -1; // clean close
                } else if (n == -2) {
                    eof = true;
                    return -1; // hard error
                }
                // n == -1: would-block — retry with brief pause
                // Streaming responses can pause for 60+ seconds during thinking
                retries++;
                if (retries > 12000) { // ~120 seconds of no data
                    eof = true;
                    return -1;
                }
                import core.thread : Thread;
                import core.time : dur;
                Thread.sleep(dur!"msecs"(10));
            }
        }
        return buf[pos++];
    }
}

/// Read HTTP headers (up to and including the blank line \r\n\r\n).
/// Returns the full header block as a string, or "" on EOF.
private string readHTTPHeaders(ref TLSBufReader reader) {
    ubyte[] headers;
    // Read until we see \r\n\r\n
    while (true) {
        int b = reader.readByte();
        if (b < 0) return "";
        headers ~= cast(ubyte)b;

        // Check for end of headers: \r\n\r\n
        if (headers.length >= 4 &&
            headers[$ - 4] == '\r' && headers[$ - 3] == '\n' &&
            headers[$ - 2] == '\r' && headers[$ - 1] == '\n') {
            return cast(string)headers.idup;
        }

        // Safety: headers shouldn't be more than 64KB
        if (headers.length > 65536) {
            logWarn("[ix-net] proxy: headers exceeded 64KB, truncating");
            return cast(string)headers.idup;
        }
    }
}

/// Read exactly `n` bytes from reader and forward to dest TLS conn.
/// Returns the accumulated body.
private ubyte[] readAndForward(ref TLSBufReader reader, ref TLSConn dest, size_t n) {
    ubyte[] body_;
    body_.reserve(n);
    ubyte[16384] chunk;
    size_t remaining = n;

    while (remaining > 0) {
        size_t toRead = remaining < chunk.length ? remaining : chunk.length;
        size_t got = 0;
        foreach (i; 0 .. toRead) {
            int b = reader.readByte();
            if (b < 0) {
                // Forward what we have and return
                if (got > 0) {
                    tlsWrite(dest, chunk[0 .. got]);
                    body_ ~= chunk[0 .. got];
                }
                return body_;
            }
            chunk[got++] = cast(ubyte)b;
        }
        tlsWrite(dest, chunk[0 .. got]);
        body_ ~= chunk[0 .. got];
        remaining -= got;
    }
    return body_;
}

/// Read chunked transfer encoding from reader, forward each chunk immediately.
/// Returns accumulated decoded body (for extraction).
private ubyte[] readAndForwardChunked(ref TLSBufReader reader, ref TLSConn dest) {
    ubyte[] body_;

    while (true) {
        // Read chunk size line
        ubyte[] sizeLine;
        while (true) {
            int b = reader.readByte();
            if (b < 0) return body_;
            sizeLine ~= cast(ubyte)b;
            if (sizeLine.length >= 2 &&
                sizeLine[$ - 2] == '\r' && sizeLine[$ - 1] == '\n') break;
        }

        // Forward the size line immediately
        tlsWrite(dest, sizeLine);

        // Parse hex chunk size
        size_t chunkSize = parseHexSize(cast(string)sizeLine);

        if (chunkSize == 0) {
            // Terminal chunk "0\r\n" — read and forward trailing \r\n
            ubyte[2] trail;
            int b1 = reader.readByte();
            int b2 = reader.readByte();
            size_t tLen = 0;
            if (b1 >= 0) trail[tLen++] = cast(ubyte)b1;
            if (b2 >= 0) trail[tLen++] = cast(ubyte)b2;
            if (tLen > 0) tlsWrite(dest, trail[0 .. tLen]);
            break;
        }

        // Read chunk data and forward immediately
        ubyte[16384] chunkBuf;
        size_t remaining = chunkSize;
        while (remaining > 0) {
            size_t toRead = remaining < chunkBuf.length ? remaining : chunkBuf.length;
            size_t got = 0;
            foreach (i; 0 .. toRead) {
                int b = reader.readByte();
                if (b < 0) {
                    if (got > 0) {
                        tlsWrite(dest, chunkBuf[0 .. got]);
                        body_ ~= chunkBuf[0 .. got];
                    }
                    return body_;
                }
                chunkBuf[got++] = cast(ubyte)b;
            }
            tlsWrite(dest, chunkBuf[0 .. got]);
            body_ ~= chunkBuf[0 .. got];
            remaining -= got;
        }

        // Read and forward trailing \r\n after chunk data
        ubyte[2] crlf;
        size_t cLen = 0;
        int cr = reader.readByte();
        int lf = reader.readByte();
        if (cr >= 0) crlf[cLen++] = cast(ubyte)cr;
        if (lf >= 0) crlf[cLen++] = cast(ubyte)lf;
        if (cLen > 0) tlsWrite(dest, crlf[0 .. cLen]);
    }

    return body_;
}

/// Parse hex chunk size from "1a2b\r\n".
private size_t parseHexSize(string line) {
    size_t result = 0;
    foreach (c; line) {
        if (c >= '0' && c <= '9') {
            result = result * 16 + (c - '0');
        } else if (c >= 'a' && c <= 'f') {
            result = result * 16 + (c - 'a' + 10);
        } else if (c >= 'A' && c <= 'F') {
            result = result * 16 + (c - 'A' + 10);
        } else {
            break; // \r or other delimiter
        }
    }
    return result;
}

/// Parse Content-Length from headers.
private size_t parseContentLength(string headers) {
    auto idx = findHeaderValue(headers, "content-length");
    if (idx.length == 0) return 0;
    size_t result = 0;
    foreach (c; idx) {
        if (c >= '0' && c <= '9') result = result * 10 + (c - '0');
        else break;
    }
    return result;
}

/// Parse HTTP status code from "HTTP/1.1 200 OK\r\n..."
private int parseStatusCode(string headers) {
    // Find first space, then parse 3 digits
    size_t i = 0;
    while (i < headers.length && headers[i] != ' ') i++;
    i++; // skip space
    if (i + 3 > headers.length) return 0;
    int code = 0;
    foreach (j; 0 .. 3) {
        char c = headers[i + j];
        if (c >= '0' && c <= '9') code = code * 10 + (c - '0');
        else return 0;
    }
    return code;
}

/// Check if headers contain a specific header with a specific value (case-insensitive).
private bool containsHeader(string headers, string name, string value) {
    auto val = findHeaderValue(headers, name);
    if (val.length == 0) return false;
    // Case-insensitive compare
    if (val.length < value.length) return false;
    foreach (i; 0 .. value.length) {
        char a = val[i];
        char b = value[i];
        if (a >= 'A' && a <= 'Z') a += 32;
        if (b >= 'A' && b <= 'Z') b += 32;
        if (a != b) return false;
    }
    return true;
}

/// Find a header value by name (case-insensitive).
private string findHeaderValue(string headers, string name) {
    size_t pos = 0;
    while (pos < headers.length) {
        // Find start of line
        size_t lineStart = pos;
        // Find end of line
        size_t lineEnd = pos;
        while (lineEnd < headers.length && headers[lineEnd] != '\r' && headers[lineEnd] != '\n')
            lineEnd++;

        auto line = headers[lineStart .. lineEnd];

        // Check if this line starts with name (case-insensitive)
        if (line.length > name.length + 1) {
            bool match = true;
            foreach (i; 0 .. name.length) {
                char a = line[i];
                char b = name[i];
                if (a >= 'A' && a <= 'Z') a += 32;
                if (b >= 'A' && b <= 'Z') b += 32;
                if (a != b) { match = false; break; }
            }
            if (match && line[name.length] == ':') {
                // Skip ": " and return value
                size_t valStart = name.length + 1;
                while (valStart < line.length && line[valStart] == ' ') valStart++;
                return cast(string)line[valStart .. $];
            }
        }

        // Advance past \r\n
        pos = lineEnd;
        if (pos < headers.length && headers[pos] == '\r') pos++;
        if (pos < headers.length && headers[pos] == '\n') pos++;
    }
    return "";
}

/// Record a capture entry in the ring buffer.
private void recordCapture(ProxyState* state, string hostPort,
                           string method, size_t reqBytes, size_t respBytes) {
    import core.stdc.time : time;
    auto idx = state.captureHead % MAX_CAPTURES;
    state.captures[idx] = Capture.init;
    state.captures[idx].method = method.length > 0 ? method : "CONNECT";
    state.captures[idx].path = hostPort;
    state.captures[idx].timestamp = cast(long)time(null);
    state.captures[idx].requestSize = reqBytes;
    state.captures[idx].responseSize = respBytes;
    state.captureHead = (state.captureHead + 1) % MAX_CAPTURES;
    state.captureCount++;
}

/// Parse HTTP request line from raw bytes: "POST /v1/messages HTTP/1.1\r\n..."
private void parseRequestLine(const ubyte[] data, ref string method, ref string path) {
    // Find first line
    size_t lineEnd = 0;
    while (lineEnd < data.length && data[lineEnd] != '\r' && data[lineEnd] != '\n') lineEnd++;
    if (lineEnd == 0) return;

    auto line = cast(string)data[0 .. lineEnd];

    // Split "POST /v1/messages HTTP/1.1"
    size_t spaceIdx = 0;
    while (spaceIdx < line.length && line[spaceIdx] != ' ') spaceIdx++;
    if (spaceIdx >= line.length) return;

    method = line[0 .. spaceIdx].idup;

    size_t pathStart = spaceIdx + 1;
    size_t pathEnd = pathStart;
    while (pathEnd < line.length && line[pathEnd] != ' ') pathEnd++;
    path = line[pathStart .. pathEnd].idup;
}

/// Connect to an upstream host:port via TCP.
private Socket connectUpstream(string hostPort) {
    // Parse host:port
    auto colonIdx = lastIndexOfChar(hostPort, ':');
    if (colonIdx < 0) return null;

    auto host = hostPort[0 .. colonIdx];
    ushort port;
    try {
        port = hostPort[colonIdx + 1 .. $].to!ushort;
    } catch (Exception) {
        return null;
    }

    auto sock = new TcpSocket();
    try {
        sock.connect(new InternetAddress(host, port));
        return sock;
    } catch (SocketOSException) {
        sock.close();
        return null;
    }
}

/// Blind byte relay between two sockets.
/// Runs until either side closes.
private void relay(Socket a, Socket b) {
    import core.time : dur;

    auto set = new SocketSet(2);
    ubyte[16384] buf;

    // Set both sockets non-blocking for select
    a.setOption(SocketOptionLevel.SOCKET, SocketOption.RCVTIMEO, dur!"msecs"(100));
    b.setOption(SocketOptionLevel.SOCKET, SocketOption.RCVTIMEO, dur!"msecs"(100));

    while (true) {
        // Try reading from a → b
        auto n = a.receive(buf[]);
        if (n > 0) {
            auto sent = b.send(buf[0 .. n]);
            if (sent <= 0) break;
        } else if (n == 0) {
            break; // connection closed
        }
        // n < 0 means timeout/EAGAIN, continue

        // Try reading from b → a
        n = b.receive(buf[]);
        if (n > 0) {
            auto sent = a.send(buf[0 .. n]);
            if (sent <= 0) break;
        } else if (n == 0) {
            break;
        }
    }
}

// ---------------------------------------------------------------------------
// HTTP helpers (plain text, not the gRPC protocol)
// ---------------------------------------------------------------------------

/// Read one line from socket (up to \r\n or \n).
private string readLine(Socket sock) {
    char[] line;
    ubyte[1] b;
    while (true) {
        auto n = sock.receive(b[]);
        if (n <= 0) break;
        if (b[0] == '\n') break;
        if (b[0] != '\r') line ~= cast(char)b[0];
    }
    return cast(string)line.idup;
}

/// Send a raw string response.
private void sendResponse(Socket sock, string data) {
    sock.send(cast(const(ubyte)[])data);
}

/// Extract host:port from "CONNECT host:port HTTP/1.1".
private string extractHostPort(string requestLine) {
    // Skip "CONNECT "
    if (requestLine.length < 9) return "";
    auto rest = requestLine[8 .. $];
    // Find the space before "HTTP/1.1"
    foreach (i; 0 .. rest.length) {
        if (rest[i] == ' ') return cast(string)rest[0 .. i];
    }
    return cast(string)rest;
}

private ptrdiff_t lastIndexOfChar(string s, char c) {
    for (ptrdiff_t i = cast(ptrdiff_t)s.length - 1; i >= 0; i--) {
        if (s[i] == c) return i;
    }
    return -1;
}

private bool startsWith(string s, string prefix) {
    if (prefix.length > s.length) return false;
    return s[0 .. prefix.length] == prefix;
}

/// Remove a header line by name (case-insensitive) from HTTP headers block.
/// Also updates Content-Length in the trailing \r\n\r\n-terminated block.
private string stripHeader(string headers, string name) {
    char[] result;
    size_t pos = 0;
    while (pos < headers.length) {
        size_t lineStart = pos;
        size_t lineEnd = pos;
        while (lineEnd < headers.length && headers[lineEnd] != '\r' && headers[lineEnd] != '\n')
            lineEnd++;

        auto line = headers[lineStart .. lineEnd];

        // Check if this line starts with the header name (case-insensitive)
        bool skip = false;
        if (line.length > name.length + 1) {
            bool match = true;
            foreach (i; 0 .. name.length) {
                char a = line[i];
                char b = name[i];
                if (a >= 'A' && a <= 'Z') a += 32;
                if (b >= 'A' && b <= 'Z') b += 32;
                if (a != b) { match = false; break; }
            }
            if (match && line[name.length] == ':') skip = true;
        }

        // Advance past \r\n
        size_t nextPos = lineEnd;
        if (nextPos < headers.length && headers[nextPos] == '\r') nextPos++;
        if (nextPos < headers.length && headers[nextPos] == '\n') nextPos++;

        if (!skip) {
            result ~= headers[lineStart .. nextPos];
        }
        pos = nextPos;
    }
    return cast(string)result.idup;
}
