// session/update and elicitation, as #162 to #177 tell the client: each update kind, the
// notification around it, and what a session's MCP server elicits, asked again over ACP.
#include "acp.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

static int says(acp_buffer *b, const char *want) {
    int same = b->data != NULL && strcmp(b->data, want) == 0;
    if (!same) fprintf(stderr, "  got:  %s\n  want: %s\n", b->data ? b->data : "(none)", want);
    acp_buffer_free(b);
    return same;
}

static void envelope(void) {
    acp_buffer out = {0};
    CHECK(acp_session_update_write("code-1-1-1", "{\"sessionUpdate\":\"usage_update\",\"used\":1,\"size\":2}", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{\"sessionId\":\"code-1-1-1\","
                     "\"update\":{\"sessionUpdate\":\"usage_update\",\"used\":1,\"size\":2}}}"));
    CHECK(acp_session_update_write("code-1-1-1", "not json", &out) == ACP_INVALID_ARGUMENT);
    // The update as it was written, so what it carries raw is passed on byte for byte.
    CHECK(acp_session_update_write("code-1-1-1", "{\"rawOutput\":{\"a\": 1}}", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{\"sessionId\":\"code-1-1-1\",\"update\":{\"rawOutput\":{\"a\": 1}}}}"));
}

// #164: thought chunks; and the message chunks the replay tells, every byte.
static void chunks(void) {
    acp_buffer out = {0};
    CHECK(acp_text_chunk_write(ACP_CHUNK_AGENT_MESSAGE, "say \"hi\"\n", 9, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"agent_message_chunk\",\"content\":{\"type\":\"text\",\"text\":\"say \\\"hi\\\"\\n\"}}"));
    CHECK(acp_text_chunk_write(ACP_CHUNK_AGENT_THOUGHT, "a/b", 3, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"agent_thought_chunk\",\"content\":{\"type\":\"text\",\"text\":\"a/b\"}}"));
    CHECK(acp_text_chunk_write(ACP_CHUNK_USER_MESSAGE, "a\0b", 3, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"user_message_chunk\",\"content\":{\"type\":\"text\",\"text\":\"a\\u0000b\"}}"));
}

// #163, #172: a call's first report names the tool, its updates say how it came out and what it
// produced: MCP's content blocks as they came, or a text.
static void tool_calls(void) {
    acp_buffer out = {0};
    acp_tool_call first = {0, "c1", "x__y", "x__y", "read", "pending", "{\"q\":1}", NULL, NULL, NULL, NULL, NULL};
    CHECK(acp_tool_call_write(&first, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call\",\"toolCallId\":\"c1\",\"title\":\"x__y\",\"name\":\"x__y\",\"kind\":\"read\","
                     "\"status\":\"pending\",\"rawInput\":{\"q\":1}}"));
    acp_tool_call replayed = {0, "c1", "x__y", "x__y", NULL, NULL, "", NULL, NULL, NULL, NULL, NULL};
    CHECK(acp_tool_call_write(&replayed, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call\",\"toolCallId\":\"c1\",\"title\":\"x__y\",\"name\":\"x__y\",\"rawInput\":\"\"}"));
    acp_tool_call running = {1, "c1", NULL, NULL, NULL, "in_progress", NULL, NULL, NULL, NULL, NULL, NULL};
    CHECK(acp_tool_call_write(&running, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"in_progress\"}"));
    acp_tool_call done = {1, "c1", NULL, NULL, NULL, "completed",
        NULL, "{\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}", "[{\"type\":\"text\",\"text\":\"ok\"}]", NULL, NULL, NULL};
    CHECK(acp_tool_call_write(&done, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"completed\","
                     "\"rawOutput\":{\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}},"
                     "\"content\":[{\"type\":\"content\",\"content\":{\"type\":\"text\",\"text\":\"ok\"}}]}"));
    acp_tool_call refused = {1, "c1", NULL, NULL, NULL, "failed", NULL, "{\"status\":\"not run\"}", NULL, "the user rejected it", NULL, NULL};
    CHECK(acp_tool_call_write(&refused, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"failed\","
                     "\"rawOutput\":{\"status\":\"not run\"},\"content\":[{\"type\":\"content\",\"content\":{\"type\":\"text\","
                     "\"text\":\"the user rejected it\"}}]}"));
    // Raw JSON is passed on byte for byte, as the server wrote it.
    acp_tool_call spaced = {1, "c1", NULL, NULL, NULL, "completed", "{ \"q\" : 1 }", "{\"jsonrpc\": \"2.0\", \"id\": 3}", NULL, NULL, NULL, NULL};
    CHECK(acp_tool_call_write(&spaced, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"completed\","
                     "\"rawInput\":{ \"q\" : 1 },\"rawOutput\":{\"jsonrpc\": \"2.0\", \"id\": 3}}"));
    // Output that is no JSON is the text it is; no blocks and no text, no content.
    acp_tool_call bare = {1, "c1", NULL, NULL, NULL, "failed", NULL, "boom", "[]", NULL, NULL, NULL};
    CHECK(acp_tool_call_write(&bare, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"failed\",\"rawOutput\":\"boom\"}"));
    acp_tool_call stray = {1, "c1", NULL, NULL, NULL, "failed", NULL, NULL, "{\"a\":1}", NULL, NULL, NULL};
    CHECK(acp_tool_call_write(&stray, &out) == ACP_INVALID_ARGUMENT);
    // A write shows the file it modified as a diff, every byte of either text, after any terminal and
    // before any blocks; a file that was not there has no old text, null.
    acp_diff changed = {"/tmp/a b", "one\0two\n", 8, "one\0three\n", 10};
    acp_tool_call wrote = {1, "c1", NULL, NULL, NULL, "completed", NULL, "{\"result\":{}}", NULL, NULL, NULL, &changed};
    CHECK(acp_tool_call_write(&wrote, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"completed\","
                     "\"rawOutput\":{\"result\":{}},\"content\":[{\"type\":\"diff\",\"path\":\"/tmp/a b\","
                     "\"oldText\":\"one\\u0000two\\n\",\"newText\":\"one\\u0000three\\n\"}]}"));
    acp_diff created = {"/tmp/new", NULL, 0, "", 0};
    acp_tool_call made = {1, "c1", NULL, NULL, NULL, "completed", NULL, NULL, NULL, NULL, NULL, &created};
    CHECK(acp_tool_call_write(&made, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"tool_call_update\",\"toolCallId\":\"c1\",\"status\":\"completed\","
                     "\"content\":[{\"type\":\"diff\",\"path\":\"/tmp/new\",\"oldText\":null,\"newText\":\"\"}]}"));
    // "The absolute file path being modified", and newText is required.
    acp_diff relative = {"a b", NULL, 0, "x", 1};
    acp_tool_call bad = {1, "c1", NULL, NULL, NULL, "completed", NULL, NULL, NULL, NULL, NULL, &relative};
    CHECK(acp_tool_call_write(&bad, &out) == ACP_INVALID_ARGUMENT);
    acp_diff textless = {"/tmp/a", NULL, 0, NULL, 0};
    bad.diff = &textless;
    CHECK(acp_tool_call_write(&bad, &out) == ACP_INVALID_ARGUMENT);
    acp_tool_call nameless = {0, NULL, "x", NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL};
    CHECK(acp_tool_call_write(&nameless, &out) == ACP_INVALID_ARGUMENT);
}

// The agent's plan: "The Agent MUST send a complete list of all plan entries in each update", each
// its content, priority and status, every byte of the content; anything else an entry holds is not
// the plan's. A plan of no entries is a plan.
static void plans(void) {
    acp_buffer out = {0};
    const char *plan = "{\"entries\":[{\"content\":\"Read the \\\"code\\\"\\u0000\",\"priority\":\"high\",\"status\":\"completed\",\"extra\":1},"
                       "{\"content\":\"Fix it\",\"priority\":\"medium\",\"status\":\"in_progress\"},"
                       "{\"content\":\"Tidy\",\"priority\":\"low\",\"status\":\"pending\"}]}";
    CHECK(acp_plan_write(plan, strlen(plan), &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"plan\",\"entries\":[{\"content\":\"Read the \\\"code\\\"\\u0000\",\"priority\":\"high\",\"status\":\"completed\"},"
                     "{\"content\":\"Fix it\",\"priority\":\"medium\",\"status\":\"in_progress\"},"
                     "{\"content\":\"Tidy\",\"priority\":\"low\",\"status\":\"pending\"}]}"));
    CHECK(acp_plan_write("{\"entries\":[]}", 14, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"plan\",\"entries\":[]}"));
    const char *bad[] = {
        "{}",
        "{\"entries\":{}}",
        "{\"entries\":[{\"priority\":\"high\",\"status\":\"pending\"}]}",
        "{\"entries\":[{\"content\":\"x\",\"priority\":\"urgent\",\"status\":\"pending\"}]}",
        "{\"entries\":[{\"content\":\"x\",\"priority\":\"high\",\"status\":\"done\"}]}",
        "{\"entries\":[{\"content\":\"x\",\"priority\":\"high\"}]}",
        "{\"entries\":[7]}",
        // A NUL makes a value another one, whatever it starts with.
        "{\"entries\":[{\"content\":\"x\",\"priority\":\"high\\u0000x\",\"status\":\"pending\"}]}",
        "{\"entries\":[{\"content\":\"x\",\"priority\":\"high\",\"status\":\"completed\\u0000unexpected\"}]}",
        "not json",
    };
    for (size_t i = 0; i < sizeof bad / sizeof *bad; i++) CHECK(acp_plan_write(bad[i], strlen(bad[i]), &out) == ACP_INVALID_ARGUMENT);
    CHECK(out.data == NULL);
}

// #169, #177: how full the context is, and what the session has cost; #166, #176: the title,
// and when it was last active; #173: the commands; #171: a notice.
static void others(void) {
    acp_buffer out = {0};
    CHECK(acp_usage_update_write(124, 1000000, "0.001236", "USD", &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"usage_update\",\"used\":124,\"size\":1000000,\"cost\":{\"amount\":0.001236,\"currency\":\"USD\"}}"));
    CHECK(acp_usage_update_write(124, 1000000, NULL, NULL, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"usage_update\",\"used\":124,\"size\":1000000}"));
    CHECK(acp_usage_update_write(1, 2, "lots", "USD", &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_session_info_update_write("Fix the build", "2026-10-27T17:03:27Z", &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"session_info_update\",\"title\":\"Fix the build\",\"updatedAt\":\"2026-10-27T17:03:27Z\"}"));
    // "Set to null to clear."
    CHECK(acp_session_title_clear_write("2026-10-27T17:03:27Z", &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"session_info_update\",\"title\":null,\"updatedAt\":\"2026-10-27T17:03:27Z\"}"));
    CHECK(acp_session_title_clear_write(NULL, &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_session_info_update_write(NULL, "2026-10-27T17:03:27Z", &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"session_info_update\",\"updatedAt\":\"2026-10-27T17:03:27Z\"}"));
    CHECK(acp_available_commands_update_write("[{\"name\":\"echo\",\"description\":\"d\"}]", &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"available_commands_update\",\"availableCommands\":[{\"name\":\"echo\",\"description\":\"d\"}]}"));
    CHECK(acp_available_commands_update_write("{}", &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_notice_write("warning", "MCP server s logged a warning", "disk low", &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"notice\",\"severity\":\"warning\",\"title\":\"MCP server s logged a warning\","
                     "\"description\":\"disk low\"}"));
}

// #167, #178: a server's elicitation/create, asked of the client tied to the session and the call:
// those lead, then its mode, form when it names none, then everything else it asked, in its order.
static void elicitation(void) {
    acp_buffer out = {0};
    int mode = -1;
    const char *form = "{\"requestedSchema\":{\"type\":\"object\"},\"sessionId\":\"spoof\",\"message\":\"Which branch?\"}";
    CHECK(acp_elicitation_create_write("\"elicit-1\"", "code-1-1-1", "c1", form, strlen(form), &mode, &out) == ACP_OK);
    CHECK(mode == ACP_ELICIT_FORM);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"elicit-1\",\"method\":\"elicitation/create\",\"params\":{\"sessionId\":\"code-1-1-1\","
                     "\"toolCallId\":\"c1\",\"mode\":\"form\",\"message\":\"Which branch?\",\"requestedSchema\":{\"type\":\"object\"}}}"));
    const char *link = "{\"mode\":\"url\",\"message\":\"Sign in\",\"elicitationId\":\"x1\",\"url\":\"https://example.com/a\"}";
    CHECK(acp_elicitation_create_write("\"elicit-2\"", "code-1-1-1", "c1", link, strlen(link), &mode, &out) == ACP_OK);
    CHECK(mode == ACP_ELICIT_URL);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"elicit-2\",\"method\":\"elicitation/create\",\"params\":{\"sessionId\":\"code-1-1-1\","
                     "\"toolCallId\":\"c1\",\"mode\":\"url\",\"message\":\"Sign in\",\"elicitationId\":\"x1\",\"url\":\"https://example.com/a\"}}"));
    const char *other = "{\"mode\":\"voice\"}";
    CHECK(acp_elicitation_create_write("\"elicit-3\"", "code-1-1-1", "c1", other, strlen(other), &mode, &out) == ACP_OK);
    CHECK(mode == ACP_ELICIT_OTHER);
    acp_buffer_free(&out);
    const char *none[] = {"[]", "{\"mode\":7}", "nope"};
    for (size_t i = 0; i < sizeof none / sizeof *none; i++) {
        CHECK(acp_elicitation_create_write("\"elicit-4\"", "code-1-1-1", "c1", none[i], strlen(none[i]), &mode, &out) == ACP_INVALID_PARAMS);
        CHECK(out.data == NULL);
    }
    CHECK(acp_elicitation_complete_write("x1", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"method\":\"elicitation/complete\",\"params\":{\"elicitationId\":\"x1\"}}"));
}

// An image the user gave, told back as a user_message_chunk: "An image provided to or from an LLM."
static void image_chunk(void) {
    acp_buffer out = {0};
    CHECK(acp_image_chunk_write(ACP_CHUNK_USER_MESSAGE, "image/png", "iVBORw0KGgo=", &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"user_message_chunk\",\"content\":{\"type\":\"image\",\"mimeType\":\"image/png\",\"data\":\"iVBORw0KGgo=\"}}"));
    CHECK(acp_image_chunk_write(ACP_CHUNK_USER_MESSAGE, NULL, "x", &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_image_chunk_write((acp_chunk_kind) 9, "image/png", "x", &out) == ACP_INVALID_ARGUMENT);
}

// Audio the user gave, told back as a user_message_chunk: "Audio provided to or from an LLM."
static void audio_chunk(void) {
    acp_buffer out = {0};
    CHECK(acp_audio_chunk_write(ACP_CHUNK_USER_MESSAGE, "audio/wav", "UklGRg==", &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionUpdate\":\"user_message_chunk\",\"content\":{\"type\":\"audio\",\"mimeType\":\"audio/wav\",\"data\":\"UklGRg==\"}}"));
    CHECK(acp_audio_chunk_write(ACP_CHUNK_USER_MESSAGE, "audio/wav", NULL, &out) == ACP_INVALID_ARGUMENT);
}

// An elicitation "tied to a specific JSON-RPC request outside of a session (e.g., during
// auth/configuration phases before any session is started)": url mode, scoped by requestId.
static void request_scoped(void) {
    acp_buffer out = {0};
    CHECK(acp_elicitation_url_write("\"login-1\"", "7", "qntx-ABCD-EFGH", "https://q.example/auth/device/approve?user_code=ABCD-EFGH",
                                    "Sign in to QNTX: ABCD-EFGH", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"login-1\",\"method\":\"elicitation/create\",\"params\":{\"requestId\":7,"
                     "\"mode\":\"url\",\"message\":\"Sign in to QNTX: ABCD-EFGH\",\"elicitationId\":\"qntx-ABCD-EFGH\","
                     "\"url\":\"https://q.example/auth/device/approve?user_code=ABCD-EFGH\"}}"));
    // A string request id stays a string.
    CHECK(acp_elicitation_url_write("\"login-2\"", "\"auth-1\"", "e", "https://q.example/a", "m", &out) == ACP_OK);
    CHECK(says(&out, "{\"jsonrpc\":\"2.0\",\"id\":\"login-2\",\"method\":\"elicitation/create\",\"params\":{\"requestId\":\"auth-1\","
                     "\"mode\":\"url\",\"message\":\"m\",\"elicitationId\":\"e\",\"url\":\"https://q.example/a\"}}"));
    CHECK(acp_elicitation_url_write("\"login-3\"", "not json", "e", "u", "m", &out) == ACP_INVALID_ARGUMENT);
    CHECK(out.data == NULL);
    CHECK(acp_elicitation_url_write("\"login-3\"", "7", NULL, "u", "m", &out) == ACP_INVALID_ARGUMENT);
}

int main(void) {
    envelope();
    request_scoped();
    image_chunk();
    audio_chunk();
    chunks();
    tool_calls();
    others();
    plans();
    elicitation();
    if (failures) {
        fprintf(stderr, "updates: %d failed\n", failures);
        return 1;
    }
    printf("updates: session/update's kinds and its notification, and elicitation, passed\n");
    return 0;
}
