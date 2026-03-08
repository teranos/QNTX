/// OpenSSL TLS wrapper for ix-net proxy MITM.
///
/// Provides TLS server context (client-facing, using leaf cert) and
/// TLS client context (upstream-facing, to api.anthropic.com).
/// Uses OpenSSL 3.x via extern(C) bindings — no dub dependency.
module ixnet.tls;

import ixnet.log;
import std.string : toStringz;
import std.socket : Socket;

// ---------------------------------------------------------------------------
// OpenSSL C bindings (minimal subset for TLS MITM)
// ---------------------------------------------------------------------------

extern(C) nothrow @nogc {
    // Method
    alias SSL_METHOD = void;
    SSL_METHOD* TLS_server_method();
    SSL_METHOD* TLS_client_method();

    // Context
    alias SSL_CTX = void;
    SSL_CTX* SSL_CTX_new(const SSL_METHOD* method);
    void SSL_CTX_free(SSL_CTX* ctx);
    int SSL_CTX_use_certificate_file(SSL_CTX* ctx, const char* file, int type);
    int SSL_CTX_use_PrivateKey_file(SSL_CTX* ctx, const char* file, int type);
    int SSL_CTX_check_private_key(const SSL_CTX* ctx);

    // SSL
    alias SSL = void;
    SSL* SSL_new(SSL_CTX* ctx);
    void SSL_free(SSL* ssl);
    int SSL_set_fd(SSL* ssl, int fd);
    int SSL_accept(SSL* ssl);
    int SSL_connect(SSL* ssl);
    int SSL_read(SSL* ssl, void* buf, int num);
    int SSL_write(SSL* ssl, const void* buf, int num);
    int SSL_shutdown(SSL* ssl);
    int SSL_get_error(const SSL* ssl, int ret);

    // Init (OpenSSL 1.1+ auto-initializes, but explicit is fine)
    int OPENSSL_init_ssl(ulong opts, const void* settings);

    // Error
    ulong ERR_get_error();
    void ERR_error_string_n(ulong e, char* buf, size_t len);

    // SNI
    enum SSL_CTRL_SET_TLSEXT_HOSTNAME = 55;
    enum TLSEXT_NAMETYPE_host_name = 0;
    long SSL_ctrl(SSL* ssl, int cmd, long larg, void* parg);

    // ALPN — force HTTP/1.1 on both sides
    int SSL_CTX_set_alpn_protos(SSL_CTX* ctx, const ubyte* protos, uint protos_len);
    alias alpn_select_cb_t = extern(C) int function(
        SSL* ssl, ubyte** out_, ubyte* outlen,
        const ubyte* in_, uint inlen, void* arg) nothrow @nogc;
    void SSL_CTX_set_alpn_select_cb(SSL_CTX* ctx, alpn_select_cb_t cb, void* arg);

    // Constants
    enum SSL_FILETYPE_PEM = 1;
    enum SSL_ERROR_WANT_READ = 2;
    enum SSL_ERROR_WANT_WRITE = 3;
    enum SSL_ERROR_SYSCALL = 5;
    enum SSL_ERROR_ZERO_RETURN = 6;
    enum OPENSSL_INIT_LOAD_SSL_STRINGS = 0x00200000;
    enum OPENSSL_INIT_LOAD_CRYPTO_STRINGS = 0x00000002;
}

// ---------------------------------------------------------------------------
// TLS context management
// ---------------------------------------------------------------------------

/// Opaque TLS connection handle.
struct TLSConn {
    SSL* ssl;
    bool established;
}

/// ALPN select callback: always choose http/1.1.
private extern(C) int alpnSelectHTTP11(
    SSL* ssl, ubyte** out_, ubyte* outlen,
    const ubyte* in_, uint inlen, void* arg) nothrow @nogc {
    // Walk the client's ALPN list looking for http/1.1
    uint pos = 0;
    while (pos < inlen) {
        ubyte plen = in_[pos];
        if (pos + 1 + plen > inlen) break;
        if (plen == 8 &&
            in_[pos+1]=='h' && in_[pos+2]=='t' && in_[pos+3]=='t' && in_[pos+4]=='p' &&
            in_[pos+5]=='/' && in_[pos+6]=='1' && in_[pos+7]=='.' && in_[pos+8]=='1') {
            *out_ = cast(ubyte*)&in_[pos + 1];
            *outlen = 8;
            return 0; // SSL_TLSEXT_ERR_OK
        }
        pos += 1 + plen;
    }
    // Client didn't offer http/1.1 — accept anyway with no ALPN
    return 3; // SSL_TLSEXT_ERR_NOACK
}

private __gshared bool tlsInitialized = false;
private __gshared SSL_CTX* serverCtx = null;  // for client-facing (leaf cert)
private __gshared SSL_CTX* clientCtx = null;  // for upstream connections

