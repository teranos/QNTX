/// Structured JSON logging to stderr.
///
/// QNTX plugin loader parses JSON log lines from stderr and routes
/// them by level (discovery.go:656-663). Plain text defaults to ERROR.
module ixbin.log;

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

    // Write JSON — escape msg for safe embedding
    stderr.write(`{"level":"`, level, `","msg":"`);
    foreach (c; msg) {
        switch (c) {
            case '"':  stderr.write(`\"`); break;
            case '\\': stderr.write(`\\`); break;
            case '\n': stderr.write(`\n`); break;
            case '\r': stderr.write(`\r`); break;
            case '\t': stderr.write(`\t`); break;
            default:   stderr.write(c); break;
        }
    }
    stderr.writeln(`"}`);
    stderr.flush();
}
