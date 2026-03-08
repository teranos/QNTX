/// Structured JSON logging to stderr.
///
/// QNTX plugin loader parses JSON log lines from stderr and routes
/// them by level (discovery.go:656-663). Plain text defaults to ERROR.
///
/// Thread-safe: builds the full line in memory, writes in one call.
module ixnet.log;

import std.stdio : stderr;

void logInfo(Args...)(string fmt, Args args) {
    writeJsonLog("info", fmt, args);
}

void logWarn(Args...)(string fmt, Args args) {
    writeJsonLog("warn", fmt, args);
}

void logError(Args...)(string fmt, Args args) {
    writeJsonLog("error", fmt, args);
}

private void writeJsonLog(Args...)(string level, string fmt, Args args) {
    // Build message
    string msg;
    static if (Args.length == 0) {
        msg = fmt;
    } else {
        import std.format : format;
        msg = format(fmt, args);
    }

    // Build the full JSON line in memory first, then write atomically
    char[] line;
    line ~= `{"level":"`;
    line ~= level;
    line ~= `","msg":"`;
    foreach (c; msg) {
        switch (c) {
            case '"':  line ~= `\"`; break;
            case '\\': line ~= `\\`; break;
            case '\n': line ~= `\n`; break;
            case '\r': line ~= `\r`; break;
            case '\t': line ~= `\t`; break;
            default:   line ~= c; break;
        }
    }
    line ~= "\"}\n";

    // Single write — atomic for lines under PIPE_BUF (4096 on macOS)
    import core.sys.posix.unistd : write;
    write(2, line.ptr, line.length);
}
