// session/resume: the session and the place it is resumed in, as #118 reads them; session/list, as #128 lists.
#include "acp.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #cond); failures++; } } while (0)

static acp_status resume(const char *params, acp_session_resume *out, acp_buffer *error) {
    memset(out, 0, sizeof *out);
    memset(error, 0, sizeof *error);
    return acp_session_resume_read(params, strlen(params), out, error);
}

static int says(const acp_buffer *error, const char *want) {
    return error->data != NULL && strcmp(error->data, want) == 0;
}

static void reads_the_place(void) {
    acp_session_resume in;
    acp_buffer error;
    CHECK(resume("{\"sessionId\":\"code-1-1-1\",\"cwd\":\"/work\",\"mcpServers\":[]}", &in, &error) == ACP_OK);
    CHECK(in.session_id != NULL && strcmp(in.session_id, "code-1-1-1") == 0);
    CHECK(in.cwd != NULL && strcmp(in.cwd, "/work") == 0);
    CHECK(error.data == NULL);
    acp_session_resume_free(&in);
    CHECK(in.session_id == NULL && in.cwd == NULL);
}

// Both are required, and the cwd is an absolute path.
static void refused(void) {
    acp_session_resume in;
    acp_buffer error;
    CHECK(resume("{\"cwd\":\"/work\"}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/resume needs sessionId\"}"));
    acp_buffer_free(&error);
    CHECK(resume("{\"sessionId\":\"code-1-1-1\"}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/resume needs cwd\"}"));
    acp_buffer_free(&error);
    CHECK(resume("{\"sessionId\":\"code-1-1-1\",\"cwd\":\"work\"}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"cwd must be an absolute path: work\"}"));
    acp_buffer_free(&error);
    CHECK(resume("{\"sessionId\":7,\"cwd\":\"/work\"}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/resume needs sessionId\"}"));
    acp_buffer_free(&error);
    CHECK(resume("[]", &in, &error) == ACP_INVALID_PARAMS);
    acp_buffer_free(&error);
    CHECK(in.session_id == NULL && in.cwd == NULL);
}

// A request that names a session and nothing else it needs, as session/delete's (#120).
static void names_a_session(void) {
    char *sid = NULL;
    acp_buffer error = {0};
    const char *ok = "{\"sessionId\":\"code-1-1-1\"}";
    CHECK(acp_session_target_read("session/delete", ok, strlen(ok), &sid, &error) == ACP_OK);
    CHECK(sid != NULL && strcmp(sid, "code-1-1-1") == 0);
    CHECK(error.data == NULL);
    acp_session_target_free(&sid);
    CHECK(sid == NULL);
    const char *none = "{\"_meta\":{\"sessionId\":\"code-1-1-1\"}}";
    CHECK(acp_session_target_read("session/delete", none, strlen(none), &sid, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/delete needs sessionId\"}"));
    CHECK(sid == NULL);
    acp_buffer_free(&error);
    CHECK(acp_session_target_read(NULL, ok, strlen(ok), &sid, &error) == ACP_INVALID_ARGUMENT);
}

static acp_status dirs(const char *params, acp_buffer *list, acp_buffer *error) {
    memset(list, 0, sizeof *list);
    memset(error, 0, sizeof *error);
    return acp_directories_read(params, strlen(params), list, error);
}

