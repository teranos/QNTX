# qntx-loom

Receives conversation events from [Graunde](https://github.com/teranos/graunde) over UDP and stitches them into embedding-sized text blocks (weaves).

**UDP port: 19470** — Graunde sends attestation JSON datagrams here. Fire-and-forget, no response.

## UDP event format

Every datagram is a JSON attestation:

```json
{
  "subjects": ["tmp3/QNTX:feat/branch-name"],
  "predicates": ["UserPromptSubmit"],
  "contexts": ["session:abc-123"],
  "attributes": { "prompt": "..." }
}
```

Graunde sends these on every hook event via `attestEvent` → `sendToLoom`. Corrective stop hooks additionally send `Hook` predicates via `notifyLoomHook`.

## Turn types

| Predicate | Label | Source |
|---|---|---|
| `UserPromptSubmit` | `[human]` | `attributes.prompt` |
| `Stop` | `[assistant]` | `attributes.last_assistant_message` |
| `PreToolUse` (Bash) | `[tool]` | `attributes.tool_input.command` (whitelist-filtered) |
| `PreToolUse` (Edit) | `[edit]` | `attributes.tool_input.file_path` |
| `PreToolUse` (Read) | `[read]` | `attributes.tool_input.file_path` |
| `PreToolUse` (Grep/Glob) | `[search]` | `attributes.tool_input.pattern` |
| `PreToolUse` (Write) | `[write]` | `attributes.tool_input.file_path` |
| `Hook` | `[hook]` | `attributes.hook_output` |
| `SessionStart` | `[session]` | `attributes.session_id` |
| `SessionEnd` | `[session]` | `attributes.session_id` |
| `PreCompact` | `[compaction]` | static marker |
| `SubagentStart/Stop` | `[agent]` | `attributes.agent_type` |
| `TaskCompleted` | `[task]` | `attributes.task_subject` |

Bash `[tool]` turns are filtered by a command whitelist (git, gh, make). All other tool types (Edit, Read, Grep, Glob, Write) are captured with their own labels.
