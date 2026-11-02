#include "internal.h"

#include <limits.h>
#include <stdlib.h>
#include <string.h>

acp_status acp_read_text_file_request_write(const char *request_id, const char *session_id, const char *path,
    acp_buffer *message_json) {
    return acp_read_text_file_lines_request_write(request_id, session_id, path, 0, 0, message_json);
}

acp_status acp_read_text_file_lines_request_write(const char *request_id, const char *session_id, const char *path,
    uint32_t line, uint32_t limit, acp_buffer *message_json) {
    if (request_id == NULL || session_id == NULL || path == NULL || message_json == NULL) return ACP_INVALID_ARGUMENT;
    json_object *id = acp_parse(request_id, strlen(request_id));
    if (id == NULL) return ACP_INVALID_ARGUMENT;
    json_object *params = json_object_new_object();
    json_object_object_add(params, "sessionId", json_object_new_string(session_id));
    json_object_object_add(params, "path", json_object_new_string(path));
    if (line != 0) json_object_object_add(params, "line", json_object_new_int64(line));
    if (limit != 0) json_object_object_add(params, "limit", json_object_new_int64(limit));
    json_object *message = json_object_new_object();
    json_object_object_add(message, "jsonrpc", json_object_new_string("2.0"));
    json_object_object_add(message, "id", id);
    json_object_object_add(message, "method", json_object_new_string(ACP_CLIENT_FS_READ_TEXT_FILE));
    json_object_object_add(message, "params", params);
    return acp_finish(message, message_json);
}

acp_status acp_read_text_file_result_read(const char *message_json, size_t length, acp_text_file *out) {
    if (message_json == NULL || out == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    json_object *message = acp_parse(message_json, length);
    json_object *content = acp_field(acp_field(message, "result"), "content");
    json_object *code = acp_field(acp_field(message, "error"), "code");
    out->not_found = json_object_is_type(code, json_type_int) && json_object_get_int64(code) == -32002;
    acp_status status = ACP_OK;
    if (json_object_is_type(content, json_type_string)) {
        // Every byte, as the text may hold a NUL a C string would stop at.
        int n = json_object_get_string_len(content);
        if ((out->content = malloc((size_t) n + 1)) == NULL) {
            status = ACP_NO_MEMORY;
        } else {
            memcpy(out->content, json_object_get_string(content), (size_t) n);
            out->content[n] = '\0';
            out->length = (size_t) n;
        }
    }
    json_object_put(message);
    return status;
}

void acp_text_file_free(acp_text_file *file) {
    if (file == NULL) return;
    free(file->content);
    file->content = NULL;
    file->length = 0;
    file->not_found = 0;
}

acp_status acp_write_text_file_request_write(const char *request_id, const char *session_id, const char *path,
    const char *content, size_t content_length, acp_buffer *message_json) {
    if (request_id == NULL || session_id == NULL || path == NULL || content == NULL || message_json == NULL ||
        content_length > INT_MAX)
        return ACP_INVALID_ARGUMENT;
    json_object *id = acp_parse(request_id, strlen(request_id));
    if (id == NULL) return ACP_INVALID_ARGUMENT;
    json_object *params = json_object_new_object();
    json_object_object_add(params, "sessionId", json_object_new_string(session_id));
    json_object_object_add(params, "path", json_object_new_string(path));
    json_object_object_add(params, "content", json_object_new_string_len(content, (int) content_length));
    json_object *message = json_object_new_object();
    json_object_object_add(message, "jsonrpc", json_object_new_string("2.0"));
    json_object_object_add(message, "id", id);
    json_object_object_add(message, "method", json_object_new_string(ACP_CLIENT_FS_WRITE_TEXT_FILE));
    json_object_object_add(message, "params", params);
    return acp_finish(message, message_json);
}

acp_status acp_write_text_file_result_read(const char *message_json, size_t length, int *written) {
    if (message_json == NULL || written == NULL) return ACP_INVALID_ARGUMENT;
    json_object *message = acp_parse(message_json, length);
    *written = json_object_is_type(acp_field(message, "result"), json_type_object);
    json_object_put(message);
    return ACP_OK;
}
