#include "internal.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Strings passed to Ground's C ABI must not silently stop at an embedded NUL.
static int new_string(json_object *value) {
    return json_object_is_type(value, json_type_string)
        && strlen(json_object_get_string(value)) == (size_t) json_object_get_string_len(value);
}

static int new_pairs(json_object *list) {
    if (!json_object_is_type(list, json_type_array)) return 0;
    for (size_t i = 0; i < json_object_array_length(list); i++) {
        json_object *entry = json_object_array_get_idx(list, i);
        if (!new_string(acp_field(entry, "name")) || !new_string(acp_field(entry, "value"))) return 0;
    }
    return 1;
}

// v1's three MCP shapes. SSE may be well formed even when the agent does not offer it.
static int new_server(json_object *server) {
    if (!new_string(acp_field(server, "name"))) return 0;
    json_object *type = acp_field(server, "type");
    if (type != NULL) {
        if (!new_string(type)) return 0;
        const char *kind = json_object_get_string(type);
        if (strcmp(kind, "http") != 0 && strcmp(kind, "sse") != 0) return 0;
        return new_string(acp_field(server, "url")) && new_pairs(acp_field(server, "headers"));
    }
    json_object *command = acp_field(server, "command");
    json_object *args = acp_field(server, "args");
    if (!new_string(command) || json_object_get_string(command)[0] != '/'
        || !json_object_is_type(args, json_type_array) || !new_pairs(acp_field(server, "env"))) return 0;
    for (size_t i = 0; i < json_object_array_length(args); i++)
        if (!new_string(json_object_array_get_idx(args, i))) return 0;
    return 1;
}

