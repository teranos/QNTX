/// Minimal HTTP/1.1 client for llama-server.
///
/// Sends POST requests to the /completion endpoint with n_probs
/// and parses the JSON response including probability data.
module infer.http;

import std.socket;
import std.conv : to;
import core.time : dur;
import infer.log;

/// Token probability from llama-server response.
struct TokenProb {
    string token;
    double prob;
    int id;
}

/// Per-token probability data from llama-server.
struct TokenProbEntry {
    string token;    // The selected token text
    double prob;     // Probability of the selected token
    int id;          // Token ID
    TokenProb[] topProbs; // Top-N alternatives with probabilities
}

/// Parsed llama-server /completion response.
struct CompletionResponse {
    string content;
    TokenProbEntry[] probs;
    bool truncated;   // Whether generation hit the token limit
    string model;
    string error;     // Non-empty on failure
}

/// HTTP client for llama-server.
struct LlamaServerClient {
    string host;
    ushort port;

    /// Configure the client endpoint from a URL like "http://127.0.0.1:8080".
    bool configure(string url) {
        // Strip scheme
        string addr = url;
        if (addr.length > 7 && addr[0 .. 7] == "http://") {
            addr = addr[7 .. $];
        } else if (addr.length > 8 && addr[0 .. 8] == "https://") {
            addr = addr[8 .. $];
            logWarn("[infer] HTTPS not supported, using HTTP for %s", url);
        }

        // Strip trailing slash
        if (addr.length > 0 && addr[$ - 1] == '/') {
            addr = addr[0 .. $ - 1];
        }

        // Split host:port
        auto colonIdx = lastIndexOf(addr, ':');
        if (colonIdx < 0) {
            host = addr;
            port = 8080;
        } else {
            host = addr[0 .. colonIdx];
            try {
                port = addr[colonIdx + 1 .. $].to!ushort;
            } catch (Exception) {
                logError("[infer] invalid port in URL %s", url);
                return false;
            }
        }
        return true;
    }

    /// Send a completion request to llama-server with probability output.
    CompletionResponse complete(string prompt, int nPredict, int nProbs, double temperature) {
        CompletionResponse result;

        // Build JSON request body
        auto body_ = buildRequestJson(prompt, nPredict, nProbs, temperature);

        // Connect
        Socket sock;
        try {
            sock = new TcpSocket();
            sock.setOption(SocketOptionLevel.SOCKET, SocketOption.RCVTIMEO, dur!"seconds"(120));
            sock.setOption(SocketOptionLevel.SOCKET, SocketOption.SNDTIMEO, dur!"seconds"(10));
            sock.connect(new InternetAddress(host, port));
        } catch (Exception e) {
            result.error = "TCP connect to " ~ host ~ ":" ~ port.to!string ~ " failed: " ~ e.msg;
            return result;
        }
        scope(exit) sock.close();

        // Send HTTP request
        auto request = "POST /completion HTTP/1.1\r\n" ~
                       "Host: " ~ host ~ ":" ~ port.to!string ~ "\r\n" ~
                       "Content-Type: application/json\r\n" ~
                       "Content-Length: " ~ body_.length.to!string ~ "\r\n" ~
                       "Connection: close\r\n\r\n" ~ body_;

        auto sent = sock.send(cast(const(ubyte)[])request);
        if (sent != request.length) {
            result.error = "failed to send request to " ~ host ~ ":" ~ port.to!string;
            return result;
        }

        // Read response
        auto responseBytes = readAll(sock);
        if (responseBytes.length == 0) {
            result.error = "empty response from " ~ host ~ ":" ~ port.to!string;
            return result;
        }

        auto response = cast(string)responseBytes;

        // Split headers and body
        auto headerEnd = indexOf(response, "\r\n\r\n");
        if (headerEnd < 0) {
            result.error = "malformed HTTP response from " ~ host ~ ":" ~ port.to!string;
            return result;
        }

        auto headers = response[0 .. headerEnd];
        auto responseBody = response[headerEnd + 4 .. $];

        // Check status code
        if (headers.length < 12 || headers[9 .. 12] != "200") {
            auto statusEnd = indexOf(headers, "\r\n");
            auto statusLine = statusEnd > 0 ? headers[0 .. statusEnd] : headers[0 .. headers.length > 80 ? 80 : $];
            result.error = "llama-server returned: " ~ statusLine;
            return result;
        }

        // Handle chunked transfer encoding
        if (indexOf(headers, "chunked") >= 0) {
            responseBody = decodeChunked(responseBody);
        }

        // Parse JSON response
        result = parseCompletionResponse(responseBody);
        return result;
    }
}

