#include "internal.h"

#include <stdlib.h>
#include <string.h>

acp_status acp_set_mode_read(const char *params_json, size_t length, acp_set_mode *out, acp_buffer *error_json) {
    if (params_json == NULL || out == NULL || error_json == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    error_json->data = NULL;
    error_json->length = 0;
    json_object *params = acp_parse(params_json, length);
    acp_status status = ACP_OK;
    if ((out->session_id = acp_string(params, "sessionId")) == NULL)
        status = acp_invalid(error_json, "session/set_mode needs sessionId");
    else if ((out->mode_id = acp_string(params, "modeId")) == NULL)
        status = acp_invalid(error_json, "session/set_mode needs modeId");
    json_object_put(params);
    if (status != ACP_OK) acp_set_mode_free(out);
    return status;
}

void acp_set_mode_free(acp_set_mode *set_mode) {
    if (set_mode == NULL) return;
    free(set_mode->session_id);
    free(set_mode->mode_id);
    set_mode->session_id = NULL;
    set_mode->mode_id = NULL;
}

acp_status acp_modes_write(const char *current_id, const acp_mode *modes, size_t count, acp_buffer *out_json) {
    if (current_id == NULL || modes == NULL || count == 0 || out_json == NULL) return ACP_INVALID_ARGUMENT;
    int offered = 0;
    for (size_t i = 0; i < count; i++) {
        if (modes[i].id == NULL || modes[i].name == NULL) return ACP_INVALID_ARGUMENT;
        if (strcmp(modes[i].id, current_id) == 0) offered = 1;
    }
    if (!offered) return ACP_INVALID_ARGUMENT;
    json_object *state = json_object_new_object();
    json_object *available = json_object_new_array();
    json_object_object_add(state, "currentModeId", json_object_new_string(current_id));
    for (size_t i = 0; i < count; i++) {
        json_object *mode = json_object_new_object();
        json_object_object_add(mode, "id", json_object_new_string(modes[i].id));
        json_object_object_add(mode, "name", json_object_new_string(modes[i].name));
        if (modes[i].description != NULL)
            json_object_object_add(mode, "description", json_object_new_string(modes[i].description));
        json_object_array_add(available, mode);
    }
    json_object_object_add(state, "availableModes", available);
    return acp_finish(state, out_json);
}
