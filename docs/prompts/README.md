---
type: prompt-template
name: prompt-system-guide
model: anthropic/claude-haiku-4.5
---

Explain how QNTX prompt templates work, using {{subject}} as the example.

Include:
1. **Frontmatter**: YAML configuration at the top (type, name, model)
2. **Template syntax**: {{field}} placeholders map to attestation fields
3. **X-sampling**: Test prompts against X random attestations before deployment
4. **Available fields**: {{subject}}, {{context}}, {{attributes}}, {{predicates}}, etc.
5. **Preview workflow**: Edit template → Run Preview → Compare outputs → Deploy

Show how {{subject}} demonstrates these concepts, referencing its implementation in {{context}}.

---

## QNTX Prompt System Development Tracker

**Vision**: Enable confidence in prompt changes through X-sampling preview: sample X attestations, execute prompt X times, compare outputs against previous versions. This transforms prompt development from risky production experiments to controlled, iterative refinement.

**Tracking PR**: [#301](https://github.com/teranos/QNTX/pull/301) - QNTX Prompt: Attestation-based LLM prompt templates with frontmatter

### ✅ Merged Work

- [#300](https://github.com/teranos/QNTX/pull/300) - Add YAML frontmatter support for prompt configuration
- [#304](https://github.com/teranos/QNTX/pull/304) - Add frontmatter UI support to Prose editor
- [#305](https://github.com/teranos/QNTX/pull/305) - Implement CSV export action (⟶)
- [#330](https://github.com/teranos/QNTX/pull/330) - Add prompt preview API endpoint with X-sampling (closes [#314](https://github.com/teranos/QNTX/issues/314))
- [#338](https://github.com/teranos/QNTX/pull/338) - Prompt UI (frontend preview panel, closes [#316](https://github.com/teranos/QNTX/issues/316))

### 🔥 Critical Issues (Next PRs)

- [#315](https://github.com/teranos/QNTX/issues/315) - Implement prompt version comparison logic using attestations

### 📋 Non-Critical Issues (Future)

- [#317](https://github.com/teranos/QNTX/issues/317) - Support .prompt.md file extension for prompt detection
- [#318](https://github.com/teranos/QNTX/issues/318) - Add visual indicators for prompt files in Prose tree
- [#319](https://github.com/teranos/QNTX/issues/319) - Implement n8n-style {{}} field selector for prompt templates
- [#320](https://github.com/teranos/QNTX/issues/320) - Add prompt action to command palette SO integration
- [#321](https://github.com/teranos/QNTX/issues/321) - Enable scheduled prompt execution via Pulse
- [#339](https://github.com/teranos/QNTX/issues/339) - Add vertical PROMPT indicator for manual toggle
- [#340](https://github.com/teranos/QNTX/issues/340) - Add loading state UI to prompt preview panel
- [#341](https://github.com/teranos/QNTX/issues/341) - Make prompt preview provider configurable
- [#342](https://github.com/teranos/QNTX/issues/342) - Add deterministic sampling option to prompt preview API
- [#344](https://github.com/teranos/QNTX/issues/344) - Make filename optional for attestation-only prompts

---

## Developer Workflow

1. **Pick an issue** from the lists above
2. **Implement the feature** in a branch
3. **Update this README** when done:
   - Move issue to ✅ Merged Work
   - Demonstrate the new capability in the prompt template above if applicable
4. **Execute this README** as a prompt to verify the documentation explains the feature

This README is itself an executable prompt - it should always accurately explain and demonstrate QNTX prompt capabilities.