// ---------------------------------------------------------------------------
// JSON parser — hand-rolled, no regex
// ---------------------------------------------------------------------------

/// Parse the llama-server /completion JSON response.
private CompletionResponse parseCompletionResponse(string json) {
    import std.json;

    CompletionResponse result;
    JSONValue root;
    try {
        root = parseJSON(json);
    } catch (Exception e) {
        result.error = "JSON parse failed: " ~ e.msg;
        return result;
    }

    // Extract content
    if (auto p = "content" in root) {
        result.content = p.str;
    }

    // Extract model
    if (auto p = "model" in root) {
        result.model = p.str;
    }

    // Check if generation was truncated (stopped by limit)
    if (auto p = "stop_type" in root) {
        result.truncated = p.str == "limit";
    }

    // Extract probability data — post_sampling_probs format uses "prob" and "top_probs"
    if (auto pProbs = "completion_probabilities" in root) {
        foreach (entry; pProbs.array) {
            TokenProbEntry tpe;

            if (auto pc = "content" in entry) {
                tpe.token = pc.str;
            }

            // Parse the top probs array
            if (auto pTopProbs = "probs" in entry) {
                foreach (tp; pTopProbs.array) {
                    TokenProb candidate;
                    if (auto pt = "tok_str" in tp) {
                        candidate.token = pt.str;
                    }
                    if (auto pp = "prob" in tp) {
                        candidate.prob = jsonToDouble(pp);
                    }
                    tpe.topProbs ~= candidate;
                }
                // Selected token prob is the first entry
                if (tpe.topProbs.length > 0) {
                    tpe.prob = tpe.topProbs[0].prob;
                }
            }

            result.probs ~= tpe;
        }
    }

    // Also handle the newer format with "probs" at top level (llama.cpp recent versions)
    if (result.probs.length == 0) {
        if (auto pProbs = "probs" in root) {
            if (pProbs.type == JSONType.array) {
                foreach (entry; pProbs.array) {
                    TokenProbEntry tpe;

                    if (auto pt = "token" in entry) {
                        tpe.token = pt.str;
                    }
                    if (auto pi = "id" in entry) {
                        tpe.id = cast(int)pi.integer;
                    }
                    if (auto pp = "prob" in entry) {
                        tpe.prob = jsonToDouble(pp);
                    }

                    // Parse top_probs (post_sampling_probs format)
                    if (auto pTopProbs = "top_probs" in entry) {
                        foreach (tp; pTopProbs.array) {
                            TokenProb candidate;
                            if (auto pt2 = "token" in tp) {
                                candidate.token = pt2.str;
                            }
                            if (auto pp2 = "prob" in tp) {
                                candidate.prob = jsonToDouble(pp2);
                            }
                            if (auto pi2 = "id" in tp) {
                                candidate.id = cast(int)pi2.integer;
                            }
                            tpe.topProbs ~= candidate;
                        }
                    }

                    // Also handle top_logprobs (pre-sampling format) — convert from logprob
                    if (tpe.topProbs.length == 0) {
                        if (auto pTopLogprobs = "top_logprobs" in entry) {
                            foreach (tp; pTopLogprobs.array) {
                                TokenProb candidate;
                                if (auto pt2 = "token" in tp) {
                                    candidate.token = pt2.str;
                                }
                                if (auto plp = "logprob" in tp) {
                                    import std.math : exp;
                                    candidate.prob = exp(jsonToDouble(plp));
                                }
                                if (auto pi2 = "id" in tp) {
                                    candidate.id = cast(int)pi2.integer;
                                }
                                tpe.topProbs ~= candidate;
                            }
                        }
                    }

                    // If no explicit prob, use logprob
                    if (tpe.prob == 0.0) {
                        if (auto plp = "logprob" in entry) {
                            import std.math : exp;
                            tpe.prob = exp(jsonToDouble(plp));
                        }
                    }
                    // Fall back to first top prob
                    if (tpe.prob == 0.0 && tpe.topProbs.length > 0) {
                        tpe.prob = tpe.topProbs[0].prob;
                    }

                    result.probs ~= tpe;
                }
            }
        }
    }

    return result;
}

