// initialize: the version a connection speaks, what the client offers, and the
// result in the selected version's shape, against the migration guide's rules.
#include "acp.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

static acp_status read_one(const char *params, const unsigned *supported, size_t n, acp_initialize *out, acp_buffer *error) {
    memset(out, 0, sizeof *out);
    memset(error, 0, sizeof *error);
    return acp_initialize_read(params, strlen(params), supported, n, out, error);
}

static void v1_shape(void) {
    const unsigned v1[] = {1};
    acp_initialize in;
    acp_buffer error;
    CHECK(read_one("{\"protocolVersion\":1,\"clientCapabilities\":{\"fs\":{\"readTextFile\":true,\"writeTextFile\":false},"
                   "\"terminal\":true,\"session\":{\"notices\":{}},\"elicitation\":{\"form\":{}}}}",
                   v1, 1, &in, &error) == ACP_OK);
    CHECK(in.requested_version == 1 && in.selected_version == 1);
    CHECK(in.capabilities.fs_read_text_file == 1 && in.capabilities.fs_write_text_file == 0);
    CHECK(in.capabilities.terminal == 1 && in.capabilities.session_notices == 1);
    CHECK(in.capabilities.elicitation_form == 1 && in.capabilities.elicitation_url == 0);
    CHECK(error.data == NULL);
}

// A v2 request is read in v2's shape, whichever version is selected:
// "a v2 request carries info and capabilities"
static void v2_shape_to_a_v1_agent(void) {
    const unsigned v1[] = {1};
    acp_initialize in;
    acp_buffer error;
    CHECK(read_one("{\"protocolVersion\":2,\"info\":{\"name\":\"zed\",\"version\":\"1\"},"
                   "\"capabilities\":{\"elicitation\":{\"form\":{},\"url\":{}}}}",
                   v1, 1, &in, &error) == ACP_OK);
    CHECK(in.requested_version == 2 && in.selected_version == 1);
    CHECK(in.capabilities.elicitation_form == 1 && in.capabilities.elicitation_url == 1);
    CHECK(in.capabilities.fs_read_text_file == 0 && in.capabilities.terminal == 0);
}

// For an agent with both:
// "a v1 request selects v1, a v2 request selects v2, and a request for a newer unsupported version selects v2"
static void dual_stack_selects(void) {
    const unsigned both[] = {2, 1};
    acp_initialize in;
    acp_buffer error;
    CHECK(read_one("{\"protocolVersion\":1}", both, 2, &in, &error) == ACP_OK && in.selected_version == 1);
    CHECK(read_one("{\"protocolVersion\":2}", both, 2, &in, &error) == ACP_OK && in.selected_version == 2);
    CHECK(read_one("{\"protocolVersion\":3}", both, 2, &in, &error) == ACP_OK && in.selected_version == 2);
}

static void none_fits(void) {
    const unsigned both[] = {1, 2};
    acp_initialize in;
    acp_buffer error;
    CHECK(read_one("{\"protocolVersion\":0}", both, 2, &in, &error) == ACP_UNSUPPORTED_VERSION);
    CHECK(error.data != NULL && strstr(error.data, "\"code\":-32022") != NULL);
    CHECK(error.data != NULL && strstr(error.data, "\"data\":{\"requested\":0,\"supported\":[1,2]}") != NULL);
    acp_buffer_free(&error);
}

static void malformed(void) {
    const unsigned v1[] = {1};
    acp_initialize in;
    acp_buffer error;
    CHECK(read_one("{\"clientCapabilities\":{}}", v1, 1, &in, &error) == ACP_INVALID_PARAMS);
    CHECK(error.data != NULL && strstr(error.data, "\"code\":-32602") != NULL);
    acp_buffer_free(&error);
    CHECK(read_one("{\"protocolVersion\":-1}", v1, 1, &in, &error) == ACP_INVALID_PARAMS);
    acp_buffer_free(&error);
    CHECK(read_one("{\"protocolVersion\":65536}", v1, 1, &in, &error) == ACP_INVALID_PARAMS);
    acp_buffer_free(&error);
    CHECK(read_one("not json", v1, 1, &in, &error) == ACP_INVALID_PARAMS);
    acp_buffer_free(&error);
    CHECK(acp_initialize_read("{\"protocolVersion\":1}", 21, NULL, 0, &in, &error) == ACP_INVALID_ARGUMENT);
}

