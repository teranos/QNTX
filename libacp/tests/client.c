// 「[Opus 5.5] The client side: what a client writes to an agent and reads of what the agent sends, each」
// 「[Opus 5.5] checked against the agent side's own reader or writer, so the two halves agree.」
#include "acp.h"

#include <json-c/json.h>

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

// 「[Opus 5.5] A message's params as JSON text, for the agent side's readers, which read params alone.」
static char *params_of(const char *message) {
    json_object *root = json_tokener_parse(message);
    json_object *params = NULL;
    char *copy = NULL;
    if (json_object_object_get_ex(root, "params", &params)) {
        const char *s = json_object_to_json_string_ext(params, JSON_C_TO_STRING_PLAIN | JSON_C_TO_STRING_NOSLASHESCAPE);
        copy = malloc(strlen(s) + 1);
        if (copy != NULL) strcpy(copy, s);
    }
    json_object_put(root);
    return copy;
}

static int has(const acp_buffer *b, const char *text) {
    return b->data != NULL && strstr(b->data, text) != NULL;
}

// 「[Opus 5.5] "Sent by the client to establish connection and negotiate capabilities." What it offers is read」
// 「[Opus 5.5] back by the agent side as offered.」
static void initialize_request(void) {
    acp_client_capabilities offer = {0};
    offer.fs_read_text_file = 1;
    offer.terminal = 1;
    acp_buffer m = {0};
    CHECK(acp_client_initialize_write("0", 1, &offer, "qntx", "1", &m) == ACP_OK);
    CHECK(m.data != NULL && strcmp(m.data, "{\"jsonrpc\":\"2.0\",\"id\":0,\"method\":\"initialize\",\"params\":{\"protocolVersion\":1,"
        "\"clientCapabilities\":{\"fs\":{\"readTextFile\":true,\"writeTextFile\":false},\"terminal\":true},"
        "\"clientInfo\":{\"name\":\"qntx\",\"version\":\"1\"}}}") == 0);
    char *params = params_of(m.data);
    const unsigned v1[] = {1};
    acp_initialize in;
    acp_buffer error = {0};
    CHECK(params != NULL && acp_initialize_read(params, strlen(params), v1, 1, &in, &error) == ACP_OK);
    CHECK(in.selected_version == 1 && in.capabilities.fs_read_text_file == 1 && in.capabilities.fs_write_text_file == 0);
    CHECK(in.capabilities.terminal == 1);
    free(params);
    acp_buffer_free(&m);

    // 「[Opus 5.5] v2's request carries info and capabilities, and offers no fs or terminal.」
    CHECK(acp_client_initialize_write("0", 2, &offer, "qntx", "1", &m) == ACP_OK);
    CHECK(has(&m, "\"capabilities\":{}") && has(&m, "\"info\":{\"name\":\"qntx\",\"version\":\"1\"}"));
    CHECK(!has(&m, "clientCapabilities") && !has(&m, "terminal"));
    acp_buffer_free(&m);

    CHECK(acp_client_initialize_write("0", 3, &offer, "qntx", "1", &m) == ACP_INVALID_ARGUMENT);
    CHECK(acp_client_initialize_write("not json", 1, &offer, "qntx", "1", &m) == ACP_INVALID_ARGUMENT);
}

// 「[Opus 5.5] The agent's answer, as the agent side writes it: the version it speaks and what it offers.」
static void initialize_response(void) {
    acp_buffer result = {0};
    CHECK(acp_initialize_result(1, "ground", "2", "{\"loadSession\":true,\"mcpCapabilities\":{\"http\":true},"
        "\"promptCapabilities\":{\"image\":true},\"sessionCapabilities\":{\"resume\":{}}}", NULL, &result) == ACP_OK);
    char message[512];
    snprintf(message, sizeof message, "{\"jsonrpc\":\"2.0\",\"id\":0,\"result\":%s}", result.data);
    acp_buffer_free(&result);
    acp_agent_info info;
    CHECK(acp_client_initialize_read(message, strlen(message), &info) == ACP_OK);
    CHECK(info.protocol_version == 1 && info.load_session == 1 && info.session_resume == 1);
    CHECK(info.mcp_http == 1 && info.mcp_sse == 0 && info.prompt_image == 1 && info.prompt_audio == 0);
    CHECK(info.agent_name != NULL && strcmp(info.agent_name, "ground") == 0);
    CHECK(info.agent_version != NULL && strcmp(info.agent_version, "2") == 0);
    acp_agent_info_free(&info);

    // 「[Opus 5.5] An error is no agent: version 0, nothing offered.」
    const char *refused = "{\"jsonrpc\":\"2.0\",\"id\":0,\"error\":{\"code\":-32022,\"message\":\"no\"}}";
    CHECK(acp_client_initialize_read(refused, strlen(refused), &info) == ACP_OK);
    CHECK(info.protocol_version == 0 && info.load_session == 0 && info.agent_name == NULL);
    acp_agent_info_free(&info);
}

