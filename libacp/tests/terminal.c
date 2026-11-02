// terminal/*, as ground code asks it of the client: each request, and what the client answers.
#include "acp.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

static int says(const acp_buffer *b, const char *want) {
    return b->data != NULL && strcmp(b->data, want) == 0;
}

// terminal/create: the command, its args, the cwd and the limit; args, cwd and limit only when given.
static void create(void) {
    acp_buffer out = {0};
    const char *args[] = {"-c", "echo \"hi\" | wc -c"};
    CHECK(acp_terminal_create_request_write("\"term-1\"", "code-1-1-1", "sh", args, 2, "/tmp/a b", 65536, &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"term-1\",\"method\":\"terminal/create\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"command\":\"sh\",\"args\":[\"-c\",\"echo \\\"hi\\\" | wc -c\"],"
                     "\"cwd\":\"/tmp/a b\",\"outputByteLimit\":65536}}"));
    acp_buffer_free(&out);
    CHECK(acp_terminal_create_request_write("7", "code-1-1-1", "ls", NULL, 0, NULL, 0, &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":7,\"method\":\"terminal/create\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"command\":\"ls\"}}"));
    acp_buffer_free(&out);
    CHECK(acp_terminal_create_request_write("term-1", "code-1-1-1", "ls", NULL, 0, NULL, 0, &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_terminal_create_request_write("\"term-1\"", "code-1-1-1", NULL, NULL, 0, NULL, 0, &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_terminal_create_request_write("\"term-1\"", "code-1-1-1", "ls", NULL, 1, NULL, 0, &out) == ACP_INVALID_ARGUMENT);
}

static char unset[] = "unset", failed[] = "failed";

static char *created(const char *message) {
    char *id = unset;
    if (acp_terminal_create_result_read(message, strlen(message), &id) != ACP_OK) return failed;
    return id;
}

// The terminalId of a result; any other answer is none.
static void create_answers(void) {
    char *id = created("{\"jsonrpc\":\"2.0\",\"id\":\"term-1\",\"result\":{\"terminalId\":\"t-9\"}}");
    CHECK(id != NULL && strcmp(id, "t-9") == 0);
    acp_terminal_id_free(id);
    CHECK(created("{\"jsonrpc\":\"2.0\",\"id\":\"term-1\",\"error\":{\"code\":-32603,\"message\":\"no shell\"}}") == NULL);
    CHECK(created("{\"jsonrpc\":\"2.0\",\"id\":\"term-1\",\"result\":{\"terminalId\":4}}") == NULL);
    CHECK(created("not json") == NULL);
}

// The four that name a terminal: sessionId and terminalId, each under its own method.
static void by_terminal(void) {
    acp_buffer out = {0};
    CHECK(acp_terminal_output_request_write("\"term-2\"", "code-1-1-1", "t-9", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"term-2\",\"method\":\"terminal/output\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"terminalId\":\"t-9\"}}"));
    acp_buffer_free(&out);
    CHECK(acp_terminal_wait_for_exit_request_write("\"term-3\"", "code-1-1-1", "t-9", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"term-3\",\"method\":\"terminal/wait_for_exit\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"terminalId\":\"t-9\"}}"));
    acp_buffer_free(&out);
    CHECK(acp_terminal_kill_request_write("\"term-4\"", "code-1-1-1", "t-9", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"term-4\",\"method\":\"terminal/kill\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"terminalId\":\"t-9\"}}"));
    acp_buffer_free(&out);
    CHECK(acp_terminal_release_request_write("\"term-5\"", "code-1-1-1", "t-9", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"term-5\",\"method\":\"terminal/release\",\"params\":"
                     "{\"sessionId\":\"code-1-1-1\",\"terminalId\":\"t-9\"}}"));
    acp_buffer_free(&out);
    CHECK(acp_terminal_kill_request_write("\"term-4\"", "code-1-1-1", NULL, &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_terminal_release_request_write("term-5", "code-1-1-1", "t-9", &out) == ACP_INVALID_ARGUMENT);
}

static acp_status exit_of(const char *message, acp_terminal_exit *out) {
    return acp_terminal_exit_read(message, strlen(message), out);
}

// wait_for_exit's result: an exit code, a signal, or neither known; any other answer is no exit.
static void exits(void) {
    acp_terminal_exit e;
    CHECK(exit_of("{\"jsonrpc\":\"2.0\",\"id\":\"term-3\",\"result\":{\"exitCode\":2,\"signal\":null}}", &e) == ACP_OK);
    CHECK(e.exited && e.has_exit_code && e.exit_code == 2 && e.signal == NULL);
    acp_terminal_exit_free(&e);
    CHECK(exit_of("{\"jsonrpc\":\"2.0\",\"id\":\"term-3\",\"result\":{\"exitCode\":null,\"signal\":\"SIGKILL\"}}", &e) == ACP_OK);
    CHECK(e.exited && !e.has_exit_code && e.signal != NULL && strcmp(e.signal, "SIGKILL") == 0);
    acp_terminal_exit_free(&e);
    CHECK(e.signal == NULL);
    CHECK(exit_of("{\"jsonrpc\":\"2.0\",\"id\":\"term-3\",\"result\":{}}", &e) == ACP_OK);
    CHECK(e.exited && !e.has_exit_code && e.signal == NULL);
    acp_terminal_exit_free(&e);
    CHECK(exit_of("{\"jsonrpc\":\"2.0\",\"id\":\"term-3\",\"error\":{\"code\":-32603,\"message\":\"gone\"}}", &e) == ACP_OK);
    CHECK(!e.exited && !e.has_exit_code && e.signal == NULL);
    acp_terminal_exit_free(&e);
    CHECK(exit_of("{\"jsonrpc\":\"2.0\",\"id\":\"term-3\",\"result\":{\"exitCode\":-1}}", &e) == ACP_OK);
    CHECK(e.exited && !e.has_exit_code);
    acp_terminal_exit_free(&e);
}

static acp_status output_of(const char *message, acp_terminal_output *out) {
    return acp_terminal_output_result_read(message, strlen(message), out);
}

// terminal/output's result: every byte of the output, whether it was cut, and the exit when there was one.
static void outputs(void) {
    acp_terminal_output o;
    CHECK(output_of("{\"jsonrpc\":\"2.0\",\"id\":\"term-2\",\"result\":{\"output\":\"a\\u0000b\\n\",\"truncated\":true,"
                    "\"exitStatus\":{\"exitCode\":0,\"signal\":null}}}", &o) == ACP_OK);
    CHECK(o.output != NULL && o.length == 4 && memcmp(o.output, "a\0b\n", 4) == 0 && o.truncated);
    CHECK(o.exit_status.exited && o.exit_status.has_exit_code && o.exit_status.exit_code == 0);
    acp_terminal_output_free(&o);
    CHECK(o.output == NULL && o.length == 0);
    CHECK(output_of("{\"jsonrpc\":\"2.0\",\"id\":\"term-2\",\"result\":{\"output\":\"\",\"truncated\":false}}", &o) == ACP_OK);
    CHECK(o.output != NULL && o.length == 0 && !o.truncated && !o.exit_status.exited);
    acp_terminal_output_free(&o);
    CHECK(output_of("{\"jsonrpc\":\"2.0\",\"id\":\"term-2\",\"error\":{\"code\":-32603,\"message\":\"gone\"}}", &o) == ACP_OK);
    CHECK(o.output == NULL && o.length == 0);
    acp_terminal_output_free(&o);
}

static int answered(const char *message) {
    int got = -1;
    if (acp_terminal_answered_read(message, strlen(message), &got) != ACP_OK) return -1;
    return got;
}

// kill's and release's answers say nothing but whether they are a result.
static void acks(void) {
    CHECK(answered("{\"jsonrpc\":\"2.0\",\"id\":\"term-4\",\"result\":{}}") == 1);
    CHECK(answered("{\"jsonrpc\":\"2.0\",\"id\":\"term-4\",\"result\":null}") == 1);
    CHECK(answered("{\"jsonrpc\":\"2.0\",\"id\":\"term-4\",\"error\":{\"code\":-32603,\"message\":\"gone\"}}") == 0);
    CHECK(answered("not json") == 0);
    CHECK(acp_terminal_answered_read("{}", 2, NULL) == ACP_INVALID_ARGUMENT);
}

// A tool call that shows the terminal: its content is the terminal, ahead of any other.
static void shown(void) {
    acp_buffer out = {0};
    acp_tool_call call = {0};
    call.update = 1;
    call.tool_call_id = "c1";
    call.status = "in_progress";
    call.terminal_id = "t-9";
    CHECK(acp_tool_call_write(&call, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"in_progress\","
                     "\"content\":[{\"type\":\"terminal\",\"terminalId\":\"t-9\"}]}"));
    acp_buffer_free(&out);
    call.content_text = "exit 0";
    CHECK(acp_tool_call_write(&call, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"in_progress\","
                     "\"content\":[{\"type\":\"terminal\",\"terminalId\":\"t-9\"},"
                     "{\"type\":\"content\",\"content\":{\"type\":\"text\",\"text\":\"exit 0\"}}]}"));
    acp_buffer_free(&out);
}

int main(void) {
    create();
    create_answers();
    by_terminal();
    exits();
    outputs();
    acks();
    shown();
    if (failures) {
        fprintf(stderr, "terminal: %d failed\n", failures);
        return 1;
    }
    printf("terminal: terminal/*'s requests and answers, and a tool call that shows a terminal, passed\n");
    return 0;
}