acp_status acp_session_new_read(const char *params_json, size_t length,
                                acp_session_new *out, acp_buffer *error_json) {
    if (params_json == NULL || out == NULL || error_json == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    memset(error_json, 0, sizeof *error_json);
    json_object *params = acp_parse(params_json, length);
    json_object *cwd = acp_field(params, "cwd");
    json_object *servers = acp_field(params, "mcpServers");
    acp_status status = ACP_OK;
    if (!json_object_is_type(params, json_type_object)) {
        status = acp_invalid(error_json, "session/new params are not a JSON object");
    } else if (!new_string(cwd)) {
        status = acp_invalid(error_json, "session/new needs cwd");
    } else if (json_object_get_string(cwd)[0] != '/') {
        char message[4200];
        snprintf(message, sizeof message, "cwd must be an absolute path: %s", json_object_get_string(cwd));
        status = acp_invalid(error_json, message);
    } else if (!json_object_is_type(servers, json_type_array)) {
        status = acp_invalid(error_json, "session/new needs mcpServers");
    } else {
        for (size_t i = 0; i < json_object_array_length(servers); i++) {
            if (!new_server(json_object_array_get_idx(servers, i))) {
                status = acp_invalid(error_json, "session/new has an invalid MCP server configuration");
                break;
            }
        }
        if (status == ACP_OK) {
            out->cwd = acp_string(params, "cwd");
            if (out->cwd == NULL) status = ACP_NO_MEMORY;
            else status = acp_directories_read(params_json, length, &out->additional_directories, error_json);
        }
    }
    json_object_put(params);
    if (status != ACP_OK) acp_session_new_free(out);
    return status;
}

void acp_session_new_free(acp_session_new *session) {
    if (session == NULL) return;
    free(session->cwd);
    session->cwd = NULL;
    acp_buffer_free(&session->additional_directories);
}

acp_status acp_session_new_result(const char *session_id, const char *modes_json,
                                  const char *config_options_json, acp_buffer *out_json) {
    if (session_id == NULL || out_json == NULL) return ACP_INVALID_ARGUMENT;
    memset(out_json, 0, sizeof *out_json);
    json_object *modes = modes_json ? acp_parse(modes_json, strlen(modes_json)) : NULL;
    json_object *config = config_options_json ? acp_parse(config_options_json, strlen(config_options_json)) : NULL;
    if ((modes_json != NULL && !json_object_is_type(modes, json_type_object))
        || (config_options_json != NULL && !json_object_is_type(config, json_type_array))) {
        json_object_put(modes);
        json_object_put(config);
        return ACP_INVALID_ARGUMENT;
    }
    json_object *result = json_object_new_object();
    if (result == NULL) {
        json_object_put(modes);
        json_object_put(config);
        return ACP_NO_MEMORY;
    }
    json_object_object_add(result, "sessionId", json_object_new_string(session_id));
    if (modes != NULL) json_object_object_add(result, "modes", modes);
    if (config != NULL) json_object_object_add(result, "configOptions", config);
    return acp_finish(result, out_json);
}

// The session and its absolute cwd, as method's request names them; load names its servers too.
static acp_status resume_read(const char *method, int servers, const char *params_json, size_t length,
                              acp_session_resume *out, acp_buffer *error_json) {
    if (params_json == NULL || out == NULL || error_json == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    error_json->data = NULL;
    error_json->length = 0;

    json_object *params = acp_parse(params_json, length);
    acp_status status = ACP_OK;
    char message[4200];
    if (!json_object_is_type(params, json_type_object)) {
        snprintf(message, sizeof message, "%s params are not a JSON object", method);
        status = acp_invalid(error_json, message);
    } else if ((out->session_id = acp_string(params, "sessionId")) == NULL) {
        snprintf(message, sizeof message, "%s needs sessionId", method);
        status = acp_invalid(error_json, message);
    } else if ((out->cwd = acp_string(params, "cwd")) == NULL) {
        snprintf(message, sizeof message, "%s needs cwd", method);
        status = acp_invalid(error_json, message);
    } else if (out->cwd[0] != '/') {
        snprintf(message, sizeof message, "cwd must be an absolute path: %s", out->cwd);
        status = acp_invalid(error_json, message);
    } else if (servers && !json_object_is_type(acp_field(params, "mcpServers"), json_type_array)) {
        snprintf(message, sizeof message, "%s needs mcpServers", method);
        status = acp_invalid(error_json, message);
    }
    json_object_put(params);
    if (status != ACP_OK) acp_session_resume_free(out);
    return status;
}

acp_status acp_session_resume_read(const char *params_json, size_t length,
                                   acp_session_resume *out, acp_buffer *error_json) {
    return resume_read(ACP_AGENT_SESSION_RESUME, 0, params_json, length, out, error_json);
}

acp_status acp_session_load_read(const char *params_json, size_t length,
                                 acp_session_resume *out, acp_buffer *error_json) {
    return resume_read(ACP_AGENT_SESSION_LOAD, 1, params_json, length, out, error_json);
}

acp_status acp_session_target_read(const char *method, const char *params_json, size_t length,
                                   char **session_id, acp_buffer *error_json) {
    if (method == NULL || params_json == NULL || session_id == NULL || error_json == NULL) return ACP_INVALID_ARGUMENT;
    *session_id = NULL;
    error_json->data = NULL;
    error_json->length = 0;
    json_object *params = acp_parse(params_json, length);
    acp_status status = ACP_OK;
    char message[256];
    if (!json_object_is_type(params, json_type_object) || (*session_id = acp_string(params, "sessionId")) == NULL) {
        snprintf(message, sizeof message, "%s needs sessionId", method);
        status = acp_invalid(error_json, message);
    }
    json_object_put(params);
    return status;
}

acp_status acp_directories_read(const char *params_json, size_t length, acp_buffer *list_json, acp_buffer *error_json) {
    if (params_json == NULL || list_json == NULL || error_json == NULL) return ACP_INVALID_ARGUMENT;
    list_json->data = NULL;
    list_json->length = 0;
    error_json->data = NULL;
    error_json->length = 0;
    json_object *params = acp_parse(params_json, length);
    json_object *named = acp_field(params, "additionalDirectories");
    acp_status status = ACP_OK;
    json_object *list = json_object_new_array();
    if (named != NULL && !json_object_is_type(named, json_type_array)) {
        status = acp_invalid(error_json, "additionalDirectories must be a list of paths");
    } else {
        size_t n = named ? json_object_array_length(named) : 0;
        for (size_t i = 0; i < n && status == ACP_OK; i++) {
            json_object *path = json_object_array_get_idx(named, i);
            if (!json_object_is_type(path, json_type_string)) {
                status = acp_invalid(error_json, "additionalDirectories must be a list of paths");
                break;
            }
            const char *s = json_object_get_string(path);
            // "Each path must be absolute."
            if (s[0] != '/') {
                char message[4200];
                snprintf(message, sizeof message, "additionalDirectories must be absolute paths: %s", s);
                status = acp_invalid(error_json, message);
                break;
            }
            json_object_array_add(list, json_object_new_string(s));
        }
    }
    json_object_put(params);
    if (status != ACP_OK) {
        json_object_put(list);
        return status;
    }
    return acp_finish(list, list_json);
}

void acp_session_target_free(char **session_id) {
    if (session_id == NULL) return;
    free(*session_id);
    *session_id = NULL;
}

void acp_session_resume_free(acp_session_resume *resume) {
    if (resume == NULL) return;
    free(resume->session_id);
    free(resume->cwd);
    resume->session_id = NULL;
    resume->cwd = NULL;
}

// A field's string, copied, or NULL when absent or null; anything else is refused as not a string.
static acp_status optional_string(json_object *value, const char *key, char **out, acp_buffer *error_json) {
    if (value == NULL || json_object_is_type(value, json_type_null)) return ACP_OK;
    if (json_object_is_type(value, json_type_string)) {
        const char *s = json_object_get_string(value);
        size_t n = strlen(s) + 1;
        if ((*out = malloc(n)) == NULL) return ACP_NO_MEMORY;
        memcpy(*out, s, n);
        return ACP_OK;
    }
    char message[64];
    snprintf(message, sizeof message, "session/list's %s must be a string", key);
    return acp_invalid(error_json, message);
}

acp_status acp_session_list_read(const char *params_json, size_t length, acp_session_list *out, acp_buffer *error_json) {
    if (params_json == NULL || out == NULL || error_json == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    error_json->data = NULL;
    error_json->length = 0;
    json_object *params = acp_parse(params_json, length);
    acp_status status = optional_string(acp_field(params, "cwd"), "cwd", &out->cwd, error_json);
    if (status == ACP_OK && out->cwd != NULL && out->cwd[0] != '/') {
        char message[4200];
        snprintf(message, sizeof message, "cwd must be an absolute path: %s", out->cwd);
        status = acp_invalid(error_json, message);
    }
    if (status == ACP_OK) status = optional_string(acp_field(params, "cursor"), "cursor", &out->cursor, error_json);
    json_object_put(params);
    if (status != ACP_OK) acp_session_list_free(out);
    return status;
}

void acp_session_list_free(acp_session_list *list) {
    if (list == NULL) return;
    free(list->cwd);
    free(list->cursor);
    list->cwd = NULL;
    list->cursor = NULL;
}

acp_status acp_session_info_write(const acp_session_info *info, acp_buffer *out_json) {
    if (info == NULL || info->session_id == NULL || info->cwd == NULL || out_json == NULL) return ACP_INVALID_ARGUMENT;
    json_object *dirs = NULL;
    if (info->additional_directories != NULL) {
        dirs = acp_parse(info->additional_directories, strlen(info->additional_directories));
        if (!json_object_is_type(dirs, json_type_array)) {
            json_object_put(dirs);
            return ACP_INVALID_ARGUMENT;
        }
    }
    json_object *entry = json_object_new_object();
    json_object_object_add(entry, "sessionId", json_object_new_string(info->session_id));
    json_object_object_add(entry, "cwd", json_object_new_string(info->cwd));
    // "Omitted and empty values are equivalent."
    if (dirs != NULL && json_object_array_length(dirs) > 0) json_object_object_add(entry, "additionalDirectories", dirs);
    else json_object_put(dirs);
    if (info->title != NULL && info->title[0] != '\0')
        json_object_object_add(entry, "title", json_object_new_string(info->title));
    if (info->updated_at != NULL) json_object_object_add(entry, "updatedAt", json_object_new_string(info->updated_at));
    return acp_finish(entry, out_json);
}