// additionalDirectories, as #123 reads them: absolute paths, in order, decoded and answered back.
static void additional_directories(void) {
    acp_buffer list, error;
    CHECK(dirs("{\"cwd\":\"/w\",\"additionalDirectories\":[\"/lib/caf\\u00e9\",\"/lib/b\"]}", &list, &error) == ACP_OK);
    CHECK(says(&list, "[\"/lib/caf\xc3\xa9\",\"/lib/b\"]"));
    acp_buffer_free(&list);
    // None named, or null, is none.
    CHECK(dirs("{\"cwd\":\"/w\"}", &list, &error) == ACP_OK && says(&list, "[]"));
    acp_buffer_free(&list);
    CHECK(dirs("{\"additionalDirectories\":null}", &list, &error) == ACP_OK && says(&list, "[]"));
    acp_buffer_free(&list);
    // Only the params' own: a list nested in another field is not theirs.
    CHECK(dirs("{\"_meta\":{\"additionalDirectories\":[\"rel\"]}}", &list, &error) == ACP_OK && says(&list, "[]"));
    acp_buffer_free(&list);
    CHECK(dirs("{\"additionalDirectories\":[\"/lib\",\"rel\"]}", &list, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"additionalDirectories must be absolute paths: rel\"}"));
    CHECK(list.data == NULL);
    acp_buffer_free(&error);
    CHECK(dirs("{\"additionalDirectories\":\"/lib\"}", &list, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"additionalDirectories must be a list of paths\"}"));
    acp_buffer_free(&error);
    CHECK(dirs("{\"additionalDirectories\":[\"/lib\",7]}", &list, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"additionalDirectories must be a list of paths\"}"));
    acp_buffer_free(&error);
    CHECK(dirs("{\"additionalDirectories\":[\"\"]}", &list, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"additionalDirectories must be absolute paths: \"}"));
    acp_buffer_free(&error);
}

// session/list's request, as #128 lists: a cwd to list in, absolute, and a cursor, both optional.
static acp_status list(const char *params, acp_session_list *out, acp_buffer *error) {
    memset(out, 0, sizeof *out);
    memset(error, 0, sizeof *error);
    return acp_session_list_read(params, strlen(params), out, error);
}

static void lists(void) {
    acp_session_list in;
    acp_buffer error;
    CHECK(list("{}", &in, &error) == ACP_OK);
    CHECK(in.cwd == NULL && in.cursor == NULL);
    CHECK(list("{\"cwd\":null,\"cursor\":null}", &in, &error) == ACP_OK);
    CHECK(in.cwd == NULL && in.cursor == NULL);
    CHECK(list("{\"cwd\":\"/work\",\"cursor\":\"c1\"}", &in, &error) == ACP_OK);
    CHECK(in.cwd && strcmp(in.cwd, "/work") == 0);
    CHECK(in.cursor && strcmp(in.cursor, "c1") == 0);
    acp_session_list_free(&in);
    CHECK(in.cwd == NULL && in.cursor == NULL);
    CHECK(list("{\"cwd\":\"work\"}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"cwd must be an absolute path: work\"}"));
    acp_buffer_free(&error);
    CHECK(list("{\"cwd\":7}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/list's cwd must be a string\"}"));
    acp_buffer_free(&error);
    CHECK(list("{\"cursor\":7}", &in, &error) == ACP_INVALID_PARAMS);
    CHECK(says(&error, "{\"code\":-32602,\"message\":\"session/list's cursor must be a string\"}"));
    acp_buffer_free(&error);
    CHECK(in.cwd == NULL && in.cursor == NULL);
}

// A SessionInfo: no additional roots and no title are none written; updatedAt as given.
static void session_info(void) {
    acp_buffer out = {0};
    acp_session_info info = {"code-1-1-1", "/work", "[\"/lib\",\"/srv\"]", "Fix the build", "2023-11-14T22:13:20Z"};
    CHECK(acp_session_info_write(&info, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionId\":\"code-1-1-1\",\"cwd\":\"/work\",\"additionalDirectories\":[\"/lib\",\"/srv\"],"
                     "\"title\":\"Fix the build\",\"updatedAt\":\"2023-11-14T22:13:20Z\"}"));
    acp_buffer_free(&out);
    acp_session_info bare = {"code-1-1-1", "/work", "[]", "", "2023-11-14T22:13:20Z"};
    CHECK(acp_session_info_write(&bare, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionId\":\"code-1-1-1\",\"cwd\":\"/work\",\"updatedAt\":\"2023-11-14T22:13:20Z\"}"));
    acp_buffer_free(&out);
    acp_session_info none = {"code-1-1-1", "/work", NULL, NULL, NULL};
    CHECK(acp_session_info_write(&none, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionId\":\"code-1-1-1\",\"cwd\":\"/work\"}"));
    acp_buffer_free(&out);
    // The roots are a JSON array of paths, or nothing is written.
    acp_session_info stray = {"code-1-1-1", "/work", "{\"a\":1}", NULL, NULL};
    CHECK(acp_session_info_write(&stray, &out) == ACP_INVALID_ARGUMENT);
    acp_session_info nameless = {NULL, "/work", NULL, NULL, NULL};
    CHECK(acp_session_info_write(&nameless, &out) == ACP_INVALID_ARGUMENT);
}