// 「[Opus 5.5] session/new, session/load and session/resume, each naming the node's MCP over HTTP with its」
// 「[Opus 5.5] header, read back by the agent side's readers, which check every server's shape.」
static void session_requests(void) {
    const acp_http_header bearer[] = {{"Authorization", "Bearer t"}};
    const acp_mcp_http servers[] = {{"qntx", "https://node/mcp", bearer, 1}};
    acp_buffer m = {0};
    CHECK(acp_client_session_new_write("1", "/work", servers, 1, &m) == ACP_OK);
    CHECK(m.data != NULL && strcmp(m.data, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"session/new\",\"params\":{\"cwd\":\"/work\","
        "\"mcpServers\":[{\"type\":\"http\",\"name\":\"qntx\",\"url\":\"https://node/mcp\","
        "\"headers\":[{\"name\":\"Authorization\",\"value\":\"Bearer t\"}]}]}}") == 0);
    char *params = params_of(m.data);
    acp_session_new made;
    acp_buffer error = {0};
    CHECK(params != NULL && acp_session_new_read(params, strlen(params), &made, &error) == ACP_OK);
    CHECK(made.cwd != NULL && strcmp(made.cwd, "/work") == 0);
    acp_session_new_free(&made);
    free(params);
    acp_buffer_free(&m);

    // 「[Opus 5.5] No servers is an empty list, which the request requires.」
    CHECK(acp_client_session_new_write("1", "/work", NULL, 0, &m) == ACP_OK);
    CHECK(has(&m, "\"mcpServers\":[]"));
    acp_buffer_free(&m);
    CHECK(acp_client_session_new_write("1", "work", NULL, 0, &m) == ACP_INVALID_ARGUMENT);

    CHECK(acp_client_session_load_write("2", "s-1", "/work", servers, 1, &m) == ACP_OK);
    CHECK(has(&m, "\"method\":\"session/load\"") && has(&m, "\"sessionId\":\"s-1\""));
    params = params_of(m.data);
    acp_session_resume loaded;
    CHECK(params != NULL && acp_session_load_read(params, strlen(params), &loaded, &error) == ACP_OK);
    CHECK(loaded.session_id != NULL && strcmp(loaded.session_id, "s-1") == 0);
    acp_session_resume_free(&loaded);
    free(params);
    acp_buffer_free(&m);

    CHECK(acp_client_session_resume_write("3", "s-1", "/work", servers, 1, &m) == ACP_OK);
    CHECK(has(&m, "\"method\":\"session/resume\"") && has(&m, "\"mcpServers\":[{"));
    params = params_of(m.data);
    acp_session_resume resumed;
    CHECK(params != NULL && acp_session_resume_read(params, strlen(params), &resumed, &error) == ACP_OK);
    CHECK(resumed.cwd != NULL && strcmp(resumed.cwd, "/work") == 0);
    acp_session_resume_free(&resumed);
    free(params);
    acp_buffer_free(&m);
}

// 「[Opus 5.5] The sessionId the agent answers session/new with, as the agent side writes it.」
static void session_new_response(void) {
    acp_buffer result = {0};
    CHECK(acp_session_new_result("s-9", NULL, NULL, &result) == ACP_OK);
    char message[256];
    snprintf(message, sizeof message, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":%s}", result.data);
    acp_buffer_free(&result);
    char *sid = NULL;
    CHECK(acp_client_session_new_read(message, strlen(message), &sid) == ACP_OK);
    CHECK(sid != NULL && strcmp(sid, "s-9") == 0);
    acp_session_id_free(&sid);
    CHECK(sid == NULL);
    const char *refused = "{\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32602,\"message\":\"no\"}}";
    CHECK(acp_client_session_new_read(refused, strlen(refused), &sid) == ACP_OK && sid == NULL);
}