/// Build the JSON request for llama-server /completion.
private string buildRequestJson(string prompt, int nPredict, int nProbs, double temperature) {
    return `{"prompt":"` ~ escapeJsonString(prompt) ~
           `","n_predict":` ~ nPredict.to!string ~
           `,"n_probs":` ~ nProbs.to!string ~
           `,"post_sampling_probs":true` ~
           `,"temperature":` ~ formatDouble(temperature) ~
           `}`;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Read all bytes from a socket until connection close.
private ubyte[] readAll(Socket sock) {
    ubyte[] result;
    ubyte[8192] buf;
    while (true) {
        auto received = sock.receive(buf[]);
        if (received <= 0) break;
        result ~= buf[0 .. received];
    }
    return result;
}

/// Decode chunked transfer encoding.
private string decodeChunked(string data) {
    char[] result;
    size_t pos = 0;
    while (pos < data.length) {
        // Read chunk size (hex)
        auto lineEnd = indexOf(data[pos .. $], "\r\n");
        if (lineEnd < 0) break;
        auto hexStr = data[pos .. pos + lineEnd];
        size_t chunkSize = 0;
        foreach (c; hexStr) {
            chunkSize <<= 4;
            if (c >= '0' && c <= '9') chunkSize |= (c - '0');
            else if (c >= 'a' && c <= 'f') chunkSize |= (c - 'a' + 10);
            else if (c >= 'A' && c <= 'F') chunkSize |= (c - 'A' + 10);
        }
        pos += lineEnd + 2; // skip hex + \r\n
        if (chunkSize == 0) break; // terminal chunk
        if (pos + chunkSize > data.length) break;
        result ~= data[pos .. pos + chunkSize];
        pos += chunkSize + 2; // skip data + \r\n
    }
    return cast(string)result.idup;
}

/// Extract a double from a JSONValue that might be integer or floating.
private double jsonToDouble(scope const std.json.JSONValue* v) {
    import std.json : JSONType;
    if (v.type == JSONType.float_) return v.floating;
    if (v.type == JSONType.integer) return cast(double)v.integer;
    return 0.0;
}

/// Find first occurrence of substring.
private ptrdiff_t indexOf(string haystack, string needle) {
    if (needle.length == 0) return 0;
    if (needle.length > haystack.length) return -1;
    foreach (i; 0 .. haystack.length - needle.length + 1) {
        if (haystack[i .. i + needle.length] == needle) return cast(ptrdiff_t)i;
    }
    return -1;
}

/// Find last occurrence of a character.
private ptrdiff_t lastIndexOf(string s, char c) {
    for (ptrdiff_t i = cast(ptrdiff_t)s.length - 1; i >= 0; i--) {
        if (s[i] == c) return i;
    }
    return -1;
}

/// Escape a string for JSON embedding.
private string escapeJsonString(string s) {
    char[] result;
    foreach (c; s) {
        switch (c) {
            case '"':  result ~= `\"`; break;
            case '\\': result ~= `\\`; break;
            case '\n': result ~= `\n`; break;
            case '\r': result ~= `\r`; break;
            case '\t': result ~= `\t`; break;
            default:   result ~= c; break;
        }
    }
    return cast(string)result.idup;
}

/// Format a double without trailing garbage.
private string formatDouble(double v) {
    import std.format : format;
    return format("%.4f", v);
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    // Test URL parsing
    LlamaServerClient client;
    assert(client.configure("http://127.0.0.1:8080"));
    assert(client.host == "127.0.0.1");
    assert(client.port == 8080);

    LlamaServerClient client2;
    assert(client2.configure("http://localhost:9999/"));
    assert(client2.host == "localhost");
    assert(client2.port == 9999);

    LlamaServerClient client3;
    assert(client3.configure("myhost"));
    assert(client3.host == "myhost");
    assert(client3.port == 8080); // default

    // Test JSON building
    auto json = buildRequestJson("Hello world", 128, 10, 0.7);
    assert(indexOf(json, `"prompt":"Hello world"`) >= 0);
    assert(indexOf(json, `"n_probs":10`) >= 0);
    assert(indexOf(json, `"post_sampling_probs":true`) >= 0);

    // Test chunked decode
    auto chunked = "5\r\nHello\r\n6\r\n World\r\n0\r\n\r\n";
    assert(decodeChunked(chunked) == "Hello World");

    // Test JSON response parsing
    auto testJson = `{"content":"Paris","completion_probabilities":[` ~
        `{"content":" Paris","probs":[{"tok_str":" Paris","prob":0.95},{"tok_str":" London","prob":0.03}]}` ~
        `]}`;
    auto resp = parseCompletionResponse(testJson);
    assert(resp.content == "Paris");
    assert(resp.probs.length == 1);
    assert(resp.probs[0].token == " Paris");
    assert(resp.probs[0].topProbs.length == 2);
    assert(resp.probs[0].topProbs[0].prob > 0.9);
}
