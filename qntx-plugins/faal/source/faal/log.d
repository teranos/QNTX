/// Structured JSON logging to stderr.
module faal.log;

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
    string msg;
    static if (Args.length == 0) {
        msg = fmt;
    } else {
        import std.format : format;
        msg = format(fmt, args);
    }

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

    import core.sys.posix.unistd : write;
    write(2, line.ptr, line.length);
}
