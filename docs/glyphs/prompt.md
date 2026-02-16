# Prompt Glyph (⟶)

LLM prompt editor on canvas. Write a prompt with YAML frontmatter config, hit play to execute.

## Frontmatter

```yaml
---
model: "anthropic/claude-haiku-4.5"
temperature: 0.7
max_tokens: 1000
---
Your prompt here.
```

Model names follow OpenRouter format (`provider/model`).

## Melding

Prompt accepts attachments via melding — other glyphs fuse onto it spatially:

| Glyph | Port | Effect |
|-------|------|--------|
| Doc (▤) | bottom | File sent as multimodal attachment (images as `image_url`, PDFs as `file`) |
| Note | bottom | Text included in prompt context |
| AX / SE / PY | right | Chains into prompt (execution pipeline) |

Multiple Doc glyphs stack via doc-to-doc melding above the prompt.

## Execution

Play button sends `POST /api/prompt/direct` with the template and any melded file IDs. A Result glyph auto-melds below the prompt with the LLM response.

## Files

| File | Role |
|------|------|
| `web/ts/components/glyph/prompt-glyph.ts` | Glyph factory + execution logic |
| `server/prompt_handlers.go` | `/api/prompt/direct` handler |
| `ai/openrouter/client.go` | OpenRouter chat completion client |
| `web/css/glyph/meld.css` | Composition layout styles |
