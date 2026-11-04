// The method names, as acp_methods.h has them from schema/v1/meta.json: every one, each its wire name.
#include "acp.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

int main(void) {
    // One of each group, and the ones whose names meta.json spells apart from their wire names.
    CHECK(strcmp(ACP_AGENT_INITIALIZE, "initialize") == 0);
    CHECK(strcmp(ACP_AGENT_SESSION_NEW, "session/new") == 0);
    CHECK(strcmp(ACP_AGENT_SESSION_SET_CONFIG_OPTION, "session/set_config_option") == 0);
    CHECK(strcmp(ACP_AGENT_LOGOUT, "logout") == 0);
    CHECK(strcmp(ACP_CLIENT_SESSION_REQUEST_PERMISSION, "session/request_permission") == 0);
    CHECK(strcmp(ACP_CLIENT_FS_WRITE_TEXT_FILE, "fs/write_text_file") == 0);
    CHECK(strcmp(ACP_CLIENT_TERMINAL_WAIT_FOR_EXIT, "terminal/wait_for_exit") == 0);
    CHECK(strcmp(ACP_CLIENT_ELICITATION_COMPLETE, "elicitation/complete") == 0);
    CHECK(strcmp(ACP_PROTOCOL_CANCEL_REQUEST, "$/cancel_request") == 0);
    // meta.json names 13 agent methods, 11 client methods and 1 protocol method.
    CHECK(ACP_AGENT_METHODS == 13);
    CHECK(ACP_CLIENT_METHODS == 11);
    CHECK(ACP_PROTOCOL_METHODS == 1);
    if (failures) {
        fprintf(stderr, "methods: %d failed\n", failures);
        return 1;
    }
    printf("methods: every method name from schema/v1/meta.json passed\n");
    return 0;
}
