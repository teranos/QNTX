# Claude Code API Wire Format

Captured from Claude Code v2.1.69 through ix-net HTTPS proxy.
Endpoint: `POST /v1/messages?beta=true` on `api.anthropic.com:443`.

## Request Headers

```
POST /v1/messages?beta=true HTTP/1.1
host: api.anthropic.com
connection: keep-alive
Accept: application/json
X-Stainless-Retry-Count: 0
X-Stainless-Timeout: 600
X-Stainless-Lang: js
X-Stainless-Package-Version: 0.74.0
X-Stainless-OS: MacOS
X-Stainless-Arch: x64
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v22.16.0
anthropic-dangerous-direct-browser-access: true
anthropic-version: 2023-06-01
authorization: Bearer sk-ant-oat01-...
x-app: cli
User-Agent: claude-cli/2.1.69 (external, cli)
content-type: application/json
anthropic-beta: claude-code-20250219,oauth-2025-04-20,adaptive-thinking-2026-01-28,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24
accept-language: *
sec-fetch-mode: cors
content-length: 302661
```

### Notable Headers

- **authorization**: OAuth token (`sk-ant-oat01-...`)
- **anthropic-version**: `2023-06-01` (API version)
- **anthropic-beta**: comma-separated beta feature flags
- **x-app**: `cli`
- **User-Agent**: `claude-cli/<version> (external, cli)`
- **X-Stainless-***: SDK metadata (language, runtime, OS, arch, package version)

## Request Body

~67KB–300KB JSON depending on conversation length and images.

### Top-Level Fields

```json
{
  "model": "claude-opus-4-6",
  "messages": [ ... ],
  "system": [ ... ],
  "tools": [ ... ],
  "metadata": {
    "user_id": "user_<hash>_account_<uuid>_session_<uuid>"
  },
  "max_tokens": 32000,
  "thinking": { "type": "adaptive" },
  "context_management": {
    "edits": [{ "type": "clear_thinking_20251015", "keep": "all" }]
  },
  "output_config": { "effort": "medium" },
  "stream": true
}
```

### metadata.user_id

Compound identifier encoding user, account, and session:

```
user_0784b260bff170af...
  _account_261d5434-c8c2-431f-b07d-2e480cd4622f
  _session_b4e5aaaa-9a3e-47c4-84ad-3a70f38c676e
```

The session UUID is always the last segment after `_session_`.

### messages

Array of conversation turns. Each entry has `role` and `content`.

```
messages[0]  role=user     content=str (deferred tools list)
messages[1]  role=user     content=list (system reminders + user text + images)
messages[2]  role=assistant content=list (thinking + tool_use blocks)
messages[3]  role=user     content=list (tool_result + system reminders)
messages[4]  role=assistant content=list (text response)
messages[5]  role=user     content=list (next user message)
```

Content block types observed:
- `text` — plain text content
- `image` — base64-encoded image (PNG/JPEG/GIF/WebP)
- `thinking` — model's chain-of-thought (adaptive thinking)
- `tool_use` — tool invocation (name, id, input)
- `tool_result` — tool output (tool_use_id, content)

Image blocks appear inside user messages:
```json
{
  "type": "image",
  "source": {
    "type": "base64",
    "media_type": "image/png",
    "data": "<base64 string>"
  }
}
```

### system

Array of system prompt blocks (type=text):

```
system[0]  billing header (cc_version, cc_entrypoint)
system[1]  "You are Claude Code, Anthropic's official CLI for Claude."
system[2]  full system prompt (~16KB) — tools, instructions, environment
```

### tools

9 tool definitions for Claude Code:

```
Agent, Bash, Glob, Grep, Read, Edit, Write, Skill, ToolSearch
```

Each tool has `name`, `description`, and `input_schema` (JSON Schema).

## Response Headers

```
HTTP/1.1 200 OK
Content-Type: text/event-stream; charset=utf-8
Transfer-Encoding: chunked
```

### Rate Limit Headers

```
anthropic-ratelimit-unified-status: allowed
anthropic-ratelimit-unified-5h-utilization: 0.07
anthropic-ratelimit-unified-7d-utilization: 0.22
anthropic-ratelimit-unified-overage-utilization: 0.0
anthropic-ratelimit-unified-fallback-percentage: 0.5
```

### Other Response Headers

- **request-id**: `req_011CYqaB5Btywh4eqCRqguF1`
- **anthropic-organization-id**: `6b19b424-3978-427a-914f-71cfc7257280`
- **x-envoy-upstream-service-time**: `2810` (ms)
- **Server**: `cloudflare`

## Response Body (Streaming)

SSE event stream (`text/event-stream`). Token usage appears in:

- **message_start**: `{"type":"message_start","message":{"usage":{"input_tokens":N}}}`
- **message_delta**: `{"type":"message_delta","usage":{"output_tokens":N}}`

## Preflight Request

Before each opus request, Claude Code sends a haiku preflight (284 bytes):

```json
{
  "model": "claude-haiku-4-5-20251001",
  "max_tokens": 1,
  "messages": [{ "role": "user", "content": "quota" }],
  "metadata": {
    "user_id": "user_<hash>_account_<uuid>_session_<uuid>"
  }
}
```

This appears to be a quota/rate-limit check before the actual request.