// 「[Opus 5.5] A prompt of text, every byte of it, read back by the agent side as one text block.」
static void prompt_request(void) {
    const char says[] = "line one\n\"quoted\" \xc3\xa9";
    acp_buffer m = {0};
    CHECK(acp_client_session_prompt_write("4", "s-1", says, sizeof says - 1, &m) == ACP_OK);
    CHECK(has(&m, "\"method\":\"session/prompt\"") && has(&m, "\"sessionId\":\"s-1\""));
    char *params = params_of(m.data);
    acp_prompt prompt;
    acp_buffer error = {0};
    CHECK(params != NULL && acp_prompt_read(params, strlen(params), &prompt, &error) == ACP_OK);
    CHECK(prompt.count == 1 && prompt.blocks[0].kind == ACP_BLOCK_TEXT);
    CHECK(prompt.count == 1 && strcmp(prompt.blocks[0].text, says) == 0);
    acp_prompt_free(&prompt);
    free(params);
    acp_buffer_free(&m);
}

// 「[Opus 5.5] Why the turn stopped, as the agent side writes it; an error is no stop.」
static void prompt_response(void) {
    const acp_stop_reason reasons[] = {ACP_STOP_END_TURN, ACP_STOP_MAX_TOKENS, ACP_STOP_MAX_TURN_REQUESTS, ACP_STOP_REFUSAL, ACP_STOP_CANCELLED};
    for (size_t i = 0; i < sizeof reasons / sizeof reasons[0]; i++) {
        acp_buffer result = {0};
        CHECK(acp_prompt_result_write(reasons[i], &result) == ACP_OK);
        char message[128];
        snprintf(message, sizeof message, "{\"jsonrpc\":\"2.0\",\"id\":4,\"result\":%s}", result.data);
        acp_buffer_free(&result);
        int stopped = 0;
        acp_stop_reason reason = ACP_STOP_END_TURN;
        CHECK(acp_client_session_prompt_read(message, strlen(message), &stopped, &reason) == ACP_OK);
        CHECK(stopped == 1 && reason == reasons[i]);
    }
    int stopped = 1;
    acp_stop_reason reason;
    const char *refused = "{\"jsonrpc\":\"2.0\",\"id\":4,\"error\":{\"code\":-32603,\"message\":\"no\"}}";
    CHECK(acp_client_session_prompt_read(refused, strlen(refused), &stopped, &reason) == ACP_OK && stopped == 0);
    const char *unknown = "{\"jsonrpc\":\"2.0\",\"id\":4,\"result\":{\"stopReason\":\"tired\"}}";
    CHECK(acp_client_session_prompt_read(unknown, strlen(unknown), &stopped, &reason) == ACP_OK && stopped == 0);
}

// 「[Opus 5.5] session/cancel, a notification, read back by the agent side as a cancel of the session.」
static void cancel(void) {
    acp_buffer m = {0};
    CHECK(acp_client_session_cancel_write("s-1", &m) == ACP_OK);
    CHECK(!has(&m, "\"id\""));
    acp_cancel c;
    CHECK(m.data != NULL && acp_cancel_read(m.data, m.length, &c) == ACP_OK);
    CHECK(c.kind == ACP_CANCEL_SESSION && c.session_id != NULL && strcmp(c.session_id, "s-1") == 0);
    acp_cancel_free(&c);
    acp_buffer_free(&m);
}

