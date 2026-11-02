#include "internal.h"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// jsonrpc and id, for the method its writer names next; NULL when request_id is no JSON.
static json_object *request_head(const char *request_id) {
    json_object *id = acp_parse(request_id, strlen(request_id));
    if (id == NULL) return NULL;
    json_object *message = json_object_new_object();
    json_object_object_add(message, "jsonrpc", json_object_new_string("2.0"));
    json_object_object_add(message, "id", id);
    return message;
}

// The params of a request that names a terminal, and the whole message written.
static acp_status terminal_finish(json_object *message, const char *session_id, const char *terminal_id,
    acp_buffer *message_json) {
    json_object *params = json_object_new_object();
    json_object_object_add(params, "sessionId", json_object_new_string(session_id));
    json_object_object_add(params, "terminalId", json_object_new_string(terminal_id));
    json_object_object_add(message, "params", params);
    return acp_finish(message, message_json);
}

static int terminal_arguments(const char *request_id, const char *session_id, const char *terminal_id,
    const acp_buffer *message_json) {
    return request_id != NULL && session_id != NULL && terminal_id != NULL && message_json != NULL;
}

acp_status acp_terminal_create_request_write(const char *request_id, const char *session_id, const char *command,
    const char *const *args, size_t arg_count, const char *cwd, unsigned long long output_byte_limit,
    acp_buffer *message_json) {
    if (request_id == NULL || session_id == NULL || command == NULL || message_json == NULL ||
        (args == NULL && arg_count > 0) || output_byte_limit > INT64_MAX)
        return ACP_INVALID_ARGUMENT;
    for (size_t i = 0; i < arg_count; i++)
        if (args[i] == NULL) return ACP_INVALID_ARGUMENT;
    json_object *message = request_head(request_id);
    if (message == NULL) return ACP_INVALID_ARGUMENT;
    json_object *params = json_object_new_object();
    json_object_object_add(params, "sessionId", json_object_new_string(session_id));
    json_object_object_add(params, "command", json_object_new_string(command));
    if (arg_count > 0) {
        json_object *list = json_object_new_array();
        for (size_t i = 0; i < arg_count; i++) json_object_array_add(list, json_object_new_string(args[i]));
        json_object_object_add(params, "args", list);
    }
    if (cwd != NULL) json_object_object_add(params, "cwd", json_object_new_string(cwd));
    if (output_byte_limit > 0)
        json_object_object_add(params, "outputByteLimit", json_object_new_int64((int64_t) output_byte_limit));
    json_object_object_add(message, "method", json_object_new_string(ACP_CLIENT_TERMINAL_CREATE));
    json_object_object_add(message, "params", params);
    return acp_finish(message, message_json);
}

acp_status acp_terminal_create_result_read(const char *message_json, size_t length, char **terminal_id) {
    if (message_json == NULL || terminal_id == NULL) return ACP_INVALID_ARGUMENT;
    json_object *message = acp_parse(message_json, length);
    json_object *result = acp_field(message, "result");
    *terminal_id = acp_string(result, "terminalId");
    // An id holding a NUL would name another terminal once it is a C string.
    json_object *id = acp_field(result, "terminalId");
    if (*terminal_id != NULL && strlen(*terminal_id) != (size_t) json_object_get_string_len(id)) {
        free(*terminal_id);
        *terminal_id = NULL;
    }
    json_object_put(message);
    return ACP_OK;
}

void acp_terminal_id_free(char *terminal_id) {
    free(terminal_id);
}

acp_status acp_terminal_output_request_write(const char *request_id, const char *session_id, const char *terminal_id,
    acp_buffer *message_json) {
    if (!terminal_arguments(request_id, session_id, terminal_id, message_json)) return ACP_INVALID_ARGUMENT;
    json_object *message = request_head(request_id);
    if (message == NULL) return ACP_INVALID_ARGUMENT;
    json_object_object_add(message, "method", json_object_new_string(ACP_CLIENT_TERMINAL_OUTPUT));
    return terminal_finish(message, session_id, terminal_id, message_json);
}

