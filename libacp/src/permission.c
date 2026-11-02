#include "internal.h"

#include <stdlib.h>
#include <string.h>

acp_status acp_permission_request_write(const acp_permission_request *request, acp_buffer *message_json) {
    if (request == NULL || message_json == NULL || request->request_id == NULL || request->session_id == NULL
        || request->tool_call_id == NULL || request->title == NULL || request->status == NULL
        || request->raw_input == NULL || request->options == NULL || request->count == 0)
        return ACP_INVALID_ARGUMENT;
    for (size_t i = 0; i < request->count; i++)
        if (request->options[i].option_id == NULL || request->options[i].name == NULL || request->options[i].kind == NULL)
            return ACP_INVALID_ARGUMENT;
    json_object *id = acp_parse(request->request_id, strlen(request->request_id));
    if (id == NULL) return ACP_INVALID_ARGUMENT;
    // The model's arguments as it gave them: JSON as JSON, anything else as the text it is.
    json_object *input = acp_raw(request->raw_input);
    if (input == NULL) {
        json_object_put(id);
        return ACP_NO_MEMORY;
    }
    json_object *call = json_object_new_object();
    json_object_object_add(call, "toolCallId", json_object_new_string(request->tool_call_id));
    json_object_object_add(call, "title", json_object_new_string(request->title));
    if (request->name != NULL) json_object_object_add(call, "name", json_object_new_string(request->name));
    if (request->kind != NULL) json_object_object_add(call, "kind", json_object_new_string(request->kind));
    json_object_object_add(call, "status", json_object_new_string(request->status));
    json_object_object_add(call, "rawInput", input);
    json_object *options = json_object_new_array();
    for (size_t i = 0; i < request->count; i++) {
        json_object *option = json_object_new_object();
        json_object_object_add(option, "optionId", json_object_new_string(request->options[i].option_id));
        json_object_object_add(option, "name", json_object_new_string(request->options[i].name));
        json_object_object_add(option, "kind", json_object_new_string(request->options[i].kind));
        json_object_array_add(options, option);
    }
    json_object *params = json_object_new_object();
    json_object_object_add(params, "sessionId", json_object_new_string(request->session_id));
    json_object_object_add(params, "toolCall", call);
    json_object_object_add(params, "options", options);
    json_object *message = json_object_new_object();
    json_object_object_add(message, "jsonrpc", json_object_new_string("2.0"));
    json_object_object_add(message, "id", id);
    json_object_object_add(message, "method", json_object_new_string(ACP_CLIENT_SESSION_REQUEST_PERMISSION));
    json_object_object_add(message, "params", params);
    return acp_finish(message, message_json);
}

acp_status acp_permission_outcome_read(const char *message_json, size_t length, acp_permission_outcome *out) {
    if (message_json == NULL || out == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    json_object *message = acp_parse(message_json, length);
    json_object *outcome = acp_field(acp_field(message, "result"), "outcome");
    char *kind = acp_string(outcome, "outcome");
    if (kind != NULL && strcmp(kind, "cancelled") == 0) {
        out->kind = ACP_PERMISSION_CANCELLED;
    } else if (kind != NULL && strcmp(kind, "selected") == 0) {
        out->option_id = acp_string(outcome, "optionId");
        if (out->option_id != NULL) out->kind = ACP_PERMISSION_SELECTED;
    }
    free(kind);
    json_object_put(message);
    return ACP_OK;
}

void acp_permission_outcome_free(acp_permission_outcome *outcome) {
    if (outcome == NULL) return;
    free(outcome->option_id);
    memset(outcome, 0, sizeof *outcome);
}
