#include "internal.h"

#include <stdio.h>
#include <string.h>

// ProtocolVersion is a uint16 in both schemas.
#define ACP_VERSION_MAX 65535

static void read_capabilities(json_object *params, unsigned requested, acp_client_capabilities *caps) {
    memset(caps, 0, sizeof *caps);
    // The request is in the shape of the version the client asked for.
    json_object *c = acp_field(params, requested >= 2 ? "capabilities" : "clientCapabilities");
    json_object *elicitation = acp_field(c, "elicitation");
    caps->elicitation_form = acp_offered(elicitation, "form");
    caps->elicitation_url = acp_offered(elicitation, "url");
    caps->auth_terminal = acp_flag(acp_field(c, "auth"), "terminal");
    if (requested >= 2) return;
    json_object *fs = acp_field(c, "fs");
    caps->fs_read_text_file = acp_flag(fs, "readTextFile");
    caps->fs_write_text_file = acp_flag(fs, "writeTextFile");
    caps->terminal = acp_flag(c, "terminal");
    caps->session_notices = acp_offered(acp_field(c, "session"), "notices");
}

static json_object *rpc_error(int code, const char *message) {
    json_object *error = json_object_new_object();
    json_object_object_add(error, "code", json_object_new_int(code));
    json_object_object_add(error, "message", json_object_new_string(message));
    return error;
}

static acp_status unsupported(acp_buffer *error_json, unsigned requested, const unsigned *supported, size_t n) {
    char message[96];
    snprintf(message, sizeof message, "no supported protocol version is at or below %u", requested);
    json_object *error = rpc_error(ACP_ERROR_UNSUPPORTED_VERSION, message);
    json_object *data = json_object_new_object();
    json_object *listed = json_object_new_array();
    // Listed in ascending order, whatever order the caller named them in.
    unsigned last = 0;
    for (size_t i = 0; i < n; i++) {
        unsigned next = 0;
        for (size_t j = 0; j < n; j++)
            if (supported[j] > last && (next == 0 || supported[j] < next)) next = supported[j];
        if (next == 0) break;
        json_object_array_add(listed, json_object_new_int64(next));
        last = next;
    }
    json_object_object_add(data, "requested", json_object_new_int64(requested));
    json_object_object_add(data, "supported", listed);
    json_object_object_add(error, "data", data);
    acp_status written = acp_finish(error, error_json);
    return written == ACP_OK ? ACP_UNSUPPORTED_VERSION : written;
}

acp_status acp_initialize_read(const char *params_json, size_t length,
                               const unsigned *supported, size_t supported_count,
                               acp_initialize *out, acp_buffer *error_json) {
    if (params_json == NULL || supported == NULL || supported_count == 0 || out == NULL || error_json == NULL)
        return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    error_json->data = NULL;
    error_json->length = 0;

    json_object *params = acp_parse(params_json, length);
    if (!json_object_is_type(params, json_type_object)) {
        json_object_put(params);
        return acp_invalid(error_json, "initialize params are not a JSON object");
    }
    json_object *version = acp_field(params, "protocolVersion");
    if (!json_object_is_type(version, json_type_int)) {
        json_object_put(params);
        return acp_invalid(error_json, "initialize params name no protocolVersion");
    }
    int64_t requested = json_object_get_int64(version);
    if (requested < 0 || requested > ACP_VERSION_MAX) {
        json_object_put(params);
        return acp_invalid(error_json, "protocolVersion is not a uint16");
    }
    out->requested_version = (unsigned) requested;

    // The highest supported at or below what was requested.
    for (size_t i = 0; i < supported_count; i++)
        if (supported[i] <= out->requested_version && supported[i] > out->selected_version)
            out->selected_version = supported[i];

    read_capabilities(params, out->requested_version, &out->capabilities);
    json_object_put(params);
    if (out->selected_version == 0)
        return unsupported(error_json, out->requested_version, supported, supported_count);
    return ACP_OK;
}

static json_object *implementation(const char *name, const char *version) {
    json_object *info = json_object_new_object();
    json_object_object_add(info, "name", json_object_new_string(name));
    json_object_object_add(info, "version", json_object_new_string(version));
    return info;
}

acp_status acp_initialize_result(unsigned selected_version,
                                 const char *agent_name, const char *agent_version,
                                 const char *agent_capabilities_json,
                                 const char *auth_methods_json,
                                 acp_buffer *result_json) {
    if (result_json == NULL || agent_name == NULL || agent_version == NULL || selected_version == 0 || selected_version > 2)
        return ACP_INVALID_ARGUMENT;
    const char *c = agent_capabilities_json ? agent_capabilities_json : "{}";
    const char *a = auth_methods_json ? auth_methods_json : "[]";
    json_object *capabilities = acp_parse(c, strlen(c));
    json_object *auth = acp_parse(a, strlen(a));
    if (!json_object_is_type(capabilities, json_type_object) || !json_object_is_type(auth, json_type_array)) {
        json_object_put(capabilities);
        json_object_put(auth);
        return ACP_INVALID_ARGUMENT;
    }

    json_object *result = json_object_new_object();
    json_object_object_add(result, "protocolVersion", json_object_new_int64(selected_version));
    if (selected_version >= 2) {
        json_object_object_add(result, "info", implementation(agent_name, agent_version));
        json_object_object_add(result, "capabilities", capabilities);
        json_object_object_add(result, "authMethods", auth);
    } else {
        json_object_object_add(result, "agentCapabilities", capabilities);
        json_object_object_add(result, "authMethods", auth);
        json_object_object_add(result, "agentInfo", implementation(agent_name, agent_version));
    }
    return acp_finish(result, result_json);
}

acp_status acp_authenticate_read(const char *params_json, size_t length, char **method_id, acp_buffer *error_json) {
    if (params_json == NULL || method_id == NULL || error_json == NULL) return ACP_INVALID_ARGUMENT;
    *method_id = NULL;
    error_json->data = NULL;
    error_json->length = 0;
    json_object *params = acp_parse(params_json, length);
    acp_status status = ACP_OK;
    if ((*method_id = acp_string(params, "methodId")) == NULL) status = acp_invalid(error_json, "authenticate needs methodId");
    json_object_put(params);
    return status;
}

void acp_authenticate_free(char **method_id) {
    if (method_id == NULL) return;
    free(*method_id);
    *method_id = NULL;
}
