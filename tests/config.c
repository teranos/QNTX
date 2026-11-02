// Session config options, as #124 gave them: set_config_option's request, and a session's selects.
#include "acp.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

static acp_status set_option(const char *params, acp_set_config_option *out, acp_buffer *error) {
    memset(out, 0, sizeof *out);
    memset(error, 0, sizeof *error);
    return acp_set_config_option_read(params, strlen(params), out, error);
}

static int says(const acp_buffer *b, const char *want) {
    return b->data != NULL && strcmp(b->data, want) == 0;
}

// A value is a value id, or a boolean with its type said: the request's two shapes.
static void set_config_option_request(void) {
    acp_set_config_option in;
    acp_buffer error;
    CHECK(set_option("{\"sessionId\":\"code-1-1-1\",\"configId\":\"effort\",\"value\":\"high\"}", &in, &error) == ACP_OK);
    CHECK(in.session_id && strcmp(in.session_id, "code-1-1-1") == 0);
    CHECK(in.config_id && strcmp(in.config_id, "effort") == 0);
    CHECK(in.value && strcmp(in.value, "high") == 0);
    CHECK(!in.is_boolean);
    acp_set_config_option_free(&in);
    CHECK(in.session_id == NULL && in.config_id == NULL && in.value == NULL);

    CHECK(set_option("{\"sessionId\":\"code-1-1-1\",\"configId\":\"x\",\"type\":\"boolean\",\"value\":true}", &in, &error) == ACP_OK);
    CHECK(in.value == NULL && in.is_boolean && in.boolean_value);
    acp_set_config_option_free(&in);
    CHECK(set_option("{\"sessionId\":\"code-1-1-1\",\"configId\":\"x\",\"type\":\"boolean\",\"value\":false}", &in, &error) == ACP_OK);
    CHECK(in.is_boolean && !in.boolean_value);
    acp_set_config_option_free(&in);

    CHECK(set_option("{\"configId\":\"effort\",\"value\":\"high\"}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/set_config_option needs sessionId\"}"));
    acp_buffer_free(&error);
    CHECK(set_option("{\"sessionId\":\"code-1-1-1\",\"value\":\"high\"}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/set_config_option needs configId\"}"));
    acp_buffer_free(&error);
    // A boolean without its type, and no value at all, are neither shape.
    const char *neither[] = {
        "{\"sessionId\":\"code-1-1-1\",\"configId\":\"effort\",\"value\":true}",
        "{\"sessionId\":\"code-1-1-1\",\"configId\":\"effort\"}",
        "{\"sessionId\":\"code-1-1-1\",\"configId\":\"effort\",\"type\":\"boolean\",\"value\":1}",
    };
    for (size_t i = 0; i < sizeof neither / sizeof *neither; i++) {
        CHECK(set_option(neither[i], &in, &error) == ACP_INVALID_PARAMS);
        CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/set_config_option needs a value: "
                           "a value id, or type boolean and a boolean\"}"));
        acp_buffer_free(&error);
        CHECK(in.session_id == NULL && in.config_id == NULL && in.value == NULL);
    }
}

// SessionConfigOption selects, in the order given: each one's current value and the values it offers.
static void options_written(void) {
    const acp_config_value modes[] = {
        {"default", "Manual", "m in a permission block"},
        {"plan", "Plan", NULL},
    };
    const acp_config_value efforts[] = {{"low", "Low", NULL}, {"medium", "Medium", NULL}};
    const acp_config_select selects[] = {
        {"mode", "Mode", "mode", "plan", modes, 2},
        {"effort", "Effort", "thought_level", "medium", efforts, 2},
    };
    acp_buffer out = {0};
    CHECK(acp_config_options_write(selects, 2, &out) == ACP_OK);
    CHECK(says(&out, "[{\"id\":\"mode\",\"name\":\"Mode\",\"category\":\"mode\",\"type\":\"select\",\"currentValue\":\"plan\","
                     "\"options\":[{\"value\":\"default\",\"name\":\"Manual\",\"description\":\"m in a permission block\"},"
                     "{\"value\":\"plan\",\"name\":\"Plan\"}]},"
                     "{\"id\":\"effort\",\"name\":\"Effort\",\"category\":\"thought_level\",\"type\":\"select\",\"currentValue\":\"medium\","
                     "\"options\":[{\"value\":\"low\",\"name\":\"Low\"},{\"value\":\"medium\",\"name\":\"Medium\"}]}]"));
    acp_buffer_free(&out);
    // No category is none written.
    const acp_config_select bare = {"x", "X", NULL, "low", efforts, 2};
    CHECK(acp_config_options_write(&bare, 1, &out) == ACP_OK);
    CHECK(says(&out, "[{\"id\":\"x\",\"name\":\"X\",\"type\":\"select\",\"currentValue\":\"low\","
                     "\"options\":[{\"value\":\"low\",\"name\":\"Low\"},{\"value\":\"medium\",\"name\":\"Medium\"}]}]"));
    acp_buffer_free(&out);
    // The current value is one of those offered.
    const acp_config_select stray = {"effort", "Effort", "thought_level", "ultra", efforts, 2};
    CHECK(acp_config_options_write(&stray, 1, &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_config_options_write(NULL, 0, &out) == ACP_INVALID_ARGUMENT);
}

int main(void) {
    set_config_option_request();
    options_written();
    if (failures) {
        fprintf(stderr, "config: %d failed\n", failures);
        return 1;
    }
    printf("config: set_config_option's request and a session's selects passed\n");
    return 0;
}
