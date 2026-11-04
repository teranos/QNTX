#include "internal.h"

#include <stdlib.h>
#include <string.h>

// What a block gives: "required" are a link's name and uri, a resource's uri, and every
// block's type. One missing any is another kind, told by the type it names, or none.
static acp_status block_of(json_object *value, acp_prompt_block *block) {
    memset(block, 0, sizeof *block);
    block->kind = ACP_BLOCK_OTHER;
    if (!json_object_is_type(value, json_type_object)) return ACP_OK;
    block->type = acp_string(value, "type");
    if (block->type == NULL) return ACP_OK;
    if (strcmp(block->type, "text") == 0) {
        block->kind = ACP_BLOCK_TEXT;
        block->text = acp_string(value, "text");
    } else if (strcmp(block->type, "resource_link") == 0) {
        block->name = acp_string(value, "name");
        block->uri = acp_string(value, "uri");
        if (block->name != NULL && block->uri != NULL) block->kind = ACP_BLOCK_RESOURCE_LINK;
    } else if (strcmp(block->type, "resource") == 0) {
        json_object *resource = acp_field(value, "resource");
        block->uri = acp_string(resource, "uri");
        block->text = acp_string(resource, "text");
        if (block->uri != NULL && block->text != NULL) block->kind = ACP_BLOCK_RESOURCE;
        else block->holds_blob = block->uri != NULL && json_object_is_type(acp_field(resource, "blob"), json_type_string);
    } else if (strcmp(block->type, "image") == 0 || strcmp(block->type, "audio") == 0) {
        block->mime_type = acp_string(value, "mimeType");
        block->data = acp_string(value, "data");
        if (block->mime_type != NULL && block->data != NULL)
            block->kind = strcmp(block->type, "image") == 0 ? ACP_BLOCK_IMAGE : ACP_BLOCK_AUDIO;
    }
    return ACP_OK;
}

acp_status acp_prompt_read(const char *params_json, size_t length, acp_prompt *out, acp_buffer *error_json) {
    if (params_json == NULL || out == NULL || error_json == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    error_json->data = NULL;
    error_json->length = 0;
    json_object *params = acp_parse(params_json, length);
    json_object *prompt = acp_field(params, "prompt");
    acp_status status = ACP_OK;
    if (!json_object_is_type(prompt, json_type_array)) {
        status = acp_invalid(error_json, "session/prompt needs prompt, a list of content blocks");
    } else {
        size_t n = json_object_array_length(prompt);
        if (n > 0 && (out->blocks = calloc(n, sizeof *out->blocks)) == NULL) status = ACP_NO_MEMORY;
        for (size_t i = 0; i < n && status == ACP_OK; i++) {
            status = block_of(json_object_array_get_idx(prompt, i), &out->blocks[i]);
            out->count = i + 1;
        }
    }
    json_object_put(params);
    if (status != ACP_OK) acp_prompt_free(out);
    return status;
}

void acp_prompt_free(acp_prompt *prompt) {
    if (prompt == NULL) return;
    for (size_t i = 0; i < prompt->count; i++) {
        free(prompt->blocks[i].type);
        free(prompt->blocks[i].text);
        free(prompt->blocks[i].name);
        free(prompt->blocks[i].uri);
        free(prompt->blocks[i].mime_type);
        free(prompt->blocks[i].data);
    }
    free(prompt->blocks);
    prompt->blocks = NULL;
    prompt->count = 0;
}

acp_status acp_cancel_read(const char *message_json, size_t length, acp_cancel *out) {
    if (message_json == NULL || out == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    json_object *message = acp_parse(message_json, length);
    char *method = acp_string(message, "method");
    json_object *params = acp_field(message, "params");
    acp_status status = ACP_OK;
    if (method != NULL && strcmp(method, ACP_AGENT_SESSION_CANCEL) == 0) {
        out->kind = ACP_CANCEL_SESSION;
        out->session_id = acp_string(params, "sessionId");
    } else if (method != NULL && strcmp(method, ACP_PROTOCOL_CANCEL_REQUEST) == 0) {
        out->kind = ACP_CANCEL_REQUEST;
        json_object *id = acp_field(params, "requestId");
        if (id != NULL) {
            const char *text = json_object_to_json_string_ext(id, JSON_C_TO_STRING_PLAIN | JSON_C_TO_STRING_NOSLASHESCAPE);
            size_t n = strlen(text) + 1;
            if ((out->request_id = malloc(n)) == NULL) status = ACP_NO_MEMORY;
            else memcpy(out->request_id, text, n);
        }
    }
    free(method);
    json_object_put(message);
    return status;
}

void acp_cancel_free(acp_cancel *cancel) {
    if (cancel == NULL) return;
    free(cancel->session_id);
    free(cancel->request_id);
    memset(cancel, 0, sizeof *cancel);
}

acp_status acp_prompt_result_write(acp_stop_reason reason, acp_buffer *result_json) {
    static const char *const named[] = {"end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled"};
    if (result_json == NULL || (unsigned) reason >= sizeof named / sizeof *named) return ACP_INVALID_ARGUMENT;
    json_object *result = json_object_new_object();
    json_object_object_add(result, "stopReason", json_object_new_string(named[reason]));
    return acp_finish(result, result_json);
}