// 「[Opus 5.5] What comes in: a request, a notification, or a response, by id and method.」
static void messages(void) {
    acp_message msg;
    const char *request = "{\"jsonrpc\":\"2.0\",\"id\":\"a\",\"method\":\"session/request_permission\",\"params\":{}}";
    CHECK(acp_client_message_read(request, strlen(request), &msg) == ACP_OK);
    CHECK(msg.kind == ACP_MESSAGE_REQUEST && strcmp(msg.id, "\"a\"") == 0 && strcmp(msg.method, "session/request_permission") == 0);
    // 「[Opus 5.5] A request's params, as JSON text, for the reader of its method.」
    CHECK(msg.params != NULL && strcmp(msg.params, "{}") == 0);
    acp_message_free(&msg);
    const char *note = "{\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{}}";
    CHECK(acp_client_message_read(note, strlen(note), &msg) == ACP_OK);
    CHECK(msg.kind == ACP_MESSAGE_NOTIFICATION && msg.id == NULL && strcmp(msg.method, "session/update") == 0);
    acp_message_free(&msg);
    const char *result = "{\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}";
    CHECK(acp_client_message_read(result, strlen(result), &msg) == ACP_OK);
    CHECK(msg.kind == ACP_MESSAGE_RESPONSE && strcmp(msg.id, "7") == 0 && msg.is_error == 0 && msg.method == NULL);
    CHECK(msg.params == NULL);
    acp_message_free(&msg);
    const char *error = "{\"jsonrpc\":\"2.0\",\"id\":7,\"error\":{\"code\":-32603,\"message\":\"broke\",\"data\":{\"why\":1}}}";
    CHECK(acp_client_message_read(error, strlen(error), &msg) == ACP_OK);
    CHECK(msg.kind == ACP_MESSAGE_RESPONSE && msg.is_error == 1 && msg.error_code == -32603);
    CHECK(msg.error_message != NULL && strcmp(msg.error_message, "broke") == 0);
    CHECK(msg.error_data != NULL && strcmp(msg.error_data, "{\"why\":1}") == 0);
    acp_message_free(&msg);
    CHECK(acp_client_message_read("not json", 8, &msg) == ACP_PARSE_ERROR && msg.kind == ACP_MESSAGE_UNREAD);
    const char *neither = "{\"jsonrpc\":\"2.0\",\"id\":7}";
    CHECK(acp_client_message_read(neither, strlen(neither), &msg) == ACP_OK && msg.kind == ACP_MESSAGE_UNREAD);
    acp_message_free(&msg);
}

// 「[Opus 5.5] Each update the agent side writes, read back: its kind, and what the client shows of it.」
static void updates(void) {
    acp_buffer u = {0}, m = {0};
    acp_update up;

    const char says[] = "hello \"there\"\n";
    CHECK(acp_text_chunk_write(ACP_CHUNK_AGENT_MESSAGE, says, sizeof says - 1, &u) == ACP_OK);
    CHECK(acp_session_update_write("s-1", u.data, &m) == ACP_OK);
    CHECK(acp_client_session_update_read(m.data, m.length, &up) == ACP_OK);
    CHECK(up.kind == ACP_UPDATE_AGENT_MESSAGE && strcmp(up.session_id, "s-1") == 0);
    CHECK(up.text != NULL && up.text_length == sizeof says - 1 && memcmp(up.text, says, up.text_length) == 0);
    CHECK(up.update != NULL && strcmp(up.update, u.data) == 0);
    acp_update_free(&up);
    acp_buffer_free(&u);
    acp_buffer_free(&m);

    CHECK(acp_text_chunk_write(ACP_CHUNK_AGENT_THOUGHT, "hm", 2, &u) == ACP_OK);
    CHECK(acp_session_update_write("s-1", u.data, &m) == ACP_OK);
    CHECK(acp_client_session_update_read(m.data, m.length, &up) == ACP_OK && up.kind == ACP_UPDATE_AGENT_THOUGHT);
    acp_update_free(&up);
    acp_buffer_free(&u);
    acp_buffer_free(&m);

    acp_tool_call call = {0};
    call.tool_call_id = "c1";
    call.title = "qntx: am";
    call.name = "mcp__qntx__am";
    call.kind = "fetch";
    call.status = "pending";
    call.raw_input = "{\"a\":1}";
    CHECK(acp_tool_call_write(&call, &u) == ACP_OK);
    CHECK(acp_session_update_write("s-1", u.data, &m) == ACP_OK);
    CHECK(acp_client_session_update_read(m.data, m.length, &up) == ACP_OK);
    CHECK(up.kind == ACP_UPDATE_TOOL_CALL && strcmp(up.tool_call_id, "c1") == 0 && strcmp(up.title, "qntx: am") == 0);
    CHECK(strcmp(up.name, "mcp__qntx__am") == 0 && strcmp(up.tool_kind, "fetch") == 0 && strcmp(up.status, "pending") == 0);
    CHECK(up.raw_input != NULL && strcmp(up.raw_input, "{\"a\":1}") == 0 && up.raw_output == NULL);
    acp_update_free(&up);
    acp_buffer_free(&u);
    acp_buffer_free(&m);

    acp_tool_call done = {0};
    done.update = 1;
    done.tool_call_id = "c1";
    done.status = "completed";
    done.raw_output = "[1,2]";
    CHECK(acp_tool_call_write(&done, &u) == ACP_OK);
    CHECK(acp_session_update_write("s-1", u.data, &m) == ACP_OK);
    CHECK(acp_client_session_update_read(m.data, m.length, &up) == ACP_OK);
    CHECK(up.kind == ACP_UPDATE_TOOL_CALL_UPDATE && strcmp(up.status, "completed") == 0 && up.title == NULL);
    CHECK(up.raw_output != NULL && strcmp(up.raw_output, "[1,2]") == 0);
    acp_update_free(&up);
    acp_buffer_free(&u);
    acp_buffer_free(&m);

    CHECK(acp_usage_update_write(1200, 200000, "0.0123", "USD", &u) == ACP_OK);
    CHECK(acp_session_update_write("s-1", u.data, &m) == ACP_OK);
    CHECK(acp_client_session_update_read(m.data, m.length, &up) == ACP_OK);
    CHECK(up.kind == ACP_UPDATE_USAGE && up.used == 1200 && up.size == 200000);
    CHECK(up.cost_amount != NULL && strcmp(up.cost_amount, "0.0123") == 0 && strcmp(up.cost_currency, "USD") == 0);
    acp_update_free(&up);
    acp_buffer_free(&u);
    acp_buffer_free(&m);

    // 「[Opus 5.5] Any other kind is OTHER, named by its type, and kept whole.」
    const char *plan = "{\"entries\":[{\"content\":\"x\",\"priority\":\"low\",\"status\":\"pending\"}]}";
    CHECK(acp_plan_write(plan, strlen(plan), &u) == ACP_OK);
    CHECK(acp_session_update_write("s-1", u.data, &m) == ACP_OK);
    CHECK(acp_client_session_update_read(m.data, m.length, &up) == ACP_OK);
    CHECK(up.kind == ACP_UPDATE_OTHER && strcmp(up.type, "plan") == 0 && strcmp(up.update, u.data) == 0);
    acp_update_free(&up);
    acp_buffer_free(&u);
    acp_buffer_free(&m);

    const char *nothing = "{\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{\"sessionId\":\"s-1\"}}";
    CHECK(acp_client_session_update_read(nothing, strlen(nothing), &up) == ACP_OK && up.kind == ACP_UPDATE_UNREAD);
    acp_update_free(&up);
}

