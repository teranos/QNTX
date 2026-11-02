#ifndef ACP_H
#define ACP_H

// libacp: the Agent Client Protocol, agent and client side, in C11 with a C ABI.
// v1 at schema-v1.26.0, v2 at schema-v2.0.0-alpha.9: https://agentclientprotocol.com

#include <stddef.h>
#include <stdint.h>

// Every method by its name in schema-v1.26.0's meta.json: ACP_AGENT_SESSION_NEW is "session/new".
#include "acp_methods.h"

#ifdef __cplusplus
extern "C" {
#endif

#define ACP_VERSION "0.1.0-dev"

#define ACP_ERROR_INVALID_PARAMS (-32602)
// A requested version none of the supported ones fits:
// "If none exists, it MUST return -32022 with requested and supported data"
#define ACP_ERROR_UNSUPPORTED_VERSION (-32022)
// "Authentication is required before this operation can be performed."
#define ACP_ERROR_AUTH_REQUIRED (-32000)

typedef enum acp_status {
    ACP_OK = 0,
    ACP_INVALID_ARGUMENT,
    ACP_NO_MEMORY,
    ACP_PARSE_ERROR,
    ACP_INVALID_PARAMS,
    ACP_UNSUPPORTED_VERSION
} acp_status;

// JSON libacp wrote. The caller owns it and frees it with acp_buffer_free.
typedef struct acp_buffer {
    char *data;
    size_t length;
} acp_buffer;

// What a client offers, read in the shape of the version it requested; 1 offered, 0 not.
// v2 offers only elicitation and auth.
typedef struct acp_client_capabilities {
    int fs_read_text_file;
    int fs_write_text_file;
    int terminal;
    int session_notices;
    int elicitation_form;
    int elicitation_url;
    int auth_terminal; // auth.terminal: the client runs a terminal auth method itself
} acp_client_capabilities;

typedef struct acp_initialize {
    unsigned requested_version; // what the client asked for
    unsigned selected_version;  // the highest supported at or below it
    acp_client_capabilities capabilities;
} acp_initialize;

const char *acp_version(void);
const char *acp_status_string(acp_status status);
void acp_buffer_free(acp_buffer *buffer);

// Reads initialize's params and selects the version the connection speaks, from supported.
// On ACP_INVALID_PARAMS or ACP_UNSUPPORTED_VERSION, error_json is the JSON-RPC error to answer.
acp_status acp_initialize_read(const char *params_json, size_t length,
                               const unsigned *supported, size_t supported_count,
                               acp_initialize *out, acp_buffer *error_json);

// The initialize result in the selected version's shape.
// Capabilities and auth methods are JSON the caller writes for that version; NULL is {} and [].
acp_status acp_initialize_result(unsigned selected_version,
                                 const char *agent_name, const char *agent_version,
                                 const char *agent_capabilities_json,
                                 const char *auth_methods_json,
                                 acp_buffer *result_json);

// authenticate's request: the methodId it names, which libacp owns until acp_authenticate_free.
acp_status acp_authenticate_read(const char *params_json, size_t length, char **method_id, acp_buffer *error_json);
void acp_authenticate_free(char **method_id);

// session/new in v1: required cwd and mcpServers, with every server shape checked.
// The library owns cwd and the normalized additionalDirectories until freed.
// Transport support and process limits remain the agent's responsibility.
typedef struct acp_session_new {
    char *cwd;
    acp_buffer additional_directories;
} acp_session_new;

acp_status acp_session_new_read(const char *params_json, size_t length,
                                acp_session_new *out, acp_buffer *error_json);
void acp_session_new_free(acp_session_new *session);
// modes/configOptions are already written by their libacp writers; NULL omits them.
acp_status acp_session_new_result(const char *session_id, const char *modes_json,
                                  const char *config_options_json, acp_buffer *out_json);

// session/resume's request: the session, and the absolute cwd it is resumed in.
// libacp owns both strings until acp_session_resume_free.
typedef struct acp_session_resume {
    char *session_id;
    char *cwd;
} acp_session_resume;

// On ACP_INVALID_PARAMS, error_json is the JSON-RPC error to answer.
acp_status acp_session_resume_read(const char *params_json, size_t length,
                                   acp_session_resume *out, acp_buffer *error_json);
// session/load's request: resume's, with mcpServers required, a list. Freed as resume's is.
acp_status acp_session_load_read(const char *params_json, size_t length,
                                 acp_session_resume *out, acp_buffer *error_json);
void acp_session_resume_free(acp_session_resume *resume);

// The session a request names in its own sessionId, as session/delete's does; a request
// without one is refused as method's. libacp owns *session_id until acp_session_target_free.
acp_status acp_session_target_read(const char *method, const char *params_json, size_t length,
                                   char **session_id, acp_buffer *error_json);
void acp_session_target_free(char **session_id);

// The additionalDirectories a session/new or session/resume names, as the JSON array a client
// is answered with: in order, [] when none or null. Each must be an absolute path.
acp_status acp_directories_read(const char *params_json, size_t length, acp_buffer *list_json, acp_buffer *error_json);

// session/list's request: the cwd to list in, absolute, and the cursor to page from; either NULL
// when absent or null.
typedef struct acp_session_list {
    char *cwd;
    char *cursor;
} acp_session_list;

acp_status acp_session_list_read(const char *params_json, size_t length, acp_session_list *out, acp_buffer *error_json);
void acp_session_list_free(acp_session_list *list);

// A SessionInfo. additional_directories is a JSON array; it, title and updated_at are not
// written when NULL, and neither are an empty array or title.
typedef struct acp_session_info {
    const char *session_id;
    const char *cwd;
    const char *additional_directories;
    const char *title;
    const char *updated_at;
} acp_session_info;

acp_status acp_session_info_write(const acp_session_info *info, acp_buffer *out_json);

// A session/prompt block, by what it gives: text, a resource link to read, a resource
// holding text, an image, audio, or another kind, told by its type.
typedef enum acp_block_kind {
    ACP_BLOCK_TEXT,
    ACP_BLOCK_RESOURCE_LINK,
    ACP_BLOCK_RESOURCE,
    ACP_BLOCK_OTHER,
    ACP_BLOCK_IMAGE,
    ACP_BLOCK_AUDIO
} acp_block_kind;

typedef struct acp_prompt_block {
    acp_block_kind kind;
    char *type;     // the type the block names; NULL when it names none
    char *text;     // TEXT's text, or RESOURCE's
    char *name;     // RESOURCE_LINK's
    char *uri;      // RESOURCE_LINK's, or RESOURCE's
    int holds_blob; // OTHER: a resource holding a blob, not text
    char *mime_type; // IMAGE's or AUDIO's mimeType
    char *data;      // IMAGE's or AUDIO's data, base64 as the client sent it
} acp_prompt_block;

// session/prompt's prompt, in order. libacp owns the blocks until acp_prompt_free.
typedef struct acp_prompt {
    acp_prompt_block *blocks;
    size_t count;
} acp_prompt;

acp_status acp_prompt_read(const char *params_json, size_t length, acp_prompt *out, acp_buffer *error_json);
void acp_prompt_free(acp_prompt *prompt);

// A cancel: session/cancel naming its session, or $/cancel_request naming a request, its
// id as JSON text. NONE for any other message; an id a cancel does not name is NULL.
typedef enum acp_cancel_kind {
    ACP_CANCEL_NONE,
    ACP_CANCEL_SESSION,
    ACP_CANCEL_REQUEST
} acp_cancel_kind;

typedef struct acp_cancel {
    acp_cancel_kind kind;
    char *session_id;
    char *request_id;
} acp_cancel;

// Reads a whole message. libacp owns the ids until acp_cancel_free.
acp_status acp_cancel_read(const char *message_json, size_t length, acp_cancel *out);
void acp_cancel_free(acp_cancel *cancel);

// A permission option ground offers: its id, its name, and its kind.
typedef struct acp_permission_option {
    const char *option_id;
    const char *name;
    const char *kind;
} acp_permission_option;

// session/request_permission, whole. request_id and raw_input are JSON text; raw_input that is
// not JSON is written as a string. name and kind are not written when NULL.
typedef struct acp_permission_request {
    const char *request_id;
    const char *session_id;
    const char *tool_call_id;
    const char *title;
    const char *name;
    const char *kind;
    const char *status;
    const char *raw_input;
    const acp_permission_option *options;
    size_t count;
} acp_permission_request;

acp_status acp_permission_request_write(const acp_permission_request *request, acp_buffer *message_json);

// The client's answer: an option selected, the turn cancelled, or neither, UNREAD.
typedef enum acp_permission_kind {
    ACP_PERMISSION_UNREAD,
    ACP_PERMISSION_SELECTED,
    ACP_PERMISSION_CANCELLED
} acp_permission_kind;

typedef struct acp_permission_outcome {
    acp_permission_kind kind;
    char *option_id;
} acp_permission_outcome;

// Reads a whole message. libacp owns option_id until acp_permission_outcome_free.
acp_status acp_permission_outcome_read(const char *message_json, size_t length, acp_permission_outcome *out);
void acp_permission_outcome_free(acp_permission_outcome *outcome);

// fs/read_text_file, whole: request_id is JSON text, path the absolute path to read.
acp_status acp_read_text_file_request_write(const char *request_id, const char *session_id, const char *path,
    acp_buffer *message_json);
// The same, from line ("Line number to start reading from (1-based)") and for limit ("Maximum number of
// lines to read"), each written when it is not 0.
acp_status acp_read_text_file_lines_request_write(const char *request_id, const char *session_id, const char *path,
    uint32_t line, uint32_t limit, acp_buffer *message_json);

// The text a client answers fs/read_text_file with, every byte of it; content is NULL for any
// other answer. not_found is 1 for the error -32002, "Resource not found: A given resource, such as
// a file, was not found". libacp owns content until acp_text_file_free.
typedef struct acp_text_file {
    char *content;
    size_t length;
    int not_found;
} acp_text_file;

acp_status acp_read_text_file_result_read(const char *message_json, size_t length, acp_text_file *out);
void acp_text_file_free(acp_text_file *file);

// fs/write_text_file, whole: request_id is JSON text, path the absolute path to write, and
// content its content_length bytes, every one of them.
acp_status acp_write_text_file_request_write(const char *request_id, const char *session_id, const char *path,
    const char *content, size_t content_length, acp_buffer *message_json);

// What a client answers fs/write_text_file with: written is 1 for a result, which is the file
// written, and 0 for any other answer, an error among them.
acp_status acp_write_text_file_result_read(const char *message_json, size_t length, int *written);

// terminal/create, whole: request_id is JSON text, command run with its arg_count args. cwd, an
// absolute path, is written when given, and output_byte_limit when it is not 0.
acp_status acp_terminal_create_request_write(const char *request_id, const char *session_id, const char *command,
    const char *const *args, size_t arg_count, const char *cwd, unsigned long long output_byte_limit,
    acp_buffer *message_json);

// The terminalId a client answers terminal/create with; NULL for any other answer. libacp owns it
// until acp_terminal_id_free.
acp_status acp_terminal_create_result_read(const char *message_json, size_t length, char **terminal_id);
void acp_terminal_id_free(char *terminal_id);

// terminal/output, terminal/wait_for_exit, terminal/kill and terminal/release, whole, each for the
// terminal terminal/create named.
acp_status acp_terminal_output_request_write(const char *request_id, const char *session_id, const char *terminal_id,
    acp_buffer *message_json);
acp_status acp_terminal_wait_for_exit_request_write(const char *request_id, const char *session_id,
    const char *terminal_id, acp_buffer *message_json);
acp_status acp_terminal_kill_request_write(const char *request_id, const char *session_id, const char *terminal_id,
    acp_buffer *message_json);
acp_status acp_terminal_release_request_write(const char *request_id, const char *session_id, const char *terminal_id,
    acp_buffer *message_json);

// How a terminal's command ended: exited is 0 for an answer that says it has not, or no result. An exit
// code is known when has_exit_code, and signal names the one that ended it, or is NULL. libacp owns
// signal until acp_terminal_exit_free.
typedef struct acp_terminal_exit {
    int exited;
    int has_exit_code;
    unsigned long exit_code;
    char *signal;
} acp_terminal_exit;

// What a client answers terminal/wait_for_exit with.
acp_status acp_terminal_exit_read(const char *message_json, size_t length, acp_terminal_exit *out);
void acp_terminal_exit_free(acp_terminal_exit *exit_status);

// What a client answers terminal/output with: the output, every byte, whether the client cut it,
// and the exit when the command has ended; output is NULL for any other answer.
typedef struct acp_terminal_output {
    char *output;
    size_t length;
    int truncated;
    acp_terminal_exit exit_status;
} acp_terminal_output;

acp_status acp_terminal_output_result_read(const char *message_json, size_t length, acp_terminal_output *out);
void acp_terminal_output_free(acp_terminal_output *output);

// What a client answers terminal/kill and terminal/release with: answered is 1 for a result, 0 for
// any other answer.
acp_status acp_terminal_answered_read(const char *message_json, size_t length, int *answered);

// Why a turn stopped: PromptResponse's stopReason, in the schema's order.
typedef enum acp_stop_reason {
    ACP_STOP_END_TURN,
    ACP_STOP_MAX_TOKENS,
    ACP_STOP_MAX_TURN_REQUESTS,
    ACP_STOP_REFUSAL,
    ACP_STOP_CANCELLED
} acp_stop_reason;

// session/prompt's result: {"stopReason":...}.
acp_status acp_prompt_result_write(acp_stop_reason reason, acp_buffer *result_json);

// session/update, whole: update_json is one SessionUpdate, as the writers below write it.
acp_status acp_session_update_write(const char *session_id, const char *update_json, acp_buffer *message_json);

// A text chunk: the agent's message, its thought, or the user's message, every byte of text.
typedef enum acp_chunk_kind {
    ACP_CHUNK_AGENT_MESSAGE,
    ACP_CHUNK_AGENT_THOUGHT,
    ACP_CHUNK_USER_MESSAGE
} acp_chunk_kind;

acp_status acp_text_chunk_write(acp_chunk_kind kind, const char *text, size_t length, acp_buffer *update_json);
// The same chunk holding an image: its mimeType and its base64 data.
acp_status acp_image_chunk_write(acp_chunk_kind kind, const char *mime_type, const char *data, acp_buffer *update_json);
// The same chunk holding audio: its mimeType and its base64 data.
acp_status acp_audio_chunk_write(acp_chunk_kind kind, const char *mime_type, const char *data, acp_buffer *update_json);

// "File modification shown as a diff": path, absolute, and every byte of either text. old_text NULL is
// a file that was not there, written as null.
typedef struct acp_diff {
    const char *path;
    const char *old_text;
    size_t old_length;
    const char *new_text;
    size_t new_length;
} acp_diff;

// tool_call, or tool_call_update when update is set; a NULL field is not written. Raw input and output are
// JSON text, or a string when not JSON. content_blocks is a ContentBlock array, each written as content;
// content_text, with no blocks, is one text block. terminal_id is a terminal shown ahead of either, and
// diff a file the call modified, after the terminal and ahead of the rest.
typedef struct acp_tool_call {
    int update;
    const char *tool_call_id;
    const char *title;
    const char *name;
    const char *kind;
    const char *status;
    const char *raw_input;
    const char *raw_output;
    const char *content_blocks;
    const char *content_text;
    const char *terminal_id;
    const acp_diff *diff;
} acp_tool_call;

acp_status acp_tool_call_write(const acp_tool_call *call, acp_buffer *update_json);

// plan: plan_json is a Plan, its entries each a content, a priority (high, medium or low) and a status
// (pending, in_progress or completed), written in its order with every byte of each content. Anything
// else an entry holds is not written; a Plan any entry of which is not one is ACP_INVALID_ARGUMENT.
acp_status acp_plan_write(const char *plan_json, size_t length, acp_buffer *update_json);

// usage_update: used and size, and the cost when cost_amount, a JSON number as text, is given.
acp_status acp_usage_update_write(unsigned long long used, unsigned long long size, const char *cost_amount,
    const char *currency, acp_buffer *update_json);

// session_info_update: the title when one is given, and updatedAt.
acp_status acp_session_info_update_write(const char *title, const char *updated_at, acp_buffer *update_json);
// session_info_update clearing the title, "Set to null to clear.", and updatedAt.
acp_status acp_session_title_clear_write(const char *updated_at, acp_buffer *update_json);

// available_commands_update: commands_json is the AvailableCommand array.
acp_status acp_available_commands_update_write(const char *commands_json, acp_buffer *update_json);

// notice: what the user should know of a session.
acp_status acp_notice_write(const char *severity, const char *title, const char *description, acp_buffer *update_json);

// The mode a server's elicitation asks in: form when it names none.
typedef enum acp_elicit_mode {
    ACP_ELICIT_FORM,
    ACP_ELICIT_URL,
    ACP_ELICIT_OTHER
} acp_elicit_mode;

// elicitation/create, whole, from what a session's MCP server asked: the session and the call
// lead, then its mode, then everything else server_params, an object, asked, in its order.
acp_status acp_elicitation_create_write(const char *request_id, const char *session_id, const char *tool_call_id,
    const char *server_params, size_t length, int *mode, acp_buffer *message_json);

// elicitation/create in url mode, tied to a request outside any session, as authenticate's is:
// request_id is that request's id, JSON as the client sent it.
acp_status acp_elicitation_url_write(const char *id, const char *request_id, const char *elicitation_id,
    const char *url, const char *message, acp_buffer *message_json);

// elicitation/complete, whole.
acp_status acp_elicitation_complete_write(const char *elicitation_id, acp_buffer *message_json);

// session/set_mode's request: the session, and the mode it is set to.
typedef struct acp_set_mode {
    char *session_id;
    char *mode_id;
} acp_set_mode;

acp_status acp_set_mode_read(const char *params_json, size_t length, acp_set_mode *out, acp_buffer *error_json);
void acp_set_mode_free(acp_set_mode *set_mode);

// A mode a session offers; description may be NULL.
typedef struct acp_mode {
    const char *id;
    const char *name;
    const char *description;
} acp_mode;

// A SessionModeState: current_id, which is one of modes, and every mode offered.
acp_status acp_modes_write(const char *current_id, const acp_mode *modes, size_t count, acp_buffer *out_json);

// session/set_config_option's request: a value id in value, or a boolean in boolean_value
// with is_boolean set.
typedef struct acp_set_config_option {
    char *session_id;
    char *config_id;
    char *value;
    int is_boolean;
    int boolean_value;
} acp_set_config_option;

acp_status acp_set_config_option_read(const char *params_json, size_t length, acp_set_config_option *out,
    acp_buffer *error_json);
void acp_set_config_option_free(acp_set_config_option *set_option);

// A value a select offers; description may be NULL.
typedef struct acp_config_value {
    const char *value;
    const char *name;
    const char *description;
} acp_config_value;

// A select SessionConfigOption; category may be NULL, current_value is one of values.
typedef struct acp_config_select {
    const char *id;
    const char *name;
    const char *category;
    const char *current_value;
    const acp_config_value *values;
    size_t count;
} acp_config_select;

// configOptions: the selects, in the order given.
acp_status acp_config_options_write(const acp_config_select *selects, size_t count, acp_buffer *out_json);

// --- The client side: what a client writes to an agent, and reads of what the agent sends. ---
// acp_client_<method>_write writes what the client sends for the method, and acp_client_<method>_read
// reads what the agent sends for it, the method by its name in acp_methods.h: ground parity measures each
// method's keys through them.

// A message as it came: a request, with its id and method; a notification, a method and no id; or a
// response, an id and a result or an error. UNREAD for any other. id is JSON text, as it was sent;
// params, a request's or a notification's, is JSON text for the reader of its method; error_data is
// the error's data as JSON text. Each is NULL when there is none. libacp owns the strings until
// acp_message_free.
typedef enum acp_message_kind {
    ACP_MESSAGE_UNREAD,
    ACP_MESSAGE_REQUEST,
    ACP_MESSAGE_NOTIFICATION,
    ACP_MESSAGE_RESPONSE
} acp_message_kind;

typedef struct acp_message {
    acp_message_kind kind;
    char *id;
    char *method;
    int is_error;
    int error_code;
    char *error_message;
    char *error_data;
    char *params;
} acp_message;

// ACP_PARSE_ERROR for text that is not one JSON value.
acp_status acp_client_message_read(const char *message_json, size_t length, acp_message *out);
void acp_message_free(acp_message *message);

// An error answering request_id, JSON text: a request the client does not answer otherwise.
acp_status acp_client_error_write(const char *request_id, int code, const char *message, acp_buffer *message_json);

// initialize, whole, from the client: the version it asks for, 1 or 2, what it offers, in that
// version's shape, and who it is. v2 offers only elicitation and auth, as acp_initialize_read reads them.
acp_status acp_client_initialize_write(const char *request_id, unsigned version, const acp_client_capabilities *offered,
    const char *client_name, const char *client_version, acp_buffer *message_json);

// What the agent answers initialize with: the version it speaks, 0 for an answer of any other kind,
// what it offers, and who it is, either NULL when it does not say. libacp owns the names until
// acp_agent_info_free.
typedef struct acp_agent_info {
    unsigned protocol_version;
    int load_session;
    int session_resume;
    int prompt_image;
    int prompt_audio;
    int prompt_embedded_context;
    int mcp_http;
    int mcp_sse;
    char *agent_name;
    char *agent_version;
} acp_agent_info;

acp_status acp_client_initialize_read(const char *message_json, size_t length, acp_agent_info *out);
void acp_agent_info_free(acp_agent_info *info);

// An MCP server a session connects to over HTTP, "Only available when the Agent capabilities indicate
// mcp_capabilities.http is true": its name, its url, and the headers each request carries.
typedef struct acp_http_header {
    const char *name;
    const char *value;
} acp_http_header;

typedef struct acp_mcp_http {
    const char *name;
    const char *url;
    const acp_http_header *headers;
    size_t header_count;
} acp_mcp_http;

// session/new, session/load and session/resume, whole, in v1: cwd absolute, and the servers, [] when none.
acp_status acp_client_session_new_write(const char *request_id, const char *cwd,
    const acp_mcp_http *servers, size_t count, acp_buffer *message_json);
acp_status acp_client_session_load_write(const char *request_id, const char *session_id, const char *cwd,
    const acp_mcp_http *servers, size_t count, acp_buffer *message_json);
acp_status acp_client_session_resume_write(const char *request_id, const char *session_id, const char *cwd,
    const acp_mcp_http *servers, size_t count, acp_buffer *message_json);

// The sessionId the agent answers session/new with; NULL for any other answer. libacp owns it until
// acp_session_id_free.
acp_status acp_client_session_new_read(const char *message_json, size_t length, char **session_id);
void acp_session_id_free(char **session_id);

// session/prompt, whole: one text block, every byte of text.
acp_status acp_client_session_prompt_write(const char *request_id, const char *session_id, const char *text, size_t length,
    acp_buffer *message_json);

// Why the agent stopped the turn: stopped is 1 for a result naming one of the five, which is in reason,
// and 0 for any other answer, an error among them.
acp_status acp_client_session_prompt_read(const char *message_json, size_t length, int *stopped, acp_stop_reason *reason);

// authenticate, whole: the method, one of the authMethods the agent's initialize result offered.
acp_status acp_client_authenticate_write(const char *request_id, const char *method_id, acp_buffer *message_json);

// session/set_config_option, whole: the option set to one of its value ids.
acp_status acp_client_session_set_config_option_write(const char *request_id, const char *session_id,
    const char *config_id, const char *value, acp_buffer *message_json);

// session/cancel, whole: a notification.
acp_status acp_client_session_cancel_write(const char *session_id, acp_buffer *message_json);

// A session/update as the client reads it: its kind, and what is shown of each.
typedef enum acp_update_kind {
    ACP_UPDATE_UNREAD,
    ACP_UPDATE_OTHER,
    ACP_UPDATE_USER_MESSAGE,
    ACP_UPDATE_AGENT_MESSAGE,
    ACP_UPDATE_AGENT_THOUGHT,
    ACP_UPDATE_TOOL_CALL,
    ACP_UPDATE_TOOL_CALL_UPDATE,
    ACP_UPDATE_USAGE
} acp_update_kind;

// update is the SessionUpdate whole, as JSON text, and type its sessionUpdate. A chunk's text is every
// byte of it, when its content is text. A tool call's fields are NULL when not sent; raw_input and
// raw_output are JSON text. A usage update's cost_amount is its JSON number as text. libacp owns every
// string until acp_update_free.
typedef struct acp_update {
    acp_update_kind kind;
    char *session_id;
    char *type;
    char *update;
    char *text;
    size_t text_length;
    char *tool_call_id;
    char *title;
    char *name;
    char *tool_kind;
    char *status;
    char *raw_input;
    char *raw_output;
    unsigned long long used;
    unsigned long long size;
    char *cost_amount;
    char *cost_currency;
} acp_update;

// Reads a whole session/update notification; UNREAD when it holds no update naming its kind.
acp_status acp_client_session_update_read(const char *message_json, size_t length, acp_update *out);
void acp_update_free(acp_update *update);

// session/request_permission's params, as the client reads them: the session, the call it asks about,
// and the options to choose from. raw_input is JSON text; a field not sent is NULL. libacp owns every
// string until acp_permission_asked_free.
typedef struct acp_permission_choice {
    char *option_id;
    char *name;
    char *kind;
} acp_permission_choice;

typedef struct acp_permission_asked {
    char *session_id;
    char *tool_call_id;
    char *title;
    char *tool_kind;
    char *raw_input;
    acp_permission_choice *options;
    size_t count;
} acp_permission_asked;

// ACP_INVALID_PARAMS when sessionId, toolCall's toolCallId or options is missing.
acp_status acp_client_session_request_permission_read(const char *params_json, size_t length, acp_permission_asked *out);
void acp_permission_asked_free(acp_permission_asked *asked);

// The client's answer to session/request_permission: option_id selected, or cancelled when NULL.
acp_status acp_client_session_request_permission_write(const char *request_id, const char *option_id, acp_buffer *message_json);

#ifdef __cplusplus
}
#endif

#endif
