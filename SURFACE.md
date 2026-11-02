# What libacp reads and writes, per method

ground code's ACP moves into libacp one part at a time; each part ports the PR that first gave
ground code the method. A row is what libacp reads or writes of it in v1 (schema-v1.26.0), and
what v2 (schema-v2.0.0-alpha.9) says of the same method, so v2 needs no second pass.

| Method | Part | Ported from | libacp, v1 | v2 alpha.9 |
|---|---|---|---|---|
| `initialize` | 0 | #179 | reads `protocolVersion`, `clientCapabilities`; writes `protocolVersion`, `agentCapabilities`, `authMethods`, `agentInfo` | request carries `info`, `capabilities`; result `info`, `capabilities`, `authMethods`; read and written by libacp in both shapes |
| `session/new` | 15 | #113 | reads its own `cwd`, `mcpServers` (stdio, HTTP and SSE shapes), and `additionalDirectories`; writes `sessionId`, `modes`, `configOptions`; Ground owns transport support, startup and storage | stdio adds explicit `type: "stdio"`; result drops `modes`, permits `availableCommands`, and config options use `configId`; this reader and result writer implement v1 only |
| `session/resume` | 1 | #118 | reads `sessionId`, `cwd` (absolute) | request adds `replayFrom`; result drops `modes`, adds `availableCommands` |
| `session/delete` | 2 | #120 | reads `sessionId` | the same: `sessionId` required, result `_meta` only |
| `additionalDirectories` in `session/new` and `session/resume` | 4 | #123 | reads the list, each path absolute; writes it back as answered | the same in `session/new`, `session/resume` and `SessionInfo` |
| `session/set_mode` | 3 | #121 | reads `sessionId`, `modeId` | absent: v2 has no session modes |
| `modes` in `session/new` and `session/resume` | 3 | #121 | writes `currentModeId`, `availableModes` (`id`, `name`, `description`) | absent: v2's results carry `configOptions` alone |
| `session/set_config_option` | 5 | #124 | reads `sessionId`, `configId`, and `value` as a value id, or `type` boolean and a boolean | the same keys; a value id carries `type` id, and an `other` shape carries custom values |
| `configOptions` in `session/new`, `session/resume`, `session/set_config_option` | 5 | #124 | writes selects: `id`, `name`, `category`, `type`, `currentValue`, `options` (`value`, `name`, `description`) | an option's `id` is `configId` |
| `session/list` | 6 | #128 | reads `cwd` (absolute) and `cursor`, each optional; writes each `SessionInfo`: `sessionId`, `cwd`, `additionalDirectories`, `title`, `updatedAt` | the same keys in the request, the result and `SessionInfo` |
| `authenticate` | 7 | #131 | reads `methodId`, required | renamed `auth/login`, `methodId` required; `auth/logout` beside it |
| `auth.terminal` in `initialize` | login | new | reads the client's `auth.terminal`, so a terminal auth method is advertised only to a client that runs one | the same, under `capabilities` |
| `elicitation/create` tied to a request | login | new | writes url mode with `requestId`, `elicitationId`, `url`, `message`: authenticate's sign-in, outside any session | the same scope |
| `logout` | login | new | nothing to read; `-32000` is `ACP_ERROR_AUTH_REQUIRED` | renamed `auth/logout` |
| `prompt` in `session/prompt` | 8 | #135 | reads each block: `text` and its `text`; `resource_link` and its `name`, `uri`; `resource` and its `resource`'s `uri`, `text` or `blob`; any other by its `type` | the same blocks; `ResourceLink` adds `icons`, and `ContentBlock` an open variant for other types |
| `session/cancel`, `$/cancel_request` | 9 | #137 | reads a whole message: `method`, and `params`' `sessionId` or `requestId` (kept as JSON text) | the same keys; `session/cancel`'s notification is named `CancelSessionNotification` |
| `session/request_permission` | 10 | #138 | writes the request: `sessionId`, `toolCall` (`toolCallId`, `title`, `name`, `kind`, `status`, `rawInput`), `options` (`optionId`, `name`, `kind`); reads the answer's `outcome`: `selected` and its `optionId`, or `cancelled` | the request names `title`, `description` and `subject` in place of `toolCall`; an outcome may be another kind |
| `session/load` | 11 | #143 | reads `sessionId`, `cwd` (absolute), `mcpServers` (a list), each required | absent: v2 has no session/load |
| `fs/read_text_file` | 12 | #156 | writes the request: `sessionId`, `path`, and `line` and `limit` when given; reads the answer's `content`, every byte, and whether its error is `-32002`, the file not found | absent: v2 has no fs/* |
| `fs/write_text_file` | 16 | #179 | writes the request: `sessionId`, `path`, `content`, every byte; reads whether the answer is a result | absent: v2 has no fs/* |
| `terminal/create`, `terminal/wait_for_exit`, `terminal/output`, `terminal/kill`, `terminal/release` | 17 | #179 | writes create's `sessionId`, `command`, `args`, `cwd`, `outputByteLimit`, and the other four's `sessionId`, `terminalId`; reads create's `terminalId`, wait_for_exit's `exitCode` and `signal`, output's `output` (every byte), `truncated` and `exitStatus`; writes a tool call's `terminal` content | absent: v2 has no terminal/* methods; its terminals are the agent's, told by `terminal_update` and `terminal_output_chunk` |
| `stopReason` in `session/prompt`'s result | 13 | #157 | writes `stopReason`: `end_turn`, `max_tokens`, `max_turn_requests`, `refusal` or `cancelled` | the same five |
| `session/update` | 14 | #163, #164, #166, #169, #171, #172, #173, #176, #177 | writes the notification, `sessionId` and `update`, the update as written; and each kind ground code tells: `agent_message_chunk`, `agent_thought_chunk`, `user_message_chunk`, `tool_call` and `tool_call_update` (`toolCallId`, `title`, `name`, `kind`, `status`, `rawInput`, `rawOutput` byte for byte, `content`, a `diff`'s `path`, `oldText` and `newText` among it), `usage_update` (`used`, `size`, `cost`), `session_info_update` (`title`, `updatedAt`), `available_commands_update`, `notice`, `plan` (`entries`, each `content`, `priority`, `status`) | no `tool_call`: a call is reported by `tool_call_update` and `tool_call_content_chunk`; adds `user_message`, `agent_message`, `agent_thought`, `state_update`, `plan_update`, `terminal_update`, `terminal_output_chunk`; drops `current_mode_update` |
| `elicitation/create`, `elicitation/complete` | 14 | #167, #178 | writes the request from what a session's MCP server asked: `sessionId`, `toolCallId`, `mode` (form when none), `message`, then the rest as the server gave it; and `elicitation/complete`'s `elicitationId` | the same two methods |
| `image` in `session/prompt`, and `promptCapabilities.image` | image | new | reads an image block's `mimeType` and `data`, both required, as `ACP_BLOCK_IMAGE`; writes a `user_message_chunk` holding an image when a session is loaded | the same `ImageContent` and `promptCapabilities.image` |
| `audio` in `session/prompt`, and `promptCapabilities.audio` | audio | new | reads an audio block's `mimeType` and `data`, both required, as `ACP_BLOCK_AUDIO`; writes a `user_message_chunk` holding audio when a session is loaded | the same `AudioContent` and `promptCapabilities.audio` |