static void results(void) {
    acp_buffer out = {0};
    CHECK(acp_initialize_result(1, "ground", "1", "{\"loadSession\":true}", NULL, &out) == ACP_OK);
    CHECK(out.data != NULL && strcmp(out.data, "{\"protocolVersion\":1,\"agentCapabilities\":{\"loadSession\":true},"
                                               "\"authMethods\":[],\"agentInfo\":{\"name\":\"ground\",\"version\":\"1\"}}") == 0);
    acp_buffer_free(&out);
    CHECK(acp_initialize_result(2, "ground", "1", NULL, "[]", &out) == ACP_OK);
    CHECK(out.data != NULL && strcmp(out.data, "{\"protocolVersion\":2,\"info\":{\"name\":\"ground\",\"version\":\"1\"},"
                                               "\"capabilities\":{},\"authMethods\":[]}") == 0);
    acp_buffer_free(&out);
    CHECK(acp_initialize_result(1, "ground", "1", "{not json", NULL, &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_initialize_result(1, "gr\"ound", "1", NULL, NULL, &out) == ACP_OK);
    CHECK(out.data != NULL && strstr(out.data, "\"name\":\"gr\\\"ound\"") != NULL);
    acp_buffer_free(&out);
}

// authenticate, as #131 answers it: the methodId it names is required, and a string.
static void authenticate(void) {
    char *method_id = NULL;
    acp_buffer error = {0};
    const char *named = "{\"methodId\":\"agent\"}";
    CHECK(acp_authenticate_read(named, strlen(named), &method_id, &error) == ACP_OK);
    CHECK(method_id && strcmp(method_id, "agent") == 0);
    acp_authenticate_free(&method_id);
    CHECK(method_id == NULL);
    const char *unnamed[] = {"{}", "{\"methodId\":7}", "{\"_meta\":{\"methodId\":\"agent\"}}"};
    for (size_t i = 0; i < sizeof unnamed / sizeof *unnamed; i++) {
        CHECK(acp_authenticate_read(unnamed[i], strlen(unnamed[i]), &method_id, &error) == ACP_INVALID_PARAMS);
        CHECK(error.data && strcmp(error.data, "{\"code\":-32602,\"message\":\"authenticate needs methodId\"}") == 0);
        CHECK(method_id == NULL);
        acp_buffer_free(&error);
    }
}

// "Whether the client supports terminal authentication methods", in either shape: an agent
// advertises a terminal method only to a client that said so.
static void auth_terminal(void) {
    const unsigned v1[] = {1};
    acp_initialize in;
    acp_buffer error;
    CHECK(read_one("{\"protocolVersion\":1,\"clientCapabilities\":{\"auth\":{\"terminal\":true}}}", v1, 1, &in, &error) == ACP_OK);
    CHECK(in.capabilities.auth_terminal == 1);
    CHECK(read_one("{\"protocolVersion\":2,\"capabilities\":{\"auth\":{\"terminal\":true}}}", v1, 1, &in, &error) == ACP_OK);
    CHECK(in.capabilities.auth_terminal == 1);
    CHECK(read_one("{\"protocolVersion\":1,\"clientCapabilities\":{\"auth\":{\"terminal\":false}}}", v1, 1, &in, &error) == ACP_OK);
    CHECK(in.capabilities.auth_terminal == 0);
    CHECK(read_one("{\"protocolVersion\":1,\"clientCapabilities\":{}}", v1, 1, &in, &error) == ACP_OK);
    CHECK(in.capabilities.auth_terminal == 0);
    CHECK(ACP_ERROR_AUTH_REQUIRED == -32000);
}

int main(void) {
    v1_shape();
    auth_terminal();
    v2_shape_to_a_v1_agent();
    dual_stack_selects();
    none_fits();
    malformed();
    results();
    authenticate();
    if (failures) {
        fprintf(stderr, "initialize: %d failed\n", failures);
        return 1;
    }
    printf("initialize: version selection, both request shapes, -32022, both result shapes and authenticate passed\n");
    return 0;
}