// session/load, as #143 reads it: resume's request, its servers required too.
static acp_status load(const char *params, acp_session_resume *out, acp_buffer *error) {
    memset(out, 0, sizeof *out);
    memset(error, 0, sizeof *error);
    return acp_session_load_read(params, strlen(params), out, error);
}

static void loads(void) {
    acp_session_resume in;
    acp_buffer error;
    CHECK(load("{\"sessionId\":\"code-1-1-1\",\"cwd\":\"/w\",\"mcpServers\":[]}", &in, &error) == ACP_OK);
    CHECK(in.session_id && strcmp(in.session_id, "code-1-1-1") == 0 && in.cwd && strcmp(in.cwd, "/w") == 0);
    acp_session_resume_free(&in);
    const char *refused_[][2] = {
        {"{\"cwd\":\"/w\",\"mcpServers\":[]}", "session/load needs sessionId"},
        {"{\"_meta\":{\"sessionId\":\"code-1-1-1\"},\"cwd\":\"/w\",\"mcpServers\":[]}", "session/load needs sessionId"},
        {"{\"sessionId\":\"code-1-1-1\",\"mcpServers\":[]}", "session/load needs cwd"},
        {"{\"sessionId\":\"code-1-1-1\",\"cwd\":\"w\",\"mcpServers\":[]}", "cwd must be an absolute path: w"},
        {"{\"sessionId\":\"code-1-1-1\",\"cwd\":\"/w\"}", "session/load needs mcpServers"},
        {"{\"sessionId\":\"code-1-1-1\",\"cwd\":\"/w\",\"mcpServers\":{}}", "session/load needs mcpServers"},
    };
    for (size_t i = 0; i < sizeof refused_ / sizeof *refused_; i++) {
        char want[256];
        snprintf(want, sizeof want, "{\"code\":-32602,\"message\":\"%s\"}", refused_[i][1]);
        CHECK(load(refused_[i][0], &in, &error) == ACP_INVALID_PARAMS);
        CHECK(says(&error, want));
        CHECK(in.session_id == NULL && in.cwd == NULL);
        acp_buffer_free(&error);
    }
}

