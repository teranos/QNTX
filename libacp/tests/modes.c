// Session modes, as #121 gave them: set_mode's request, and a session's modes.
#include "acp.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

static acp_status set_mode(const char *params, acp_set_mode *out, acp_buffer *error) {
    memset(out, 0, sizeof *out);
    memset(error, 0, sizeof *error);
    return acp_set_mode_read(params, strlen(params), out, error);
}

static int says(const acp_buffer *b, const char *want) {
    return b->data != NULL && strcmp(b->data, want) == 0;
}

static void set_mode_request(void) {
    acp_set_mode in;
    acp_buffer error;
    CHECK(set_mode("{\"sessionId\":\"code-1-1-1\",\"modeId\":\"plan\"}", &in, &error) == ACP_OK);
    CHECK(in.session_id && strcmp(in.session_id, "code-1-1-1") == 0);
    CHECK(in.mode_id && strcmp(in.mode_id, "plan") == 0);
    acp_set_mode_free(&in);
    CHECK(in.session_id == NULL && in.mode_id == NULL);
    CHECK(set_mode("{\"modeId\":\"plan\"}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/set_mode needs sessionId\"}"));
    acp_buffer_free(&error);
    CHECK(set_mode("{\"sessionId\":\"code-1-1-1\"}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/set_mode needs modeId\"}"));
    acp_buffer_free(&error);
    CHECK(in.session_id == NULL && in.mode_id == NULL);
}

// A SessionModeState: the current mode, then every mode offered, each with its description.
static void modes_written(void) {
    const acp_mode offered[] = {
        {"default", "Manual", "m in a permission block"},
        {"plan", "Plan", NULL},
    };
    acp_buffer out = {0};
    CHECK(acp_modes_write("plan", offered, 2, &out) == ACP_OK);
    CHECK(says(&out, "{\"currentModeId\":\"plan\",\"availableModes\":[{\"id\":\"default\",\"name\":\"Manual\","
                     "\"description\":\"m in a permission block\"},{\"id\":\"plan\",\"name\":\"Plan\"}]}"));
    acp_buffer_free(&out);
    // The current mode is one of those offered.
    CHECK(acp_modes_write("yolo", offered, 2, &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_modes_write("plan", NULL, 0, &out) == ACP_INVALID_ARGUMENT);
}

int main(void) {
    set_mode_request();
    modes_written();
    if (failures) {
        fprintf(stderr, "modes: %d failed\n", failures);
        return 1;
    }
    printf("modes: set_mode's request and a session's modes passed\n");
    return 0;
}
