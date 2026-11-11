// 「[Opus 5.5] The client side: what a client writes to an agent, and reads of what the agent sends. Each is the」
// 「[Opus 5.5] mirror of what the agent side reads or writes of the same method, in v1 at schema-v1.26.0.」
#include "internal.h"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// 「[Opus 5.5] A JSON value as text, copied; NULL for none. The caller frees it.」
static char *json_text(json_object *value) {
    if (value == NULL) return NULL;
    const char *text = json_object_to_json_string_ext(value, JSON_C_TO_STRING_PLAIN | JSON_C_TO_STRING_NOSLASHESCAPE);
    size_t n = strlen(text) + 1;
    char *copy = malloc(n);
    if (copy != NULL) memcpy(copy, text, n);
    return copy;
}

// 「[Opus 5.5] Whether a key is there at all, null among its values: a response's result may be null.」
static int present(json_object *object, const char *key) {
    return json_object_is_type(object, json_type_object) && json_object_object_get_ex(object, key, NULL);
}

// 「[Opus 5.5] A request or a notification, whole: id is JSON text, or NULL for a notification. NULL when the id is」
// 「[Opus 5.5] not JSON, and params is put.」
static json_object *outgoing(const char *id, const char *method, json_object *params) {
    json_object *message = json_object_new_object();
    json_object_object_add(message, "jsonrpc", json_object_new_string("2.0"));
    if (id != NULL) {
        json_object *parsed = acp_parse(id, strlen(id));
        if (parsed == NULL) {
            json_object_put(params);
            json_object_put(message);
            return NULL;
        }
        json_object_object_add(message, "id", parsed);
    }
    json_object_object_add(message, "method", json_object_new_string(method));
    json_object_object_add(message, "params", params);
    return message;
}

// 「[Opus 5.5] A response, whole, to the request whose id is JSON text: its result, or its error.」
static json_object *response(const char *id, const char *key, json_object *value) {
    json_object *parsed = acp_parse(id, strlen(id));
    if (parsed == NULL) {
        json_object_put(value);
        return NULL;
    }
    json_object *message = json_object_new_object();
    json_object_object_add(message, "jsonrpc", json_object_new_string("2.0"));
    json_object_object_add(message, "id", parsed);
    json_object_object_add(message, key, value);
    return message;
}

static acp_status send_or_refuse(json_object *message, acp_buffer *message_json) {
    if (message == NULL) return ACP_INVALID_ARGUMENT;
    return acp_finish(message, message_json);
}