acp_status acp_terminal_wait_for_exit_request_write(const char *request_id, const char *session_id,
    const char *terminal_id, acp_buffer *message_json) {
    if (!terminal_arguments(request_id, session_id, terminal_id, message_json)) return ACP_INVALID_ARGUMENT;
    json_object *message = request_head(request_id);
    if (message == NULL) return ACP_INVALID_ARGUMENT;
    json_object_object_add(message, "method", json_object_new_string(ACP_CLIENT_TERMINAL_WAIT_FOR_EXIT));
    return terminal_finish(message, session_id, terminal_id, message_json);
}

acp_status acp_terminal_kill_request_write(const char *request_id, const char *session_id, const char *terminal_id,
    acp_buffer *message_json) {
    if (!terminal_arguments(request_id, session_id, terminal_id, message_json)) return ACP_INVALID_ARGUMENT;
    json_object *message = request_head(request_id);
    if (message == NULL) return ACP_INVALID_ARGUMENT;
    json_object_object_add(message, "method", json_object_new_string(ACP_CLIENT_TERMINAL_KILL));
    return terminal_finish(message, session_id, terminal_id, message_json);
}

acp_status acp_terminal_release_request_write(const char *request_id, const char *session_id, const char *terminal_id,
    acp_buffer *message_json) {
    if (!terminal_arguments(request_id, session_id, terminal_id, message_json)) return ACP_INVALID_ARGUMENT;
    json_object *message = request_head(request_id);
    if (message == NULL) return ACP_INVALID_ARGUMENT;
    json_object_object_add(message, "method", json_object_new_string(ACP_CLIENT_TERMINAL_RELEASE));
    return terminal_finish(message, session_id, terminal_id, message_json);
}

// TerminalExitStatus: exitCode, a uint32, and signal, each null when the other ended it:
// "The process exit code (may be null if terminated by signal)"
// "The signal that terminated the process (may be null if exited normally)"
static void exit_status(json_object *status, acp_terminal_exit *out) {
    out->exited = json_object_is_type(status, json_type_object);
    json_object *code = acp_field(status, "exitCode");
    if (json_object_is_type(code, json_type_int)) {
        int64_t n = json_object_get_int64(code);
        if (n >= 0 && n <= UINT32_MAX) {
            out->has_exit_code = 1;
            out->exit_code = (unsigned long) n;
        }
    }
    out->signal = acp_string(status, "signal");
}

acp_status acp_terminal_exit_read(const char *message_json, size_t length, acp_terminal_exit *out) {
    if (message_json == NULL || out == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    json_object *message = acp_parse(message_json, length);
    exit_status(acp_field(message, "result"), out);
    json_object_put(message);
    return ACP_OK;
}

void acp_terminal_exit_free(acp_terminal_exit *ended) {
    if (ended == NULL) return;
    free(ended->signal);
    memset(ended, 0, sizeof *ended);
}

acp_status acp_terminal_output_result_read(const char *message_json, size_t length, acp_terminal_output *out) {
    if (message_json == NULL || out == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    json_object *message = acp_parse(message_json, length);
    json_object *result = acp_field(message, "result");
    json_object *output = acp_field(result, "output");
    acp_status status = ACP_OK;
    if (json_object_is_type(output, json_type_string)) {
        // Every byte, as the output may hold a NUL a C string would stop at.
        int n = json_object_get_string_len(output);
        if ((out->output = malloc((size_t) n + 1)) == NULL) {
            status = ACP_NO_MEMORY;
        } else {
            memcpy(out->output, json_object_get_string(output), (size_t) n);
            out->output[n] = '\0';
            out->length = (size_t) n;
            out->truncated = acp_flag(result, "truncated");
            exit_status(acp_field(result, "exitStatus"), &out->exit_status);
        }
    }
    json_object_put(message);
    return status;
}

void acp_terminal_output_free(acp_terminal_output *output) {
    if (output == NULL) return;
    free(output->output);
    acp_terminal_exit_free(&output->exit_status);
    memset(output, 0, sizeof *output);
}

acp_status acp_terminal_answered_read(const char *message_json, size_t length, int *answered) {
    if (message_json == NULL || answered == NULL) return ACP_INVALID_ARGUMENT;
    json_object *message = acp_parse(message_json, length);
    json_object *result = NULL;
    *answered = json_object_is_type(message, json_type_object) && json_object_object_get_ex(message, "result", &result);
    json_object_put(message);
    return ACP_OK;
}