// Part 15: creation reads only its own params and validates every server before Ground starts any.
static void creates(void) {
    const char *bad[] = {
        "{}", "[]", "{\"_meta\":{\"cwd\":\"/w\",\"mcpServers\":[]}}",
        "{\"cwd\":\"/w\",\"_meta\":{\"mcpServers\":[]}}",
        "{\"cwd\":\"relative\",\"mcpServers\":[]}",
        "{\"cwd\":\"/w\\u0000hidden\",\"mcpServers\":[]}",
        "{\"cwd\":\"/w\",\"mcpServers\":null}",
        "{\"cwd\":\"/w\",\"mcpServers\":[7]}",
        "{\"cwd\":\"/w\",\"mcpServers\":[{\"name\":\"x\",\"command\":\"relative\",\"args\":[],\"env\":[]}]}",
        "{\"cwd\":\"/w\",\"mcpServers\":[{\"name\":\"x\",\"command\":\"/bin/x\",\"args\":[],\"env\":[{}]}]}",
        "{\"cwd\":\"/w\",\"mcpServers\":[{\"name\":\"x\",\"command\":\"/bin/x\",\"args\":[7],\"env\":[]}]}",
        "{\"cwd\":\"/w\",\"mcpServers\":[{\"type\":\"http\",\"name\":\"x\",\"url\":\"https://x\"}]}",
        "{\"cwd\":\"/w\",\"mcpServers\":[{\"type\":\"http\",\"name\":\"x\",\"url\":\"https://x\",\"headers\":[{\"name\":\"X\",\"value\":7}]}]}",
        "{\"cwd\":\"/w\",\"mcpServers\":[],\"additionalDirectories\":[\"relative\"]}"
    };
    acp_session_new in = {0};
    acp_buffer error = {0};
    for (size_t i = 0; i < sizeof bad / sizeof *bad; i++) {
        CHECK(acp_session_new_read(bad[i], strlen(bad[i]), &in, &error) == ACP_INVALID_PARAMS);
        CHECK(error.data != NULL && strstr(error.data, "-32602") != NULL);
        CHECK(in.cwd == NULL && in.additional_directories.data == NULL);
        acp_buffer_free(&error);
        acp_session_new_free(&in);
    }
    const char *ok = "{\"_meta\":{\"cwd\":\"relative\",\"mcpServers\":[7]},\"cwd\":\"/caf\\u00e9\",\"mcpServers\":[],\"additionalDirectories\":[\"/lib\"]}";
    CHECK(acp_session_new_read(ok, strlen(ok), &in, &error) == ACP_OK);
    CHECK(in.cwd != NULL && strcmp(in.cwd, "/caf\xc3\xa9") == 0);
    CHECK(says(&in.additional_directories, "[\"/lib\"]"));
    acp_session_new_free(&in);
    const char *servers = "{\"cwd\":\"/w\",\"mcpServers\":[{\"name\":\"x\",\"command\":\"/bin/x\",\"args\":[\"a\"],\"env\":[{\"name\":\"K\",\"value\":\"V\"}]},{\"type\":\"http\",\"name\":\"h\",\"url\":\"https://x\",\"headers\":[]},{\"type\":\"sse\",\"name\":\"s\",\"url\":\"https://x\",\"headers\":[]}]}";
    CHECK(acp_session_new_read(servers, strlen(servers), &in, &error) == ACP_OK);
    CHECK(says(&in.additional_directories, "[]"));
    acp_session_new_free(&in);
    CHECK(acp_session_new_read(NULL, 0, &in, &error) == ACP_INVALID_ARGUMENT);
}

static void creation_result(void) {
    acp_buffer out = {0};
    CHECK(acp_session_new_result("s\"1", NULL, NULL, &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionId\":\"s\\\"1\"}"));
    acp_buffer_free(&out);
    CHECK(acp_session_new_result("s", "{\"currentModeId\":\"m\",\"availableModes\":[]}", "[]", &out) == ACP_OK);
    CHECK(says(&out, "{\"sessionId\":\"s\",\"modes\":{\"currentModeId\":\"m\",\"availableModes\":[]},\"configOptions\":[]}"));
    acp_buffer_free(&out);
    CHECK(acp_session_new_result("s", "[]", NULL, &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_session_new_result("s", NULL, "{}", &out) == ACP_INVALID_ARGUMENT);
    CHECK(acp_session_new_result(NULL, NULL, NULL, &out) == ACP_INVALID_ARGUMENT);
}

int main(void) {
    creates();
    creation_result();
    reads_the_place();
    refused();
    names_a_session();
    additional_directories();
    lists();
    session_info();
    loads();
    if (failures) {
        fprintf(stderr, "session: %d failed\n", failures);
        return 1;
    }
    printf("session: session/new request and result, session/resume's session and cwd, session/list's request and SessionInfo, session/load's request, and their refusals passed\n");
    return 0;
}
