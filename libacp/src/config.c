#include "internal.h"

#include <stdlib.h>
#include <string.h>

acp_status acp_set_config_option_read(const char *params_json, size_t length, acp_set_config_option *out,
    acp_buffer *error_json) {
    if (params_json == NULL || out == NULL || error_json == NULL) return ACP_INVALID_ARGUMENT;
    memset(out, 0, sizeof *out);
    error_json->data = NULL;
    error_json->length = 0;
    json_object *params = acp_parse(params_json, length);
    acp_status status = ACP_OK;
    if ((out->session_id = acp_string(params, "sessionId")) == NULL)
        status = acp_invalid(error_json, "session/set_config_option needs sessionId");
    else if ((out->config_id = acp_string(params, "configId")) == NULL)
        status = acp_invalid(error_json, "session/set_config_option needs configId");
    else if ((out->value = acp_string(params, "value")) == NULL) {
        // The boolean shape says its type: {"type":"boolean","value":true}.
        char *type = acp_string(params, "type");
        if (type != NULL && strcmp(type, "boolean") == 0
            && json_object_is_type(acp_field(params, "value"), json_type_boolean)) {
            out->is_boolean = 1;
            out->boolean_value = acp_flag(params, "value");
        } else {
            status = acp_invalid(error_json, "session/set_config_option needs a value: a value id, or type boolean and a boolean");
        }
        free(type);
    }
    json_object_put(params);
    if (status != ACP_OK) acp_set_config_option_free(out);
    return status;
}

void acp_set_config_option_free(acp_set_config_option *set_option) {
    if (set_option == NULL) return;
    free(set_option->session_id);
    free(set_option->config_id);
    free(set_option->value);
    memset(set_option, 0, sizeof *set_option);
}

static json_object *select_of(const acp_config_select *select) {
    json_object *option = json_object_new_object();
    json_object *values = json_object_new_array();
    json_object_object_add(option, "id", json_object_new_string(select->id));
    json_object_object_add(option, "name", json_object_new_string(select->name));
    if (select->category != NULL)
        json_object_object_add(option, "category", json_object_new_string(select->category));
    json_object_object_add(option, "type", json_object_new_string("select"));
    json_object_object_add(option, "currentValue", json_object_new_string(select->current_value));
    for (size_t i = 0; i < select->count; i++) {
        json_object *value = json_object_new_object();
        json_object_object_add(value, "value", json_object_new_string(select->values[i].value));
        json_object_object_add(value, "name", json_object_new_string(select->values[i].name));
        if (select->values[i].description != NULL)
            json_object_object_add(value, "description", json_object_new_string(select->values[i].description));
        json_object_array_add(values, value);
    }
    json_object_object_add(option, "options", values);
    return option;
}

acp_status acp_config_options_write(const acp_config_select *selects, size_t count, acp_buffer *out_json) {
    if (selects == NULL || count == 0 || out_json == NULL) return ACP_INVALID_ARGUMENT;
    for (size_t s = 0; s < count; s++) {
        const acp_config_select *select = &selects[s];
        if (select->id == NULL || select->name == NULL || select->current_value == NULL
            || select->values == NULL || select->count == 0) return ACP_INVALID_ARGUMENT;
        int offered = 0;
        for (size_t i = 0; i < select->count; i++) {
            if (select->values[i].value == NULL || select->values[i].name == NULL) return ACP_INVALID_ARGUMENT;
            if (strcmp(select->values[i].value, select->current_value) == 0) offered = 1;
        }
        if (!offered) return ACP_INVALID_ARGUMENT;
    }
    json_object *options = json_object_new_array();
    for (size_t s = 0; s < count; s++) json_object_array_add(options, select_of(&selects[s]));
    return acp_finish(options, out_json);
}