## The client side

What a client writes to an agent and reads of what it sends, v1 (schema-v1.26.0). `tests/client.c` checks
each against the agent side's own reader or writer of the same method.
`acp_client_<method>_write` writes what the client sends for a method, `acp_client_<method>_read` reads
what the agent sends for it, and `ground_parity` scores each method's client side through them.

| Method | libacp, client side, v1 |
|---|---|
| any message | reads it as a request (`id`, `method`, `params`), a notification (`method`, `params`), or a response (`id`, and `result` or `error`'s `code`, `message`, `data`); writes an error answering a request |
| `initialize` | writes `protocolVersion`, `clientCapabilities` (`fs`'s `readTextFile`, `writeTextFile`; `terminal`; `session.notices`, `elicitation`, `auth.terminal` when offered), `clientInfo`; v2's `capabilities` and `info`. Reads the result's `protocolVersion`, `agentCapabilities` (`loadSession`, `sessionCapabilities.resume`, `promptCapabilities`' `image`, `audio`, `embeddedContext`, `mcpCapabilities`' `http`, `sse`) and `agentInfo`, or v2's `capabilities` and `info` |
| `session/new`, `session/load`, `session/resume` | writes `sessionId` for the latter two, `cwd` (absolute), `mcpServers` in the HTTP shape: `type`, `name`, `url`, `headers` (`name`, `value`). Reads session/new's `sessionId` |
| `session/prompt` | writes `sessionId` and `prompt` as one `text` block, every byte; reads `stopReason`, one of the five |
| `authenticate` | writes `methodId`, one of the `authMethods` initialize offered |
| `session/set_config_option` | writes `sessionId`, `configId`, and `value` as a value id |
| `session/cancel` | writes the notification's `sessionId` |
| `session/update` | reads `sessionId`, `update` whole, and its `sessionUpdate`; of a chunk, its `content`'s `text`, every byte; of `tool_call` and `tool_call_update`, `toolCallId`, `title`, `name`, `kind`, `status`, `rawInput`, `rawOutput`; of `usage_update`, `used`, `size`, `cost`'s `amount` and `currency` |
| `session/request_permission` | reads `sessionId`, `toolCall`'s `toolCallId`, `title`, `kind`, `rawInput`, and `options` (`optionId`, `name`, `kind`); writes the outcome, `selected` with its `optionId`, or `cancelled` |