// 「[Opus 5.5] session/request_permission as the agent side writes it, read by the client; the client's」
// 「[Opus 5.5] answer read back by the agent side as the outcome it is.」
static void permission(void) {
    const acp_permission_option options[] = {{"allow", "Allow", "allow_once"}, {"reject", "Reject", "reject_once"}};
    acp_permission_request request = {0};
    request.request_id = "\"p1\"";
    request.session_id = "s-1";
    request.tool_call_id = "c1";
    request.title = "run ls";
    request.kind = "execute";
    request.status = "pending";
    request.raw_input = "{\"command\":\"ls\"}";
    request.options = options;
    request.count = 2;
    acp_buffer m = {0};
    CHECK(acp_permission_request_write(&request, &m) == ACP_OK);
    char *params = params_of(m.data);
    acp_permission_asked asked;
    CHECK(params != NULL && acp_client_session_request_permission_read(params, strlen(params), &asked) == ACP_OK);
    CHECK(asked.session_id != NULL && strcmp(asked.session_id, "s-1") == 0);
    CHECK(asked.tool_call_id != NULL && strcmp(asked.tool_call_id, "c1") == 0);
    CHECK(asked.title != NULL && strcmp(asked.title, "run ls") == 0 && strcmp(asked.tool_kind, "execute") == 0);
    CHECK(asked.raw_input != NULL && strcmp(asked.raw_input, "{\"command\":\"ls\"}") == 0);
    CHECK(asked.count == 2 && strcmp(asked.options[1].option_id, "reject") == 0 && strcmp(asked.options[1].kind, "reject_once") == 0);
    acp_permission_asked_free(&asked);
    free(params);
    acp_buffer_free(&m);

    acp_permission_outcome outcome;
    CHECK(acp_client_session_request_permission_write("\"p1\"", "allow", &m) == ACP_OK);
    CHECK(has(&m, "\"id\":\"p1\""));
    CHECK(acp_permission_outcome_read(m.data, m.length, &outcome) == ACP_OK);
    CHECK(outcome.kind == ACP_PERMISSION_SELECTED && strcmp(outcome.option_id, "allow") == 0);
    acp_permission_outcome_free(&outcome);
    acp_buffer_free(&m);

    // "it MUST respond to all pending session/request_permission requests with this Cancelled outcome"
    CHECK(acp_client_session_request_permission_write("\"p1\"", NULL, &m) == ACP_OK);
    CHECK(acp_permission_outcome_read(m.data, m.length, &outcome) == ACP_OK && outcome.kind == ACP_PERMISSION_CANCELLED);
    acp_permission_outcome_free(&outcome);
    acp_buffer_free(&m);

    const char *empty = "{\"sessionId\":\"s-1\",\"toolCall\":{\"toolCallId\":\"c1\"},\"options\":[]}";
    CHECK(acp_client_session_request_permission_read(empty, strlen(empty), &asked) == ACP_OK && asked.count == 0);
    acp_permission_asked_free(&asked);
    const char *missing = "{\"sessionId\":\"s-1\",\"options\":[]}";
    CHECK(acp_client_session_request_permission_read(missing, strlen(missing), &asked) == ACP_INVALID_PARAMS);
}