acp_status acp_client_message_read(const char *message_json, size_t length, acp_message *out) {
    if (message_json == NULL || out == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    json_object *message = acp_parse(message_json, length);
    if (!json_object_is_type(message, json_type_object)) {
        json_object_put(message);
        return ACP_PARSE_ERROR;
    }
    out->method = acp_string(message, "method");
    out->id = json_text(acp_field(message, "id"));
    if (out->method != NULL) {
        out->kind = out->id != NULL ? ACP_MESSAGE_REQUEST : ACP_MESSAGE_NOTIFICATION;
        out->params = json_text(acp_field(message, "params"));
    } else if (out->id != NULL && (present(message, "result") || present(message, "error"))) {
        out->kind = ACP_MESSAGE_RESPONSE;
        json_object *error = acp_field(message, "error");
        if (error != NULL) {
            out->is_error = 1;
            json_object *code = acp_field(error, "code");
            if (json_object_is_type(code, json_type_int)) out->error_code = json_object_get_int(code);
            out->error_message = acp_string(error, "message");
            out->error_data = json_text(acp_field(error, "data"));
        }
    }
    json_object_put(message);
    return ACP_OK;
}

void acp_message_free(acp_message *message) {
    if (message == NULL) return;
    free(message->id);
    free(message->method);
    free(message->error_message);
    free(message->error_data);
    free(message->params);
    memset(message, 0, sizeof *message);
}

acp_status acp_client_error_write(const char *request_id, int code, const char *message, acp_buffer *message_json) {
    if (request_id == NULL || message == NULL || message_json == NULL) return ACP_INVALID_ARGUMENT;
    json_object *error = json_object_new_object();
    json_object_object_add(error, "code", json_object_new_int(code));
    json_object_object_add(error, "message", json_object_new_string(message));
    return send_or_refuse(response(request_id, "error", error), message_json);
}

// 「[Opus 5.5] --- initialize ---」

static json_object *implementation(const char *name, const char *version) {
    json_object *info = json_object_new_object();
    json_object_object_add(info, "name", json_object_new_string(name));
    json_object_object_add(info, "version", json_object_new_string(version));
    return info;
}

// 「[Opus 5.5] What the client offers in version's shape, as acp_initialize_read reads it.」
static json_object *offering(unsigned version, const acp_client_capabilities *offered) {
    acp_client_capabilities none = {0};
    const acp_client_capabilities *c = offered != NULL ? offered : &none;
    json_object *caps = json_object_new_object();
    if (version == 1) {
        json_object *fs = json_object_new_object();
        json_object_object_add(fs, "readTextFile", json_object_new_boolean(c->fs_read_text_file != 0));
        json_object_object_add(fs, "writeTextFile", json_object_new_boolean(c->fs_write_text_file != 0));
        json_object_object_add(caps, "fs", fs);
        json_object_object_add(caps, "terminal", json_object_new_boolean(c->terminal != 0));
        if (c->session_notices) {
            json_object *session = json_object_new_object();
            json_object_object_add(session, "notices", json_object_new_object());
            json_object_object_add(caps, "session", session);
        }
    }
    if (c->elicitation_form || c->elicitation_url) {
        json_object *elicitation = json_object_new_object();
        if (c->elicitation_form) json_object_object_add(elicitation, "form", json_object_new_object());
        if (c->elicitation_url) json_object_object_add(elicitation, "url", json_object_new_object());
        json_object_object_add(caps, "elicitation", elicitation);
    }
    if (c->auth_terminal) {
        json_object *auth = json_object_new_object();
        json_object_object_add(auth, "terminal", json_object_new_boolean(1));
        json_object_object_add(caps, "auth", auth);
    }
    return caps;
}

acp_status acp_client_initialize_write(const char *request_id, unsigned version, const acp_client_capabilities *offered,
    const char *client_name, const char *client_version, acp_buffer *message_json) {
    if (request_id == NULL || client_name == NULL || client_version == NULL || message_json == NULL
        || version < 1 || version > 2)
        return ACP_INVALID_ARGUMENT;
    json_object *params = json_object_new_object();
    json_object_object_add(params, "protocolVersion", json_object_new_int64(version));
    if (version >= 2) {
        json_object_object_add(params, "info", implementation(client_name, client_version));
        json_object_object_add(params, "capabilities", offering(version, offered));
    } else {
        json_object_object_add(params, "clientCapabilities", offering(version, offered));
        json_object_object_add(params, "clientInfo", implementation(client_name, client_version));
    }
    return send_or_refuse(outgoing(request_id, ACP_AGENT_INITIALIZE, params), message_json);
}

acp_status acp_client_initialize_read(const char *message_json, size_t length, acp_agent_info *out) {
    if (message_json == NULL || out == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    json_object *message = acp_parse(message_json, length);
    json_object *result = acp_field(message, "result");
    json_object *version = acp_field(result, "protocolVersion");
    if (json_object_is_type(version, json_type_int)) {
        int64_t v = json_object_get_int64(version);
        if (v > 0 && v <= 65535) out->protocol_version = (unsigned) v;
    }
    if (out->protocol_version > 0) {
        // 「[Opus 5.5] The result is in the shape of the version it names.」
        int v2 = out->protocol_version >= 2;
        json_object *caps = acp_field(result, v2 ? "capabilities" : "agentCapabilities");
        json_object *info = acp_field(result, v2 ? "info" : "agentInfo");
        out->load_session = acp_flag(caps, "loadSession");
        out->session_resume = acp_offered(acp_field(caps, "sessionCapabilities"), "resume");
        json_object *prompt = acp_field(caps, "promptCapabilities");
        out->prompt_image = acp_flag(prompt, "image");
        out->prompt_audio = acp_flag(prompt, "audio");
        out->prompt_embedded_context = acp_flag(prompt, "embeddedContext");
        json_object *mcp = acp_field(caps, "mcpCapabilities");
        out->mcp_http = acp_flag(mcp, "http");
        out->mcp_sse = acp_flag(mcp, "sse");
        out->agent_name = acp_string(info, "name");
        out->agent_version = acp_string(info, "version");
    }
    json_object_put(message);
    return ACP_OK;
}

void acp_agent_info_free(acp_agent_info *info) {
    if (info == NULL) return;
    free(info->agent_name);
    free(info->agent_version);
    memset(info, 0, sizeof *info);
}

acp_status acp_client_authenticate_write(const char *request_id, const char *method_id, acp_buffer *message_json) {
    if (request_id == NULL || method_id == NULL || message_json == NULL) return ACP_INVALID_ARGUMENT;
    json_object *params = json_object_new_object();
    json_object_object_add(params, "methodId", json_object_new_string(method_id));
    return send_or_refuse(outgoing(request_id, ACP_AGENT_AUTHENTICATE, params), message_json);
}

// 「[Opus 5.5] --- sessions ---」

// 「[Opus 5.5] The servers as McpServer's http shape, or NULL when one is not whole.」
static json_object *servers_of(const acp_mcp_http *servers, size_t count) {
    if (count > 0 && servers == NULL) return NULL;
    for (size_t i = 0; i < count; i++) {
        if (servers[i].name == NULL || servers[i].url == NULL) return NULL;
        if (servers[i].header_count > 0 && servers[i].headers == NULL) return NULL;
        for (size_t h = 0; h < servers[i].header_count; h++)
            if (servers[i].headers[h].name == NULL || servers[i].headers[h].value == NULL) return NULL;
    }
    json_object *list = json_object_new_array();
    for (size_t i = 0; i < count; i++) {
        json_object *server = json_object_new_object();
        json_object_object_add(server, "type", json_object_new_string("http"));
        json_object_object_add(server, "name", json_object_new_string(servers[i].name));
        json_object_object_add(server, "url", json_object_new_string(servers[i].url));
        json_object *headers = json_object_new_array();
        for (size_t h = 0; h < servers[i].header_count; h++) {
            json_object *header = json_object_new_object();
            json_object_object_add(header, "name", json_object_new_string(servers[i].headers[h].name));
            json_object_object_add(header, "value", json_object_new_string(servers[i].headers[h].value));
            json_object_array_add(headers, header);
        }
        json_object_object_add(server, "headers", headers);
        json_object_array_add(list, server);
    }
    return list;
}

// 「[Opus 5.5] session/new, session/load or session/resume: the session for the latter two, the absolute cwd, the servers.」
static acp_status session_request(const char *method, const char *request_id, const char *session_id, const char *cwd,
    const acp_mcp_http *servers, size_t count, acp_buffer *message_json) {
    if (request_id == NULL || cwd == NULL || cwd[0] != '/' || message_json == NULL) return ACP_INVALID_ARGUMENT;
    json_object *list = servers_of(servers, count);
    if (list == NULL) return ACP_INVALID_ARGUMENT;
    json_object *params = json_object_new_object();
    if (session_id != NULL) json_object_object_add(params, "sessionId", json_object_new_string(session_id));
    json_object_object_add(params, "cwd", json_object_new_string(cwd));
    json_object_object_add(params, "mcpServers", list);
    return send_or_refuse(outgoing(request_id, method, params), message_json);
}

acp_status acp_client_session_new_write(const char *request_id, const char *cwd,
    const acp_mcp_http *servers, size_t count, acp_buffer *message_json) {
    return session_request(ACP_AGENT_SESSION_NEW, request_id, NULL, cwd, servers, count, message_json);
}

acp_status acp_client_session_load_write(const char *request_id, const char *session_id, const char *cwd,
    const acp_mcp_http *servers, size_t count, acp_buffer *message_json) {
    if (session_id == NULL) return ACP_INVALID_ARGUMENT;
    return session_request(ACP_AGENT_SESSION_LOAD, request_id, session_id, cwd, servers, count, message_json);
}

acp_status acp_client_session_resume_write(const char *request_id, const char *session_id, const char *cwd,
    const acp_mcp_http *servers, size_t count, acp_buffer *message_json) {
    if (session_id == NULL) return ACP_INVALID_ARGUMENT;
    return session_request(ACP_AGENT_SESSION_RESUME, request_id, session_id, cwd, servers, count, message_json);
}

acp_status acp_client_session_new_read(const char *message_json, size_t length, char **session_id) {
    if (message_json == NULL || session_id == NULL) return ACP_INVALID_ARGUMENT;
    json_object *message = acp_parse(message_json, length);
    *session_id = acp_string(acp_field(message, "result"), "sessionId");
    json_object_put(message);
    return ACP_OK;
}

void acp_session_id_free(char **session_id) {
    if (session_id == NULL) return;
    free(*session_id);
    *session_id = NULL;
}

acp_status acp_client_session_set_config_option_write(const char *request_id, const char *session_id,
    const char *config_id, const char *value, acp_buffer *message_json) {
    if (request_id == NULL || session_id == NULL || config_id == NULL || value == NULL || message_json == NULL)
        return ACP_INVALID_ARGUMENT;
    json_object *params = json_object_new_object();
    json_object_object_add(params, "sessionId", json_object_new_string(session_id));
    json_object_object_add(params, "configId", json_object_new_string(config_id));
    json_object_object_add(params, "value", json_object_new_string(value));
    return send_or_refuse(outgoing(request_id, ACP_AGENT_SESSION_SET_CONFIG_OPTION, params), message_json);
}

// 「[Opus 5.5] --- prompts ---」

acp_status acp_client_session_prompt_write(const char *request_id, const char *session_id, const char *text, size_t length,
    acp_buffer *message_json) {
    if (request_id == NULL || session_id == NULL || text == NULL || length > INT32_MAX || message_json == NULL)
        return ACP_INVALID_ARGUMENT;
    json_object *block = json_object_new_object();
    json_object_object_add(block, "type", json_object_new_string("text"));
    json_object_object_add(block, "text", json_object_new_string_len(text, (int) length));
    json_object *prompt = json_object_new_array();
    json_object_array_add(prompt, block);
    json_object *params = json_object_new_object();
    json_object_object_add(params, "sessionId", json_object_new_string(session_id));
    json_object_object_add(params, "prompt", prompt);
    return send_or_refuse(outgoing(request_id, ACP_AGENT_SESSION_PROMPT, params), message_json);
}

acp_status acp_client_session_prompt_read(const char *message_json, size_t length, int *stopped, acp_stop_reason *reason) {
    if (message_json == NULL || stopped == NULL || reason == NULL) return ACP_INVALID_ARGUMENT;
    // 「[Opus 5.5] StopReason, in the schema's order, as acp_stop_reason numbers them.」
    static const char *const named[] = {"end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled"};
    *stopped = 0;
    json_object *message = acp_parse(message_json, length);
    char *said = acp_string(acp_field(message, "result"), "stopReason");
    for (size_t i = 0; said != NULL && i < sizeof named / sizeof named[0]; i++) {
        if (strcmp(said, named[i]) == 0) {
            *stopped = 1;
            *reason = (acp_stop_reason) i;
        }
    }
    free(said);
    json_object_put(message);
    return ACP_OK;
}

acp_status acp_client_session_cancel_write(const char *session_id, acp_buffer *message_json) {
    if (session_id == NULL || message_json == NULL) return ACP_INVALID_ARGUMENT;
    json_object *params = json_object_new_object();
    json_object_object_add(params, "sessionId", json_object_new_string(session_id));
    return send_or_refuse(outgoing(NULL, ACP_AGENT_SESSION_CANCEL, params), message_json);
}

// 「[Opus 5.5] --- updates ---」

static int uint64_of(json_object *value, unsigned long long *out) {
    if (!json_object_is_type(value, json_type_int)) return 0;
    *out = (unsigned long long) json_object_get_uint64(value);
    return 1;
}

acp_status acp_client_session_update_read(const char *message_json, size_t length, acp_update *out) {
    if (message_json == NULL || out == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    json_object *message = acp_parse(message_json, length);
    char *method = acp_string(message, "method");
    int is_update = method != NULL && strcmp(method, ACP_CLIENT_SESSION_UPDATE) == 0;
    free(method);
    json_object *params = acp_field(message, "params");
    json_object *update = acp_field(params, "update");
    if (is_update) {
        out->session_id = acp_string(params, "sessionId");
        out->type = acp_string(update, "sessionUpdate");
    }
    if (out->type != NULL) {
        out->kind = ACP_UPDATE_OTHER;
        out->update = json_text(update);
        static const struct { const char *type; acp_update_kind kind; } chunks[] = {
            {"user_message_chunk", ACP_UPDATE_USER_MESSAGE},
            {"agent_message_chunk", ACP_UPDATE_AGENT_MESSAGE},
            {"agent_thought_chunk", ACP_UPDATE_AGENT_THOUGHT},
        };
        for (size_t i = 0; i < sizeof chunks / sizeof chunks[0]; i++) {
            if (strcmp(out->type, chunks[i].type) != 0) continue;
            out->kind = chunks[i].kind;
            json_object *content = acp_field(update, "content");
            char *type = acp_string(content, "type");
            json_object *text = acp_field(content, "text");
            if (type != NULL && strcmp(type, "text") == 0 && json_object_is_type(text, json_type_string)) {
                // 「[Opus 5.5] Every byte, a NUL among them.」
                size_t n = (size_t) json_object_get_string_len(text);
                if ((out->text = malloc(n + 1)) != NULL) {
                    memcpy(out->text, json_object_get_string(text), n);
                    out->text[n] = '\0';
                    out->text_length = n;
                }
            }
            free(type);
        }
        int call = strcmp(out->type, "tool_call") == 0, call_update = strcmp(out->type, "tool_call_update") == 0;
        if (call || call_update) {
            out->kind = call ? ACP_UPDATE_TOOL_CALL : ACP_UPDATE_TOOL_CALL_UPDATE;
            out->tool_call_id = acp_string(update, "toolCallId");
            out->title = acp_string(update, "title");
            out->name = acp_string(update, "name");
            out->tool_kind = acp_string(update, "kind");
            out->status = acp_string(update, "status");
            out->raw_input = json_text(acp_field(update, "rawInput"));
            out->raw_output = json_text(acp_field(update, "rawOutput"));
        }
        if (strcmp(out->type, "usage_update") == 0) {
            out->kind = ACP_UPDATE_USAGE;
            uint64_of(acp_field(update, "used"), &out->used);
            uint64_of(acp_field(update, "size"), &out->size);
            json_object *cost = acp_field(update, "cost");
            out->cost_amount = json_text(acp_field(cost, "amount"));
            out->cost_currency = acp_string(cost, "currency");
        }
    }
    json_object_put(message);
    return ACP_OK;
}

void acp_update_free(acp_update *update) {
    if (update == NULL) return;
    char *const owned[] = {update->session_id, update->type, update->update, update->text, update->tool_call_id,
        update->title, update->name, update->tool_kind, update->status, update->raw_input, update->raw_output,
        update->cost_amount, update->cost_currency};
    for (size_t i = 0; i < sizeof owned / sizeof owned[0]; i++) free(owned[i]);
    memset(update, 0, sizeof *update);
}

// 「[Opus 5.5] --- permission ---」

acp_status acp_client_session_request_permission_read(const char *params_json, size_t length, acp_permission_asked *out) {
    if (params_json == NULL || out == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    json_object *params = acp_parse(params_json, length);
    json_object *call = acp_field(params, "toolCall");
    json_object *options = acp_field(params, "options");
    acp_status status = ACP_OK;
    out->session_id = acp_string(params, "sessionId");
    out->tool_call_id = acp_string(call, "toolCallId");
    if (out->session_id == NULL || out->tool_call_id == NULL || !json_object_is_type(options, json_type_array)) {
        status = ACP_INVALID_PARAMS;
    } else {
        out->title = acp_string(call, "title");
        out->tool_kind = acp_string(call, "kind");
        out->raw_input = json_text(acp_field(call, "rawInput"));
        size_t n = json_object_array_length(options);
        if (n > 0 && (out->options = calloc(n, sizeof *out->options)) == NULL) status = ACP_NO_MEMORY;
        for (size_t i = 0; status == ACP_OK && i < n; i++) {
            json_object *option = json_object_array_get_idx(options, i);
            acp_permission_choice *choice = &out->options[out->count++];
            choice->option_id = acp_string(option, "optionId");
            choice->name = acp_string(option, "name");
            choice->kind = acp_string(option, "kind");
            if (choice->option_id == NULL || choice->name == NULL || choice->kind == NULL) status = ACP_INVALID_PARAMS;
        }
    }
    json_object_put(params);
    if (status != ACP_OK) acp_permission_asked_free(out);
    return status;
}

void acp_permission_asked_free(acp_permission_asked *asked) {
    if (asked == NULL) return;
    for (size_t i = 0; i < asked->count; i++) {
        free(asked->options[i].option_id);
        free(asked->options[i].name);
        free(asked->options[i].kind);
    }
    free(asked->options);
    free(asked->session_id);
    free(asked->tool_call_id);
    free(asked->title);
    free(asked->tool_kind);
    free(asked->raw_input);
    memset(asked, 0, sizeof *asked);
}

acp_status acp_client_session_request_permission_write(const char *request_id, const char *option_id, acp_buffer *message_json) {
    if (request_id == NULL || message_json == NULL) return ACP_INVALID_ARGUMENT;
    json_object *outcome = json_object_new_object();
    json_object_object_add(outcome, "outcome", json_object_new_string(option_id != NULL ? "selected" : "cancelled"));
    if (option_id != NULL) json_object_object_add(outcome, "optionId", json_object_new_string(option_id));
    json_object *result = json_object_new_object();
    json_object_object_add(result, "outcome", outcome);
    return send_or_refuse(response(request_id, "result", result), message_json);
}
