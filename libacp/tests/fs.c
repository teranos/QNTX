// fs/read_text_file, as #156 asks it of the client: the request, and the text it answers with;
// fs/write_text_file: the request, and whether the client wrote the file.
#include "acp.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

static int says(const acp_buffer *b, const char *want) {
    return b->data != NULL && strcmp(b->data, want) == 0;
}

// The whole request: its id, the session, and the absolute path, written as it is.
static void request(void) {
    acp_buffer out = {0};
    CHECK(acp_read_text_file_request_write("\"read-1\"", "code-1-1-1", "/tmp/a b/c\"d", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"method\":\"fs/read_text_file\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"path\":\"/tmp/a b/c\\\"d\"}}"));
    acp_buffer_free(&out);
    CHECK(acp_read_text_file_request_write("read-1", "code-1-1-1", "/tmp/a", &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_read_text_file_request_write("\"read-1\"", "code-1-1-1", NULL, &out) == ACP_INVALID_ARGUMENT);
    // "Line number to start reading from (1-based)" and "Maximum number of lines to read", each
    // written when given, 0 being none.
    CHECK(acp_read_text_file_lines_request_write("\"read-2\"", "code-1-1-1", "/tmp/a", 10, 50, &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"read-2\",\"method\":\"fs/read_text_file\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"path\":\"/tmp/a\",\"line\":10,\"limit\":50}}"));
    acp_buffer_free(&out);
    CHECK(acp_read_text_file_lines_request_write("\"read-2\"", "code-1-1-1", "/tmp/a", 0, 3, &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"read-2\",\"method\":\"fs/read_text_file\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"path\":\"/tmp/a\",\"limit\":3}}"));
    acp_buffer_free(&out);
}

static acp_status answer(const char *message, acp_text_file *out) {
    memset(out, 0, sizeof *out);
    return acp_read_text_file_result_read(message, strlen(message), out);
}

// result.content and its every byte, a NUL among them; any other answer is no text, and the one
// whose error is -32002, "Resource not found: A given resource, such as a file, was not found",
// says the file is not there.
static void answers(void) {
    acp_text_file file;
    CHECK(answer("{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"result\":{\"content\":\"line one\\nline two\"}}", &file) == ACP_OK);
    CHECK(file.content && file.length == 17 && memcmp(file.content, "line one\nline two", 17) == 0);
    acp_text_file_free(&file);
    CHECK(file.content == NULL && file.length == 0);
    CHECK(answer("{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"result\":{\"content\":\"a\\u0000b\"}}", &file) == ACP_OK);
    CHECK(file.content && file.length == 3 && memcmp(file.content, "a\0b", 3) == 0);
    acp_text_file_free(&file);
    const char *none[] = {
        "{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"error\":{\"code\":-32002,\"message\":\"no such file\"}}",
        "{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"result\":{\"content\":7}}",
        "{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"result\":{}}",
        "{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"error\":{\"code\":-32603,\"message\":\"no such file\"}}",
        "{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"error\":{\"code\":\"-32002\"}}",
        "{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"result\":{\"content\":7}}",
        "{\"jsonrpc\":\"2.0\",\"id\":\"read-1\",\"result\":{}}",
        "not json",
    };
    for (size_t i = 0; i < sizeof none / sizeof *none; i++) {
        CHECK(answer(none[i], &file) == ACP_OK);
        CHECK(file.content == NULL && file.length == 0);
        CHECK(file.not_found == (i == 0));
        // Freed, it is no file at all, found or not.
        acp_text_file_free(&file);
        CHECK(file.content == NULL && file.length == 0 && file.not_found == 0);
    }
}

// fs/write_text_file's whole request: the content's every byte, a NUL among them, as JSON escapes it.
static void write_request(void) {
    acp_buffer out = {0};
    CHECK(acp_write_text_file_request_write("\"write-1\"", "code-1-1-1", "/tmp/a b/c\"d", "one\0two\n", 8, &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"write-1\",\"method\":\"fs/write_text_file\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"path\":\"/tmp/a b/c\\\"d\",\"content\":\"one\\u0000two\\n\"}}"));
    acp_buffer_free(&out);
    CHECK(acp_write_text_file_request_write("\"write-1\"", "code-1-1-1", "/tmp/empty", "", 0, &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"write-1\",\"method\":\"fs/write_text_file\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"path\":\"/tmp/empty\",\"content\":\"\"}}"));
    acp_buffer_free(&out);
    CHECK(acp_write_text_file_request_write("write-1", "code-1-1-1", "/tmp/a", "x", 1, &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_write_text_file_request_write("\"write-1\"", "code-1-1-1", "/tmp/a", NULL, 0, &out) == ACP_INVALID_ARGUMENT);
}

static int written(const char *message) {
    int got = -1;
    if (acp_write_text_file_result_read(message, strlen(message), &got) != ACP_OK) return -1;
    return got;
}

// WriteTextFileResponse is an object: a result that is one is the file written; any other answer is not.
static void write_answers(void) {
    CHECK(written("{\"jsonrpc\":\"2.0\",\"id\":\"write-1\",\"result\":{}}") == 1);
    CHECK(written("{\"jsonrpc\":\"2.0\",\"id\":\"write-1\",\"result\":{\"_meta\":{\"x\":1}}}") == 1);
    CHECK(written("{\"jsonrpc\":\"2.0\",\"id\":\"write-1\",\"error\":{\"code\":-32002,\"message\":\"read-only\"}}") == 0);
    CHECK(written("{\"jsonrpc\":\"2.0\",\"id\":\"write-1\",\"result\":null}") == 0);
    CHECK(written("not json") == 0);
    CHECK(acp_write_text_file_result_read("{}", 2, NULL) == ACP_INVALID_ARGUMENT);
}

int main(void) {
    request();
    answers();
    write_request();
    write_answers();
    if (failures) {
        fprintf(stderr, "fs: %d failed\n", failures);
        return 1;
    }
    printf("fs: fs/read_text_file's and fs/write_text_file's requests and answers passed\n");
    return 0;
}