// 「[Opus 5.5] A request the client does not answer, answered with the error saying why.」
static void errors(void) {
    acp_buffer m = {0};
    CHECK(acp_client_error_write("\"r\"", -32601, "fs/read_text_file is not offered", &m) == ACP_OK);
    CHECK(m.data != NULL && strcmp(m.data, "{\"jsonrpc\":\"2.0\",\"id\":\"r\",\"error\":{\"code\":-32601,"
        "\"message\":\"fs/read_text_file is not offered\"}}") == 0);
    acp_message msg;
    CHECK(acp_client_message_read(m.data, m.length, &msg) == ACP_OK && msg.is_error == 1 && msg.error_code == -32601);
    acp_message_free(&msg);
    acp_buffer_free(&m);
    CHECK(acp_client_error_write("\"r\"", -32601, NULL, &m) == ACP_INVALID_ARGUMENT);
}

// 「[Opus 5.5] authenticate with the method the agent offered, read back by the agent side as that method.」
static void authenticate(void) {
    acp_buffer m = {0};
    CHECK(acp_client_authenticate_write("5", "gemini-api-key", &m) == ACP_OK);
    CHECK(m.data != NULL && strcmp(m.data, "{\"jsonrpc\":\"2.0\",\"id\":5,\"method\":\"authenticate\",\"params\":{\"methodId\":\"gemini-api-key\"}}") == 0);
    char *params = params_of(m.data);
    char *method = NULL;
    acp_buffer error = {0};
    CHECK(params != NULL && acp_authenticate_read(params, strlen(params), &method, &error) == ACP_OK);
    CHECK(method != NULL && strcmp(method, "gemini-api-key") == 0);
    acp_authenticate_free(&method);
    free(params);
    acp_buffer_free(&m);
    CHECK(acp_client_authenticate_write("5", NULL, &m) == ACP_INVALID_ARGUMENT);
}

// 「[Opus 5.5] A config option set to a value id, as the model a session runs, read back by the agent side.」
static void set_config_option(void) {
    acp_buffer m = {0};
    CHECK(acp_client_session_set_config_option_write("6", "s-1", "model", "gemini-3.8-flash-low", &m) == ACP_OK);
    CHECK(has(&m, "\"method\":\"session/set_config_option\""));
    char *params = params_of(m.data);
    acp_set_config_option set;
    acp_buffer error = {0};
    CHECK(params != NULL && acp_set_config_option_read(params, strlen(params), &set, &error) == ACP_OK);
    CHECK(set.session_id != NULL && strcmp(set.session_id, "s-1") == 0);
    CHECK(set.config_id != NULL && strcmp(set.config_id, "model") == 0);
    CHECK(set.value != NULL && strcmp(set.value, "gemini-3.8-flash-low") == 0 && set.is_boolean == 0);
    acp_set_config_option_free(&set);
    free(params);
    acp_buffer_free(&m);
    CHECK(acp_client_session_set_config_option_write("6", "s-1", NULL, "x", &m) == ACP_INVALID_ARGUMENT);
}

int main(void) {
    initialize_request();
    initialize_response();
    session_requests();
    session_new_response();
    prompt_request();
    prompt_response();
    cancel();
    messages();
    updates();
    permission();
    errors();
    authenticate();
    set_config_option();
    if (failures > 0) {
        fprintf(stderr, "client: %d failed\n", failures);
        return 1;
    }
    printf("client: initialize, authenticate, sessions, config options, prompts, cancels, updates, permission and errors, as a client, passed\n");
    return 0;
}
