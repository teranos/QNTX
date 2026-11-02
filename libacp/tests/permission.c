// session/request_permission, as #138 asks it: the request, and the client's outcome.
#include "acp.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

static int says(const acp_buffer *b, const char *want) {
    return b->data != NULL && strcmp(b->data, want) == 0;
}

static const acp_permission_option options[] = {
    {"allow_once", "Allow once", "allow_once"},
    {"reject_always", "Always reject x__y", "reject_always"},
};

// The whole request: its id, the tool call pending, and the options in order.
static void request(void) {
    acp_permission_request asked = {"\"perm-1\"", "code-1-1-1", "c1", "x__y", "x__y", "read", "pending", "{\"q\":\"a/b\"}", options, 2};
    acp_buffer out = {0};
    CHECK(acp_permission_request_write(&asked, &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"perm-1\",\"method\":\"session/request_permission\",\"params\":{"
                     "\"sessionId\":\"code-1-1-1\",\"toolCall\":{\"toolCallId\":\"c1\",\"title\":\"x__y\",\"name\":\"x__y\","
                     "\"kind\":\"read\",\"status\":\"pending\",\"rawInput\":{\"q\":\"a/b\"}},\"options\":["
                     "{\"optionId\":\"allow_once\",\"name\":\"Allow once\",\"kind\":\"allow_once\"},"
                     "{\"optionId\":\"reject_always\",\"name\":\"Always reject x__y\",\"kind\":\"reject_always\"}]}}"));
    acp_buffer_free(&out);
    // No name and no kind are none written; input that is not JSON is written as the text it is.
    acp_permission_request bare = {"7", "code-1-1-1", "c1", "x__y", NULL, NULL, "pending", "", options, 1};
    CHECK(acp_permission_request_write(&bare, &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":7,\"method\":\"session/request_permission\",\"params\":{"
                     "\"sessionId\":\"code-1-1-1\",\"toolCall\":{\"toolCallId\":\"c1\",\"title\":\"x__y\",\"status\":\"pending\","
                     "\"rawInput\":\"\"},\"options\":[{\"optionId\":\"allow_once\",\"name\":\"Allow once\",\"kind\":\"allow_once\"}]}}"));
    acp_buffer_free(&out);
    acp_permission_request half = {"7", "code-1-1-1", "c1", "x__y", NULL, NULL, "pending", "{\"q\":", options, 1};
    CHECK(acp_permission_request_write(&half, &out) == ACP_OK);
    CHECK(out.data && strstr(out.data, "\"rawInput\":\"{\\\"q\\\":\"}") != NULL);
    acp_buffer_free(&out);
    // An id that is not JSON, or no options, is no request.
    acp_permission_request stray = {"perm-1", "code-1-1-1", "c1", "x__y", NULL, NULL, "pending", "{}", options, 1};
    CHECK(acp_permission_request_write(&stray, &out) == ACP_INVALID_ARGUMENT);
    acp_permission_request none = {"7", "code-1-1-1", "c1", "x__y", NULL, NULL, "pending", "{}", options, 0};
    CHECK(acp_permission_request_write(&none, &out) == ACP_INVALID_ARGUMENT);
}

static acp_status outcome(const char *answer, acp_permission_outcome *out) {
    memset(out, 0, sizeof *out);
    return acp_permission_outcome_read(answer, strlen(answer), out);
}

// "selected" and its optionId, "cancelled", or neither.
static void outcomes(void) {
    acp_permission_outcome o;
    CHECK(outcome("{\"jsonrpc\":\"2.0\",\"id\":\"perm-1\",\"result\":{\"outcome\":{\"outcome\":\"selected\",\"optionId\":\"allow_once\"}}}", &o) == ACP_OK);
    CHECK(o.kind == ACP_PERMISSION_SELECTED && o.option_id && strcmp(o.option_id, "allow_once") == 0);
    acp_permission_outcome_free(&o);
    CHECK(o.option_id == NULL);
    CHECK(outcome("{\"jsonrpc\":\"2.0\",\"id\":\"perm-1\",\"result\":{\"outcome\":{\"outcome\":\"cancelled\"}}}", &o) == ACP_OK);
    CHECK(o.kind == ACP_PERMISSION_CANCELLED && o.option_id == NULL);
    const char *neither[] = {
        "{\"jsonrpc\":\"2.0\",\"id\":\"perm-1\",\"result\":{\"outcome\":{\"outcome\":\"selected\"}}}",
        "{\"jsonrpc\":\"2.0\",\"id\":\"perm-1\",\"result\":{\"outcome\":{\"outcome\":\"later\"}}}",
        "{\"jsonrpc\":\"2.0\",\"id\":\"perm-1\",\"error\":{\"code\":-32603,\"message\":\"no\"}}",
        "not json",
    };
    for (size_t i = 0; i < sizeof neither / sizeof *neither; i++) {
        CHECK(outcome(neither[i], &o) == ACP_OK);
        CHECK(o.kind == ACP_PERMISSION_UNREAD && o.option_id == NULL);
    }
}

int main(void) {
    request();
    outcomes();
    if (failures) {
        fprintf(stderr, "permission: %d failed\n", failures);
        return 1;
    }
    printf("permission: session/request_permission's request and the client's outcome passed\n");
    return 0;
}
