# Prompt Element (⟶)

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

Prompt accepts attachments via melding — other elements fuse onto it spatially:

| Element | Port | Effect |
|-------|------|--------|
| Doc (▤) | bottom | File sent as multimodal attachment (images as `image_url`, PDFs as `file`) |
| Note | bottom | Text included in prompt context |
| AX / SE / PY | right | Chains into prompt (execution pipeline) |

Multiple Doc elements stack via doc-to-doc melding above the prompt.

## Execution

Play button sends `POST /api/prompt/direct` with the template and any melded file IDs. A Result element auto-melds below the prompt with the LLM response.

## Vision: Model-Styled Prompt Elements

The prompt element's visual identity is the model's visual identity. An OpenAI prompt element looks like OpenAI. An Anthropic prompt element looks like Anthropic. Deepseek, Mistral, local llama.cpp — each carries the visual language of the model it's talking to.

Provider resolution is server-owned. The user doesn't pick a provider in a settings panel — QNTX decides based on configuration, availability, and routing rules. The prompt element reflects what was decided through its chrome, not through a dropdown. The model identity is expressed, not configured.

The current LLM provider element (a global toggle panel) is a transitional artifact. The end state: each prompt element carries its model affinity visually. Per-element override is possible but not required — most users never need to think about it.