/// Initialize OpenSSL and load certificates.
/// certFile/keyFile = leaf cert for MITM.
/// Returns true on success.
bool tlsInit(string certFile, string keyFile) {
    if (tlsInitialized) return true;

    OPENSSL_init_ssl(
        OPENSSL_INIT_LOAD_SSL_STRINGS | OPENSSL_INIT_LOAD_CRYPTO_STRINGS,
        null
    );

    // Server context (client-facing) with leaf cert
    serverCtx = SSL_CTX_new(TLS_server_method());
    if (serverCtx is null) {
        logError("[ix-net] tls: failed to create server SSL_CTX");
        logOpenSSLError();
        return false;
    }

    if (SSL_CTX_use_certificate_file(serverCtx, toStringz(certFile), SSL_FILETYPE_PEM) != 1) {
        logError("[ix-net] tls: failed to load cert %s", certFile);
        logOpenSSLError();
        return false;
    }

    if (SSL_CTX_use_PrivateKey_file(serverCtx, toStringz(keyFile), SSL_FILETYPE_PEM) != 1) {
        logError("[ix-net] tls: failed to load key %s", keyFile);
        logOpenSSLError();
        return false;
    }

    if (SSL_CTX_check_private_key(serverCtx) != 1) {
        logError("[ix-net] tls: cert/key mismatch");
        logOpenSSLError();
        return false;
    }

    // Force HTTP/1.1 on client-facing side too — we parse text HTTP, not h2 frames
    SSL_CTX_set_alpn_select_cb(serverCtx, &alpnSelectHTTP11, null);

    // Client context (upstream-facing) — no cert needed, just TLS client
    clientCtx = SSL_CTX_new(TLS_client_method());
    if (clientCtx is null) {
        logError("[ix-net] tls: failed to create client SSL_CTX");
        logOpenSSLError();
        return false;
    }

    // Force HTTP/1.1 via ALPN — we parse HTTP/1.1 text, not HTTP/2 binary frames
    static immutable ubyte[] alpn = [8, 'h','t','t','p','/','1','.','1'];
    SSL_CTX_set_alpn_protos(clientCtx, alpn.ptr, cast(uint)alpn.length);

    tlsInitialized = true;
    logInfo("[ix-net] tls: initialized (cert=%s)", certFile);
    return true;
}

/// Clean up OpenSSL contexts.
void tlsCleanup() {
    if (serverCtx !is null) { SSL_CTX_free(serverCtx); serverCtx = null; }
    if (clientCtx !is null) { SSL_CTX_free(clientCtx); clientCtx = null; }
    tlsInitialized = false;
}

/// Wrap an accepted client socket in TLS (server-side handshake).
/// The socket must already be connected (from proxy accept).
TLSConn tlsAccept(Socket sock) {
    TLSConn conn;
    if (serverCtx is null) return conn;

    conn.ssl = SSL_new(serverCtx);
    if (conn.ssl is null) {
        logError("[ix-net] tls: SSL_new(server) failed");
        return conn;
    }

    SSL_set_fd(conn.ssl, sock.handle);

    int ret = SSL_accept(conn.ssl);
    if (ret != 1) {
        int err = SSL_get_error(conn.ssl, ret);
        logError("[ix-net] tls: SSL_accept failed (err=%d)", err);
        logOpenSSLError();
        SSL_free(conn.ssl);
        conn.ssl = null;
        return conn;
    }

    conn.established = true;
    return conn;
}

/// Wrap an outgoing socket in TLS (client-side handshake to upstream).
/// hostname is used for SNI (required by most modern servers).
TLSConn tlsConnect(Socket sock, string hostname = "") {
    TLSConn conn;
    if (clientCtx is null) return conn;

    conn.ssl = SSL_new(clientCtx);
    if (conn.ssl is null) {
        logError("[ix-net] tls: SSL_new(client) failed");
        return conn;
    }

    SSL_set_fd(conn.ssl, sock.handle);

    // Set SNI hostname
    if (hostname.length > 0) {
        SSL_ctrl(conn.ssl, SSL_CTRL_SET_TLSEXT_HOSTNAME,
                 TLSEXT_NAMETYPE_host_name,
                 cast(void*)toStringz(hostname));
    }

    int ret = SSL_connect(conn.ssl);
    if (ret != 1) {
        int err = SSL_get_error(conn.ssl, ret);
        logError("[ix-net] tls: SSL_connect failed (err=%d)", err);
        logOpenSSLError();
        SSL_free(conn.ssl);
        conn.ssl = null;
        return conn;
    }

    conn.established = true;
    return conn;
}

/// Read from TLS connection. Returns bytes read, 0 on close, -1 on error.
int tlsRead(ref TLSConn conn, ubyte[] buf) {
    if (!conn.established) return -1;
    int n = SSL_read(conn.ssl, buf.ptr, cast(int)buf.length);
    if (n <= 0) {
        int err = SSL_get_error(conn.ssl, n);
        if (err == SSL_ERROR_ZERO_RETURN) return 0;     // clean shutdown
        if (err == SSL_ERROR_WANT_READ) return -1;       // would block
        if (err == SSL_ERROR_WANT_WRITE) return -1;      // would block
        return -2; // real error
    }
    return n;
}

/// Write to TLS connection. Returns bytes written, or <= 0 on error.
int tlsWrite(ref TLSConn conn, const ubyte[] data) {
    if (!conn.established) return -1;
    return SSL_write(conn.ssl, data.ptr, cast(int)data.length);
}

/// Shut down and free a TLS connection.
void tlsClose(ref TLSConn conn) {
    if (conn.ssl !is null) {
        SSL_shutdown(conn.ssl);
        SSL_free(conn.ssl);
        conn.ssl = null;
    }
    conn.established = false;
}

// ---------------------------------------------------------------------------
// Error reporting
// ---------------------------------------------------------------------------

private void logOpenSSLError() {
    char[256] buf;
    while (true) {
        ulong e = ERR_get_error();
        if (e == 0) break;
        ERR_error_string_n(e, buf.ptr, buf.length);
        auto msg = cast(string)buf[0 .. strlen(buf)];
        logError("[ix-net] tls: openssl: %s", msg);
    }
}

private size_t strlen(char[256] buf) {
    foreach (i; 0 .. buf.length) {
        if (buf[i] == 0) return i;
    }
    return buf.length;
}
